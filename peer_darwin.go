//go:build darwin

package main

import (
	"fmt"
	"time"

	"tinygo.org/x/bluetooth"
)

func sendPeerData(p *peerSession, data []byte) error {
	if p.centralChar == nil {
		return fmt.Errorf("no active central chat channel")
	}
	_, err := p.centralChar.WriteWithoutResponse(data)
	return err
}

func runPeer() error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return err
	}

	fmt.Println("macOS: TinyGo BLE is Central-only. Running scan/connect peer mode.")
	fmt.Println("Note: macOS-to-macOS will not connect with this backend because neither side can advertise.")

	for {
		result, found, err := scanForPeerDarwin(adapter, 3*time.Second)
		if err != nil {
			fmt.Printf("Scan error: %v\n", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if !found {
			fmt.Println("No peer found yet, rescanning...")
			continue
		}

		fmt.Printf("Peer found: %s\n", result.Address.String())
		device, err := adapter.Connect(result.Address, bluetooth.ConnectionParams{})
		if err != nil {
			fmt.Printf("Connect failed: %v\n", err)
			continue
		}

		remoteChar, err := discoverChatCharacteristicDarwin(device)
		if err != nil {
			_ = device.Disconnect()
			fmt.Printf("Discovery failed: %v\n", err)
			continue
		}

		if err := remoteChar.EnableNotifications(func(value []byte) {
			msg := make([]byte, len(value))
			copy(msg, value)
			fmt.Printf("\n[Peer]: %s\nYou: ", string(msg))
		}); err != nil {
			_ = device.Disconnect()
			fmt.Printf("Notification setup failed: %v\n", err)
			continue
		}

		session := &peerSession{role: "Central", centralChar: &remoteChar, centralDevice: &device}
		defer session.Close()
		return chatLoop(session)
	}
}

func scanForPeerDarwin(adapter *bluetooth.Adapter, d time.Duration) (bluetooth.ScanResult, bool, error) {
	foundCh := make(chan bluetooth.ScanResult, 1)
	scanDone := make(chan error, 1)

	go func() {
		scanDone <- adapter.Scan(func(a *bluetooth.Adapter, result bluetooth.ScanResult) {
			if result.LocalName() != "ChatPeer" && !result.HasServiceUUID(chatServiceUUID) {
				return
			}
			select {
			case foundCh <- result:
			default:
			}
			_ = a.StopScan()
		})
	}()

	timer := time.AfterFunc(d, func() {
		_ = adapter.StopScan()
	})
	scanErr := <-scanDone
	timer.Stop()

	select {
	case result := <-foundCh:
		return result, true, nil
	default:
		return bluetooth.ScanResult{}, false, scanErr
	}
}

func discoverChatCharacteristicDarwin(device bluetooth.Device) (bluetooth.DeviceCharacteristic, error) {
	services, err := device.DiscoverServices([]bluetooth.UUID{chatServiceUUID})
	if err != nil {
		return bluetooth.DeviceCharacteristic{}, err
	}
	if len(services) == 0 {
		return bluetooth.DeviceCharacteristic{}, fmt.Errorf("chat service not found")
	}

	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{chatCharUUID})
	if err != nil {
		return bluetooth.DeviceCharacteristic{}, err
	}
	if len(chars) == 0 {
		return bluetooth.DeviceCharacteristic{}, fmt.Errorf("chat characteristic not found")
	}

	return chars[0], nil
}
