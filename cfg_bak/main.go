package main

import (
	omniDB "database_tool"
	"fmt"
	"io/ioutil"
	omniLOG "logger"
	omniSSH "ssh_tool/base"

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

// cmd前序
var cmdPrefixMap = map[string]string{
	"huawei":      "screen-length 0 temporary",
	"alcatel_aos": "environment no more",
}

var logCFGMap = map[string]string{
	"huawei":      "dis cur",
	"alcatel_aos": "admin disp",
}

var endCmdMap = map[string]string{
	"huawei":      "quit",
	"alcatel_aos": "logout",
}

func getCmdReady(manufactor string) []string {
	return []string{cmdPrefixMap[manufactor], logCFGMap[manufactor]}
}

type watchDogDevice struct {
	Device     string `db:"device_name"`
	IP         string `db:"device_ip"`
	Network    string `db:"device_network"`
	LoginPort  uint   `db:"login_port"`
	LoginUser  string `db:"login_user"`
	LoginPwd   string `db:"login_password"`
	Manufactor string `db:"device_manufactor_id"`
}

// getTargetDevice 取出设备清单
func getTargetDevice(devcieChan chan<- *watchDogDevice, cmd string) {
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

func getCFG(deviceChan <-chan *watchDogDevice, walkExitChan chan<- bool, savePath string) {
	for d := range deviceChan {
		logger.LogInfo("getCFG", "loginDevice", "device", d.Device, "none")
		sev := omniSSH.NewServer(d.LoginUser, d.LoginPwd, "tcp", d.IP, d.LoginPort)
		if err := sev.Connect2Server(); err != nil {
			logger.LogError("getCFG", "failed to login", d.Device, d.IP, err)
			walkExitChan <- true
			return
		}
		defer sev.Client.Close()

		logCmd := getCmdReady(d.Manufactor)
		// TODO 根据专业划分文件夹
		// savePath :=
		// if err := sev.GetOutputToFile(logCmd, endCmdMap[d.Manufactor], fmt.Sprintf("%s/%s_%s.txt", savePath, d.Device, time.Now().Format("20060102150405"))); err != nil {
		// 	logger.LogError("getCFG", "failed to login", d.Device, d.IP, err)
		// 	walkExitChan <- true
		// 	return
		// }
		result, err := sev.GetShellOutput(logCmd, endCmdMap[d.Manufactor], 1000)
		if err != nil {
			logger.LogError("getCFG", "failed to login", d.Device, d.IP, err)
			walkExitChan <- true
			return
		}
		// writeByIoUtil(result, fmt.Sprintf("%s/%s_%s.txt", savePath, d.Device, time.Now().Format("20060102150405")))
		fmt.Println(result)
	}
	walkExitChan <- true
}

func writeByIoUtil(s string, filePath string) {
	err := ioutil.WriteFile(filePath, []byte(s), 0644) // 但是这里面就没有追加、清理等操作，会直接清空再写入
	if err != nil {
		fmt.Printf("write file failed, err:%v\n", err)
		return
	}
}

func main() {

	deviceChan := make(chan *watchDogDevice, 10)
	chanNum := 5 // 协程池大小
	walkExitChan := make(chan bool, chanNum)
	// cmd := `select device_name, device_ip, device_network, login_port, login_user, login_password, device_manufactor_id from MR_REC_watchdog_device where device_network="IPMAN" and device_name like "GDFOS-MS-IPMAN-B%" limit 5`
	cmd := `select device_name, device_ip, device_network, login_port, login_user, login_password, device_manufactor_id from MR_REC_watchdog_device where device_name = 'GDFOS-MS-IPMAN-BNG01-BJ-AL'`
	go getTargetDevice(deviceChan, cmd)
	for i := 0; i < chanNum; i++ {
		go getCFG(deviceChan, walkExitChan, "./cfg")
	}
	func() {
		for i := 0; i < chanNum; i++ {
			<-walkExitChan
		}
		close(walkExitChan)
	}()

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
}
