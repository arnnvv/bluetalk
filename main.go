package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"tinygo.org/x/bluetooth"
)

var (
	chatServiceUUID = bluetooth.NewUUID([16]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef})
	chatCharUUID    = bluetooth.NewUUID([16]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xee})
)

func main() {
	fmt.Println("--- Bluetooth Chat CLI (Auto Peer Mode) ---")
	if err := runPeer(); err != nil {
		fmt.Println("Peer error:", err)
	}
}

type peerSession struct {
	role           string
	centralChar    *bluetooth.DeviceCharacteristic
	centralDevice  *bluetooth.Device
	peripheralChar *bluetooth.Characteristic
}

func (p *peerSession) Send(data []byte) error {
	return sendPeerData(p, data)
}

func (p *peerSession) Close() {
	if p.centralDevice != nil {
		_ = p.centralDevice.Disconnect()
	}
}

func chatLoop(session *peerSession) error {
	fmt.Printf("Connected (role: %s). Type messages and press Enter.\n", session.role)
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			return scanner.Err()
		}

		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if err := session.Send([]byte(text)); err != nil {
			return err
		}
	}
}
