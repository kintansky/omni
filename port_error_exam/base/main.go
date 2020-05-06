package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	omniDB "database_tool"
	omniSNMP "go_snmp/base"
	omniLOG "logger"

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
var oldDataMap = make(map[string]map[string][]int64, 0)  // 旧数据，用于对比
var cmpOrNot bool                                        // 标记是否每次更新是否比较再更新

const (
	saveTable     string = "OM_REC_snmp_port_error_full" // 全局变量，保存的数据位置
	saveDiffTable string = "OM_REC_snmp_port_error_diff" // 新旧数据对比结果保存位置
	loopInterval  uint   = 30                            // 轮询的时间间隔，min
)

type watchDogDevice struct {
	Device string `db:"device_name"`
	IP     string `db:"device_ip"`
	// Network    string `db:"device_network"`
	// LoginPort  int    `db:"login_port"`
	// LoginUser  string `db:"login_user"`
	// LoginPwd   string `db:"login_password"`
	Manufactor string `db:"device_manufactor_id"`
}

// getTargetDevice 取出设备清单
func getTargetDevice(devcieChan chan<- *watchDogDevice) {
	cmd := `select device_name, device_ip, device_manufactor_id from MR_REC_watchdog_device where device_network="IPMAN" and device_name like "GDFOS-MS-IPMAN-B%"`
	var devices []*watchDogDevice
	if err := omniDB.DB.Select(&devices, cmd); err != nil {
		logger.LogError("getTargetDevice", "sql query", "cmd", cmd, err)
		close(devcieChan)
		return
	}
	for _, d := range devices {
		logger.LogInfo("getTargetDevice", "write channel", "channel", d.Device, "writing to channel")
		devcieChan <- d
	}
	close(devcieChan)

}

// walkOid 遍历oid
func walkOid(devcieChan <-chan *watchDogDevice, snmpResultChan chan<- map[string]map[string]map[string]string, exitWalkOidChan chan<- bool, nodes map[string]string) {
	for d := range devcieChan {
		logger.LogInfo("walkOid", "read channel", "channel", d.Device, "begin to walk oid")
		community := "getgmcc!)"                                             // SNMP 的community
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
			logger.LogInfo("walkOid", "walk node", "NodeOID", d.Device+":"+node, "walk node")
			ret, err := snmp.BulkWalkAll(node)
			if err != nil {
				logger.LogError("walkOid", "BulkWalk", "NodeOID", d.Device+":"+node, err)
				continue
			}
			oidMap := make(map[string]string, 0)
			for _, pdu := range ret {
				k, v, err := omniSNMP.ParseAsKeyVal(&pdu)
				if err != nil {
					logger.LogWarn("walkOid", "BulkWalk", d.Device+":"+k, pdu.Type.String(), "parse OID return error")
					oidMap[k] = "parse error"
				}
				// fmt.Println(k, ":", v)
				oidMap[strings.TrimPrefix(k, "."+node)] = v
			}
			nodeMap[nodeName] = oidMap
		}
		snmpResult[d.Device] = nodeMap
		// fmt.Printf("%#v\n", snmpResult)
		snmpResultChan <- snmpResult
	}

	// 增加了协程池，不在这里关闭snmpResultChan，需等待所有协程都完成之后再关闭，即exitWalkOidChan写完后
	// close(snmpResultChan)
	exitWalkOidChan <- true
}

// saveData 写入数据库
func saveData(snmpResultChan <-chan map[string]map[string]map[string]string, exitWriteDBChan chan<- bool, nodes map[string]string, orderedField []string, cmp bool, saveFunc func(string, map[string]string, map[string]map[string]string, []string, bool) error) {
	for ret := range snmpResultChan {
		dataProcess(ret, nodes, orderedField, cmp, saveFunc)
	}
	exitWriteDBChan <- true
	// close(exitWriteDBChan)
}

// dataProcess 数据处理
func dataProcess(snmpResult map[string]map[string]map[string]string, nodes map[string]string, orderedField []string, cmp bool, saveFunc func(string, map[string]string, map[string]map[string]string, []string, bool) error) {
	// {deviceName: {
	// 		node1:{oid1:val; oid2:val;...};
	// 		node2:{oid1:val; oid2:val;...};
	// 		...
	// 		}
	// }
	for deviceName, nodeMap := range snmpResult {
		logger.LogInfo("dataProcess", "begin", "none", deviceName, "begin data processing")
		indexNameMap := make(map[string]string, 0)       // {index:idexName,...}
		dataMap := make(map[string]map[string]string, 0) // {dataName:{index: data_value},...}
		for _, f := range orderedField[1:] {
			dataMap[f] = nodeMap[f]
		}
		for node, name := range nodes {
			// index 额外处理
			if name == orderedField[0] { // orderedField[0] 规定为index
				for ifNode, ifName := range nodeMap[node] {
					tmpNode := strings.Split(ifNode, ".")
					n := tmpNode[len(tmpNode)-1] // node最后一位是index
					indexNameMap[n] = ifName
				}
				continue
			}
			// 其他都是作为data处理
			oidMap := make(map[string]string, 0)
			for oid, v := range nodeMap[node] {
				tmpNode := strings.Split(oid, ".")
				n := tmpNode[len(tmpNode)-1] // node最后一位是index
				oidMap[n] = v
			}
			dataMap[name] = oidMap
		}

		if err := saveFunc(deviceName, indexNameMap, dataMap, orderedField, cmp); err != nil {
			continue
		}

	}

}

// writeDB 直接保存数据，不会进行比较
func writeDB(deviceName string, indexNameMap map[string]string, dataMap map[string]map[string]string, orderedDataField []string, cmp bool) (err error) {
	sqlCmd := fmt.Sprintf("insert into %s (%s,%s,%s) values (?,?%s)", saveTable, "device", "record_time", strings.Join(orderedDataField, ","), strings.Repeat(",?", len(orderedDataField)-1))
	stmt, err := omniDB.DB.Preparex(sqlCmd)
	if err != nil {
		logger.LogError("writeDB", "DataBase Prepare", "none", "none", err)
		return
	}
	recordTime := time.Now()
	// 因为每次需合并成一条完成数据，所以按照index进行取出遍历
	for index, name := range indexNameMap { // index, ifName
		// TODO: 可以增加name的过滤，不全部写入
		dbData := make([]string, 0, len(orderedDataField)+2) // 长度即为字段数量+2
		dbData = append(dbData, deviceName, recordTime.Format("2006-01-02 15:04:05"), name)
		// 按照orderedDataField的顺序吧data取出，添加到dbData
		// 一次循环，即对dataMap中所有同一个index的数据取出，并按照orderDataField的顺序加入dbData
		for _, dataName := range orderedDataField[1:] { // 按照排序字段取出
			val, ok := dataMap[dataName][index] // 对应的值
			if !ok {                            // 如果没有这个index数据的情况处理
				logger.LogWarn("writeDB", "get data", dataName, index, fmt.Sprintf("%s has no index %v so use default value 0 to fill", dataName, index))
				dbData = append(dbData, "0")
			} else {
				dbData = append(dbData, val)
			}
		}
		fmt.Println(dbData)

		if _, err := stmt.Exec(dbData); err != nil {
			logger.LogError("writeDB", "execute sql", sqlCmd, dbData, err)
			continue
		}
	}
	return
}

// compareAndWriteDB 根据deviceCmp字段决定是否比较数据和写入，全量数据不会判断deviceCmp
func compareAndWriteDB(deviceName string, indexNameMap map[string]string, dataMap map[string]map[string]string, orderedDataField []string, deviceCmp bool) (err error) {
	oldPortData, ok := oldDataMap[deviceName]
	if deviceCmp && !ok { // 如果这台设备不存在于旧表，说明是新设备不比较
		deviceCmp = false
	}
	sqlCmdFull := fmt.Sprintf("insert into %s (%s,%s,%s) values (?,?%s)", saveTable, "device", "record_time", strings.Join(orderedDataField, ","), strings.Repeat(",?", len(orderedDataField)))

	diffField := make([]string, 0)
	diffField = append(diffField, orderedDataField[0])
	for _, f := range orderedDataField[1:] { // 默认diff的字段名有一个state_前序
		diffField = append(diffField, "state_"+f)
	}
	sqlCmdDiff := fmt.Sprintf("insert into %s (%s,%s,%s) values (?,?%s)", saveDiffTable, "device", "record_time", strings.Join(diffField, ","), strings.Repeat(",?", len(orderedDataField)))

	recordTime := time.Now()
	// 因为每次需合并成一条完成数据，所以按照index进行取出遍历
	for index, name := range indexNameMap { // index, ifName
		// 根据设备型号过滤一些不需要的端口
		if strings.HasSuffix(deviceName, "HW") {
			if strings.HasPrefix(strings.ToLower(name), "eth-trunk") || strings.HasPrefix(strings.ToLower(name), "loopback") || strings.HasPrefix(strings.ToLower(name), "virtual") || strings.HasPrefix(strings.ToLower(name), "null") || strings.HasPrefix(strings.ToLower(name), "inloop") {
				continue // 过滤掉一些数据不处理，只需要物理端口
			}
			if matched, _ := regexp.MatchString(`\D*0/0/0`, name); matched {
				continue
			}
		} else if strings.HasSuffix(deviceName, "AL") {
			if matched, _ := regexp.MatchString(`(\d+/){2}\d+`, name); !matched {
				continue
			}
		}

		cmp := deviceCmp
		dataLength := len(orderedDataField) + 2

		dbData := make([]interface{}, 0, dataLength) // 长度即为字段数量+2
		dbData = append(dbData, deviceName, recordTime.Format("2006-01-02 15:04:05"), name)
		// 用于比较的数据
		var diffData []interface{}
		var old []int64
		if cmp {
			old, ok = oldPortData[name]
			if !ok { // 如果子节点不存在于旧数据，也是不用比较的，譬如新增的端口
				cmp = false
			}
			if cmp {
				diffData = make([]interface{}, 0, dataLength)
				diffData = append(diffData, deviceName, recordTime.Format("2006-01-02 15:04:05"), name)
			}
		}

		// 按照orderedDataField的顺序吧data取出，添加到dbData
		// 一次循环，即对dataMap中所有同一个index的数据取出，并按照orderDataField的顺序加入dbData
		haveDiffDataToWrite := false
		for i, dataName := range orderedDataField[1:] { // 按照排序字段取出
			val, ok := dataMap[dataName][index] // 对应的值
			if !ok {                            // 如果没有这个index数据的情况处理
				logger.LogWarn("writeDB", "get data", dataName, index, fmt.Sprintf("%s has no index %v so use default value 0 to fill", dataName, index))
				dbData = append(dbData, "0")
				if cmp {
					diffData = append(diffData, "0")
				}
			} else {
				dbData = append(dbData, val) // 原数据
				// 比较数据
				if cmp {
					n, err := strconv.ParseInt(val, 10, 64)
					if err != nil {
						logger.LogError("compareAndWriteDB", "fail to parse str as int", dataName, val, err)
						continue
					}
					if n > old[i] {
						state := float64(n-old[i]) / float64((recordTime.Unix()-old[len(old)-1])/60) // 平均每分钟的增速
						diffData = append(diffData, fmt.Sprintf("%.2f", state))
						logger.LogInfo("compareAndWriteDB", "compare data", deviceName+":"+name+":"+dataName, fmt.Sprintf("Old:%d,New:%d", old[i], n), "found different")
						haveDiffDataToWrite = true
					} else {
						diffData = append(diffData, "0")
					}
				}

			}
		}
		// 更新全量表
		if _, err := omniDB.DB.Exec(sqlCmdFull, dbData...); err != nil {
			logger.LogError("writeDB", "execute sql", sqlCmdFull, dbData, err)
		}
		// 更新变化表
		if haveDiffDataToWrite {
			if _, err := omniDB.DB.Exec(sqlCmdDiff, diffData...); err != nil {
				logger.LogError("writeDB", "execute sql", sqlCmdDiff, diffData, err)
			}
		}
	}
	return
}

// saveData2 写入数据库，优化后的写法
func saveData2(snmpResultChan <-chan map[string]map[string]map[string]string, exitWriteDBChan chan<- bool, orderedField []string, cmp bool, saveFunc func(string, map[string]map[string]string, []string, bool) error) {
	for ret := range snmpResultChan {
		dataProcess2(ret, orderedField, cmp, saveFunc)
	}
	exitWriteDBChan <- true
	// close(exitWriteDBChan)
}

func dataProcess2(snmpResult map[string]map[string]map[string]string, orderedField []string, cmp bool, saveFunc func(string, map[string]map[string]string, []string, bool) error) {
	for deviceName, nodeMap := range snmpResult {
		logger.LogInfo("dataProcess", "begin", "none", deviceName, "begin data processing")
		if err := saveFunc(deviceName, nodeMap, orderedField, cmp); err != nil {
			continue
		}
	}
}

func writeDB2(deviceName string, nodeMap map[string]map[string]string, orderedDataField []string, cmp bool) (err error) {
	sqlCmd := fmt.Sprintf("insert into %s (%s,%s,%s) values (?,?%s)", saveTable, "device", "record_time", strings.Join(orderedDataField, ","), strings.Repeat(",?", len(orderedDataField)-1))
	stmt, err := omniDB.DB.Preparex(sqlCmd)
	if err != nil {
		logger.LogError("writeDB", "DataBase Prepare", "none", "none", err)
		return
	}
	recordTime := time.Now()
	// 因为每次需合并成一条完成数据，所以按照index进行取出遍历
	for index, name := range nodeMap[orderedDataField[0]] { // index, ifName
		// TODO: 可以增加name的过滤，不全部写入
		dbData := make([]string, 0, len(orderedDataField)+2) // 长度即为字段数量+2
		dbData = append(dbData, deviceName, recordTime.Format("2006-01-02 15:04:05"), name)
		// 按照orderedDataField的顺序吧data取出，添加到dbData
		// 一次循环，即对dataMap中所有同一个index的数据取出，并按照orderDataField的顺序加入dbData
		for _, dataName := range orderedDataField[1:] { // 按照排序字段取出
			val, ok := nodeMap[dataName][index] // 对应的值
			if !ok {                            // 如果没有这个index数据的情况处理
				logger.LogWarn("writeDB", "get data", dataName, index, fmt.Sprintf("%s has no index %v so use default value 0 to fill", dataName, index))
				dbData = append(dbData, "0")
			} else {
				dbData = append(dbData, val)
			}
		}
		fmt.Println(dbData)

		if _, err := stmt.Exec(dbData); err != nil {
			logger.LogError("writeDB", "execute sql", sqlCmd, dbData, err)
			continue
		}
	}
	return
}

// compareAndWriteDB 根据deviceCmp字段决定是否比较数据和写入，全量数据不会判断deviceCmp
func compareAndWriteDB2(deviceName string, nodeMap map[string]map[string]string, orderedDataField []string, deviceCmp bool) (err error) {
	oldPortData, ok := oldDataMap[deviceName]
	if deviceCmp && !ok { // 如果这台设备不存在于旧表，说明是新设备不比较
		deviceCmp = false
	}
	sqlCmdFull := fmt.Sprintf("insert into %s (%s,%s,%s) values (?,?%s)", saveTable, "device", "record_time", strings.Join(orderedDataField, ","), strings.Repeat(",?", len(orderedDataField)))

	diffField := make([]string, 0)
	diffField = append(diffField, orderedDataField[0])
	for _, f := range orderedDataField[1:] { // 默认diff的字段名有一个state_前序
		diffField = append(diffField, "state_"+f)
	}
	sqlCmdDiff := fmt.Sprintf("insert into %s (%s,%s,%s) values (?,?%s)", saveDiffTable, "device", "record_time", strings.Join(diffField, ","), strings.Repeat(",?", len(orderedDataField)))

	recordTime := time.Now()
	// 因为每次需合并成一条完成数据，所以按照index进行取出遍历
	for index, name := range nodeMap[orderedDataField[0]] { // index, ifName
		// 根据设备型号过滤一些不需要的端口
		if strings.HasSuffix(deviceName, "HW") {
			if strings.HasPrefix(strings.ToLower(name), "eth-trunk") || strings.HasPrefix(strings.ToLower(name), "loopback") || strings.HasPrefix(strings.ToLower(name), "virtual") || strings.HasPrefix(strings.ToLower(name), "null") || strings.HasPrefix(strings.ToLower(name), "inloop") {
				continue // 过滤掉一些数据不处理，只需要物理端口
			}
			if matched, _ := regexp.MatchString(`\D*0/0/0`, name); matched {
				continue
			}
		} else if strings.HasSuffix(deviceName, "AL") {
			if matched, _ := regexp.MatchString(`(\d+/){2}\d+`, name); !matched {
				continue
			}
		}

		cmp := deviceCmp
		dataLength := len(orderedDataField) + 2

		dbData := make([]interface{}, 0, dataLength) // 长度即为字段数量+2
		dbData = append(dbData, deviceName, recordTime.Format("2006-01-02 15:04:05"), name)
		// 用于比较的数据
		var diffData []interface{}
		var old []int64
		if cmp {
			old, ok = oldPortData[name]
			if !ok { // 如果子节点不存在于旧数据，也是不用比较的，譬如新增的端口
				cmp = false
			}
			if cmp {
				diffData = make([]interface{}, 0, dataLength)
				diffData = append(diffData, deviceName, recordTime.Format("2006-01-02 15:04:05"), name)
			}
		}

		// 按照orderedDataField的顺序吧data取出，添加到dbData
		// 一次循环，即对dataMap中所有同一个index的数据取出，并按照orderDataField的顺序加入dbData
		haveDiffDataToWrite := false
		for i, dataName := range orderedDataField[1:] { // 按照排序字段取出
			val, ok := nodeMap[dataName][index] // 对应的值
			if !ok {                            // 如果没有这个index数据的情况处理
				logger.LogWarn("writeDB", "get data", dataName, index, fmt.Sprintf("%s has no index %v so use default value 0 to fill", dataName, index))
				dbData = append(dbData, "0")
				if cmp {
					diffData = append(diffData, "0")
				}
			} else {
				dbData = append(dbData, val) // 原数据
				// 比较数据
				if cmp {
					n, err := strconv.ParseInt(val, 10, 64)
					if err != nil {
						logger.LogError("compareAndWriteDB", "fail to parse str as int", dataName, val, err)
						continue
					}
					if n > old[i] {
						state := float64(n-old[i]) / float64((recordTime.Unix()-old[len(old)-1])/60) // 平均每分钟的增速
						diffData = append(diffData, fmt.Sprintf("%.2f", state))
						logger.LogInfo("compareAndWriteDB", "compare data", deviceName+":"+name+":"+dataName, fmt.Sprintf("Old:%d,New:%d", old[i], n), "found different")
						haveDiffDataToWrite = true
					} else {
						diffData = append(diffData, "0")
					}
				}

			}
		}
		// 更新全量表
		if _, err := omniDB.DB.Exec(sqlCmdFull, dbData...); err != nil {
			logger.LogError("writeDB", "execute sql", sqlCmdFull, dbData, err)
		}
		// 更新变化表
		if haveDiffDataToWrite {
			if _, err := omniDB.DB.Exec(sqlCmdDiff, diffData...); err != nil {
				logger.LogError("writeDB", "execute sql", sqlCmdDiff, diffData, err)
			}
		}
	}
	return
}

// oldPortErrorData 旧的端口质量数据
type oldPortErrorData struct {
	Device        string `db:"device"`
	Port          string `db:"port"`
	CRC           int64  `db:"crc"`
	IPv4HeadError int64  `db:"ipv4HeadError"`
	RecordTime    string `db:"record_time"`
}

// 旧数据取出保存至全局变量oldDataMap
func getOldData(orderedField []string) {
	cmd := fmt.Sprintf("select device, port, record_time, %s from %s", strings.Join(orderedField, ","), saveTable)
	var oldData []oldPortErrorData
	if err := omniDB.DB.Select(&oldData, cmd); err != nil {
		logger.LogError("getOldData", "sql query", "cmd", cmd, err)
		return
	}
	// 旧数据整理成map[string]map[string][2]int
	// {device:{
	// 	port: [crc, hrdError]
	// 	...
	// }
	loc, _ := time.LoadLocation("Asia/Shanghai") // 需要解析成当地时间，不然后面时间间隔计算错误
	for _, old := range oldData {
		alreadyInsertedData, ok := oldDataMap[old.Device]
		recordTime, err := time.ParseInLocation("2006-01-02 15:04:05", old.RecordTime, loc)
		if err != nil {
			logger.LogError("getOldData", "parse time format error", "oldData's recordTime", old.RecordTime, err)
			cmpOrNot = false // 如果parse失败会导致后面无法比较，所以将比较标记位置为false
			return
		}
		if !ok { // 不存在这台设备，添加设备
			var portData = map[string][]int64{
				old.Port: []int64{old.CRC, old.IPv4HeadError, recordTime.Unix()},
			}
			oldDataMap[old.Device] = portData
		} else { // 存在这台设备，直接增加端口数据
			alreadyInsertedData[old.Port] = []int64{old.CRC, old.IPv4HeadError, recordTime.Unix()}
		}
	}
}

func cleanOldData() {
	cmd := `truncate table ` + saveTable
	if _, err := omniDB.DB.Exec(cmd); err != nil {
		logger.LogError("cleanOldData", "sql query", "cmd", cmd, err)
		return
	}
}

func sleeper(interval uint) {
	endTime := time.Now().Add(time.Duration(interval) * time.Minute)
	logger.LogWarn("Sleep", "begin", "next activation", endTime.Format("2006/01/02 15:04:05"), "Sleep begin")
	for t := range time.Tick(1 * time.Second) {
		if t.After(endTime) {
			break
		}
		fmt.Printf("\r%s", t.Format("2006/01/02 15:04:05"))
	}
	logger.LogWarn("Sleep", "end", "none", "none", "Sleep end")
	fmt.Println()
}

func main() {
	for {
		logger.LogWarn("mainProcess", "begin", "none", "none", "main process begin")

		// 需要遍历的OIDS
		// nodes对应的value即为数据库字段名
		nodes := map[string]string{
			"1.3.6.1.2.1.31.1.1.1.1": "port",
			"1.3.6.1.2.1.2.2.1.14":   "crc",
			"1.3.6.1.2.1.4.31.3.1.7": "ipv4HeadError",
		}
		// 注意取出来的顺序和oldDataMap取出来的字段顺序要一致，不然比较的时候会出错
		orderedField := []string{"port", "crc", "ipv4HeadError"} // 排好序的field，将按照排序写进数据库
		if cmpOrNot {
			getOldData(orderedField[1:])
		}
		cleanOldData()

		devcieChan := make(chan *watchDogDevice, 10)
		snmpResultChan := make(chan map[string]map[string]map[string]string, 20)
		chanNum := 5 // 协程池大小
		exitWalkOidChan := make(chan bool, chanNum)
		exitWriteDBChan := make(chan bool, chanNum)

		go getTargetDevice(devcieChan)
		for i := 0; i < chanNum; i++ {
			go walkOid(devcieChan, snmpResultChan, exitWalkOidChan, nodes)
		}
		go func() {
			for i := 0; i < chanNum; i++ {
				<-exitWalkOidChan
			}
			close(exitWalkOidChan) // 取完，关闭
			close(snmpResultChan)  // 写入完毕，关闭
		}()

		for i := 0; i < chanNum; i++ {
			// go saveData(snmpResultChan, exitWriteDBChan, nodes, orderedField, cmpOrNot, compareAndWriteDB)
			go saveData2(snmpResultChan, exitWriteDBChan, orderedField, cmpOrNot, compareAndWriteDB2)
		}
		func() { // 阻塞主线程
			for i := 0; i < chanNum; i++ {
				<-exitWriteDBChan
			}
			close(exitWriteDBChan)
		}()

		logger.LogWarn("mainProcess", "end", "none", "none", "main process end")

		sleeper(loopInterval)
	}

}

func init() {
	dbType = "mysql"
	dbName = "omni_agent"
	cnxType = "tcp"
	user = "root"

	pwd = "hlw2019!@#$"
	address = "10.201.163.202"
	port = 9003
	// pwd = "password"
	// address = "localhost"
	// port = 3306

	// 初始化数据库连接
	if err := omniDB.InitDB(dbType, dbName, user, pwd, cnxType, address, port); err != nil {
		logger.LogError("InitDB", "none", "none", "none", err)
		return
	}
	cmpOrNot = true // 标记需要比较数据
}
