package main

import (
	"fmt"
	"strings"
	"time"

	omniDB "database_tool"
	pingbase "ping_client/base"
	omniSSH "ssh_tool/base"

	_ "github.com/go-sql-driver/mysql"
)

var (
	dbType  string
	dbName  string
	user    string
	pwd     string
	cnxType string
	address string
	port    int
)

const saveTable = "OM_REP_group_client_ping_test" // 要保存的数据表名

// targetDevice 对应getTargetinfo返回的字段的结构体
type targetDevice struct {
	Bng        string `db:"bng"`
	IPs        string `db:"ips"`
	Manufactor string `db:"device_manufactor_id"`
	LoginIP    string `db:"device_ip"`
	LoginUser  string `db:"login_user"`
	LoginPwd   string `db:"login_password"`
	LoginPort  uint   `db:"login_port"`
	Cnt        int    `db:"cnt"`
	server     *omniSSH.Server
}

func (t *targetDevice) LoginDevice() (err error) {
	t.server = omniSSH.NewServer(t.LoginUser, t.LoginPwd, "tcp", t.LoginIP, t.LoginPort)
	err = t.server.Connect2Server()
	return
}

func (t *targetDevice) CloseSSHConnection() {
	t.server.Client.Close()
}

func (t *targetDevice) GetDeviceName() string {
	return t.Bng
}

func (t *targetDevice) GetManufactor() string {
	return t.Manufactor
}

// Ping 发起ping测试，会将一台设备上的目标IP按照batchSize大小进行分组测试，以避免长时间的连接维持
func (t *targetDevice) Ping(cmdPrefix string, endCmd string, batchSize int) [][]interface{} {
	ipSlice := strings.Split(t.IPs, ",")
	// 批处理，由于部分目标清单较大，避免等待时间长的问题，进行分组批处理
	result, err := pingbase.BatchPing(ipSlice, cmdPrefix, endCmd, batchSize, t.server.GetShellOutput, t.ParsePingResultMulti)
	if err != nil {
		pingbase.Logger.LogError("BatchPing", "getOutputFunc", "none", "none", err)
	}
	return result
}

// ParsePingResultMulti 解析ping数据
func (t *targetDevice) ParsePingResultMulti(pingResult string) [][]interface{} {
	exp := pingbase.PingParseExpMap[t.Manufactor]
	parseResult := pingbase.ParsePingResultRaw(pingResult, exp[0].(string), exp[1].(int), exp[2].(int), exp[3].(int))
	return parseResult
}

// getTargetinfo 获取任务信息
func getTargetInfo(pingTargetChan chan<- pingbase.Target) {
	pingbase.Logger.LogInfo("getTargetInfo", "begin", "none", "none", "none")
	sqlCmd := `
	SELECT a.*, b.device_ip, b.login_port, b.login_user, b.login_password, b.device_manufactor_id
	FROM 
	(SELECT bng, GROUP_CONCAT(DISTINCT(ip)) AS ips, COUNT(DISTINCT(ip)) AS cnt FROM MR_STS_ip_olt_detail
	GROUP BY bng
	ORDER BY cnt asc) AS a
	LEFT JOIN MR_REC_watchdog_device_copy AS b 
	ON a.bng = b.device_name limit 2`
	var targets []*targetDevice // 必须使用指针传递
	if err := omniDB.DB.Select(&targets, sqlCmd); err != nil {
		close(pingTargetChan)
		return
	}
	for _, t := range targets {
		pingTargetChan <- t
	}
	close(pingTargetChan)

}

// saveData 保存数据
func saveData(saveTable string, targetResultChan <-chan map[string][][]interface{}, exitSaveChan chan<- bool) {
	title := []string{"record_time", "source_device", "target_ip", "loss", "cost"}
	recordTime := time.Now()
	sqlCmd := fmt.Sprintf("insert into %s (%s) values (?%s)", saveTable, strings.Join(title, ","), strings.Repeat(",?", len(title)-1))
	stmt, err := omniDB.DB.Preparex(sqlCmd)
	if err != nil {
		pingbase.Logger.LogError("saveData", "DataBase Prepare", "none", "none", err)
		exitSaveChan <- true
		return
	}
	defer stmt.Close()
	for ret := range targetResultChan {
		for deviceName, datas := range ret {
			pingbase.Logger.LogInfo("saveData", "begin", "device", deviceName, "none")
			for _, data := range datas {
				val := []interface{}{recordTime.Format("2006-01-02 15:04:05"), deviceName}
				if _, err := stmt.Exec(append(val, data...)...); err != nil {
					pingbase.Logger.LogError("saveData", "execute sql", sqlCmd, val, err)
					exitSaveChan <- true
					return
				}
			}
		}
	}
	exitSaveChan <- true
}

func main() {
	pingbase.Logger.LogWarn("mainProcess", "begin", "none", "none", "main process begin")

	pingTargetChan := make(chan pingbase.Target, 20)
	chanNum := 10 // 协程池大小
	targetResultChan := make(chan map[string][][]interface{}, 10)
	exitPingChan := make(chan bool, chanNum)
	exitSaveChan := make(chan bool, chanNum)

	go getTargetInfo(pingTargetChan)
	for i := 0; i < chanNum; i++ {
		go pingbase.PingAndParse(pingTargetChan, targetResultChan, 50, exitPingChan) // 批处理大小为50，视情况调整
	}
	go func() {
		for i := 0; i < chanNum; i++ {
			<-exitPingChan
		}
		close(exitPingChan)
		close(targetResultChan)
	}()
	for i := 0; i < chanNum; i++ {
		go saveData(saveTable, targetResultChan, exitSaveChan)
	}
	func() {
		for i := 0; i < chanNum; i++ {
			<-exitSaveChan
		}
		close(exitSaveChan)
	}()

	pingbase.Logger.LogWarn("mainProcess", "end", "none", "none", "main process end")
}

func init() {
	// 初始化数据库信息
	dbType = "mysql"
	dbName = "omni_agent"
	cnxType = "tcp"
	user = "root"
	// 生产数据库
	// pwd = "hlw2019!@#$"
	// address = "10.201.163.202"
	// port = 9003
	// 测试数据库
	pwd = "password"
	address = "localhost"
	port = 3306
	// 数据库信息
	if err := omniDB.InitDB(dbType, dbName, user, pwd, cnxType, address, port); err != nil {
		pingbase.Logger.LogError("InitDB", "none", "none", "none", err)
		return
	}

	// CmdPrefixMap ping测试的命令前序，默认使用1400的包
	pingbase.CmdPrefixMap = map[string]string{
		"huawei":      "ping -s 1400 ",
		"alcatel_aos": "ping size 1400 ",
		"linux":       "ping -c 5 ",
	}

	// EndCmdMap 设备的退出命令
	pingbase.EndCmdMap = map[string]string{
		"huawei":      "quit",
		"alcatel_aos": "logout",
		"linux":       "exit",
	}
}
