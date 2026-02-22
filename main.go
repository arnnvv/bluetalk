package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	fmt.Println("--- BlueTalk: Robust P2P Chat ---")
	fmt.Println("State: Initializing BLE stack...")

	sendChan := make(chan string, 32)
	recvChan := make(chan string, 32)
	statusChan := make(chan string, 32)

	peer := NewPeer(sendChan, recvChan, statusChan)
	go peer.Run()

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("You: ")
			if !scanner.Scan() {
				return
			}
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}
			sendChan <- text
		}
	}()

	for {
		select {
		case msg := <-recvChan:
			fmt.Printf("\r\033[K[Peer]: %s\n", msg)
		case status := <-statusChan:
			fmt.Printf("\r\033[K[System]: %s\n", status)
		}
	}
}
