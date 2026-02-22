package bluez

import (
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

// Adapter wraps the BlueZ adapter (e.g. /org/bluez/hci0).
type Adapter struct {
	conn *dbus.Conn
	path dbus.ObjectPath
}

// DefaultAdapter returns the first BlueZ adapter (hci0).
func DefaultAdapter(conn *dbus.Conn) (*Adapter, error) {
	var out map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	obj := conn.Object(bluezDest, bluezRoot)
	err := obj.Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&out)
	if err != nil {
		return nil, fmt.Errorf("GetManagedObjects: %w", err)
	}
	for path := range out {
		p := string(path)
		if strings.HasPrefix(p, adapterPrefix) && strings.Count(p, "/") == 2 {
			// e.g. /org/bluez/hci0
			return &Adapter{conn: conn, path: path}, nil
		}
	}
	return nil, fmt.Errorf("no BlueZ adapter found")
}

// StartDiscovery starts LE discovery.
func (a *Adapter) StartDiscovery() error {
	return a.conn.Object(bluezDest, a.path).Call("org.bluez.Adapter1.StartDiscovery", 0).Err
}

// StopDiscovery stops discovery.
func (a *Adapter) StopDiscovery() error {
	return a.conn.Object(bluezDest, a.path).Call("org.bluez.Adapter1.StopDiscovery", 0).Err
}

// SetDiscoveryFilter sets UUID filter for LE scan (optional).
func (a *Adapter) SetDiscoveryFilter(uuidStr string) error {
	filter := map[string]any{
		"Transport": "le",
	}
	if uuidStr != "" {
		filter["UUIDs"] = []string{uuidStr}
	}
	return a.conn.Object(bluezDest, a.path).Call("org.bluez.Adapter1.SetDiscoveryFilter", 0, filter).Err
}

// Path returns the adapter object path.
func (a *Adapter) Path() dbus.ObjectPath {
	return a.path
}
