//go:build !darwin

package main

import (
	"fmt"
	"time"

	"tinygo.org/x/bluetooth"
)

func (p *Peer) setupPlatform() error {
	incoming := make(chan bluetooth.Device, 4)

	p.adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
		if connected {
			select {
			case incoming <- device:
			default:
			}
			return
		}
		p.handleDisconnect(fmt.Sprintf("Disconnected from %s", device.Address.String()))
	})

	if err := p.adapter.AddService(&bluetooth.Service{
		UUID: serviceUUID,
		Characteristics: []bluetooth.CharacteristicConfig{
			{
				UUID:  rxUUID,
				Flags: bluetooth.CharacteristicWritePermission | bluetooth.CharacteristicWriteWithoutResponsePermission,
				WriteEvent: func(client bluetooth.Connection, offset int, value []byte) {
					pkt := make([]byte, len(value))
					copy(pkt, value)
					p.transport.OnReceivePacket(pkt)
				},
			},
			{
				UUID:   txUUID,
				Flags:  bluetooth.CharacteristicNotifyPermission | bluetooth.CharacteristicReadPermission,
				Handle: &p.peripheralTX,
			},
		},
	}); err != nil {
		return err
	}

	p.publishStatus("Local GATT ready")
	pIncoming = incoming
	return nil
}

var pIncoming chan bluetooth.Device

func (p *Peer) discoveryLoop() error {
	adv := p.adapter.DefaultAdvertisement()
	if err := adv.Configure(bluetooth.AdvertisementOptions{
		LocalName:    serviceName,
		ServiceUUIDs: []bluetooth.UUID{serviceUUID},
	}); err != nil {
		return err
	}

	for {
		if p.connected.Load() {
			p.waitUntilDisconnected()
			continue
		}

		advDuration := randomPhaseDuration(500, 1400)
		p.publishStatus("Discovery: advertising")
		if err := adv.Start(); err == nil {
			select {
			case device := <-pIncoming:
				_ = adv.Stop()
				p.setConnectedAsPeripheral()
				p.publishStatus(fmt.Sprintf("Connected as Peripheral to %s", device.Address.String()))
				continue
			case <-time.After(advDuration):
				_ = adv.Stop()
			}
		}

		if p.connected.Load() {
			continue
		}

		scanDuration := randomPhaseDuration(700, 1600)
		p.publishStatus("Discovery: scanning")
		res, found, err := p.scanForPeer(scanDuration)
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
		}
	}
}

func (p *Peer) scanForPeer(window time.Duration) (bluetooth.ScanResult, bool, error) {
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

func (p *Peer) writePeripheral(data []byte) error {
	_, err := p.peripheralTX.Write(data)
	if err != nil {
		go p.handleDisconnect("Disconnected: peripheral notify failed")
	}
	return err
}
