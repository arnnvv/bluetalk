//go:build darwin

package main

import "fmt"

func runHost() error {
	fmt.Println("Host mode is not supported on macOS (TinyGo BLE supports Central only on macOS).")
	fmt.Println("Run Host on Linux/Windows and use this machine as Client.")
	return nil
}
