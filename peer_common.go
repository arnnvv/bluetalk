package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	serviceName = "BlueTalk"
	bleMTU      = 20
)

// 128-bit custom UUIDs for BlueTalk (raw bytes for platform use).
var (
	serviceUUID = []byte{0xa1, 0xb2, 0xc3, 0xd4, 0xe5, 0xf6, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0x44, 0x44, 0x55, 0x55}
	rxUUID      = []byte{0xa1, 0xb2, 0xc3, 0xd4, 0xe5, 0xf6, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0x44, 0x44, 0x55, 0x66}
	txUUID      = []byte{0xa1, 0xb2, 0xc3, 0xd4, 0xe5, 0xf6, 0x11, 0x11, 0x22, 0x22, 0x33, 0x33, 0x44, 0x44, 0x55, 0x77}
)

// centralConn is the interface for an active BLE central connection (write + disconnect).
type centralConn interface {
	WriteNoResponse(data []byte) error
	Close() error
	Disconnected() <-chan struct{}
}

// peripheralNotifier is the interface for sending notifications as a BLE peripheral.
type peripheralNotifier interface {
	Write(data []byte) (int, error)
	Close() error
}

type Peer struct {
	sendCh   chan string
	recvCh   chan string
	statusCh chan string

	mu        sync.Mutex
	connected atomic.Bool
	isCentral bool

	centralClient centralConn

	peripheralNotifierMu sync.Mutex
	peripheralNotifier   peripheralNotifier

	transport *Transport
}

func NewPeer(send, recv, status chan string) *Peer {
	p := &Peer{
		sendCh:   send,
		recvCh:   recv,
		statusCh: status,
	}
	p.transport = NewTransport(p, recv, status)
	return p
}

func (p *Peer) Run() {
	if err := p.setupPlatform(); err != nil {
		p.publishStatus(fmt.Sprintf("BLE setup failed: %v", err))
		return
	}

	go p.writeLoop()

	p.runDiscoveryAndConnection()
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

func (p *Peer) setConnectedAsCentral(client centralConn) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.centralClient = client
	p.isCentral = true
	p.connected.Store(true)
	p.transport.OnConnected()
}

func (p *Peer) setConnectedAsPeripheral() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.centralClient = nil
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
	client := p.centralClient
	p.centralClient = nil
	p.isCentral = false

	p.peripheralNotifierMu.Lock()
	if p.peripheralNotifier != nil {
		_ = p.peripheralNotifier.Close()
		p.peripheralNotifier = nil
	}
	p.peripheralNotifierMu.Unlock()
	p.mu.Unlock()

	if client != nil {
		_ = client.Close()
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
		err := p.centralClient.WriteNoResponse(data)
		if err != nil {
			go p.handleDisconnect("Disconnected: write failed")
		}
		return err
	}
	_, err := p.writePeripheral(data)
	return err
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
