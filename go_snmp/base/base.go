package go_snmp

import (
	"errors"
	"fmt"
	"time"

	"github.com/soniah/gosnmp"
)

func selectSnmpVer(i uint) (v gosnmp.SnmpVersion, err error) {
	switch i {
	case 1:
		v = gosnmp.Version1
	case 2:
		v = gosnmp.Version2c
	case 3:
		v = gosnmp.Version3
	default:
		err = errors.New("Unsupported snmp version")
	}
	return
}

// InitSnmp 初始化SNMP的参数
func InitSnmp(target string, port uint16, transport string, community string, ver uint, t int, retries int) (*gosnmp.GoSNMP, error) {
	v, err := selectSnmpVer(ver)
	if err != nil {
		return nil, err
	}
	g := new(gosnmp.GoSNMP)
	g.Target = target
	g.Port = port
	g.Transport = transport
	g.Community = community
	g.Version = v
	g.Timeout = time.Duration(t) * time.Second
	g.Retries = retries
	g.ExponentialTimeout = true
	g.MaxOids = gosnmp.MaxOids
	return g, err
}

// PrintValue 直接打印所有结果
func PrintValue(pdu gosnmp.SnmpPDU) error {
	fmt.Printf("%s = ", pdu.Name)

	switch pdu.Type {
	case gosnmp.OctetString:
		b := pdu.Value.([]byte)
		fmt.Printf("TYPE %v: %s, %T\n", pdu.Type.String(), string(b), pdu.Value)
	case gosnmp.ObjectDescription, gosnmp.ObjectIdentifier:
		fmt.Printf("TYPE %v: %s, %T\n", pdu.Type.String(), pdu.Value, pdu.Value)
	case gosnmp.Integer, gosnmp.Counter32, gosnmp.Counter64, gosnmp.Gauge32, gosnmp.TimeTicks:
		fmt.Printf("TYPE %v: %v, %T\n", pdu.Type.String(), gosnmp.ToBigInt(pdu.Value), pdu.Value)
	default:
		fmt.Printf("TYPE %v: %v, %T\n", pdu.Type.String(), pdu.Value, pdu.Value)
	}
	return nil
}

// ParseAsKeyVal 会将所有结果转换成string
func ParseAsKeyVal(pdu *gosnmp.SnmpPDU) (key string, val string, err error) {
	defer func() {
		if err := recover(); err != nil {
			return
		}
	}()
	key = pdu.Name
	switch pdu.Type {
	case gosnmp.OctetString:
		b := pdu.Value.([]byte)
		val = string(b)
	default:
		val = fmt.Sprintf("%v", pdu.Value)
	}
	return
}
