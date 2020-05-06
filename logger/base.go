package logger

import (
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

//logLevel 字典，用于确定log level
var logLevel = map[string]logrus.Level{
	"info":  logrus.InfoLevel,
	"warn":  logrus.WarnLevel,
	"error": logrus.ErrorLevel,
	"fatal": logrus.FatalLevel,
	"panic": logrus.PanicLevel,
}

// OmniLog logrus的再封装
type OmniLog struct {
	log *logrus.Logger
}

// NewOmniLog 构造函数
func NewOmniLog(filePath string, level string) *OmniLog {
	var log = logrus.New()
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.Out = file
	} else {
		log.Infoln("Failed to log to file, use default stderr")
		log.Out = os.Stdout
	}
	log.SetLevel(logLevel[level])
	return &OmniLog{log: log}
}

func (o *OmniLog) createLogEntry(event string, topic string, key interface{}, data interface{}) *logrus.Entry {
	return o.log.WithFields(logrus.Fields{
		"event": event,
		"topic": topic,
		"key":   key,
		"data":  data,
	})
}

// LogInfo InfoLevel Log
func (o *OmniLog) LogInfo(event string, topic string, key interface{}, data interface{}, info string) {
	logEntry := o.createLogEntry(event, topic, key, data)
	logEntry.Info(info)
	fmt.Printf("%s [%s] MSG: %#v DATA: %v\n", time.Now().Format("2006-01-02 15:04:05"), "INFO", info, logEntry.Data)
}

// LogWarn WarnLevel Log
func (o *OmniLog) LogWarn(event string, topic string, key interface{}, data interface{}, info string) {
	logEntry := o.createLogEntry(event, topic, key, data)
	logEntry.Warn(info)
	fmt.Printf("%s [%s] MSG: %#v DATA: %v\n", time.Now().Format("2006-01-02 15:04:05"), "WARNING", info, logEntry.Data)
}

// LogError ErrorLevel Log
func (o *OmniLog) LogError(event string, topic string, key interface{}, data interface{}, err error) {
	logEntry := o.createLogEntry(event, topic, key, data)
	logEntry.Error(err)
	fmt.Printf("%s [%s] MSG: %#v DATA: %v\n", time.Now().Format("2006-01-02 15:04:05"), "ERROR", err, logEntry.Data)
}

// LogFatal FatalLevel Log
func (o *OmniLog) LogFatal(event string, topic string, key interface{}, data interface{}, err error) {
	logEntry := o.createLogEntry(event, topic, key, data)
	logEntry.Fatal(err)
	fmt.Printf("%s [%s] MSG: %#v DATA: %v\n", time.Now().Format("2006-01-02 15:04:05"), "FATAL", err, logEntry.Data)
}

// LogPanic PanicLevel Log
func (o *OmniLog) LogPanic(event string, topic string, key interface{}, data interface{}, err error) {
	logEntry := o.createLogEntry(event, topic, key, data)
	logEntry.Panic(err)
	fmt.Printf("%s [%s] MSG: %#v DATA: %v\n", time.Now().Format("2006-01-02 15:04:05"), "PANIC", err, logEntry.Data)
}
