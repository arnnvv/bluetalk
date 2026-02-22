// Package dbus implements a minimal D-Bus client (stdlib only) for BlueZ.
package dbus

// ObjectPath is a D-Bus object path.
type ObjectPath string

// Variant holds a single D-Bus variant (type + value).
type Variant struct {
	Signature string
	Value     any
}

// Signal is a received D-Bus signal.
type Signal struct {
	Path      ObjectPath
	Interface string
	Member    string
	Body      []any
}
