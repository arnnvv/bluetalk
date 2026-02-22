# bluetalk

Bluetooth-only chat CLI. No server, no database. One host (BLE Peripheral) and one or more clients (BLE Central) over BLE.

## Platform support

- **Host (H)** must run on **Linux or Windows**. TinyGo BLE on macOS supports Central only; Host mode is not available on macOS.
- **Client (C)** works on **macOS, Linux, and Windows**.

## Usage

1. On a Linux or Windows machine, run as Host:
   ```bash
   go run .
   # Choose: H
   ```

2. On any supported machine (including macOS), run as Client:
   ```bash
   go run .
   # Choose: C
   ```

3. Type messages and press Enter. Host messages are prefixed with "Host: "; client messages appear as "[Client]: ..." on the host and in the notification callback on clients.
4. Client scan has a 25-second timeout and prints a hint if no host is advertising.

## Build

```bash
go build .
./bluetalk
```

## Dependencies

Uses [tinygo.org/x/bluetooth](https://pkg.go.dev/tinygo.org/x/bluetooth) for cross-platform BLE (Linux: BlueZ, macOS: CoreBluetooth, Windows: WinRT).
