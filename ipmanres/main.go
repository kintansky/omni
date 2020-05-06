package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
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

// entPhysicalClassMap index所属类型字典
var entPhysicalClassMap map[string]string

// 数据表
const (
	chassisTable string = "MR_REC_chassis"
	slotTable    string = "MR_REC_slot"
	cardTable    string = "MR_REC_card"
	portTable    string = "MR_REC_port"
)

// readFromFile 测试函数，需要迁移到testing
func readFromFile(path string) map[string]string {
	ret := make(map[string]string, 0)
	fileObj, err := os.Open(path)
	if err != nil {
		fmt.Println("Open File error", err)
		return ret
	}
	defer fileObj.Close()
	re := regexp.MustCompile(`OID:(\S+)\s+Type:.*?Value:(\S+)`)
	reader := bufio.NewReader(fileObj)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			reResult := re.FindStringSubmatch(line)
			if reResult != nil {
				tmp := strings.Split(reResult[1], ".")
				ret[tmp[len(tmp)-1]] = reResult[2]
			}
			return ret
		}
		if err != nil {
			fmt.Println("Read File error")
		}
		reResult := re.FindStringSubmatch(line)
		if reResult != nil {
			tmp := strings.Split(reResult[1], ".")
			ret[tmp[len(tmp)-1]] = reResult[2]
		}
	}

}

// NEDevice 数据库NE表的对应结构体
type NEDevice struct {
	Device string `db:"NE"`
	IP     string `db:"NM_ip"`
	NEID   int64  `db:"NEID"`
}

// getNEDevice 拉取NE设备
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

// walkOid 写入的chan的key是一个结构体地址，用于传递更多信息
func walkOid(deviceChan <-chan *NEDevice, snmpResultChan chan<- map[*NEDevice]map[string]map[string]string, exitWalkOidChan chan<- bool, nodes map[string]string) {
	for d := range deviceChan {
		community := "getgmcc!)"
		snmp, err := omniSNMP.InitSnmp(d.IP, 161, "udp", community, 2, 5, 2) // 版本v2c, 等待5秒，最多重复2次
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
		snmpResult := make(map[*NEDevice]map[string]map[string]string, 1)
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
				oidMap[strings.TrimPrefix(k, "."+node+".")] = v // 干脆把前序也去掉
			}
			nodeMap[nodeName] = oidMap // 使用nodeName替代OID作为key
		}
		snmpResult[d] = nodeMap
		// fmt.Printf("%#v\n", snmpResult)
		snmpResultChan <- snmpResult
	}

	exitWalkOidChan <- true
}

// parseHW 解析华为的节点信息，未知阿朗是否适用
func parseHW(snmpResultChan <-chan map[*NEDevice]map[string]map[string]string, exitParseChan chan<- bool) {
	for ret := range snmpResultChan {
		for neDevice, snmpNodeMap := range ret {
			portTreeNodeMap := snmpNodeMap["entPhysicalContainedIn"]
			logger.LogInfo("parseHW", "Device:"+neDevice.Device, "Device", neDevice.Device, "walkFromTop")
			portTree := createTree(portTreeNodeMap)
			emptyLink := make([]string, 0)
			logger.LogInfo("parseHW", "Devcie:"+neDevice.Device, "Device", neDevice.Device, "walkFromTop")
			walkFromTop("0", portTree, emptyLink, snmpNodeMap, neDevice) // 从父节点0开始遍历
		}
	}
	exitParseChan <- true
}

// dataNode 设备组件的逻辑节点
type dataNode struct {
	data     string
	children []*dataNode
}

// createTree 设备节点生成树
func createTree(nodeMap map[string]string) map[string]*dataNode {
	createdNodeMap := make(map[string]*dataNode, 0)
	for childData, parentData := range nodeMap {
		// 创建子节点，并添加至已创建列表，因为childNode全局唯一，所以不用继续判断添加children
		childNode, ok := createdNodeMap[childData]
		if !ok {
			childNode = &dataNode{data: childData}
			createdNodeMap[childData] = childNode
		}
		parentNode, ok := createdNodeMap[parentData]
		if !ok {
			parentNode = &dataNode{data: parentData, children: []*dataNode{childNode}}
			createdNodeMap[parentData] = parentNode
		} else {
			parentNode.children = append(parentNode.children, childNode)
		}
	}
	return createdNodeMap
}

// walkFromTop 递归遍历整棵设备节点树
func walkFromTop(topNodeIndex string, createdNodeMap map[string]*dataNode, link []string, snmpNodeMap map[string]map[string]string, neDevice *NEDevice) {
	// link生成直系祖先
	fatherNode := createdNodeMap[topNodeIndex]
	link = append(link, fatherNode.data) // 保存了父节点之当前节点的全路径信息
	// 各层次数据单独处理，可以按照classMap进行分层写入
	classIndex := snmpNodeMap["entPhysicalClass"][fatherNode.data]
	var level string
	switch classIndex {
	case "3": // chassis
		// fmt.Printf("chassis:%v:%v:%v\n", fatherNode.data, entPhysicalClassMap[classIndex], snmpNodeMap["entPhysicalName"][fatherNode.data])
		level = "chassis"
	case "9": // module
		upperNodeIndex := link[len(link)-2]
		name := snmpNodeMap["entPhysicalName"][upperNodeIndex]
		switch {
		case strings.HasPrefix(name, "LPU") || strings.HasPrefix(name, "MPU"):
			// 写到slot表格
			// fmt.Printf("slot:%v:%v:%v\n", fatherNode.data, entPhysicalClassMap[classIndex], snmpNodeMap["entPhysicalName"][fatherNode.data])
			level = "slot"
		case strings.HasPrefix(name, "CFCARD") || strings.HasPrefix(name, "Card"):
			// 写到子卡表格
			// fmt.Printf("subcard:%v:%v:%v\n", fatherNode.data, entPhysicalClassMap[classIndex], snmpNodeMap["entPhysicalName"][fatherNode.data])
			level = "card"
		}
	case "10": // port
		// 写到端口表
		// fmt.Printf("port:%v:%v:%v\n", fatherNode.data, entPhysicalClassMap[classIndex], snmpNodeMap["entPhysicalName"][fatherNode.data])
		level = "port"
	default: // 其他情况跳过
		level = "other"
	}
	writeDB(level, link, snmpNodeMap, neDevice)
	// // 测试输出部分
	// for _, idx := range link {
	// 	classIndex := snmpNodeMap["entPhysicalClass"][idx]
	// 	fmt.Printf("idx(%v):class(%v):name(%v):pos(%v):hw(%v):sw(%v):sn(%v):mfg(%v), ", idx, entPhysicalClassMap[classIndex], snmpNodeMap["entPhysicalName"][idx], snmpNodeMap["entPhysicalParentRelPos"][idx], snmpNodeMap["entPhysicalHardwareRev"][idx], snmpNodeMap["entPhysicalSoftwareRev"][idx], snmpNodeMap["entPhysicalSerialNum"][idx], snmpNodeMap["entPhysicalMfgName"][idx])
	// }
	// fmt.Println()
	for _, childNode := range fatherNode.children {
		walkFromTop(childNode.data, createdNodeMap, link, snmpNodeMap, neDevice)
	}
}

func mustGetFkID(cmd string, args ...interface{}) (id int64, err error) {
	err = omniDB.DB.Get(&id, cmd, args...)
	if err != nil {
		logger.LogError("mustGetFkID", "get", cmd, args, err)
		return
	}
	return
}

func insertOrUpdate(oldRecordID int64, tableName string, sqlData map[string]interface{}) (insertCmd string, insertData []interface{}) {
	fields := make([]string, 0)

	if oldRecordID == 0 {
		insertCmd = fmt.Sprintf("insert into %s ", tableName)
		insertData = make([]interface{}, 0)
		for k, v := range sqlData {
			fields = append(fields, k)
			insertData = append(insertData, v)
		}
		insertCmd = fmt.Sprintf(insertCmd+"(%s) values (%s?)", strings.Join(fields, ","), strings.Repeat("?,", len(sqlData)-1))
	} else {
		insertCmd = fmt.Sprintf("update %s set ", tableName)
		for k, v := range sqlData {
			fields = append(fields, k+"=?")
			insertData = append(insertData, v)
		}
		insertData = append(insertData, oldRecordID)
		insertCmd = fmt.Sprintf(insertCmd+"%s where id=?", strings.Join(fields, ","))
	}
	return
}

// writeDB 将设备节点树写入数据库
func writeDB(level string, link []string, snmpNodeMap map[string]map[string]string, neDevice *NEDevice) {
	if level == "other" {
		return
	}
	relPosMap := snmpNodeMap["entPhysicalParentRelPos"]
	nameMap := snmpNodeMap["entPhysicalName"]
	hardRevMap := snmpNodeMap["entPhysicalHardwareRev"]
	softRevMap := snmpNodeMap["entPhysicalSoftwareRev"]
	snMap := snmpNodeMap["entPhysicalSerialNum"]
	mfgMap := snmpNodeMap["entPhysicalMfgName"]
	desMap := snmpNodeMap["entPhysicalDescr"]
	endIdx := link[len(link)-1]
	// 闭包，找父节点信息
	findFatherNode := func(containerIndex string, fatherNodeIndex string, findFatherCmd string, step int64) (fkFatherIDData sql.NullInt64, nodeIDData sql.NullInt64, err error) {
		var fnID, upperLevelID int64
		// 按照cmd查找对应的父节点自增ID和唯一ID
		if err = omniDB.DB.QueryRowx(findFatherCmd, neDevice.NEID, fatherNodeIndex, snMap[fatherNodeIndex]).Scan(&fnID, &upperLevelID); err != nil {
			// 如果找不到父节点，这里会提示无法scan返回错误
			logger.LogError("writeDB", "findFatherNode:scan error, check your syntax if error occur when not finding the wg port's upper card", findFatherCmd, []interface{}{neDevice.NEID, fatherNodeIndex, snMap[fatherNodeIndex]}, err)
			return
		}
		// 按照父节点的唯一ID和当前节点的relPos，生成当前节点的唯一ID
		relPos, err := strconv.ParseInt(relPosMap[containerIndex], 10, 64)
		if err != nil { // 如果解析出错，返回错误
			logger.LogError("writeDB", "findFatherNode:cant parse relpos", "relPosMap[containerIndex]", relPosMap[containerIndex], err)
			// nodeIDData = sql.NullInt64{Int64: upperLevelID*100 + relPos, Valid: false} // 生成唯一性node_id
			return
		}
		nodeIDData = sql.NullInt64{Int64: upperLevelID*step + relPos, Valid: true} // 生成唯一性node_id，TODO：修改100，不同的层级预留文职可能不一样
		// 判断是否有效
		fkFatherIDData = sql.NullInt64{Int64: fnID, Valid: true} // 初始化父节点的自增ID
		if fnID == 0 {                                           // 父节点没有找到，使用null值回填
			logger.LogWarn("writeDB", "findFatherNode", "fnID", fnID, "father node not found and this field will set null")
			fkFatherIDData.Valid = false
			nodeIDData.Valid = false
		}
		return
	}
	var insertCmd string
	var data []interface{}
	switch level {
	case "chassis":
		// 1、准备数据
		relPos, err := strconv.ParseInt(relPosMap[endIdx], 10, 64)
		if err != nil {
			logger.LogError("writeDB", "cant parse chassis relpos", "relPosMap[endIdx]", relPosMap[endIdx], err)
		}
		chassisID := neDevice.NEID*100 + relPos
		sqlData := map[string]interface{}{
			"NEID":                 neDevice.NEID,
			"device_name":          neDevice.Device,
			"chassis_id":           chassisID,
			"chassis_pos":          relPosMap[endIdx],
			"chassis_snmp_index":   endIdx,
			"chassis_name":         nameMap[endIdx],
			"chassis_hardware_ver": hardRevMap[endIdx],
			"chassis_software_ver": softRevMap[endIdx],
			"chassis_sn":           snMap[endIdx],
			"chassis_mfg":          mfgMap[endIdx],
			"chassis_des":          desMap[endIdx],
			"record_time":          time.Now().Format("2006-01-02 15:04:05"),
		}
		// 2、判断是更新还是新增
		// cmd := fmt.Sprintf(`select ifnull((select id from %s where NEID = ? and chassis_snmp_index = ? and chassis_sn = ?), 0)`, chassisTable)
		// oldRecordID, err := mustGetFkID(cmd, neDevice.NEID, endIdx, snMap[endIdx])
		cmd := fmt.Sprintf(`select ifnull((select id from %s where chassis_id = ? and chassis_sn = ?), 0)`, chassisTable)
		oldRecordID, err := mustGetFkID(cmd, chassisID, snMap[endIdx])

		if err != nil { // 查询出错的直接pass
			return
		}
		insertCmd, data = insertOrUpdate(oldRecordID, chassisTable, sqlData)
	case "slot":
		containerIdx := link[len(link)-2]
		fnIdx := link[len(link)-3]
		cmd := fmt.Sprintf(`select id, chassis_id from %s where NEID = ? and chassis_snmp_index = ? and chassis_sn = ?`, chassisTable)
		fnData, slotIDData, err := findFatherNode(containerIdx, fnIdx, cmd, 100)
		if err != nil {
			return
		}
		sqlData := map[string]interface{}{
			"NEID":                 neDevice.NEID,
			"slot_snmp_index":      endIdx,
			"slot_sn":              snMap[endIdx],
			"chassis_fkid":         fnData,
			"slot_id":              slotIDData,
			"slot_container":       nameMap[containerIdx],
			"slot_container_index": containerIdx,
			"slot_pos":             relPosMap[containerIdx],
			"slot_name":            strings.Split(nameMap[endIdx], " ")[0],
			"slot_hardware_ver":    hardRevMap[endIdx],
			"slot_software_ver":    softRevMap[endIdx],
			"slot_mfg":             mfgMap[endIdx],
			"slot_des":             desMap[endIdx],
			"record_time":          time.Now().Format("2006-01-02 15:04:05"),
		}
		// cmd = fmt.Sprintf(`select ifnull((select id from %s where NEID = ? and slot_snmp_index = ? and slot_sn = ?), 0)`, slotTable)
		// oldRecordID, err := mustGetFkID(cmd, neDevice.NEID, endIdx, snMap[endIdx])
		// 更改为使用slot_id + slot_sn来确认是否更新还是新增
		cmd = fmt.Sprintf(`select ifnull((select id from %s where slot_id = ? and slot_sn = ?), 0)`, slotTable)
		oldRecordID, err := mustGetFkID(cmd, slotIDData, snMap[endIdx])
		if err != nil { // 找不到父节点的不写入
			return
		}
		insertCmd, data = insertOrUpdate(oldRecordID, slotTable, sqlData)
	case "card":
		containerIdx := link[len(link)-2]
		fnIdx := link[len(link)-3]
		cmd := fmt.Sprintf(`select id, slot_id from %s where NEID = ? and slot_snmp_index = ? and slot_sn = ?`, slotTable)
		fnData, cardIDData, err := findFatherNode(containerIdx, fnIdx, cmd, 100)
		if err != nil { // 找不到父节点的不写入
			return
		}
		sqlData := map[string]interface{}{
			"NEID":                 neDevice.NEID,
			"card_snmp_index":      endIdx,
			"card_sn":              snMap[endIdx],
			"slot_fkid":            fnData,
			"card_id":              cardIDData,
			"card_container":       nameMap[containerIdx],
			"card_container_index": containerIdx,
			"card_pos":             relPosMap[containerIdx],
			"card_name":            nameMap[endIdx],
			"card_hardware_ver":    hardRevMap[endIdx],
			"card_software_ver":    softRevMap[endIdx],
			"card_mfg":             mfgMap[endIdx],
			"card_des":             desMap[endIdx],
			"record_time":          time.Now().Format("2006-01-02 15:04:05"),
		}
		// cmd = fmt.Sprintf(`select ifnull((select id from %s where NEID = ? and card_snmp_index = ? and card_sn = ?), 0)`, cardTable)
		// 更改为使用card_id + card_sn来确认是否更新还是新增
		// oldRecordID, err := mustGetFkID(cmd, neDevice.NEID, endIdx, snMap[endIdx])
		cmd = fmt.Sprintf(`select ifnull((select id from %s where card_id = ? and card_sn = ?), 0)`, cardTable)
		oldRecordID, err := mustGetFkID(cmd, cardIDData, snMap[endIdx])
		if err != nil { // 找不到父节点的不写入
			return
		}
		insertCmd, data = insertOrUpdate(oldRecordID, cardTable, sqlData)
	case "port":
		fnIdx := link[len(link)-2]
		cmd := fmt.Sprintf(`select id, card_id from %s where NEID = ? and card_snmp_index = ? and card_sn = ?`, cardTable)
		fnData, portIDData, err := findFatherNode(endIdx, fnIdx, cmd, 1000)
		if err != nil { // 找不到父节点的不写入
			return
		}
		sqlData := map[string]interface{}{
			"NEID":            neDevice.NEID,
			"port_snmp_index": endIdx,
			"port_sn":         snMap[endIdx],
			"card_fkid":       fnData,
			"port_id":         portIDData,
			"port_pos":        relPosMap[endIdx],
			"port_name":       nameMap[endIdx],
			"port_mfg":        mfgMap[endIdx],
			"port_des":        desMap[endIdx],
			"record_time":     time.Now().Format("2006-01-02 15:04:05"),
		}
		// cmd = fmt.Sprintf(`select ifnull((select id from %s where NEID = ? and port_snmp_index = ? and port_sn = ?), 0)`, portTable)
		// oldRecordID, err := mustGetFkID(cmd, neDevice.NEID, endIdx, snMap[endIdx])
		// 更改为使用port_id + port_sn来确认是否更新还是新增
		cmd = fmt.Sprintf(`select ifnull((select id from %s where port_id = ? and port_sn = ?), 0)`, portTable)
		oldRecordID, err := mustGetFkID(cmd, portIDData, snMap[endIdx])
		if err != nil {
			return
		}
		insertCmd, data = insertOrUpdate(oldRecordID, portTable, sqlData)
	default: // 其他不需要的数据跳过
		return
	}
	// 统一插入或者跟新数据
	if _, err := omniDB.DB.Exec(insertCmd, data...); err != nil {
		logger.LogError("writeDB", "insert data", insertCmd, data, err)
		return
	}
}

func main() {
	logger.LogWarn("mainProcess", "begin", "none", "none", "main process begin")

	nodes := map[string]string{
		"1.3.6.1.2.1.47.1.1.1.1.4":  "entPhysicalContainedIn",  // 主要信息，index等信息直接从这里取出即可
		"1.3.6.1.2.1.47.1.1.1.1.5":  "entPhysicalClass",        // 主要信息：类型
		"1.3.6.1.2.1.47.1.1.1.1.6":  "entPhysicalParentRelPos", // 主要信息：相对位置
		"1.3.6.1.2.1.47.1.1.1.1.7":  "entPhysicalName",         // 主要信息：名
		"1.3.6.1.2.1.47.1.1.1.1.8":  "entPhysicalHardwareRev",  // 附加信息：硬件版本
		"1.3.6.1.2.1.47.1.1.1.1.10": "entPhysicalSoftwareRev",  // 附加信息：软件版本
		"1.3.6.1.2.1.47.1.1.1.1.11": "entPhysicalSerialNum",    // 附加信息：SN码
		"1.3.6.1.2.1.47.1.1.1.1.12": "entPhysicalMfgName",      // 附加信息：厂商
		"1.3.6.1.2.1.47.1.1.1.1.2":  "entPhysicalDescr",        // 附加信息：描述
	}

	deviceChan := make(chan *NEDevice, 10)
	snmpResultChan := make(chan map[*NEDevice]map[string]map[string]string, 20)

	chanNum := 5
	exitWalkOidChan := make(chan bool, chanNum)
	exitParseChan := make(chan bool, chanNum)

	// cmd := `select NE, NM_ip, NEID from MR_REC_NE where NM_ip is not null and brand = "HUAWEI" and NE like "GDFOS-MS-IPMAN-B%%"`
	cmd := `select NE, NM_ip, NEID from MR_REC_NE where NM_ip is not null and brand = "HUAWEI" and NE like "GDFOS-MS-IPMAN-BNG01-LLWM-HW"` // test data
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
		go parseHW(snmpResultChan, exitParseChan)
	}
	func() {
		for i := 0; i < chanNum; i++ {
			<-exitParseChan
		}
		close(exitParseChan)
	}()
}

func init() {
	entPhysicalClassMap = map[string]string{
		"1":  "other",
		"2":  "unknown",
		"3":  "chassis",
		"4":  "backplane",
		"5":  "container",
		"6":  "powersupply",
		"7":  "fan",
		"8":  "sensor",
		"9":  "module",
		"10": "port",
		"11": "stack",
	}

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
