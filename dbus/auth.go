package dbus

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
)

const defaultSystemBus = "unix:path=/var/run/dbus/system_bus_socket"

func getSystemBusAddress() string {
	if s := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS"); s != "" {
		return s
	}
	return defaultSystemBus
}

func connectUnix(addr string) (net.Conn, error) {
	const pref = "unix:path="
	if len(addr) < len(pref) || addr[:len(pref)] != pref {
		return nil, fmt.Errorf("dbus: unsupported address %q", addr)
	}
	path := addr[len(pref):]
	return net.Dial("unix", path)
}

func auth(conn net.Conn) error {
	if _, err := conn.Write([]byte{0}); err != nil {
		return err
	}
	rd := bufio.NewReader(conn)
	line, err := rd.ReadString('\n')
	if err != nil {
		return err
	}
	line = trimCRLF(line)
	if line == "OK" || len(line) > 3 && line[:3] == "OK " {
		return sendLine(conn, "BEGIN")
	}
	hexUID := hex.EncodeToString([]byte(strconv.Itoa(os.Getuid())))
	if _, err := sendLineBuf(conn, "AUTH EXTERNAL ", hexUID); err != nil {
		return err
	}
	line, err = rd.ReadString('\n')
	if err != nil {
		return err
	}
	line = trimCRLF(line)
	if line != "OK" && (len(line) < 3 || line[:3] != "OK ") {
		return fmt.Errorf("dbus: auth failed: %s", line)
	}
	_, err = conn.Write([]byte("BEGIN\r\n"))
	return err
}

func trimCRLF(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\r' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

func sendLine(conn net.Conn, s string) error {
	_, err := conn.Write(append([]byte(s), '\r', '\n'))
	return err
}

func sendLineBuf(conn net.Conn, a, b string) (int, error) {
	return conn.Write(append(append([]byte(a), []byte(b)...), '\r', '\n'))
}
