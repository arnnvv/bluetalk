//go:build !darwin

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"

	"bluetalk/bluez"
)

var (
	dbusConn     *dbus.Conn
	bluezAdapter *bluez.Adapter
)

func (p *Peer) setupPlatform() error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("dbus: %w", err)
	}
	dbusConn = conn
	adapter, err := bluez.DefaultAdapter(conn)
	if err != nil {
		conn.Close()
		return err
	}
	bluezAdapter = adapter
	p.publishStatus("BLE ready (BlueZ)")
	return nil
}

func (p *Peer) connectAndSubscribePlatform(ctx context.Context, addr string) error {
	client, err := bluez.Connect(ctx, dbusConn, bluezAdapter.Path(), addr, serviceUUID, rxUUID, txUUID, func(data []byte) {
		p.transport.OnReceivePacket(data)
	})
	if err != nil {
		return err
	}
	go func() {
		<-client.Disconnected()
		p.handleDisconnect(fmt.Sprintf("Disconnected from %s", addr))
	}()
	p.setConnectedAsCentral(client)
	p.publishStatus(fmt.Sprintf("Connected as Central to %s", addr))
	return nil
}

func (p *Peer) discoveryLoop() error {
	for {
		if p.connected.Load() {
			p.waitUntilDisconnected()
			continue
		}

		// Pure-Go Linux: central-only for now (no advertising)
		scanDuration := randomPhaseDuration(700, 1600)
		p.publishStatus("Discovery: scanning")
		addr, found, err := p.scanForPeer(scanDuration)
		if err != nil {
			p.publishStatus(fmt.Sprintf("Scan error: %v", err))
			continue
		}
		if !found {
			continue
		}

		p.publishStatus(fmt.Sprintf("Peer found: %s", addr))
		if err := p.connectAndSubscribe(addr); err != nil {
			p.publishStatus(fmt.Sprintf("Connect failed: %v", err))
		}
	}
}

func (p *Peer) scanForPeer(window time.Duration) (addr string, found bool, err error) {
	foundCh := make(chan bluez.ScanResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()

	go func() {
		_ = bluez.Scan(ctx, dbusConn, bluezAdapter, bluez.UUIDToStr(serviceUUID), serviceName, foundCh)
	}()

	select {
	case res := <-foundCh:
		return res.Addr, true, nil
	case <-ctx.Done():
		return "", false, nil
	}
}

func (p *Peer) writePeripheral(data []byte) error {
	return fmt.Errorf("peripheral not implemented in pure-Go build (central-only)")
}
