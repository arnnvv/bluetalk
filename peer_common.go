package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"tinygo.org/x/bluetooth"
)

const (
	serviceName = "BlueTalk"
	bleMTU      = 20
)

var (
	serviceUUID = bluetooth.NewUUID([16]byte{0xa1, 0xb2, 0xc3, 0xd4, 0xe5, 0xf6, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0x44, 0x44, 0x55, 0x55})
	rxUUID      = bluetooth.NewUUID([16]byte{0xa1, 0xb2, 0xc3, 0xd4, 0xe5, 0xf6, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0x44, 0x44, 0x55, 0x66})
	txUUID      = bluetooth.NewUUID([16]byte{0xa1, 0xb2, 0xc3, 0xd4, 0xe5, 0xf6, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0x44, 0x44, 0x55, 0x77})
)

type Peer struct {
	adapter *bluetooth.Adapter

	sendCh   chan string
	recvCh   chan string
	statusCh chan string

	mu           sync.Mutex
	connected    atomic.Bool
	isCentral    bool
	centralDev   *bluetooth.Device
	centralRX    bluetooth.DeviceCharacteristic
	peripheralTX bluetooth.Characteristic

	transport *Transport
}

func NewPeer(send, recv, status chan string) *Peer {
	p := &Peer{
		adapter:  bluetooth.DefaultAdapter,
		sendCh:   send,
		recvCh:   recv,
		statusCh: status,
	}
	p.transport = NewTransport(p, recv, status)
	return p
}

func (p *Peer) Run() {
	if err := p.adapter.Enable(); err != nil {
		p.publishStatus(fmt.Sprintf("BLE init failed: %v", err))
		return
	}

	if err := p.setupPlatform(); err != nil {
		p.publishStatus(fmt.Sprintf("BLE setup failed: %v", err))
		return
	}

	go p.writeLoop()

	if err := p.discoveryLoop(); err != nil {
		p.publishStatus(fmt.Sprintf("Discovery loop stopped: %v", err))
	}
}

func (p *Peer) writeLoop() {
	for msg := range p.sendCh {
		if !p.connected.Load() {
			p.publishStatus("Message ignored: not connected")
			continue
		}
		if err := p.transport.SendMessage(msg); err != nil {
			p.publishStatus(fmt.Sprintf("Send failed: %v", err))
		}
	}
}

func (p *Peer) setConnectedAsCentral(device bluetooth.Device, writeChar bluetooth.DeviceCharacteristic) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.centralDev = &device
	p.centralRX = writeChar
	p.isCentral = true
	p.connected.Store(true)
	p.transport.OnConnected()
}

func (p *Peer) setConnectedAsPeripheral() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.centralDev = nil
	p.isCentral = false
	p.connected.Store(true)
	p.transport.OnConnected()
}

func (p *Peer) handleDisconnect(reason string) {
	wasConnected := p.connected.Swap(false)
	if !wasConnected {
		return
	}

	p.mu.Lock()
	dev := p.centralDev
	p.centralDev = nil
	p.isCentral = false
	p.mu.Unlock()

	if dev != nil {
		_ = dev.Disconnect()
	}

	p.transport.OnDisconnected()
	p.publishStatus(reason)
}

func (p *Peer) writeRaw(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.connected.Load() {
		return fmt.Errorf("not connected")
	}

	if p.isCentral {
		_, err := p.centralRX.WriteWithoutResponse(data)
		if err != nil {
			go p.handleDisconnect("Disconnected: write failed")
		}
		return err
	}
	return p.writePeripheral()
}

func (p *Peer) connectAndSubscribe(addr bluetooth.Address) error {
	device, err := p.adapter.Connect(addr, bluetooth.ConnectionParams{})
	if err != nil {
		return err
	}

	services, err := device.DiscoverServices([]bluetooth.UUID{serviceUUID})
	if err != nil || len(services) == 0 {
		_ = device.Disconnect()
		if err == nil {
			err = fmt.Errorf("service not found")
		}
		return err
	}

	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{rxUUID, txUUID})
	if err != nil {
		_ = device.Disconnect()
		return err
	}

	var remoteRX bluetooth.DeviceCharacteristic
	var remoteTX bluetooth.DeviceCharacteristic
	var foundRX bool
	var foundTX bool
	for _, c := range chars {
		if c.UUID() == rxUUID {
			remoteRX = c
			foundRX = true
		}
		if c.UUID() == txUUID {
			remoteTX = c
			foundTX = true
		}
	}
	if !foundRX || !foundTX {
		_ = device.Disconnect()
		return fmt.Errorf("required characteristic missing")
	}

	if err := remoteTX.EnableNotifications(func(value []byte) {
		pkt := make([]byte, len(value))
		copy(pkt, value)
		p.transport.OnReceivePacket(pkt)
	}); err != nil {
		_ = device.Disconnect()
		return err
	}

	p.setConnectedAsCentral(device, remoteRX)
	p.publishStatus(fmt.Sprintf("Connected as Central to %s", addr.String()))
	return nil
}

func (p *Peer) publishStatus(msg string) {
	select {
	case p.statusCh <- msg:
	default:
	}
}

func (p *Peer) waitUntilDisconnected() {
	for p.connected.Load() {
		time.Sleep(250 * time.Millisecond)
	}
}

func randomPhaseDuration(minMs, spanMs int) time.Duration {
	return time.Duration(minMs+randIntn(spanMs)) * time.Millisecond
}

func randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(time.Now().UnixNano() % int64(n))
}
