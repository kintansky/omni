module production_tool

go 1.13

replace snmp => ../snmp

require (
	github.com/k-sone/snmpgo v3.2.0+incompatible
	snmp v0.0.0-00010101000000-000000000000
)
