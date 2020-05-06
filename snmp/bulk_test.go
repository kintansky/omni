package snmp

import (
	"fmt"
	"testing"
)

func TestMakeSnmpBulkWalk(t *testing.T) {
	serverInfo := NewServerInfo("192.168.1.19", "public", 161, 2)
	pdu, err := MakeSnmpBulkWalk(serverInfo, &[]string{"1.3.6.1.2.1.25.1.1", "1.3.6.1.2.1.1"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(pdu)
}
