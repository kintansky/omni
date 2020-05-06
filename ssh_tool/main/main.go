package main

import (
	"fmt"
	ssh_tool "ssh_tool/base"
)

func main() {
	s := ssh_tool.NewServer("tjx", "tanjianxiong", "tcp", "192.168.1.19", 22)
	if err := s.Connect2Server(); err != nil {
		return
	}
	defer s.Client.Close()
	// cmdList := []string{"cat ~/Downloads/GDFOS-MS-IPMAN-SR02-FSZHL-AL.txt", "pwd"}
	// if err := s.GetOutputToFile(cmdList, "exit", "./cfgs/tjx.txt"); err != nil {
	// 	fmt.Println(err)
	// }
	cmdList := []string{"ll /usr/local", "pwd", "ping -c 5 8.8.8.8"}
	result, err := s.GetShellOutput(cmdList, "exit", 300)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(result)
}
