package dbus

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Header field codes
const (
	fieldPath        = 1
	fieldInterface   = 2
	fieldMember      = 3
	fieldReplySerial = 5
	fieldDestination = 6
	fieldSignature   = 8
)

const (
	msgMethodCall   = 1
	msgMethodReturn = 2
	msgError        = 3
	msgSignal       = 4
	byteOrderLittle = 'l'
	protoVersion    = 1
)

type wireWriter struct {
	buf []byte
	pos int
}

func (w *wireWriter) align(n int) {
	for (w.pos)%n != 0 {
		w.buf = append(w.buf, 0)
		w.pos++
	}
}

func (w *wireWriter) writeByte(v byte) {
	w.buf = append(w.buf, v)
	w.pos++
}

func (w *wireWriter) writeUint32(v uint32) {
	w.align(4)
	w.buf = binary.LittleEndian.AppendUint32(w.buf, v)
	w.pos += 4
}

func (w *wireWriter) writeString(s string) {
	w.align(4)
	w.buf = binary.LittleEndian.AppendUint32(w.buf, uint32(len(s)))
	w.buf = append(w.buf, s...)
	w.buf = append(w.buf, 0)
	w.pos += 4 + len(s) + 1
}

func (w *wireWriter) writeSignature(s string) {
	w.buf = append(w.buf, byte(len(s)))
	w.buf = append(w.buf, s...)
	w.buf = append(w.buf, 0)
	w.pos += 1 + len(s) + 1
}

// writeVariantString writes a variant containing a string (signature "s").
func (w *wireWriter) writeVariantString(s string) {
	w.writeSignature("s")
	w.writeString(s)
}

// writeVariantPath writes a variant containing an object path (signature "o").
func (w *wireWriter) writeVariantPath(o string) {
	w.writeSignature("o")
	w.writeString(o)
}

// writeVariantSignature writes a variant containing a signature (signature "g").
func (w *wireWriter) writeVariantSignature(g string) {
	w.writeSignature("g")
	w.writeSignature(g)
}

func (w *wireWriter) writeHeaderField(code byte, sig string, writeVal func()) {
	w.buf = append(w.buf, code)
	w.pos++
	w.writeSignature(sig)
	writeVal()
}

// writeBodyBytes writes body signature "ay" (byte array).
func (w *wireWriter) writeBodyBytes(data []byte) {
	w.align(4)
	w.buf = binary.LittleEndian.AppendUint32(w.buf, uint32(len(data)))
	w.buf = append(w.buf, data...)
	w.pos += 4 + len(data)
}

// writeBodyDictSV writes body "a{sv}" (dict string -> variant).
func (w *wireWriter) writeBodyDictSV(m map[string]any) {
	w.align(4)
	start := len(w.buf) + 4
	for k, v := range m {
		w.align(8) // dict entry
		w.writeString(k)
		switch x := v.(type) {
		case string:
			w.writeVariantString(x)
		case []string:
			w.writeSignature("as")
			w.align(4)
			arrStart := len(w.buf) + 4
			for _, s := range x {
				w.writeString(s)
			}
			w.rewriteLenAt(arrStart-4, uint32(len(w.buf)-arrStart))
		}
	}
	w.rewriteLenAt(start-4, uint32(len(w.buf)-start))
}

func (w *wireWriter) rewriteLenAt(off int, ln uint32) {
	binary.LittleEndian.PutUint32(w.buf[off:off+4], ln)
}

// buildMethodCall builds a METHOD_CALL message (no body).
func buildMethodCall(serial uint32, path, iface, member, dest string) []byte {
	w := &wireWriter{}
	w.writeByte(byteOrderLittle)
	w.writeByte(msgMethodCall)
	w.writeByte(0)
	w.writeByte(protoVersion)
	bodyLen := 0
	w.writeUint32(uint32(bodyLen))
	w.writeUint32(serial)
	// Header fields array: (Path, Interface, Member, Destination) as (byte, variant)
	fieldsStart := len(w.buf)
	w.writeUint32(0) // placeholder
	w.align(8)
	w.writeHeaderField(fieldPath, "o", func() { w.writeString(path) })
	w.writeHeaderField(fieldInterface, "s", func() { w.writeString(iface) })
	w.writeHeaderField(fieldMember, "s", func() { w.writeString(member) })
	w.writeHeaderField(fieldDestination, "s", func() { w.writeString(dest) })
	fieldsLen := len(w.buf) - fieldsStart - 4
	w.rewriteLenAt(fieldsStart, uint32(fieldsLen))
	// Pad header to 8-byte boundary
	for len(w.buf)%8 != 0 {
		w.buf = append(w.buf, 0)
	}
	return w.buf
}

// buildMethodCallWithBody builds METHOD_CALL with pre-built body.
func buildMethodCallWithBody(serial uint32, path, iface, member, dest, bodySig string, body []byte) []byte {
	w := &wireWriter{}
	w.writeByte(byteOrderLittle)
	w.writeByte(msgMethodCall)
	w.writeByte(0)
	w.writeByte(protoVersion)
	bodyLen := len(body)
	w.writeUint32(uint32(bodyLen))
	w.writeUint32(serial)
	fieldsStart := len(w.buf)
	w.writeUint32(0)
	w.align(8)
	w.writeHeaderField(fieldPath, "o", func() { w.writeString(path) })
	w.writeHeaderField(fieldInterface, "s", func() { w.writeString(iface) })
	w.writeHeaderField(fieldMember, "s", func() { w.writeString(member) })
	w.writeHeaderField(fieldDestination, "s", func() { w.writeString(dest) })
	if bodySig != "" {
		w.writeHeaderField(fieldSignature, "g", func() { w.writeSignature(bodySig) })
	}
	fieldsLen := len(w.buf) - fieldsStart - 4
	w.rewriteLenAt(fieldsStart, uint32(fieldsLen))
	for len(w.buf)%8 != 0 {
		w.buf = append(w.buf, 0)
	}
	w.buf = append(w.buf, body...)
	return w.buf
}

// buildBodyAyAndDict builds body "aya{sv}" for WriteValue.
func buildBodyAyAndDict(data []byte, opts map[string]any) []byte {
	w := &wireWriter{}
	w.writeBodyBytes(data)
	w.writeBodyDictSV(opts)
	return w.buf
}

// buildBodyString builds body "s" (single string).
func buildBodyString(s string) []byte {
	w := &wireWriter{}
	w.align(4)
	w.writeString(s)
	return w.buf
}

// buildBodySS builds body "ss" (two strings).
func buildBodySS(a, b string) []byte {
	w := &wireWriter{}
	w.align(4)
	w.writeString(a)
	w.writeString(b)
	return w.buf
}

// --- Reader ---

type wireReader struct {
	buf []byte
	pos int
}

func (r *wireReader) align(n int) {
	for (r.pos)%n != 0 {
		r.pos++
	}
}

func (r *wireReader) readByte() byte {
	b := r.buf[r.pos]
	r.pos++
	return b
}

func (r *wireReader) readUint32() uint32 {
	r.align(4)
	v := binary.LittleEndian.Uint32(r.buf[r.pos:])
	r.pos += 4
	return v
}

func (r *wireReader) readString() string {
	r.align(4)
	ln := r.readUint32()
	s := string(r.buf[r.pos : r.pos+int(ln)])
	r.pos += int(ln) + 1 // +1 nul
	return s
}

func (r *wireReader) readSignature() string {
	ln := int(r.buf[r.pos])
	r.pos++
	s := string(r.buf[r.pos : r.pos+ln])
	r.pos += ln + 1
	return s
}

func (r *wireReader) remaining() int {
	if r.pos > len(r.buf) {
		return 0
	}
	return len(r.buf) - r.pos
}

// parsedMsg holds a received message.
type parsedMsg struct {
	Type        byte
	Serial      uint32
	ReplySerial uint32
	Path        string
	Interface   string
	Member      string
	Body        []byte
}

func readMessage(rd io.Reader) (*parsedMsg, error) {
	// Read fixed header: byte order, type, flags, version, body_len, serial
	h := make([]byte, 16)
	if _, err := io.ReadFull(rd, h); err != nil {
		return nil, err
	}
	if h[0] != byteOrderLittle {
		return nil, fmt.Errorf("dbus: unsupported byte order")
	}
	msg := &parsedMsg{
		Type:   h[1],
		Serial: binary.LittleEndian.Uint32(h[8:12]),
	}
	bodyLen := binary.LittleEndian.Uint32(h[4:8])
	// Header fields array length (at offset 12)
	fieldsLen := binary.LittleEndian.Uint32(h[12:16])
	headerRest := make([]byte, int(fieldsLen))
	if _, err := io.ReadFull(rd, headerRest); err != nil {
		return nil, err
	}
	rf := &wireReader{buf: headerRest}
	for rf.remaining() > 0 {
		rf.align(8)
		if rf.remaining() < 1 {
			break
		}
		code := rf.readByte()
		sig := rf.readSignature()
		switch code {
		case fieldPath:
			msg.Path = rf.readString()
		case fieldInterface:
			msg.Interface = rf.readString()
		case fieldMember:
			msg.Member = rf.readString()
		case fieldReplySerial:
			// variant (sig "u") = uint32
			rf.align(4)
			msg.ReplySerial = binary.LittleEndian.Uint32(rf.buf[rf.pos:])
			rf.pos += 4
		default:
			skipVariant(rf, sig)
		}
	}
	// Header padding to 8
	totalHeader := 16 + int(fieldsLen)
	for totalHeader%8 != 0 {
		totalHeader++
	}
	if totalHeader > 16+int(fieldsLen) {
		pad := make([]byte, totalHeader-16-int(fieldsLen))
		io.ReadFull(rd, pad)
	}
	// Body
	if bodyLen > 0 {
		msg.Body = make([]byte, bodyLen)
		if _, err := io.ReadFull(rd, msg.Body); err != nil {
			return nil, err
		}
	}
	return msg, nil
}

func skipVariant(r *wireReader, sig string) {
	if len(sig) == 0 {
		return
	}
	switch sig[0] {
	case 's', 'o':
		r.align(4)
		ln := binary.LittleEndian.Uint32(r.buf[r.pos:])
		r.pos += 4 + int(ln) + 1
	case 'g':
		r.readSignature()
	case 'u', 'i':
		r.align(4)
		r.pos += 4
	case 'a':
		r.align(4)
		ln := binary.LittleEndian.Uint32(r.buf[r.pos:])
		r.pos += 4
		r.align(8)
		r.pos += int(ln)
	default:
		// skip 1 byte for unknown
		r.pos++
	}
}
