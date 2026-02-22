package dbus

import (
	"encoding/binary"
)

// DecodeGetManagedObjects decodes body signature "a{oa{sa{sv}}}" into path -> iface -> prop -> variant.
func DecodeGetManagedObjects(body []byte) (map[ObjectPath]map[string]map[string]Variant, error) {
	if len(body) < 4 {
		return nil, nil
	}
	r := &wireReader{buf: body}
	r.align(4)
	arrLen := int(r.readUint32())
	if arrLen == 0 {
		return nil, nil
	}
	r.align(8)
	out := make(map[ObjectPath]map[string]map[string]Variant)
	for r.remaining() >= 8 {
		r.align(8)
		if r.remaining() < 4 {
			break
		}
		path := ObjectPath(r.readString())
		// value is a{sa{sv}}
		r.align(4)
		innerLen := int(r.readUint32())
		innerStart := r.pos
		ifaces := make(map[string]map[string]Variant)
		for r.pos < innerStart+innerLen && r.remaining() >= 8 {
			r.align(8)
			if r.remaining() < 4 {
				break
			}
			ifaceName := r.readString()
			r.align(4)
			propsLen := int(r.readUint32())
			propsStart := r.pos
			props := make(map[string]Variant)
			for r.pos < propsStart+propsLen && r.remaining() > 0 {
				r.align(8)
				if r.remaining() < 4 {
					break
				}
				propName := r.readString()
				// variant: 1 byte sig len, sig, value
				if r.remaining() < 1 {
					break
				}
				sigLen := int(r.buf[r.pos])
				r.pos++
				if r.remaining() < sigLen+1 {
					break
				}
				sig := string(r.buf[r.pos : r.pos+sigLen])
				r.pos += sigLen + 1
				val := decodeVariantValue(r, sig)
				props[propName] = Variant{Signature: sig, Value: val}
			}
			ifaces[ifaceName] = props
		}
		out[path] = ifaces
	}
	return out, nil
}

// DecodeBodyVariant decodes a body that is a single variant.
func DecodeBodyVariant(body []byte) (Variant, error) {
	if len(body) < 1 {
		return Variant{}, nil
	}
	r := &wireReader{buf: body}
	sigLen := int(r.buf[r.pos])
	r.pos++
	if r.pos+sigLen+1 > len(r.buf) {
		return Variant{}, nil
	}
	sig := string(r.buf[r.pos : r.pos+sigLen])
	r.pos += sigLen + 1
	val := decodeVariantValue(r, sig)
	return Variant{Signature: sig, Value: val}, nil
}

func decodeVariantValue(r *wireReader, sig string) any {
	if len(sig) == 0 {
		return nil
	}
	switch sig[0] {
	case 's', 'o':
		r.align(4)
		ln := binary.LittleEndian.Uint32(r.buf[r.pos:])
		r.pos += 4
		s := string(r.buf[r.pos : r.pos+int(ln)])
		r.pos += int(ln) + 1
		return s
	case 'b':
		r.align(4)
		v := binary.LittleEndian.Uint32(r.buf[r.pos:])
		r.pos += 4
		return v == 1
	case 'y':
		b := r.buf[r.pos]
		r.pos++
		return b
	case 'q', 'n':
		r.align(2)
		v := binary.LittleEndian.Uint16(r.buf[r.pos:])
		r.pos += 2
		return v
	case 'u', 'i':
		r.align(4)
		v := binary.LittleEndian.Uint32(r.buf[r.pos:])
		r.pos += 4
		return v
	case 'a':
		if len(sig) > 1 && sig[1] == 'y' {
			r.align(4)
			ln := int(r.readUint32())
			b := make([]byte, ln)
			copy(b, r.buf[r.pos:r.pos+ln])
			r.pos += ln
			return b
		}
		if len(sig) > 2 && sig[1] == 's' && sig[2] == '}' {
			// as - array of string
			r.align(4)
			ln := int(r.readUint32())
			end := r.pos + ln
			var out []string
			for r.pos < end && r.remaining() >= 4 {
				r.align(4)
				sln := int(binary.LittleEndian.Uint32(r.buf[r.pos:]))
				r.pos += 4
				out = append(out, string(r.buf[r.pos:r.pos+sln]))
				r.pos += sln + 1
			}
			return out
		}
		// a{sv} - skip
		r.align(4)
		ln := int(r.readUint32())
		r.align(8)
		r.pos += ln
		return nil
	default:
		return nil
	}
}

// DecodeSignalBodyInterfacesAdded decodes (o, a{sa{sv}}) for InterfacesAdded.
func DecodeSignalBodyInterfacesAdded(body []byte) (path string, ifaces map[string]map[string]Variant) {
	if len(body) < 4 {
		return "", nil
	}
	r := &wireReader{buf: body}
	r.align(4)
	path = r.readString()
	r.align(4)
	innerLen := int(r.readUint32())
	innerStart := r.pos
	ifaces = make(map[string]map[string]Variant)
	for r.pos < innerStart+innerLen && r.remaining() >= 8 {
		r.align(8)
		if r.remaining() < 4 {
			break
		}
		ifaceName := r.readString()
		r.align(4)
		propsLen := int(r.readUint32())
		propsStart := r.pos
		props := make(map[string]Variant)
		for r.pos < propsStart+propsLen && r.remaining() > 0 {
			r.align(8)
			if r.remaining() < 4 {
				break
			}
			propName := r.readString()
			if r.remaining() < 1 {
				break
			}
			sigLen := int(r.buf[r.pos])
			r.pos++
			if r.remaining() < sigLen+1 {
				break
			}
			sig := string(r.buf[r.pos : r.pos+sigLen])
			r.pos += sigLen + 1
			props[propName] = Variant{Signature: sig, Value: decodeVariantValue(r, sig)}
		}
		ifaces[ifaceName] = props
	}
	return path, ifaces
}

// DecodeSignalBodyPropertiesChanged decodes (s, a{sv}, as) for PropertiesChanged.
func DecodeSignalBodyPropertiesChanged(body []byte) (iface string, changed map[string]Variant) {
	if len(body) < 4 {
		return "", nil
	}
	r := &wireReader{buf: body}
	r.align(4)
	iface = r.readString()
	r.align(4)
	dictLen := int(r.readUint32())
	dictStart := r.pos
	changed = make(map[string]Variant)
	for r.pos < dictStart+dictLen && r.remaining() >= 8 {
		r.align(8)
		if r.remaining() < 4 {
			break
		}
		propName := r.readString()
		if r.remaining() < 1 {
			break
		}
		sigLen := int(r.buf[r.pos])
		r.pos++
		if r.remaining() < sigLen+1 {
			break
		}
		sig := string(r.buf[r.pos : r.pos+sigLen])
		r.pos += sigLen + 1
		changed[propName] = Variant{Signature: sig, Value: decodeVariantValue(r, sig)}
	}
	return iface, changed
}
