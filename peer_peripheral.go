//go:build !darwin

package main

import (
	"fmt"
	"math/rand"
	"time"

	"tinygo.org/x/bluetooth"
)

type connectEvent struct {
	device    bluetooth.Device
	connected bool
}

func sendPeerData(p *peerSession, data []byte) error {
	if p.centralChar != nil {
		_, err := p.centralChar.WriteWithoutResponse(data)
		return err
	}
	if p.peripheralChar != nil {
		_, err := p.peripheralChar.Write(data)
		return err
	}
	return fmt.Errorf("no active chat channel")
}

func runPeer() error {
	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		return err
	}

	events := make(chan connectEvent, 8)
	adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
		select {
		case events <- connectEvent{device: device, connected: connected}:
		default:
		}
	})

	var localChar bluetooth.Characteristic
	if err := adapter.AddService(&bluetooth.Service{
		UUID: chatServiceUUID,
		Characteristics: []bluetooth.CharacteristicConfig{
			{
				Handle: &localChar,
				UUID:   chatCharUUID,
				Flags: bluetooth.CharacteristicWritePermission |
					bluetooth.CharacteristicWriteWithoutResponsePermission |
					bluetooth.CharacteristicNotifyPermission,
				WriteEvent: func(client bluetooth.Connection, offset int, value []byte) {
					msg := make([]byte, len(value))
					copy(msg, value)
					fmt.Printf("\n[Peer]: %s\nYou: ", string(msg))
				},
			},
		},
	}); err != nil {
		return err
	}

	adv := adapter.DefaultAdvertisement()
	if err := adv.Configure(bluetooth.AdvertisementOptions{
		LocalName:    "ChatPeer",
		ServiceUUIDs: []bluetooth.UUID{chatServiceUUID},
	}); err != nil {
		return err
	}

	jitter := rand.New(rand.NewSource(time.Now().UnixNano()))
	fmt.Println("Starting peer discovery (auto role selection)...")

	for {
		advWindow := time.Duration(500+jitter.Intn(1200)) * time.Millisecond
		scanWindow := time.Duration(700+jitter.Intn(1400)) * time.Millisecond

		if err := adv.Start(); err == nil {
			fmt.Println("Phase: advertise")
			if device, ok := waitForIncoming(events, advWindow); ok {
				_ = adv.Stop()
				fmt.Printf("Connected as Peripheral with %s\n", device.Address.String())
				session := &peerSession{role: "Peripheral", peripheralChar: &localChar}
				defer session.Close()
				return chatLoop(session)
			}
			_ = adv.Stop()
		}

		fmt.Println("Phase: scan")
		result, found, err := scanForPeer(adapter, scanWindow)
		if err != nil {
			fmt.Printf("Scan error: %v\n", err)
			continue
		}
		if !found {
			continue
		}

		fmt.Printf("Peer found: %s\n", result.Address.String())
		device, err := adapter.Connect(result.Address, bluetooth.ConnectionParams{})
		if err != nil {
			fmt.Printf("Connect failed: %v\n", err)
			continue
		}

		remoteChar, err := discoverChatCharacteristic(device)
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

func waitForIncoming(events <-chan connectEvent, d time.Duration) (bluetooth.Device, bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()

	for {
		select {
		case evt := <-events:
			if evt.connected {
				return evt.device, true
			}
		case <-timer.C:
			return bluetooth.Device{}, false
		}
	}
}

func scanForPeer(adapter *bluetooth.Adapter, d time.Duration) (bluetooth.ScanResult, bool, error) {
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

func discoverChatCharacteristic(device bluetooth.Device) (bluetooth.DeviceCharacteristic, error) {
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
