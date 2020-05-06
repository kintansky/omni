package main

import (
	"fmt"
	"strings"
	"time"

	omniDB "database_tool"
	omniSNMP "go_snmp/base"
	omniLOG "logger"

	"database/sql"

	_ "github.com/go-sql-driver/mysql"
)

// 数据库的全局变量
var (
	dbType  string
	dbName  string
	user    string
	pwd     string
	cnxType string
	address string
	port    int
)

// 其他全局变量
var logger = omniLOG.NewOmniLog("./logFile.log", "warn") // log日志

const updateNeTable string = "MR_REC_NE"               // 需要更新的表格
const modRecordNeTable string = "MR_REC_NE_mod_record" // 变化记录

type NEDevice struct {
	Device string `db:"NE"`
	IP     string `db:"NM_ip"`
}

func getNEDevcie(deviceChan chan<- *NEDevice, cmd string) {
	var devices []*NEDevice
	if err := omniDB.DB.Select(&devices, cmd); err != nil {
		logger.LogError("getTargetDevice", "sql query", "cmd", cmd, err)
		close(deviceChan)
		return
	}
	for _, d := range devices {
		logger.LogInfo("getTargetDevice", "write channel", "channel", d.Device, "writing to channel")
		deviceChan <- d
	}
	close(deviceChan)
}

func walkOid(deviceChan <-chan *NEDevice, snmpResultChan chan<- map[string]map[string]map[string]string, exitWalkOidChan chan<- bool, nodes map[string]string) {
	for d := range deviceChan {
		community := "getgmcc!)"
		snmp, err := omniSNMP.InitSnmp(d.IP, 161, "udp", community, 2, 5, 2) // 版本v2c, 等待3秒，最多重复2次
		if err != nil {
			logger.LogError("walkOid", "snmp init", "none", map[string]interface{}{"address": address, "port": port, "tran": "udp", "community": community}, err)
			exitWalkOidChan <- true
			return
		}
		if err := snmp.Connect(); err != nil {
			logger.LogError("walkOid", "snmp connect", "device", fmt.Sprintf("%s:%d", address, port), err)
			exitWalkOidChan <- true
			return
		}
		defer snmp.Conn.Close()
		// 遍历提供的nodes
		// 每次的结果是一个三层map
		// {deviceName: {
		// 		node1:{oid1:val; oid2:val;...};
		// 		node2:{oid1:val; oid2:val;...};
		// 		...
		// 		}
		// }
		snmpResult := make(map[string]map[string]map[string]string, 1)
		nodeMap := make(map[string]map[string]string, len(nodes))
		for node, nodeName := range nodes { // 因为nodes字典的key就是oid，所以取第一位
			logger.LogInfo("walkOid", "walk node", "NodeOID", d.IP+":"+node, "walk node")
			ret, err := snmp.BulkWalkAll(node)
			if err != nil {
				logger.LogError("walkOid", "BulkWalk", "NodeOID", d.IP+":"+node, err)
				continue
			}
			oidMap := make(map[string]string, 0)
			for _, pdu := range ret {
				k, v, err := omniSNMP.ParseAsKeyVal(&pdu)
				if err != nil {
					logger.LogWarn("walkOid", "BulkWalk", d.IP+":"+k, pdu.Type.String(), "parse OID return error")
					oidMap[strings.TrimPrefix(k, node)] = "parse error"
				}
				// fmt.Println(k, ":", v)
				oidMap[strings.TrimPrefix(k, "."+node)] = v // 干脆把前序也去掉
			}
			nodeMap[nodeName] = oidMap // 使用nodeName替代OID作为key
		}
		// snmpResult[d.Device] = nodeMap
		snmpResult[d.IP] = nodeMap // 以网管IP作为区分
		// fmt.Printf("%#v\n", snmpResult)
		snmpResultChan <- snmpResult
	}

	exitWalkOidChan <- true
}

func parseHW(snmpResultChan <-chan map[string]map[string]map[string]string, parsedDataChan chan<- map[string]map[string]string, exitParseChan chan<- bool, orderField []string) {
	// {deviceName: {
	// 		node1:{oid1:val; oid2:val;...};
	// 		node2:{oid1:val; oid2:val;...};
	// 		...
	// 		}
	// }
	result := make(map[string]map[string]string, 0)
	for ret := range snmpResultChan {
		for nmIP, nodeMap := range ret {
			logger.LogInfo("parseHW", "NM_IP:"+nmIP, "NM_IP", nmIP, "parse node")
			// 先处理需要需要关系解析的数据
			// 先找到需要的index作为targetKey
			targetKey := "-1" // 注意要使用一个不可能存在的key来初始化，避免何其他key混淆
			for indexKey, indexVal := range nodeMap[orderField[0]] {
				if indexVal == "3" {
					targetKey = indexKey
					break
				}
			}
			delete(nodeMap, orderField[0])
			// 再在对应的数据中按照targetKey过滤
			parsedMap := make(map[string]string, 0)
			for _, field := range orderField[1:] {
				data, ok := nodeMap[field][targetKey]
				if !ok {
					logger.LogWarn("parseHW", "NM_IP:"+nmIP, field, targetKey, fmt.Sprintf("no index %s for %s", targetKey, field))
					parsedMap[field] = ""
					delete(nodeMap, field)
					continue
				}
				parsedMap[field] = data
				delete(nodeMap, field)
			}
			// 剩下的是只有单条数据的node，直接加入到parsedMap
			for node, oidMap := range nodeMap {
				for _, val := range oidMap {
					// 因为只有单条数据，所以可以直接使用node作为key
					parsedMap[node] = val
					delete(nodeMap, node) // 这里可以不删除
				}
			}
			result[nmIP] = parsedMap
		}
	}
	// fmt.Println(result)
	parsedDataChan <- result
	exitParseChan <- true
}

func writeDB(parsedDataChan <-chan map[string]map[string]string, exitChan chan<- bool, oldDeviceMap map[string]map[string]sql.NullString) {
	cmdPrefix := fmt.Sprintf("update %s set ", updateNeTable)
	for parsedData := range parsedDataChan {
		for nmIP, deviceDataMap := range parsedData {
			logger.LogInfo("writeDB", "NM_IP:"+nmIP, "NM_IP", nmIP, "writeDB")
			tmpData := make([]interface{}, 0)
			var mod_msg string
			var cmdMiddle string
			oldDevice := oldDeviceMap[nmIP]
			for key, val := range deviceDataMap {
				oldData, ok := oldDevice[key]
				if !ok {
					logger.LogError("writeDB", "diff old data", "NM_IP", nmIP, fmt.Errorf("no key:%s", key))
					continue
				}
				// if oldData != val {
				// 	cmdMiddle += fmt.Sprintf("%s = ?,", key)
				// 	tmpData = append(tmpData, val)
				// 	mod_msg = key + "(CHANGED),"
				// }
				// 因为oldData是个NullString结构体
				if oldData.Valid {
					if oldData.String != val {
						cmdMiddle += fmt.Sprintf("%s = ?,", key)
						tmpData = append(tmpData, val)
						mod_msg += key + "(CHANGED),"
					}
				} else {
					if val != "" {
						cmdMiddle += fmt.Sprintf("%s = ?,", key)
						tmpData = append(tmpData, val)
						mod_msg += key + "(CHANGED),"
					}
				}
			}
			if cmdMiddle != "" {
				// 新数据更新
				cmdMiddle = strings.TrimSuffix(cmdMiddle, ",")
				cmdSuffix := " where NM_IP = ?"
				tmpData = append(tmpData, nmIP)
				if _, err := omniDB.DB.Exec(cmdPrefix+cmdMiddle+cmdSuffix, tmpData...); err != nil {
					logger.LogError("writeDB", "save new record", cmdPrefix+cmdMiddle+cmdSuffix, tmpData, err)
					continue
				}
				// 旧数据备份
				oldFields := make([]string, 0)
				oldData := make([]interface{}, 0)
				for f, v := range oldDevice {
					oldFields = append(oldFields, f)
					oldData = append(oldData, v)
				}
				oldFields = append(oldFields, "NM_ip", "mod_time", "mod_msg")
				oldData = append(oldData, nmIP, time.Now().Format("2006-01-02 15:04:05"), strings.TrimSuffix(mod_msg, ","))
				modRecordCmd := fmt.Sprintf("insert into %s (%s) values (%s?)", modRecordNeTable, strings.Join(oldFields, ","), strings.Repeat("?,", len(oldFields)-1))
				if _, err := omniDB.DB.Exec(modRecordCmd, oldData...); err != nil {
					logger.LogError("writeDB", "save mod record", modRecordCmd, oldData, err)
					continue
				}
			}
		}
	}
	exitChan <- true
}

type oldNERecord struct {
	IND           sql.NullString `db:"IND"`
	NEID          sql.NullString `db:"NEID"`
	PeerID        sql.NullString `db:"Peer_ID"`
	NE            sql.NullString `db:"NE"`
	FINDA         sql.NullString `db:"FINDA"`
	FINDB         sql.NullString `db:"FINDB"`
	FINDC         sql.NullString `db:"FINDC"`
	FINDD         sql.NullString `db:"FINDD"`
	FINDE         sql.NullString `db:"FINDE"`
	FINDF         sql.NullString `db:"FINDF"`
	NECNPREV      sql.NullString `db:"NE_CN_prev"`
	NEcn          sql.NullString `db:"NE_CN"`
	District      sql.NullString `db:"DISTRICT"`
	AccID         sql.NullString `db:"ACC_ID"`
	NodeName      sql.NullString `db:"Node_Name"`
	NodeAdd       sql.NullString `db:"Node_Add"`
	Longitude     sql.NullString `db:"Longitude"`
	Latitude      sql.NullString `db:"Latitude"`
	LgCalibrate   sql.NullString `db:"Lg_calibrate"`
	LaCalibrate   sql.NullString `db:"La_calibrate"`
	Loopback0IP   sql.NullString `db:"loopback0_ip"`
	NMIP          sql.NullString `db:"NM_ip"`
	Loopback10IP  sql.NullString `db:"loopback10_ip"`
	Loopback0IPv6 sql.NullString `db:"loopback0_ipv6"`
	Brand         sql.NullString `db:"brand"`
	Model         sql.NullString `db:"model"`
	Type          sql.NullString `db:"type"`
	Version       sql.NullString `db:"version"`
	VersionPatch  sql.NullString `db:"version_patch"`
	Coordinate    sql.NullString `db:"coordinate"`
	Status        sql.NullString `db:"status"`
	StartTime     sql.NullString `db:"start_time"`
	BuilChapter   sql.NullString `db:"buil_chapter"`
	SN            sql.NullString `db:"SN"`
	PropertyNo    sql.NullString `db:"propertyNo"`
	CoordinatePW  sql.NullString `db:"coordinate_PW"`
	TypePW        sql.NullString `db:"type_PW"`
	ElogServer    sql.NullString `db:"elog_server"`
	IPnetServer   sql.NullString `db:"IPNET_server"`
}

// getOldData 返回值是一个以NMIP为第一层key，数据内容为除NMIP外的字段形成的map
func getOldData(cmd string) map[string]map[string]sql.NullString {
	oldDeviceMap := make(map[string]map[string]sql.NullString, 0)
	var oldNE []*oldNERecord

	if err := omniDB.DB.Select(&oldNE, cmd); err != nil {
		logger.LogError("getOldData", "sql query", "cmd", cmd, err)
		return oldDeviceMap
	}
	for _, oldDevice := range oldNE {
		oldDeviceMap[oldDevice.NMIP.String] = map[string]sql.NullString{
			"IND":            oldDevice.IND,
			"NEID":           oldDevice.NEID,
			"Peer_ID":        oldDevice.PeerID,
			"NE":             oldDevice.NE,
			"FINDA":          oldDevice.FINDA,
			"FINDB":          oldDevice.FINDB,
			"FINDC":          oldDevice.FINDC,
			"FINDD":          oldDevice.FINDD,
			"FINDE":          oldDevice.FINDE,
			"FINDF":          oldDevice.FINDF,
			"NE_CN_prev":     oldDevice.NECNPREV,
			"NE_CN":          oldDevice.NEcn,
			"DISTRICT":       oldDevice.District,
			"ACC_ID":         oldDevice.AccID,
			"Node_Name":      oldDevice.NodeName,
			"Node_Add":       oldDevice.NodeAdd,
			"Longitude":      oldDevice.Longitude,
			"Latitude":       oldDevice.Latitude,
			"Lg_calibrate":   oldDevice.LgCalibrate,
			"La_calibrate":   oldDevice.LaCalibrate,
			"loopback0_ip":   oldDevice.Loopback0IP,
			"loopback10_ip":  oldDevice.Loopback10IP,
			"loopback0_ipv6": oldDevice.Loopback0IPv6,
			"brand":          oldDevice.Brand,
			"model":          oldDevice.Model,
			"type":           oldDevice.Type,
			"version":        oldDevice.Version,
			"version_patch":  oldDevice.VersionPatch,
			"coordinate":     oldDevice.Coordinate,
			"status":         oldDevice.Status,
			"start_time":     oldDevice.StartTime,
			"buil_chapter":   oldDevice.BuilChapter,
			"SN":             oldDevice.SN,
			"propertyNo":     oldDevice.PropertyNo,
			"coordinate_PW":  oldDevice.CoordinatePW,
			"type_PW":        oldDevice.TypePW,
			"elog_server":    oldDevice.ElogServer,
			"IPNET_server":   oldDevice.IPnetServer,
		}
	}
	return oldDeviceMap
}

func main() {
	logger.LogWarn("mainProcess", "begin", "none", "none", "main process begin")
	nodes := map[string]string{
		"1.3.6.1.2.1.1.5":                      "NE",            // 单个数据
		"1.3.6.1.4.1.2011.5.25.188.1.5":        "version",       // 单个数据，华为独有
		"1.3.6.1.4.1.2011.5.25.19.1.8.5.1.1.4": "version_patch", // 华为独有
		"1.3.6.1.2.1.47.1.1.1.1.5":             "physicalClass", // SN和model的index
		"1.3.6.1.2.1.47.1.1.1.1.7":             "model",         // productName,TODO 筛选1.3.6.1.2.1.47.1.1.1.1.5 value为3的index然后匹配
		"1.3.6.1.2.1.47.1.1.1.1.11":            "SN",            // 筛选1.3.6.1.2.1.47.1.1.1.1.5 value为3的index然后匹配
	}
	orderField := []string{"physicalClass", "model", "SN"}

	// 原表数据，用于比对
	// TODO这里的*需要修改一下，以及上面oldDevice相关的内容也需要
	cmd := fmt.Sprintf(`select * from %s where NM_ip is not null and brand = "HUAWEI" and NE like "GDFOS-MS-IPMAN-B%%"`, updateNeTable)
	oldDeviceMap := getOldData(cmd)

	deviceChan := make(chan *NEDevice, 10)
	snmpResultChan := make(chan map[string]map[string]map[string]string, 20)
	parsedDataChan := make(chan map[string]map[string]string, 20)

	chanNum := 5 // 协程池大小
	exitWalkOidChan := make(chan bool, chanNum)
	exitParseChan := make(chan bool, chanNum)
	exitChan := make(chan bool, chanNum)

	cmd = fmt.Sprintf(`select NE, NM_ip from %s where NM_ip is not null and brand = "HUAWEI" and NE like "GDFOS-MS-IPMAN-B%%"`, updateNeTable)
	go getNEDevcie(deviceChan, cmd)

	for i := 0; i < chanNum; i++ {
		go walkOid(deviceChan, snmpResultChan, exitWalkOidChan, nodes)
	}
	go func() {
		for i := 0; i < chanNum; i++ {
			<-exitWalkOidChan
		}
		close(exitWalkOidChan)
		close(snmpResultChan)
	}()

	for i := 0; i < chanNum; i++ {
		go parseHW(snmpResultChan, parsedDataChan, exitParseChan, orderField)
	}
	go func() {
		for i := 0; i < chanNum; i++ {
			<-exitParseChan
		}
		close(exitParseChan)
		close(parsedDataChan)
	}()

	for i := 0; i < chanNum; i++ {
		go writeDB(parsedDataChan, exitChan, oldDeviceMap)
	}
	func() {
		for i := 0; i < chanNum; i++ {
			<-exitChan
		}
		close(exitChan)
	}()
	logger.LogWarn("mainProcess", "end", "none", "none", "main process end")
}

func init() {
	dbType = "mysql"
	dbName = "cmdb"
	cnxType = "tcp"
	user = "root"

	pwd = "hlw2019!@#$"
	address = "10.201.163.202"
	port = 9003

	// 初始化数据库连接
	if err := omniDB.InitDB(dbType, dbName, user, pwd, cnxType, address, port); err != nil {
		logger.LogError("InitDB", "none", "none", "none", err)
		return
	}
}
