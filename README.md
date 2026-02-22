# bluetalk

Bluetooth-only chat CLI with automatic peer mode. No server, no database.

## Platform support

- **Linux / Windows**: full auto role selection (alternates advertise + scan until connected).
- **macOS**: Central-only with TinyGo BLE. The app auto-scans/connects, but cannot advertise.
- **macOS <-> macOS**: not supported with current backend because neither side can advertise.

## Usage

On both devices, just run:

```bash
go run .
```

The app automatically performs discovery and connects when a peer is found. After connection, type messages and press Enter.

## Build

```bash
go build .
./bluetalk
```

## Dependencies

Uses [tinygo.org/x/bluetooth](https://pkg.go.dev/tinygo.org/x/bluetooth) for cross-platform BLE (Linux: BlueZ, macOS: CoreBluetooth, Windows: WinRT).
