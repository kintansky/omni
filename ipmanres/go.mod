module ipmanres

go 1.14

replace (
	database_tool => ../database_tool
	go_snmp => ../go_snmp
	logger => ../logger
)

require (
	database_tool v0.0.0-00010101000000-000000000000
	github.com/go-sql-driver/mysql v1.5.0
	go_snmp v0.0.0-00010101000000-000000000000
	logger v0.0.0-00010101000000-000000000000
)
