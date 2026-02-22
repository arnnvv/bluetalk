package bluez

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// CentralClient represents a BLE central connection (device + RX char for write, TX for notify).
type CentralClient struct {
	conn           *dbus.Conn
	devicePath     dbus.ObjectPath
	writeCharPath  dbus.ObjectPath
	notifyCharPath dbus.ObjectPath
	addr           string
	disconnected   chan struct{}
	once           sync.Once
}

// WriteNoResponse writes to the RX characteristic (write-without-response).
func (c *CentralClient) WriteNoResponse(data []byte) error {
	obj := c.conn.Object(bluezDest, c.writeCharPath)
	opts := map[string]any{"type": "command"}
	err := obj.Call("org.bluez.GattCharacteristic1.WriteValue", 0, data, opts).Err
	if err != nil {
		return err
	}
	return nil
}

// Close disconnects the device.
func (c *CentralClient) Close() error {
	c.signalDisconnect()
	return c.conn.Object(bluezDest, c.devicePath).Call("org.bluez.Device1.Disconnect", 0).Err
}

// Disconnected returns a channel that is closed when the device disconnects.
func (c *CentralClient) Disconnected() <-chan struct{} {
	return c.disconnected
}

func (c *CentralClient) signalDisconnect() {
	c.once.Do(func() { close(c.disconnected) })
}

// Connect discovers the adapter, connects to the device, resolves GATT service/characteristics,
// subscribes to TX notifications, and returns a CentralClient. It also starts a goroutine that
// watches for disconnect and closes the Disconnected() channel.
func Connect(ctx context.Context, conn *dbus.Conn, adapterPath dbus.ObjectPath, addr string, serviceUUID, rxUUID, txUUID []byte, onNotify func([]byte)) (*CentralClient, error) {
	devicePath := PathFromAddr(adapterPath, addr)
	obj := conn.Object(bluezDest, devicePath)
	if err := obj.Call("org.bluez.Device1.Connect", 0).Err; err != nil {
		return nil, fmt.Errorf("Connect: %w", err)
	}

	// Wait for services resolved (with timeout)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			_ = conn.Object(bluezDest, devicePath).Call("org.bluez.Device1.Disconnect", 0)
			return nil, ctx.Err()
		default:
		}
		var v dbus.Variant
		if err := conn.Object(bluezDest, devicePath).Call("org.freedesktop.DBus.Properties.Get", 0, "org.bluez.Device1", "ServicesResolved").Store(&v); err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if resolved, ok := v.Value().(bool); ok && resolved {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Resolve GATT paths
	var out map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := conn.Object(bluezDest, bluezRoot).Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&out); err != nil {
		_ = conn.Object(bluezDest, devicePath).Call("org.bluez.Device1.Disconnect", 0)
		return nil, fmt.Errorf("GetManagedObjects: %w", err)
	}

	devPathStr := string(devicePath)
	svcStr := UUIDToStr(serviceUUID)
	rxStr := UUIDToStr(rxUUID)
	txStr := UUIDToStr(txUUID)

	var servicePath dbus.ObjectPath
	for path, ifaces := range out {
		p := string(path)
		if !strings.HasPrefix(p, devPathStr+"/") {
			continue
		}
		g, ok := ifaces["org.bluez.GattService1"]
		if !ok {
			continue
		}
		u, _ := g["UUID"].Value().(string)
		if u == svcStr {
			servicePath = path
			break
		}
	}
	if servicePath == "" {
		_ = conn.Object(bluezDest, devicePath).Call("org.bluez.Device1.Disconnect", 0)
		return nil, fmt.Errorf("service not found")
	}

	var writeCharPath, notifyCharPath dbus.ObjectPath
	svcPrefix := string(servicePath) + "/"
	for path, ifaces := range out {
		p := string(path)
		if !strings.HasPrefix(p, svcPrefix) || p == svcPrefix {
			continue
		}
		g, ok := ifaces["org.bluez.GattCharacteristic1"]
		if !ok {
			continue
		}
		u, _ := g["UUID"].Value().(string)
		if u == rxStr {
			writeCharPath = path
		}
		if u == txStr {
			notifyCharPath = path
		}
	}
	if writeCharPath == "" || notifyCharPath == "" {
		_ = conn.Object(bluezDest, devicePath).Call("org.bluez.Device1.Disconnect", 0)
		return nil, fmt.Errorf("characteristics not found")
	}

	client := &CentralClient{
		conn:           conn,
		devicePath:     devicePath,
		writeCharPath:  writeCharPath,
		notifyCharPath: notifyCharPath,
		addr:           addr,
		disconnected:   make(chan struct{}),
	}

	// StartNotify and listen for PropertiesChanged (Value)
	if err := conn.Object(bluezDest, notifyCharPath).Call("org.bluez.GattCharacteristic1.StartNotify", 0).Err; err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("StartNotify: %w", err)
	}

	ch := make(chan *dbus.Signal, 16)
	conn.Signal(ch)
	matchNotify := fmt.Sprintf("type='signal',path='%s',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged'", notifyCharPath)
	conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchNotify)
	matchDev := fmt.Sprintf("type='signal',path='%s',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged'", devicePath)
	conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchDev)
	go func() {
		for sig := range ch {
			if len(sig.Body) < 2 {
				continue
			}
			changed, ok := sig.Body[1].(map[string]dbus.Variant)
			if !ok {
				continue
			}
			if sig.Path == notifyCharPath {
				if v, ok := changed["Value"]; ok {
					if b, ok := v.Value().([]byte); ok && len(b) > 0 {
						pkt := make([]byte, len(b))
						copy(pkt, b)
						onNotify(pkt)
					}
				}
			} else if sig.Path == devicePath {
				if _, has := changed["Connected"]; has {
					client.signalDisconnect()
					return
				}
			}
		}
	}()

	return client, nil
}
