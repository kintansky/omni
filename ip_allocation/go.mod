module ip_allocation

go 1.13

replace database_tool => ../database_tool

require (
	database_tool v0.0.0-00010101000000-000000000000
	github.com/go-sql-driver/mysql v1.5.0
)
