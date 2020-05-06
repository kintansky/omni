package main

import (
	"bytes"
	"fmt"
	"net"

	"golang.org/x/crypto/ssh"
)

var hostKey ssh.PublicKey

func main() {
	cfg := &ssh.ClientConfig{
		User: "tjx",
		Auth: []ssh.AuthMethod{
			ssh.Password("tanjianxiong"),
		},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
		// Config: ssh.Config{
		// 	Ciphers: []string{"aes128-cbc"},
		// },
	}
	client, err := ssh.Dial("tcp", "192.168.0.195:22", cfg)
	if err != nil {
		fmt.Println("dial error", err)
		return
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		fmt.Println("create new session error")
		return
	}
	defer sess.Close()

	var b bytes.Buffer
	sess.Stdout = &b
	if err := sess.Run("pwd"); err != nil {
		fmt.Println(err)
	}
	fmt.Println(b.String())

}
