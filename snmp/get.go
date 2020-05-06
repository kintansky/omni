package snmp

import (
	"fmt"

	"github.com/k-sone/snmpgo"
)

// MakeSnmpGet GET主函数，只能取出一个OID的值
func MakeSnmpGet(serverInfoPtr *ServerInfo, oidStr *[]string) {
	snmp, err := InitSnmp(serverInfoPtr)
	if err != nil {
		fmt.Println("Connect Server Failed. ", err)
		return
	}

	oids, err := InitOids(oidStr)
	if err != nil {
		fmt.Println("OID Parsed Failed. ", err)
		return
	}
	if err = snmp.Open(); err != nil {
		fmt.Println("Open Failed. ", err)
		return
	}
	defer snmp.Close() // 关闭连接

	pdu, err := snmp.GetRequest(oids)
	if err != nil {
		// Failed to request
		fmt.Println(err)
		return
	}
	if pdu.ErrorStatus() != snmpgo.NoError {
		// Received an error from the agent
		fmt.Println(pdu.ErrorStatus(), pdu.ErrorIndex())
	}

	// 返回[]*Varbind切片
	// fmt.Printf("%T, %v\n", pdu.VarBinds(), pdu.VarBinds())
	// VarBinds 嵌套的Variable是一个interface的结构体，而且是非匿名嵌套
	for _, val := range pdu.VarBinds() {
		fmt.Printf("OID:%s Type:%s Value:%s\n", val.Oid, val.Variable.Type(), val.Variable)
	}
}
