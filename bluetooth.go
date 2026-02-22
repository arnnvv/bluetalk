//go:build !darwin

package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"tinygo.org/x/bluetooth"
)

var (
	serviceUUID = bluetooth.NewUUID([16]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef})
	txCharUUID  = bluetooth.NewUUID([16]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xee})
	rxCharUUID  = bluetooth.NewUUID([16]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xed})
)

type BLEManager struct {
	adapter      *bluetooth.Adapter
	sendQueue    chan []byte
	receiveQueue chan []byte
	ackChan      chan uint32

	connected atomic.Bool
	isCentral atomic.Bool

	centralDevice *bluetooth.Device
	centralTX     bluetooth.DeviceCharacteristic

	peripheralRX bluetooth.Characteristic

	mu sync.Mutex
}

func NewBLEManager(sendQ, recvQ chan []byte) *BLEManager {
	return &BLEManager{
		adapter:      bluetooth.DefaultAdapter,
		sendQueue:    sendQ,
		receiveQueue: recvQ,
		ackChan:      make(chan uint32, 10),
	}
}

func (m *BLEManager) Start() error {
	err := m.adapter.Enable()
	if err != nil {
		return err
	}

	m.adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
		if connected {
			if m.connected.CompareAndSwap(false, true) {
				m.isCentral.Store(false)
				fmt.Printf("\n[+] Peer connected (Peripheral Role): %s\nYou: ", device.Address.String())
				m.adapter.StopScan()
			}
		} else {
			if !m.isCentral.Load() {
				m.handleDisconnect()
			}
		}
	})

	err = m.adapter.AddService(&bluetooth.Service{
		UUID: serviceUUID,
		Characteristics: []bluetooth.CharacteristicConfig{
			{
				UUID:  txCharUUID,
				Flags: bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,
				WriteEvent: func(client bluetooth.Connection, offset int, value []byte) {
					// Copy value because stack might reuse the buffer
					buf := make([]byte, len(value))
					copy(buf, value)
					m.receiveQueue <- buf
				},
			},
			{
				UUID:   rxCharUUID,
				Flags:  bluetooth.CharacteristicNotifyPermission | bluetooth.CharacteristicReadPermission,
				Handle: &m.peripheralRX,
			},
		},
	})
	if err != nil {
		return err
	}

	go m.resumeDiscovery()
	go m.sendLoop()

	return nil
}

func (m *BLEManager) handleDisconnect() {
	if m.connected.CompareAndSwap(true, false) {
		fmt.Printf("\n[-] Peer disconnected\nYou: ")
		go m.resumeDiscovery()
	}
}

func (m *BLEManager) resumeDiscovery() {
	// 1. Start Advertising
	adv := m.adapter.DefaultAdvertisement()
	_ = adv.Configure(bluetooth.AdvertisementOptions{
		LocalName:    "ChatPeer",
		ServiceUUIDs: []bluetooth.UUID{serviceUUID},
	})
	_ = adv.Start()

	// 2. Start Scanning
	_ = m.adapter.Scan(func(a *bluetooth.Adapter, device bluetooth.ScanResult) {
		if m.connected.Load() {
			a.StopScan()
			return
		}

		if device.LocalName() == "ChatPeer" || device.HasServiceUUID(serviceUUID) {
			if m.connected.CompareAndSwap(false, true) {
				m.isCentral.Store(true)
				a.StopScan()

				go func() {
					dev, err := a.Connect(device.Address, bluetooth.ConnectionParams{})
					if err != nil {
						m.connected.Store(false)
						m.resumeDiscovery()
						return
					}

					fmt.Printf("\n[+] Connected to peer (Central Role): %s\nYou: ", device.Address.String())
					m.centralDevice = &dev

					srvcs, err := dev.DiscoverServices([]bluetooth.UUID{serviceUUID})
					if err != nil || len(srvcs) == 0 {
						dev.Disconnect()
						m.handleDisconnect()
						return
					}

					chars, err := srvcs[0].DiscoverCharacteristics([]bluetooth.UUID{txCharUUID, rxCharUUID})
					if err != nil {
						dev.Disconnect()
						m.handleDisconnect()
						return
					}

					var rxChar bluetooth.DeviceCharacteristic
					for _, c := range chars {
						if c.UUID() == txCharUUID {
							m.centralTX = c
						} else if c.UUID() == rxCharUUID {
							rxChar = c
						}
					}

					err = rxChar.EnableNotifications(func(value []byte) {
						buf := make([]byte, len(value))
						copy(buf, value)
						m.receiveQueue <- buf
					})

					if err != nil {
						dev.Disconnect()
						m.handleDisconnect()
						return
					}
				}()
			}
		}
	})
}

// SendRaw writes data over the active BLE characteristic
func (m *BLEManager) SendRaw(data []byte) error {
	if !m.connected.Load() {
		return fmt.Errorf("not connected")
	}

	var err error
	if m.isCentral.Load() {
		_, err = m.centralTX.WriteWithoutResponse(data)
	} else {
		_, err = m.peripheralRX.Write(data)
	}

	if err != nil {
		if m.isCentral.Load() && m.centralDevice != nil {
			m.centralDevice.Disconnect()
		}
		m.handleDisconnect()
	}
	return err
}

func (m *BLEManager) HandleAck(msgID uint32) {
	select {
	case m.ackChan <- msgID:
	default:
	}
}

func (m *BLEManager) sendLoop() {
	var msgIDCounter uint32 = 1

	for {
		data := <-m.sendQueue
		if !m.connected.Load() {
			fmt.Println("\n[!] Not connected. Message dropped.\nYou: ")
			continue
		}

		msgID := atomic.AddUint32(&msgIDCounter, 1)
		frags := EncodeMessage(msgID, data)

		success := false
		retries := 3

		for r := 0; r < retries && !success; r++ {
			// Clear stale ACKs
			for len(m.ackChan) > 0 {
				<-m.ackChan
			}

			// Send with windowing/throttling
			for i := range frags {
				m.SendRaw(frags[i])

				// 4-fragment window throttling
				if (i+1)%4 == 0 {
					time.Sleep(50 * time.Millisecond)
				} else {
					time.Sleep(15 * time.Millisecond)
				}
			}

			// Wait for ACK
			select {
			case ackID := <-m.ackChan:
				if ackID == msgID {
					success = true
				}
			case <-time.After(3 * time.Second):
				fmt.Printf("\n[!] Timeout waiting for ACK, retrying... (%d/%d)\nYou: ", r+1, retries)
			}
		}

		if !success {
			fmt.Print("\n[!] Message failed to send after retries.\nYou: ")
		}
	}
}
