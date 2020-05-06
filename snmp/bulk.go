package snmp

import (
	"fmt"

	"github.com/k-sone/snmpgo"
)

// MakeSnmpBulkWalk SNMPBulkWalk主函数, Bulk会将提供OID节点下的所有子节点全部遍历
func MakeSnmpBulkWalk(serverInfoPtr *ServerInfo, oidStr *[]string) (pdu snmpgo.Pdu, err error) {
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

	return snmp.GetBulkWalk(oids, 0, 1)

}
