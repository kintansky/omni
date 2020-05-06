package snmp

import (
	"fmt"

	"github.com/k-sone/snmpgo"
)

func selectSnmpVer(i int) snmpgo.SNMPVersion {
	switch i {
	case 1:
		return snmpgo.V1
	case 2:
		return snmpgo.V2c
	case 3:
		return snmpgo.V3
	default:
		panic("unknow snmp version")
	}
}

// ServerInfo 服务器信息，构建SNMP目标对象
type ServerInfo struct {
	Address, Community string
	Port, SnmpVer      int
}

// NewServerInfo ServerInfo的构造函数
func NewServerInfo(address, community string, port, snmpVer int) *ServerInfo {
	return &ServerInfo{
		Address:   address,
		Port:      port,
		SnmpVer:   snmpVer,
		Community: community,
	}
}

// InitSnmp 初始化SNMP
func InitSnmp(serverInfoPtr *ServerInfo) (*snmpgo.SNMP, error) {
	return snmpgo.NewSNMP(snmpgo.SNMPArguments{
		Version:   selectSnmpVer(serverInfoPtr.SnmpVer),
		Address:   fmt.Sprintf("%s:%d", serverInfoPtr.Address, serverInfoPtr.Port),
		Retries:   1,
		Community: serverInfoPtr.Community,
	})
}

// InitOids 初始化OIDS
func InitOids(oidStr *[]string) (snmpgo.Oids, error) {
	return snmpgo.NewOids(*oidStr)
}
