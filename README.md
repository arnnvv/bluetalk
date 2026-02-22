# bluetalk

Bluetooth-only robust P2P chat CLI. No server and no database.

## What it does

- Auto peer mode: no Host/Client prompt.
- Discovery: scan for peers (Linux: BlueZ; pure Go).
- Reliable transport over BLE:
  - MTU-safe fragmentation
  - per-fragment ACK
  - retry with timeout (stop-and-wait)
  - reassembly and stale-buffer cleanup
- Auto-reconnect: on disconnect, returns to discovery loop.

## Platform support (pure-Go build)

- **Linux**: BLE central only via BlueZ over D-Bus. Scan and connect to a peer that advertises the BlueTalk service. Peripheral (advertise + GATT server) not yet implemented in pure Go.
- **macOS**: Not supported (no BLE in pure Go without CGo/CoreBluetooth).

## Dependency

- **`github.com/godbus/dbus/v5`** — only dependency; used to talk to BlueZ on Linux (pure Go, no CGo).

## Run

On Linux, with BlueZ running and a BLE adapter:

```bash
go run .
```

Connect to another device that is advertising the BlueTalk service (e.g. a second Linux host running a BLE peripheral, or another client that uses the same service UUIDs).

## Build

```bash
go build ./...
```
