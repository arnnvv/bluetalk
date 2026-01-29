package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// RC_CHANNEL is the RFCOMM channel (1-30). Must be same on all devices.
const RC_CHANNEL = 4

// A thread-safe map to store connected clients (for the Host)
var (
	clients   = make(map[int]string) // Key: File Descriptor, Value: Name
	clientsMu sync.Mutex
)

func main() {
	fmt.Println("--- Bluetooth Chat (RFCOMM) ---")
	fmt.Print("Run as (H)ost or (C)lient? ")

	reader := bufio.NewReader(os.Stdin)
	mode, _ := reader.ReadString('\n')
	mode = strings.TrimSpace(strings.ToUpper(mode))

	if mode == "H" {
		runHost()
	} else if mode == "C" {
		fmt.Print("Enter Host MAC Address (XX:XX:XX:XX:XX:XX): ")
		mac, _ := reader.ReadString('\n')
		mac = strings.TrimSpace(mac)
		runClient(mac)
	} else {
		fmt.Println("Invalid mode.")
	}
}

func runHost() {
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_RFCOMM)
	if err != nil {
		log.Fatalf("Socket creation failed: %v", err)
	}
	defer unix.Close(fd)

	addr := &unix.SockaddrRFCOMM{Channel: RC_CHANNEL}
	// Address [0,0,0,0,0,0] means "Any Local Adapter"
	copy(addr.Addr[:], []byte{0, 0, 0, 0, 0, 0})

	if err := unix.Bind(fd, addr); err != nil {
		log.Fatalf("Bind failed: %v", err)
	}

	// Listen for connections
	if err := unix.Listen(fd, 1); err != nil {
		log.Fatalf("Listen failed: %v", err)
	}

	fmt.Printf("Hosting on Channel %d. Waiting for friends...\n", RC_CHANNEL)

	// Start a goroutine to read Host's typing and broadcast it
	go hostInputHandler()

	for {
		// Accept new connection
		nfd, sa, err := unix.Accept(fd)
		if err != nil {
			log.Println("Accept error:", err)
			continue
		}

		// Convert raw sockaddr to a readable string (rudimentary)
		// Go's unix package doesn't make converting MACs easy, so we just use an ID.
		clientID := nfd
		clientAddr := sa.(*unix.SockaddrRFCOMM).Addr
		macStr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
			clientAddr[5], clientAddr[4], clientAddr[3], clientAddr[2], clientAddr[1], clientAddr[0])

		fmt.Printf("\n[+] Connected: %s\n", macStr)

		clientsMu.Lock()
		clients[clientID] = macStr
		clientsMu.Unlock()

		// Handle this client in a new Goroutine
		go handleConnection(nfd, macStr)
	}
}

func handleConnection(fd int, name string) {
	defer func() {
		unix.Close(fd)
		clientsMu.Lock()
		delete(clients, fd)
		clientsMu.Unlock()
		fmt.Printf("\n[-] Disconnected: %s\n", name)
	}()

	buf := make([]byte, 1024)
	for {
		n, err := unix.Read(fd, buf)
		if err != nil || n == 0 {
			return
		}
		msg := string(buf[:n])
		fmt.Printf("\n[%s]: %s\nYou: ", name, msg)

		// Broadcast to others
		broadcast(msg, fd, name)
	}
}

func broadcast(msg string, senderFD int, senderName string) {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	formattedMsg := fmt.Sprintf("[%s]: %s", senderName, msg)

	for fd := range clients {
		if fd != senderFD {
			unix.Write(fd, []byte(formattedMsg))
		}
	}
}

func hostInputHandler() {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("You: ")
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)

		// Broadcast host message to all clients
		clientsMu.Lock()
		for fd := range clients {
			unix.Write(fd, []byte("Host: "+text))
		}
		clientsMu.Unlock()
	}
}

// --- CLIENT LOGIC ---

func runClient(mac string) {
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM, unix.BTPROTO_RFCOMM)
	if err != nil {
		log.Fatalf("Socket error: %v", err)
	}
	defer unix.Close(fd)

	// Parse MAC String to Byte Array
	addrBytes, err := parseMAC(mac)
	if err != nil {
		log.Fatalf("Invalid MAC: %v", err)
	}

	// Connect
	rsa := &unix.SockaddrRFCOMM{Channel: RC_CHANNEL}
	copy(rsa.Addr[:], addrBytes)

	fmt.Printf("Connecting to %s...\n", mac)
	if err := unix.Connect(fd, rsa); err != nil {
		log.Fatalf("Connection failed. Are you paired? Error: %v", err)
	}
	fmt.Println("Connected! Start typing.")

	// Goroutine to listen for incoming messages
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := unix.Read(fd, buf)
			if err != nil {
				fmt.Println("\nServer disconnected.")
				os.Exit(0)
			}
			fmt.Printf("\n%s\nYou: ", string(buf[:n]))
		}
	}()

	// Main loop to send messages
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("You: ")
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		_, err := unix.Write(fd, []byte(text))
		if err != nil {
			break
		}
	}
}

// Helper: Convert "AA:BB:CC:11:22:33" -> [6]byte (reversed for Little Endian)
func parseMAC(s string) ([]byte, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return nil, fmt.Errorf("invalid format")
	}
	var addr [6]byte
	// Bluetooth addresses are often stored in Little Endian in C structs,
	// so we reverse the order: input[0] goes to addr[5]
	for i := 0; i < 6; i++ {
		val, err := hexToByte(parts[i])
		if err != nil {
			return nil, err
		}
		addr[5-i] = val
	}
	return addr[:], nil
}

func hexToByte(s string) (byte, error) {
	var b byte
	_, err := fmt.Sscanf(s, "%x", &b)
	return b, err
}
