package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"

	_ "github.com/go-sql-driver/mysql"

	omniDB "database_tool"
)

var (
	wg sync.WaitGroup
)

// 返回第一行是title
func produceData(producer chan<- string, titleChan chan<- []string) {
	fileObj, err := os.Open(`./test_data_from_local.csv`)
	if err != nil {
		fmt.Printf("pen failed, %v\n", err)
		return
	}
	defer fileObj.Close() // 关闭文件

	reader := bufio.NewReader(fileObj)
	line, _ := reader.ReadString('\n')                           // 第一行是title
	titleChan <- strings.Split(strings.TrimSpace(line), "\",\"") // 注意空字符
	close(titleChan)
	for {
		line, err = reader.ReadString('\n') // 注意传参是byte
		if err == io.EOF {
			// fmt.Println(line) // 最后一行数据
			producer <- strings.TrimSpace(line)
			break
		}
		if err != nil {
			fmt.Printf("Read line from buf failed, %v\n", err)
			break
		}
		producer <- strings.TrimSpace(line) // 数据
	}
	close(producer) // 记得关闭channel，不然deadlock
}

func consumeData(producer <-chan string, titleChan <-chan []string, x interface{}) {
	title := <-titleChan
	title = title[1:]
	// 去掉没用字符
	// for index := range title {
	// 	title[index] = strings.Trim(title[index], "\"")
	// }
	title[0] = strings.Trim(title[0], "\"")                       // 去掉没用字符
	title[len(title)-1] = strings.Trim(title[len(title)-1], "\"") // 去掉没用字符
	// fmt.Printf("%#v\n", title)
	sqlStr := fmt.Sprintf("insert into MR_REC_ip_allocation (%s) values (%s?)", strings.Join(title, ","), strings.Repeat("?,", len(title)-1))
	stmt, err := omniDB.DB.Preparex(sqlStr)
	if err != nil {
		fmt.Printf("Prepare failed,  %v\n", err)
		wg.Done()
		return
	}
	defer stmt.Close()

	// // 遍历chan，数据反射成struct
	// for line := range producer {
	// 	data := strings.Split(strings.TrimSpace(line), ",")[1:]
	// 	// fmt.Println(data)
	// 	err := parseField(x, data, title)
	// 	if err != nil {
	// 		fmt.Println(line)
	// 		fmt.Printf("load failed, %v\n", err)
	// 		continue
	// 	}
	// 	// fmt.Printf("%#v\n", x)
	// }
	i := 0
	for line := range producer {
		fmt.Printf("\rLine %d inserting...", i)
		data := strings.Split(line, "\",\"")[1:]
		if len(data) == 0 { // 空行跳过
			continue
		}
		if len(data) != len(title) {
			fmt.Printf("Not inserted data: %#v\n", data)
			continue
		}

		vals := make([]interface{}, len(data)) // 转换成[]interface{}
		for i, v := range data {
			vals[i] = strings.Trim(v, "\"") // 数据有"引注，需要去掉
		}
		// fmt.Println(vals)
		_, err = stmt.Exec(vals...)
		if err != nil {
			fmt.Printf("Execute Failed, %v\n", err)
			break
		}
		i++
	}
	fmt.Println("\nCmoplete")
	func() {
		var temp string
		fmt.Println("回车退出")
		fmt.Scanf("%s\n", &temp)
	}()
	wg.Done()
}

func parseField(x interface{}, values []string, keys []string) (err error) {
	var dataMap = make(map[string]string, len(keys))
	for i, v := range keys {
		dataMap[strings.Trim(v, "\"")] = strings.Trim(values[i], "\"")
	}
	// fmt.Printf("%#v\n", dataMap)

	v := reflect.ValueOf(x)
	t := v.Type()
	for key, value := range dataMap {
		var fieldName string
		for i := 0; i < v.Elem().NumField(); i++ {
			field := t.Elem().Field(i)
			// fmt.Println(field.Tag.Get("db"), key)
			if field.Tag.Get("db") == key {
				fieldName = field.Name
				break
			}
		}
		// fmt.Println(fieldName)
		// 如果struct没有对应字段，跳过
		if len(fieldName) == 0 {
			continue
		}
		fieldObj := v.Elem().FieldByName(fieldName)
		// 类型判断并对struct设置值
		switch fieldObj.Type().Kind() {
		case reflect.String:
			fieldObj.SetString(value)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			var valueInt int64
			valueInt, err = strconv.ParseInt(value, 10, 64)
			if err != nil {
				err = fmt.Errorf("Field:%s value:%v parse as int failed, type error", key, value)
				return
			}
			fieldObj.SetInt(valueInt)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			var valueUint uint64
			valueUint, err = strconv.ParseUint(value, 10, 64)
			if err != nil {
				err = fmt.Errorf("Field:%s value:%v parse as uint failed, type error", key, value)
				return
			}
			fieldObj.SetUint(valueUint)
		case reflect.Bool:
			var valueBool bool
			valueBool, err = strconv.ParseBool(value)
			if err != nil {
				err = fmt.Errorf("Field:%s value:%v parse as uint failed, type error", key, value)
				return
			}
			fieldObj.SetBool(valueBool)
		}
	}
	// fmt.Printf("%#v\n", x)
	return
}

type IpAllocation struct {
	// ID          int    `db:"id"`
	OrderNum    string `db:"order_num"`
	ClientName  string `db:"client_name"`
	State       string `db:"state"`
	IP          string `db:"ip"`
	Gateway     string `db:"gateway"`
	Bng         string `db:"bng"`
	LogicPort   string `db:"logic_port"`
	Svlan       int    `db:"svlan"`
	Cevlan      int    `db:"cevlan"`
	Description string `db:"description"`
	IPFunc      string `db:"ip_func"`
	Olt         string `db:"olt"`
	ServiceID   int    `db:"service_id"`
	BrandWidth  int    `db:"brand_width"`
	GroupID     uint64 `db:"group_id"`
	ProductID   uint64 `db:"product_id"`
	NetworkType string `db:"network_type"`
	Community   string `db:"community"`
	Rt          string `db:"rt"`
	Rd          string `db:"rd"`
	Comment     string `db:"comment"`
	AlcUser     string `db:"alc_user"`
	AlcTime     string `db:"alc_time"`
	AccessType  string `db:"access_type"`
	LastModTime string `db:"last_mod_time"`
	IPMask      int    `db:"ip_mask"`
}

func main() {
	var user, pwd, address string
	var port int
	fmt.Printf("请输入数据库地址:")
	fmt.Scanf("%s\n", &address)
	fmt.Printf("请输入数据库端口:")
	fmt.Scanf("%d\n", &port)
	fmt.Printf("请输入数据库账号:")
	fmt.Scanf("%s\n", &user)
	fmt.Printf("请输入数据库密码:")
	fmt.Scanf("%s\n", &pwd)
	err := omniDB.InitDB("mysql", "omni_agent", user, pwd, "tcp", address, port)
	if err != nil {
		return
	}

	producer := make(chan string, 1000)
	titleChan := make(chan []string, 1)
	go produceData(producer, titleChan)
	var ia IpAllocation
	wg.Add(1)
	go consumeData(producer, titleChan, &ia)

	wg.Wait()
}
