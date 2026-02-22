//go:build darwin

package main

import (
	"context"
	"fmt"
	"time"
)

func (p *Peer) setupPlatform() error {
	return fmt.Errorf("BLE on macOS is not supported in pure-Go build (no CGo/CoreBluetooth); use Linux or a build with a BLE library")
}

func (p *Peer) connectAndSubscribePlatform(ctx context.Context, addr string) error {
	return fmt.Errorf("BLE not available on macOS in pure-Go build")
}

func (p *Peer) discoveryLoop() error {
	for {
		p.publishStatus("BLE not available (macOS pure-Go build)")
		time.Sleep(5 * time.Second)
	}
}

func (p *Peer) scanForPeer(window time.Duration) (addr string, found bool, err error) {
	return "", false, nil
}

func (p *Peer) writePeripheral(data []byte) error {
	return fmt.Errorf("peripheral not available on macOS")
}
