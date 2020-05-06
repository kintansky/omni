package main

import (
	"fmt"

	ssh "github.com/shenbowei/switch-ssh-go"
)

func main() {
	user := "tjx"
	password := "tanjianxiong"
	ipPort := "192.168.0.195:22"

	// //get the switch brand(vendor), include h3c,huawei and cisco
	// brand, err := ssh.GetSSHBrand(user, password, ipPort)
	// if err != nil {
	// 	fmt.Println("GetSSHBrand err:\n", err.Error())
	// }
	// fmt.Println("Device brand is:\n", brand)

	//run the cmds in the switch, and get the execution results
	cmds := make([]string, 0)
	cmds = append(cmds, "pwd")
	cmds = append(cmds, "ls")
	result, err := ssh.RunCommands(user, password, ipPort, cmds...)
	if err != nil {
		fmt.Println("RunCommands err:\n", err.Error())
	}
	fmt.Println("RunCommands result:\n", result)

}
