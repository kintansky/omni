package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	omniSNMP "snmp"
	"strings"

	"github.com/k-sone/snmpgo"
)

func getServerInput() *omniSNMP.ServerInfo {
	var (
		address, community string
		port, snmpVer      int
	)
	addBool := false
	for !addBool {
		fmt.Print("# 请输入设备地址：")
		fmt.Scanf("%s\n", &address)
		re := regexp.MustCompile(`^(\d+\.){3}\d+$`)
		addBool = re.MatchString(address)
		if !addBool {
			fmt.Println("设备地址有误")
		}
	}
	fmt.Print("# 请输入端口，默认161，回车跳过：")
	fmt.Scanf("%d\n", &port)
	if port == 0 {
		port = 161
	}
	verBool := true
	for verBool {
		fmt.Print("# 请输入SNMP版本，默认V2c，可选2, 3，回车跳过：")
		fmt.Scanf("%d\n", &snmpVer)
		switch {
		case snmpVer == 0:
			snmpVer = 2
			verBool = false
		case snmpVer > 1 && snmpVer < 4:
			verBool = false
		default:
			fmt.Println("无效版本，可选版本1, 2, 3")
			verBool = true
		}
	}
	for {
		fmt.Print("# 请输入SNMP Community: ")
		fmt.Scanf("%s\n", &community)
		if community != "" {
			break
		} else {
			fmt.Println("Community不能为空")
		}
	}
	return omniSNMP.NewServerInfo(address, community, port, snmpVer)
}

func appendOids() *[]string {
	var oidStr []string
	fmt.Println("# 请输入OIDS，多个OID以英文','分隔：")
	cmdReader := bufio.NewReader(os.Stdin) // 不能使用 Scanf,不然会遇到空白符就自动断行
	cmdStr, err := cmdReader.ReadString('\n')
	if err != nil {
		fmt.Println("oid input error ")
	}
	cmdStr = strings.Trim(cmdStr, "\n")
	cmdStr = strings.Trim(cmdStr, "\r")
	oidStr = strings.Split(cmdStr, ",")
	for i, v := range oidStr {
		oidStr[i] = strings.Trim(v, " ")
	}
	return &oidStr
}

func menu() *int {
	var mode int
	fmt.Printf(`=======================================
# 请选择功能
0.退出
1.Single模式(OID下无子节点)
2.Bulk模式(OID含子节点)
3.更改目标服务器
=======================================
Select:`)
	fmt.Scanf("%d\n", &mode)
	return &mode
}

// getInfoReady 会把serverInfo 和OID都一起初始化
func getInfoReady() (*omniSNMP.ServerInfo, *[]string) {
	serverInfo := getServerInput()
	fmt.Println("================Server=================")
	fmt.Printf(
		"服务器信息如下：IP:%s:%d\tSNMP Ver.%d\tSNMP COMMUNITY:%s\n",
		serverInfo.Address, serverInfo.Port, serverInfo.SnmpVer, serverInfo.Community,
	)
	fmt.Println("================OID====================")
	oidStr := appendOids()
	fmt.Println("请求的OID信息如下：")
	for _, v := range *oidStr {
		fmt.Println(v)
	}
	fmt.Println("=======================================")
	return serverInfo, oidStr
}

func main() {
	fmt.Println(`=======================================
=============SNMP TEST TOOL============
=================================KEN===`)
	serverInfo := getServerInput() // 外部初始化作为公用信息，避免每次循环都要填一次信息
	fmt.Println("============NEW Server=================")
	fmt.Printf(
		"服务器信息如下：IP:%s:%d\tSNMP Ver.%d\tSNMP COMMUNITY:%s\n",
		serverInfo.Address, serverInfo.Port, serverInfo.SnmpVer, serverInfo.Community,
	)
	mode := menu()
	for {
		switch *mode {
		case 0:
			os.Exit(0)
		case 1:
			oidStr := appendOids() // 只需填写OID，server信息不再每次更新
			omniSNMP.MakeSnmpGet(serverInfo, oidStr)
		case 2:
			oidStr := appendOids()
			pdu, err := omniSNMP.MakeSnmpBulkWalk(serverInfo, oidStr)
			if err != nil {
				fmt.Println("Walk Failed. ", err)
				return
			}

			if pdu.ErrorStatus() != snmpgo.NoError {
				fmt.Println(pdu.ErrorStatus(), pdu.ErrorIndex())
				return
			}

			for _, val := range pdu.VarBinds() {
				fmt.Printf("OID:%s Type:%s Value:%s, %T\n", val.Oid, val.Variable.Type(), val.Variable, val.Variable)
			}
		case 3:
			fmt.Println("# 请输入新的服务器信息")
			serverInfo = getServerInput()
			fmt.Println("============NEW Server=================")
			fmt.Printf(
				"服务器信息如下：IP:%s:%d\tSNMP Ver.%d\tSNMP COMMUNITY:%s\n",
				serverInfo.Address, serverInfo.Port, serverInfo.SnmpVer, serverInfo.Community,
			)
		default:
			fmt.Println("=======================================")
			fmt.Println("INFO: 无效参数")
		}
		mode = menu()
	}

}
