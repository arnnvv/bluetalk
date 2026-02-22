//go:build !darwin

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

	"tinygo.org/x/bluetooth"
)

func runHost() error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return err
	}

	fmt.Println("Starting Host mode...")

	var clients sync.Map // key: bluetooth.Address, val: struct{}
	adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
		addr := device.Address
		if connected {
			clients.Store(addr, struct{}{})
			fmt.Printf("\n[+] Client connected: %s\nHost: ", addr.String())
			return
		}
		clients.Delete(addr)
		fmt.Printf("\n[-] Client disconnected: %s\nHost: ", addr.String())
	})

	var charHandle bluetooth.Characteristic
	if err := adapter.AddService(&bluetooth.Service{
		UUID: chatServiceUUID,
		Characteristics: []bluetooth.CharacteristicConfig{
			{
				Handle: &charHandle,
				UUID:   chatCharUUID,
				Flags: bluetooth.CharacteristicWritePermission |
					bluetooth.CharacteristicWriteWithoutResponsePermission |
					bluetooth.CharacteristicNotifyPermission,
				WriteEvent: func(client bluetooth.Connection, offset int, value []byte) {
					msg := make([]byte, len(value))
					copy(msg, value)
					fmt.Printf("\n[Client]: %s\nHost: ", string(msg))
					if _, err := charHandle.Write(msg); err != nil {
						fmt.Printf("\n[!] Broadcast failed: %v\nHost: ", err)
					}
				},
			},
		},
	}); err != nil {
		return err
	}

	adv := adapter.DefaultAdvertisement()
	if err := adv.Configure(bluetooth.AdvertisementOptions{
		LocalName:    "ChatHost",
		ServiceUUIDs: []bluetooth.UUID{chatServiceUUID},
	}); err != nil {
		return err
	}
	if err := adv.Start(); err != nil {
		return err
	}

	fmt.Println("Advertising as ChatHost. Waiting for clients...")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("Host: ")
		if !scanner.Scan() {
			return scanner.Err()
		}

		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}

		msg := []byte("Host: " + text)
		if _, err := charHandle.Write(msg); err != nil {
			fmt.Printf("[!] Send failed: %v\n", err)
		}
	}
}
