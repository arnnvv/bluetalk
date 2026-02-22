package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"tinygo.org/x/bluetooth"
)

var (
	chatServiceUUID = bluetooth.NewUUID([16]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef})
	chatCharUUID    = bluetooth.NewUUID([16]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xee})
)

func main() {
	fmt.Println("--- Bluetooth Chat CLI ---")
	fmt.Print("Choose mode Host (H) or Client (C): ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return
	}

	switch strings.ToUpper(strings.TrimSpace(scanner.Text())) {
	case "H":
		if err := runHost(); err != nil {
			fmt.Println("Host error:", err)
		}
	case "C":
		if err := runClient(); err != nil {
			fmt.Println("Client error:", err)
		}
	default:
		fmt.Println("Invalid mode. Use H (Host) or C (Client).")
	}
}

func runClient() error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return err
	}

	fmt.Println("Starting Client mode...")
	fmt.Println("Scanning for ChatHost...")

	var target bluetooth.ScanResult
	var found atomic.Bool
	var timedOut atomic.Bool

	scanDone := make(chan error, 1)
	go func() {
		scanDone <- adapter.Scan(func(a *bluetooth.Adapter, result bluetooth.ScanResult) {
			if found.Load() {
				return
			}
			if result.LocalName() == "ChatHost" || result.HasServiceUUID(chatServiceUUID) {
				target = result
				found.Store(true)
				_ = a.StopScan()
			}
		})
	}()

	timeout := time.AfterFunc(25*time.Second, func() {
		if !found.Load() {
			timedOut.Store(true)
			_ = adapter.StopScan()
		}
	})
	scanErr := <-scanDone
	timeout.Stop()

	if scanErr != nil && !found.Load() {
		return scanErr
	}
	if !found.Load() {
		if timedOut.Load() {
			fmt.Println("Host not found. Is the host running and advertising?")
			return nil
		}
		return fmt.Errorf("scan ended before finding host")
	}

	fmt.Printf("Found host: %s\n", target.Address.String())
	device, err := adapter.Connect(target.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return err
	}
	defer device.Disconnect()

	services, err := device.DiscoverServices([]bluetooth.UUID{chatServiceUUID})
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return fmt.Errorf("chat service not found")
	}

	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{chatCharUUID})
	if err != nil {
		return err
	}
	if len(chars) == 0 {
		return fmt.Errorf("chat characteristic not found")
	}
	char := chars[0]

	if err := char.EnableNotifications(func(value []byte) {
		msg := make([]byte, len(value))
		copy(msg, value)
		fmt.Printf("\n%s\nYou: ", string(msg))
	}); err != nil {
		return err
	}

	fmt.Println("Connected. Type messages and press Enter.")
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
		if _, err := char.WriteWithoutResponse([]byte(text)); err != nil {
			return err
		}
	}
}
