//go:build darwin

package main

import (
	"fmt"
	"time"

	"tinygo.org/x/bluetooth"
)

func (p *Peer) setupPlatform() error {
	p.adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
		if !connected {
			p.handleDisconnect(fmt.Sprintf("Disconnected from %s", device.Address.String()))
		}
	})
	p.publishStatus("macOS detected: Central-only mode (no advertising)")
	return nil
}

func (p *Peer) discoveryLoop() error {
	for {
		if p.connected.Load() {
			p.waitUntilDisconnected()
			continue
		}

		p.publishStatus("Discovery: scanning (macOS central-only)")
		res, found, err := p.scanForPeerDarwin(randomPhaseDuration(900, 1700))
		if err != nil {
			p.publishStatus(fmt.Sprintf("Scan error: %v", err))
			continue
		}
		if !found {
			continue
		}

		p.publishStatus(fmt.Sprintf("Peer found: %s", res.Address.String()))
		if err := p.connectAndSubscribe(res.Address); err != nil {
			p.publishStatus(fmt.Sprintf("Connect failed: %v", err))
			time.Sleep(300 * time.Millisecond)
		}
	}
}

func (p *Peer) scanForPeerDarwin(window time.Duration) (bluetooth.ScanResult, bool, error) {
	foundCh := make(chan bluetooth.ScanResult, 1)
	scanDone := make(chan error, 1)

	go func() {
		scanDone <- p.adapter.Scan(func(a *bluetooth.Adapter, res bluetooth.ScanResult) {
			if res.LocalName() != serviceName && !res.HasServiceUUID(serviceUUID) {
				return
			}
			select {
			case foundCh <- res:
			default:
			}
			_ = a.StopScan()
		})
	}()

	timer := time.AfterFunc(window, func() {
		_ = p.adapter.StopScan()
	})
	err := <-scanDone
	timer.Stop()

	select {
	case res := <-foundCh:
		return res, true, nil
	default:
		if err != nil {
			return bluetooth.ScanResult{}, false, err
		}
		return bluetooth.ScanResult{}, false, nil
	}
}

func (p *Peer) writePeripheral() error {
	return fmt.Errorf("peripheral mode unavailable on macOS")
}
