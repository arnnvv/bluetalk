# bluetalk

Bluetooth-only robust P2P chat CLI. No server and no database.

## What it does

- Auto peer mode: no Host/Client prompt.
- Discovery dance on Linux/Windows: alternates advertise and scan with jitter.
- Reliable transport over BLE:
  - MTU-safe fragmentation
  - per-fragment ACK
  - retry with timeout (stop-and-wait)
  - reassembly and stale-buffer cleanup
- Auto-reconnect: on disconnect, returns to discovery loop.

## Platform support

- Linux <-> Linux: supported.
- Linux <-> Windows: supported.
- macOS <-> Linux/Windows: supported (macOS runs Central-only with TinyGo BLE).
- macOS <-> macOS: not supported with current TinyGo backend (no macOS peripheral/advertising path).

## Run

On both devices:

```bash
go run .
```

Type and press Enter when connected.

## Build

```bash
go build ./...
```

## Dependency

- `tinygo.org/x/bluetooth` (current repo version)
