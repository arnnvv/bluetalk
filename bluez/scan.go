package bluez

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/godbus/dbus/v5"
)

// ScanResult holds a discovered device's address, name, and service UUIDs.
type ScanResult struct {
	Addr  string
	Name  string
	UUIDs []string
}

// Scan runs discovery for the given duration and sends matching results to the channel.
// Filter by name and/or serviceUUIDStr (empty string = any). Cancel ctx to stop early.
func Scan(ctx context.Context, conn *dbus.Conn, adapter *Adapter, serviceUUIDStr, nameFilter string, foundCh chan<- ScanResult) error {
	if err := adapter.SetDiscoveryFilter(serviceUUIDStr); err != nil {
		// non-fatal
		_ = adapter.SetDiscoveryFilter("")
	}
	if err := adapter.StartDiscovery(); err != nil {
		return fmt.Errorf("StartDiscovery: %w", err)
	}
	defer adapter.StopDiscovery()

	// InterfacesAdded is emitted by org.bluez; body is (object_path, interfaces).
	match := "type='signal',interface='org.freedesktop.DBus.ObjectManager',member='InterfacesAdded'"
	conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, match)
	ch := make(chan *dbus.Signal, 8)
	conn.Signal(ch)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig, ok := <-ch:
			if !ok {
				return nil
			}
			if len(sig.Body) < 2 {
				continue
			}
			path, ok := sig.Body[0].(dbus.ObjectPath)
			if !ok {
				continue
			}
			// Only consider devices under our adapter.
			if !strings.HasPrefix(string(path), string(adapter.Path())+"/") {
				continue
			}
			ifaces, ok := sig.Body[1].(map[string]map[string]dbus.Variant)
			if !ok {
				continue
			}
			dev, ok := ifaces["org.bluez.Device1"]
			if !ok {
				continue
			}
			addr := AddrFromPath(path)
			if addr == "" {
				continue
			}
			name := ""
			if n, ok := dev["Alias"]; ok {
				name, _ = n.Value().(string)
			}
			if name == "" {
				if n, ok := dev["Name"]; ok {
					name, _ = n.Value().(string)
				}
			}
			var uuids []string
			if u, ok := dev["UUIDs"]; ok {
				uuids, _ = u.Value().([]string)
			}
			matchName := nameFilter == "" || name == nameFilter
			matchUUID := serviceUUIDStr == "" || slices.Contains(uuids, serviceUUIDStr)
			if matchName || matchUUID {
				select {
				case foundCh <- ScanResult{Addr: addr, Name: name, UUIDs: uuids}:
				default:
				}
			}
		}
	}
}
