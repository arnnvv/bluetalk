package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	packetData byte = 0x01
	packetAck  byte = 0x02

	headerSize  = 4
	payloadSize = bleMTU - headerSize

	ackTimeout = 900 * time.Millisecond
	maxRetries = 5
)

type pendingAckKey struct {
	seq uint8
	idx uint8
}

type rxMessage struct {
	total     uint8
	fragments [][]byte
	createdAt time.Time
}

type Transport struct {
	peer *Peer

	recvCh   chan string
	statusCh chan string

	nextSeq atomic.Uint32

	ackMu       sync.Mutex
	pendingAcks map[pendingAckKey]chan struct{}

	rxMu       sync.Mutex
	reassembly map[uint8]*rxMessage
}

func NewTransport(peer *Peer, recvCh, statusCh chan string) *Transport {
	return &Transport{
		peer:        peer,
		recvCh:      recvCh,
		statusCh:    statusCh,
		pendingAcks: make(map[pendingAckKey]chan struct{}),
		reassembly:  make(map[uint8]*rxMessage),
	}
}

func (t *Transport) OnConnected() {
	t.ackMu.Lock()
	for key, ch := range t.pendingAcks {
		delete(t.pendingAcks, key)
		close(ch)
	}
	t.ackMu.Unlock()

	t.rxMu.Lock()
	clear(t.reassembly)
	t.rxMu.Unlock()
}

func (t *Transport) OnDisconnected() {
	t.OnConnected()
}

func (t *Transport) SendMessage(text string) error {
	data := []byte(text)
	if len(data) == 0 {
		return nil
	}

	total := (len(data) + payloadSize - 1) / payloadSize
	if total > 255 {
		return fmt.Errorf("message too large: max %d bytes", 255*payloadSize)
	}

	seq := uint8(t.nextSeq.Add(1) % 256)
	if seq == 0 {
		seq = 1
	}

	for i := range total {
		start := i * payloadSize
		end := start + payloadSize
		end = min(end, len(data))

		idx := uint8(i)
		packet := make([]byte, headerSize+(end-start))
		packet[0] = packetData
		packet[1] = seq
		packet[2] = uint8(total)
		packet[3] = idx
		copy(packet[4:], data[start:end])

		ackCh := t.registerAck(seq, idx)
		sent := false
		for range maxRetries {
			if err := t.peer.writeRaw(packet); err != nil {
				time.Sleep(250 * time.Millisecond)
				continue
			}

			select {
			case _, ok := <-ackCh:
				if ok {
					sent = true
				}
			case <-time.After(ackTimeout):
			}

			if sent {
				break
			}
		}
		t.unregisterAck(seq, idx)

		if !sent {
			return fmt.Errorf("delivery timeout (seq=%d, frag=%d)", seq, idx)
		}
	}

	return nil
}

func (t *Transport) OnReceivePacket(data []byte) {
	if len(data) < headerSize {
		return
	}

	typeByte := data[0]
	seq := data[1]
	total := data[2]
	idx := data[3]

	switch typeByte {
	case packetAck:
		t.signalAck(seq, idx)
	case packetData:
		ack := []byte{packetAck, seq, total, idx}
		_ = t.peer.writeRaw(ack)
		t.acceptData(seq, total, idx, data[4:])
	}
}

func (t *Transport) registerAck(seq, idx uint8) chan struct{} {
	t.ackMu.Lock()
	defer t.ackMu.Unlock()

	key := pendingAckKey{seq: seq, idx: idx}
	ch := make(chan struct{}, 1)
	t.pendingAcks[key] = ch
	return ch
}

func (t *Transport) unregisterAck(seq, idx uint8) {
	t.ackMu.Lock()
	defer t.ackMu.Unlock()
	delete(t.pendingAcks, pendingAckKey{seq: seq, idx: idx})
}

func (t *Transport) signalAck(seq, idx uint8) {
	t.ackMu.Lock()
	ch, ok := t.pendingAcks[pendingAckKey{seq: seq, idx: idx}]
	t.ackMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (t *Transport) acceptData(seq, total, idx uint8, payload []byte) {
	if total == 0 || idx >= total {
		return
	}

	t.rxMu.Lock()
	defer t.rxMu.Unlock()

	now := time.Now()
	for s, msg := range t.reassembly {
		if now.Sub(msg.createdAt) > 2*time.Minute {
			delete(t.reassembly, s)
		}
	}

	msg, ok := t.reassembly[seq]
	if !ok || msg.total != total {
		msg = &rxMessage{total: total, fragments: make([][]byte, total), createdAt: now}
		t.reassembly[seq] = msg
	}

	if msg.fragments[idx] == nil {
		frag := make([]byte, len(payload))
		copy(frag, payload)
		msg.fragments[idx] = frag
	}

	complete := true
	size := 0
	for i := 0; i < int(msg.total); i++ {
		if msg.fragments[i] == nil {
			complete = false
			break
		}
		size += len(msg.fragments[i])
	}
	if !complete {
		return
	}

	full := make([]byte, 0, size)
	for i := 0; i < int(msg.total); i++ {
		full = append(full, msg.fragments[i]...)
	}
	delete(t.reassembly, seq)

	select {
	case t.recvCh <- string(full):
	default:
	}
}
