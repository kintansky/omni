package databasetool

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// DB 全局变量
var DB *sqlx.DB

// dbServer 数据库登陆信息
type dbServer struct {
	dbName  string
	user    string
	pwd     string
	cnxType string
	address string
	port    int
}

// NewdbServer 构造函数
func newdbServer(dbName, user, pwd, cnxType, address string, port int) *dbServer {
	return &dbServer{
		dbName:  dbName,
		user:    user,
		pwd:     pwd,
		cnxType: cnxType,
		address: address,
		port:    port,
	}
}

// CreateDSN 根据dbServer构建DSN传给DB
func (d *dbServer) createDSN() *string {
	dsn := fmt.Sprintf("%s:%s@%s(%s:%d)/%s", d.user, d.pwd, d.cnxType, d.address, d.port, d.dbName)
	return &dsn
}

// InitDB 初始化DB连接, dbType: mysql, sqlite...
func InitDB(dbType, dbName, user, pwd, cnxType, address string, port int) (err error) {
	serverInfo := newdbServer(dbName, user, pwd, cnxType, address, port)
	dsn := serverInfo.createDSN()
	// dsn := "root:password@tcp(127.0.0.1:3306)/omni_agent"
	DB, err = sqlx.Connect(dbType, *dsn) // OPEN不会校验DSN的参数，校验数据在ping进行
	if err != nil {
		fmt.Printf("DB Open Failed, %v\n", err)
		return
	}
	DB.SetMaxOpenConns(20) // 设置连接池最大连接数，0为不限制
	DB.SetMaxIdleConns(10) // 限制可保持的最大空闲连接数，0为不保存空闲连接
	fmt.Println("DB Connect Success. ")
	return
}
