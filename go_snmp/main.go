package main

import (
	"fmt"
	snmpBase "go_snmp/base"
)

func main() {
	snmp := snmpBase.InitSnmp("192.168.0.192", 161, "udp", "public", 2, 3, 3)
	if err := snmp.Connect(); err != nil {
		fmt.Println("SNMP Connect failed")
		return
	}
	defer snmp.Conn.Close()

	// if err := snmp.BulkWalk("1.3.6.1.2.1.1", snmpBase.PrintValue); err != nil {
	// 	fmt.Printf("Walk Error: %v\n", err)
	// 	return
	// }
	ret, err := snmp.BulkWalkAll("1.3.6.1.2.1.1")
	if err != nil {
		fmt.Printf("Walk Error: %v\n", err)
		return
	}
	for _, pdu := range ret {
		snmpBase.PrintValue(pdu)
	}

}
