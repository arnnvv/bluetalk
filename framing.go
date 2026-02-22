package main

import (
	"encoding/binary"
	"fmt"
	"sync"
)

const (
	MTU           = 20
	HeaderSize    = 8
	MaxPayloadLen = MTU - HeaderSize
)

// Fragment represents a single BLE chunk (max 20 bytes)
type Fragment struct {
	MessageID      uint32
	TotalFragments uint16
	FragmentIndex  uint16
	Payload        []byte
}

// EncodeMessage chunks a payload into multiple fragments
func EncodeMessage(msgID uint32, data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	total := uint16((len(data) + MaxPayloadLen - 1) / MaxPayloadLen)
	var frags [][]byte
	for i := uint16(0); i < total; i++ {
		start := int(i) * MaxPayloadLen
		end := start + MaxPayloadLen
		if end > len(data) {
			end = len(data)
		}
		chunk := data[start:end]
		buf := make([]byte, HeaderSize+len(chunk))
		binary.BigEndian.PutUint32(buf[0:4], msgID)
		binary.BigEndian.PutUint16(buf[4:6], total)
		binary.BigEndian.PutUint16(buf[6:8], i)
		copy(buf[8:], chunk)
		frags = append(frags, buf)
	}
	return frags
}

// EncodeAck creates an ACK message for a given MessageID
func EncodeAck(msgID uint32) []byte {
	buf := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(buf[0:4], msgID)
	binary.BigEndian.PutUint16(buf[4:6], 0) // Total=0 means ACK
	binary.BigEndian.PutUint16(buf[6:8], 0)
	return buf
}

// DecodeFragment parses a BLE chunk into a Fragment struct
func DecodeFragment(data []byte) (*Fragment, error) {
	if len(data) < HeaderSize {
		return nil, fmt.Errorf("fragment too small")
	}
	f := &Fragment{
		MessageID:      binary.BigEndian.Uint32(data[0:4]),
		TotalFragments: binary.BigEndian.Uint16(data[4:6]),
		FragmentIndex:  binary.BigEndian.Uint16(data[6:8]),
		Payload:        make([]byte, len(data)-HeaderSize),
	}
	copy(f.Payload, data[8:])
	return f, nil
}

// Reassembler handles putting fragments back together into complete messages
type Reassembler struct {
	mu       sync.Mutex
	messages map[uint32]*messageBuffer
}

type messageBuffer struct {
	totalFragments uint16
	fragments      map[uint16][]byte
}

func NewReassembler() *Reassembler {
	return &Reassembler{
		messages: make(map[uint32]*messageBuffer),
	}
}

// AddFragment adds a fragment and returns the full message payload if complete
func (r *Reassembler) AddFragment(f *Fragment) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	msgBuf, exists := r.messages[f.MessageID]
	if !exists {
		msgBuf = &messageBuffer{
			totalFragments: f.TotalFragments,
			fragments:      make(map[uint16][]byte),
		}
		r.messages[f.MessageID] = msgBuf
	}

	msgBuf.fragments[f.FragmentIndex] = f.Payload

	if len(msgBuf.fragments) == int(msgBuf.totalFragments) {
		var fullMessage []byte
		for i := uint16(0); i < msgBuf.totalFragments; i++ {
			fullMessage = append(fullMessage, msgBuf.fragments[i]...)
		}
		delete(r.messages, f.MessageID)
		return fullMessage
	}

	return nil
}
