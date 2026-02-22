//go:build darwin

package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

var adapter = bluetooth.DefaultAdapter

func bytesToUUID(b []byte) bluetooth.UUID {
	var arr [16]byte
	copy(arr[:], b)
	return bluetooth.NewUUID(arr)
}

func (p *Peer) setupPlatform() error {
	if err := adapter.Enable(); err != nil {
		return fmt.Errorf("failed to enable BLE adapter: %w", err)
	}
	p.publishStatus("BLE adapter enabled")
	return nil
}

// startAdvertising is a no-op on macOS (peripheral advertising not supported by tinygo bluetooth).
func (p *Peer) startAdvertising() error {
	return nil
}

// stopAdvertising is a no-op on macOS.
func (p *Peer) stopAdvertising() error {
	return nil
}

func (p *Peer) startScanning(callback func(bluetooth.ScanResult)) error {
	return adapter.Scan(func(adapter *bluetooth.Adapter, device bluetooth.ScanResult) {
		if device.HasServiceUUID(bytesToUUID(serviceUUID)) {
			callback(device)
		}
	})
}

func (p *Peer) stopScan() error {
	return adapter.StopScan()
}

func (p *Peer) connectAndSubscribePlatform(ctx context.Context, addr bluetooth.Address) error {
	device, err := adapter.Connect(addr, bluetooth.ConnectionParams{})
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	bleSvc := bytesToUUID(serviceUUID)
	bleRX := bytesToUUID(rxUUID)
	bleTX := bytesToUUID(txUUID)

	services, err := device.DiscoverServices([]bluetooth.UUID{bleSvc})
	if err != nil || len(services) == 0 {
		_ = device.Disconnect()
		return fmt.Errorf("service discovery failed: %w", err)
	}
	svc := services[0]

	chars, err := svc.DiscoverCharacteristics([]bluetooth.UUID{bleRX, bleTX})
	if err != nil {
		_ = device.Disconnect()
		return fmt.Errorf("characteristic discovery failed: %w", err)
	}

	var rxChar, txChar bluetooth.DeviceCharacteristic
	for _, c := range chars {
		if c.UUID() == bleRX {
			rxChar = c
		}
		if c.UUID() == bleTX {
			txChar = c
		}
	}
	if rxChar.UUID() != bleRX || txChar.UUID() != bleTX {
		_ = device.Disconnect()
		return fmt.Errorf("required characteristics not found")
	}

	err = txChar.EnableNotifications(func(buf []byte) {
		p.transport.OnReceivePacket(buf)
	})
	if err != nil {
		_ = device.Disconnect()
		return fmt.Errorf("failed to enable notifications: %w", err)
	}

	client := &CentralClient{
		device:         device,
		writeChar:      rxChar,
		disconnectedCh: make(chan struct{}),
	}

	go func() {
		<-client.Disconnected()
		p.handleDisconnect(fmt.Sprintf("Disconnected from %s", addr.String()))
	}()

	p.setConnectedAsCentral(client)
	p.publishStatus(fmt.Sprintf("Connected to %s", addr.String()))
	return nil
}

type CentralClient struct {
	device         bluetooth.Device
	writeChar      bluetooth.DeviceCharacteristic
	disconnectedCh chan struct{}
	once           sync.Once
}

func (c *CentralClient) WriteNoResponse(data []byte) error {
	_, err := c.writeChar.WriteWithoutResponse(data)
	if err != nil {
		c.signalDisconnect()
	}
	return err
}

func (c *CentralClient) Close() error {
	c.signalDisconnect()
	return c.device.Disconnect()
}

func (c *CentralClient) Disconnected() <-chan struct{} {
	return c.disconnectedCh
}

func (c *CentralClient) signalDisconnect() {
	c.once.Do(func() { close(c.disconnectedCh) })
}

func (p *Peer) runDiscoveryAndConnection() {
	for {
		if p.connected.Load() {
			p.waitUntilDisconnected()
			continue
		}

		p.publishStatus("Scanning for peers...")
		found := make(chan bluetooth.ScanResult, 10)
		go func() {
			_ = p.startScanning(func(device bluetooth.ScanResult) {
				select {
				case found <- device:
				default:
				}
			})
		}()

		var devices []bluetooth.ScanResult
		timeout := time.After(5 * time.Second)
	loop:
		for {
			select {
			case dev := <-found:
				devices = append(devices, dev)
			case <-timeout:
				break loop
			}
		}
		_ = p.stopScan()

		if len(devices) > 0 {
			selected := devices[0]
			p.publishStatus(fmt.Sprintf("Connecting to %s (%s)...", selected.LocalName(), selected.Address.String()))
			err := p.connectAndSubscribePlatform(context.Background(), selected.Address)
			if err != nil {
				p.publishStatus(fmt.Sprintf("Connection failed: %v", err))
				time.Sleep(2 * time.Second)
			}
			continue
		}

		// macOS does not support BLE peripheral advertising; just wait and rescan.
		p.publishStatus("No peers found. Will rescan in 5s (macOS cannot advertise).")
		time.Sleep(5 * time.Second)
	}
}

func (p *Peer) writePeripheral(data []byte) (int, error) {
	return 0, fmt.Errorf("peripheral write not implemented")
}
