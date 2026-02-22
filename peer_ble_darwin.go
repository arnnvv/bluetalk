//go:build darwin

package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tinygo-org/cbgo"
	"tinygo.org/x/bluetooth"
)

var adapter = bluetooth.DefaultAdapter

// darwinAdvState holds a dedicated PeripheralManager for advertising on macOS
// (tinygo bluetooth does not expose DefaultAdvertisement on darwin).
var darwinAdvState struct {
	pm         cbgo.PeripheralManager
	pmOnce     sync.Once
	poweredCh  chan struct{}
	poweredSet int32
}

type darwinAdvDelegate struct {
	cbgo.PeripheralManagerDelegateBase
}

func (d *darwinAdvDelegate) PeripheralManagerDidUpdateState(pmgr cbgo.PeripheralManager) {
	if pmgr.State() == cbgo.ManagerStatePoweredOn && atomic.CompareAndSwapInt32(&darwinAdvState.poweredSet, 0, 1) {
		close(darwinAdvState.poweredCh)
	}
}

func (d *darwinAdvDelegate) DidStartAdvertising(pmgr cbgo.PeripheralManager, err error) {
	// Optional: could surface err to caller
	_ = err
}

func bytesToUUID(b []byte) bluetooth.UUID {
	var arr [16]byte
	copy(arr[:], b)
	return bluetooth.NewUUID(arr)
}

// serviceUUIDForCBGO returns the BlueTalk service UUID in cbgo format for advertisement.
func serviceUUIDForCBGO() cbgo.UUID {
	s := bytesToUUID(serviceUUID).String()
	u, err := cbgo.ParseUUID(s)
	if err != nil {
		panic("blueTalk service UUID: " + err.Error())
	}
	return u
}

func (p *Peer) setupPlatform() error {
	if err := adapter.Enable(); err != nil {
		return fmt.Errorf("failed to enable BLE adapter: %w", err)
	}
	p.publishStatus("BLE adapter enabled")
	return nil
}

func (p *Peer) startAdvertising() error {
	darwinAdvState.pmOnce.Do(func() {
		darwinAdvState.poweredCh = make(chan struct{})
		darwinAdvState.pm = cbgo.NewPeripheralManager(nil)
		darwinAdvState.pm.SetDelegate(&darwinAdvDelegate{})
	})

	// Wait for peripheral manager to be powered on (same radio as central).
	select {
	case <-darwinAdvState.poweredCh:
	case <-time.After(10 * time.Second):
		return fmt.Errorf("BLE peripheral manager did not become ready in time")
	}

	darwinAdvState.pm.StartAdvertising(cbgo.AdvData{
		LocalName:     serviceName,
		ServiceUUIDs:  []cbgo.UUID{serviceUUIDForCBGO()},
	})
	return nil
}

func (p *Peer) stopAdvertising() error {
	if atomic.LoadInt32(&darwinAdvState.poweredSet) != 1 {
		return nil // never started advertising
	}
	darwinAdvState.pm.StopAdvertising()
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

		p.publishStatus("No peers found. Advertising...")
		if err := p.startAdvertising(); err != nil {
			p.publishStatus(fmt.Sprintf("Advertising failed: %v", err))
		} else {
			time.Sleep(5 * time.Second)
			_ = p.stopAdvertising()
		}
	}
}

func (p *Peer) writePeripheral(data []byte) (int, error) {
	return 0, fmt.Errorf("peripheral write not implemented")
}
