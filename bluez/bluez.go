// Package bluez implements BLE over BlueZ D-Bus (Linux only, pure Go).
package bluez

import (
	"fmt"
	"strings"

	"bluetalk/dbus"
)

const (
	bluezDest     = "org.bluez"
	bluezRoot     = "/"
	adapterPrefix = "/org/bluez/"
)

func UUIDToStr(b []byte) string {
	if len(b) != 16 {
		return ""
	}
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
		b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}

// AddrFromPath extracts MAC from device path dev_AA_BB_CC_DD_EE_FF -> AA:BB:CC:DD:EE:FF.
func AddrFromPath(path dbus.ObjectPath) string {
	s := string(path)
	i := strings.LastIndex(s, "/")
	if i < 0 {
		return ""
	}
	s = s[i+1:]
	if !strings.HasPrefix(s, "dev_") {
		return ""
	}
	s = s[4:]
	return strings.ReplaceAll(s, "_", ":")
}

// PathFromAddr converts MAC to device object path (e.g. AA:BB:CC:DD:EE:FF -> dev_AA_BB_CC_DD_EE_FF).
func PathFromAddr(adapterPath dbus.ObjectPath, addr string) dbus.ObjectPath {
	s := strings.ReplaceAll(strings.ToUpper(addr), ":", "_")
	return dbus.ObjectPath(string(adapterPath) + "/dev_" + s)
}
