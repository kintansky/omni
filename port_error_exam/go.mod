module port_error_exam

go 1.13

replace (
	database_tool => ../database_tool
	go_snmp => ../go_snmp
	logger => ../logger
)

require (
	database_tool v0.0.0-00010101000000-000000000000
	github.com/go-sql-driver/mysql v1.4.0
	github.com/sirupsen/logrus v1.4.2
	go_snmp v0.0.0-00010101000000-000000000000
	logger v0.0.0-00010101000000-000000000000
)
