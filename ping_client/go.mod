module ping_client

go 1.13

replace (
	database_tool => ../database_tool
	logger => ../logger
	ssh_tool => ../ssh_tool
)

require (
	database_tool v0.0.0-00010101000000-000000000000
	github.com/go-sql-driver/mysql v1.4.0
	golang.org/x/crypto v0.0.0-20200429183012-4b2356b1ed79 // indirect
	logger v0.0.0-00010101000000-000000000000
	ssh_tool v0.0.0-00010101000000-000000000000
)
