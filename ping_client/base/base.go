package pingbase

import (
	"fmt"
	"regexp"
	"strconv"

	omniLOG "logger"
)

// Logger 日志
var Logger = omniLOG.NewOmniLog("./logFile.log", "warn")

// CmdPrefixMap ping测试的命令前序
var CmdPrefixMap map[string]string

// EndCmdMap 设备的退出命令
var EndCmdMap map[string]string

// BatchPing 分组批量ping测试
func BatchPing(targetIPSlice []string, cmdPrefix string, endCmd string, batchSize int, getOutputFunc func([]string, string, int) (string, error), parsefunc func(string) [][]interface{}) ([][]interface{}, error) {
	result := make([][]interface{}, 0)
	round := len(targetIPSlice) / batchSize
	if len(targetIPSlice)%batchSize > 0 {
		round++
	}
	for i := 0; i < round; i++ {
		var targets []string
		pingCmds := make([]string, 0)
		if len(targetIPSlice) < batchSize*(i+1) {
			targets = targetIPSlice[batchSize*i : len(targetIPSlice)]
		} else {
			targets = targetIPSlice[batchSize*i : batchSize*(i+1)]
		}
		for _, ip := range targets {
			pingCmds = append(pingCmds, cmdPrefix+" "+ip)
		}
		ret, err := getOutputFunc(pingCmds, endCmd, 300) // 注意退出命令需要按厂商区分，管道读取大小可适当放大
		if err != nil {
			return result, err
		}
		groupResult := parsefunc(ret)
		result = append(result, groupResult...)
	}
	return result, nil
}

// PingParseExpMap ping数据解析的正则表达式和关键数据位置字典[exp, ipPos, lossPos, costPos]
var PingParseExpMap = map[string][]interface{}{
	"huawei":      []interface{}{`(\d+\.\d+\.\d+\.\d+)\s+ping\s+statistics\s+.*?[\s\S]*?(\d+\.*\d*)%\s+packet\s+loss\s+(round-trip\s+min/avg/max\s+=\s+\d+/(\d+)/\d+\s+ms)*`, 1, 2, 4},
	"alcatel_aos": []interface{}{`(\d+\.\d+\.\d+\.\d+)\s+PING\s+Statistics\s+.*?[\s\S]*?(\d+\.*\d*)%\s+packet\s+loss\s+(round-trip\s+.*?avg\s+=\s+(\d+\.\d*)ms)*`, 1, 2, 4},
	"linux":       []interface{}{`(\d+\.\d+\.\d+\.\d+)\s+ping\s+statistics\s+.*?[\s\S]*?(\d+\.*\d*)%\s+packet\s+loss(.*?\s+.*?avg.*?=.*?/(.*?)/)*`, 1, 2, 4},
}

// ParsePingResultRaw 对返回的ping数据进行解析，如果有多条数据返回多条[ip, loss, cost]
func ParsePingResultRaw(pingResult string, exp string, ipPos int, lossPos int, costPos int) [][]interface{} {
	parseResult := make([][]interface{}, 0)
	re := regexp.MustCompile(exp)
	if re == nil {
		Logger.LogError("parsePingResult", "compile reg", "", "", fmt.Errorf("compile reg error"))
		return parseResult
	}
	ret := re.FindAllStringSubmatch(pingResult, -1)
	for _, r := range ret {
		lossAndCost, err := getLossAndCost(r[lossPos], r[costPos])
		if err != nil {
			return parseResult
		}
		parseResult = append(parseResult, []interface{}{r[ipPos], lossAndCost[0], lossAndCost[1]})
	}
	return parseResult
}

func getLossAndCost(lossStr string, costStr string) (ret [2]float64, err error) {
	loss, err := strconv.ParseFloat(lossStr, 64)
	if err != nil {
		Logger.LogError("parsePingResultAL", "parse loss", "loss_str", lossStr, err)
		return
	}
	var cost float64 = -1
	if costStr != "" {
		cost, err = strconv.ParseFloat(costStr, 64)
		if err != nil && loss != 100 {
			Logger.LogError("parsePingResultAL", "parse cost", "cost_str", costStr, err)
			return
		}
	}
	ret = [2]float64{loss, cost}
	return
}

// Target 传递的目标接口，注意，因为是在不同包内实现，需留意方法的公开性
type Target interface {
	LoginDevice() error
	CloseSSHConnection()
	GetDeviceName() string
	GetManufactor() string
	Ping(string, string, int) [][]interface{}
	// ParsePingResultMulti(string) [][]interface{}
}

// PingAndParse 登陆发起Ping并解析返回数据
func PingAndParse(pingTargetChan <-chan Target, targetResultChan chan<- map[string][][]interface{}, groupMenCnt int, exitPingChan chan<- bool) {
	for t := range pingTargetChan {
		Logger.LogInfo("PingAndParse", "begin", "device", t.GetDeviceName(), "none")
		// fmt.Println(t)
		if err := t.LoginDevice(); err != nil {
			Logger.LogError("pingAndParse", "failed to login", "deviceName", t.GetDeviceName(), err)
			exitPingChan <- true
			return
		}
		defer t.CloseSSHConnection()

		targetResult := make(map[string][][]interface{}, 1)
		result := t.Ping(CmdPrefixMap[t.GetManufactor()], EndCmdMap[t.GetManufactor()], groupMenCnt) // 分组大小视情况而定
		targetResult[t.GetDeviceName()] = result
		targetResultChan <- targetResult
	}
	exitPingChan <- true
}
