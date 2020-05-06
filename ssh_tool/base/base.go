package sshtool

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

// Server 登录信息
type Server struct {
	user        string
	password    string
	connectType string
	ip          string
	port        uint
	Client      *ssh.Client
}

// NewServer Server构造函数
func NewServer(user, password, connectType, ip string, port uint) *Server {
	return &Server{
		user:        user,
		password:    password,
		connectType: connectType,
		ip:          ip,
		port:        port,
	}
}

// Connect2Server 连接Server
func (s *Server) Connect2Server() (err error) {
	config := &ssh.ClientConfig{
		User: s.user,
		Auth: []ssh.AuthMethod{
			ssh.Password(s.password),
		},
		Config: ssh.Config{
			Ciphers: []string{"aes128-ctr", "aes192-ctr", "aes256-ctr", "aes128-gcm@openssh.com",
				"arcfour256", "arcfour128", "aes128-cbc", "aes256-cbc", "3des-cbc", "des-cbc",
			},
		},
		Timeout:         10 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		// HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		// 	return nil
		// },
	}
	s.Client, err = ssh.Dial(s.connectType, fmt.Sprintf("%s:%d", s.ip, s.port), config)
	return
}

func (s *Server) GetCombineOutput(cmd string) (result string, err error) {
	session, err := s.Client.NewSession()
	if err != nil {
		return
	}
	defer session.Close()

	byteRet, _ := session.CombinedOutput(cmd)
	result = string(byteRet)
	return
}

func (s *Server) GetShellOutput(cmdList []string, endCmd string, stdPipeSize int) (string, error) {
	result := ""
	session, err := s.Client.NewSession()
	if err != nil {
		return result, err
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}
	// 注意终端模式
	if err := session.RequestPty("VT100", 24, 80, modes); err != nil {
		return result, err
	}
	// 输入管道
	stdinBuf, err := session.StdinPipe()
	if err != nil {
		return result, err
	}
	defer stdinBuf.Close()
	// 输出管道
	stdoutBuf, err := session.StdoutPipe()
	if err != nil {
		return result, err
	}
	if err = session.Shell(); err != nil {
		return result, err
	}
	for _, cmd := range append(cmdList, endCmd) {
		stdinBuf.Write([]byte(cmd + "\n"))
	}
	for {
		tmp := make([]byte, stdPipeSize) // 每次读取管道的大小
		n, err := stdoutBuf.Read(tmp)
		if err == io.EOF {
			result += string(tmp[:n])
			break
		}
		if err != nil {
			fmt.Println(err)
			return result, err
		}
		result += string(tmp[:n])
	}
	session.Wait()
	return result, err
}

func (s *Server) GetOutputToFile(cmdList []string, endCmd string, filePath string) error {
	session, err := s.Client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}
	// 注意终端模式
	if err := session.RequestPty("dumb", 24, 80, modes); err != nil {
		return err
	}
	stdinBuf, err := session.StdinPipe()
	if err != nil {
		return err
	}
	defer stdinBuf.Close()
	// 创建文件句柄传给interface Stdout
	fileObj, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer fileObj.Close()
	session.Stdout = fileObj
	// var errbt bytes.Buffer
	// session.Stderr = &errbt
	if err = session.Shell(); err != nil {
		return err
	}
	for _, cmd := range cmdList {
		cmd = cmd + "\n"
		stdinBuf.Write([]byte(cmd))
	}
	stdinBuf.Write([]byte(endCmd + "\n")) // 使用命令退出终端
	session.Wait()
	return nil
}
