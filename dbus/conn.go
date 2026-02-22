package dbus

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
)

type Conn struct {
	conn       net.Conn
	serial     uint32
	uniqueName string
	mu         sync.Mutex
	pending    map[uint32]chan *parsedMsg
	sigCh      chan *Signal
	closed     bool
	readErr    error
}

func ConnectSystemBus() (*Conn, error) {
	addr := getSystemBusAddress()
	nc, err := connectUnix(addr)
	if err != nil {
		return nil, err
	}
	if err := auth(nc); err != nil {
		nc.Close()
		return nil, err
	}
	c := &Conn{
		conn:    nc,
		pending: make(map[uint32]chan *parsedMsg),
		sigCh:   make(chan *Signal, 16),
	}
	serial := c.nextSerial()
	msg := buildMethodCall(serial, "/org/freedesktop/DBus", "org.freedesktop.DBus", "Hello", "org.freedesktop.DBus")
	if _, err := nc.Write(msg); err != nil {
		nc.Close()
		return nil, err
	}
	reply, err := c.waitReply(serial)
	if err != nil {
		nc.Close()
		return nil, err
	}
	if reply.Type != msgMethodReturn {
		nc.Close()
		return nil, fmt.Errorf("dbus: Hello failed")
	}
	if len(reply.Body) >= 4 {
		r := &wireReader{buf: reply.Body}
		r.align(4)
		c.uniqueName = r.readString()
	}
	go c.readLoop()
	return c, nil
}

func (c *Conn) nextSerial() uint32 {
	return atomic.AddUint32(&c.serial, 1)
}

func (c *Conn) waitReply(serial uint32) (*parsedMsg, error) {
	ch := make(chan *parsedMsg, 1)
	c.mu.Lock()
	c.pending[serial] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, serial)
		c.mu.Unlock()
	}()
	msg := <-ch
	if msg == nil {
		return nil, c.readErr
	}
	return msg, nil
}

func (c *Conn) readLoop() {
	for {
		msg, err := readMessage(c.conn)
		if err != nil {
			if err != io.EOF {
				c.readErr = err
			}
			c.mu.Lock()
			for _, ch := range c.pending {
				select {
				case ch <- nil:
				default:
				}
			}
			c.closed = true
			c.mu.Unlock()
			close(c.sigCh)
			return
		}
		switch msg.Type {
		case msgMethodReturn, msgError:
			c.mu.Lock()
			ch := c.pending[msg.ReplySerial]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				default:
				}
			}
		case msgSignal:
			sig := &Signal{Path: ObjectPath(msg.Path), Interface: msg.Interface, Member: msg.Member}
			if len(msg.Body) > 0 {
				sig.Body = decodeSignalBody(msg.Member, msg.Body)
			}
			select {
			case c.sigCh <- sig:
			default:
			}
		}
	}
}

func decodeSignalBody(member string, body []byte) []any {
	switch member {
	case "InterfacesAdded":
		path, ifaces := DecodeSignalBodyInterfacesAdded(body)
		return []any{path, ifaces}
	case "PropertiesChanged":
		iface, changed := DecodeSignalBodyPropertiesChanged(body)
		return []any{iface, changed}
	}
	return nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.conn.Close()
}

func (c *Conn) Signal() chan *Signal {
	return c.sigCh
}

func (c *Conn) Object(dest string, path ObjectPath) *Object {
	return &Object{conn: c, dest: dest, path: string(path)}
}

func (c *Conn) BusObject() *Object {
	return c.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
}

type Object struct {
	conn *Conn
	dest string
	path string
}

func (o *Object) Call(method string, flags int, args ...any) *Call {
	iface := ""
	member := method
	for i := len(method) - 1; i >= 0; i-- {
		if method[i] == '.' {
			iface = method[:i]
			member = method[i+1:]
			break
		}
	}
	serial := o.conn.nextSerial()
	var msg []byte
	switch len(args) {
	case 0:
		msg = buildMethodCall(serial, o.path, iface, member, o.dest)
	case 1:
		if s, ok := args[0].(string); ok {
			msg = buildMethodCallWithBody(serial, o.path, iface, member, o.dest, "s", buildBodyString(s))
		} else if m, ok := args[0].(map[string]any); ok {
			w := &wireWriter{}
			w.writeBodyDictSV(m)
			msg = buildMethodCallWithBody(serial, o.path, iface, member, o.dest, "a{sv}", w.buf)
		}
	case 2:
		if data, ok := args[0].([]byte); ok {
			opts, _ := args[1].(map[string]any)
			body := buildBodyAyAndDict(data, opts)
			msg = buildMethodCallWithBody(serial, o.path, iface, member, o.dest, "aya{sv}", body)
		} else if a, ok := args[0].(string); ok {
			b, _ := args[1].(string)
			msg = buildMethodCallWithBody(serial, o.path, iface, member, o.dest, "ss", buildBodySS(a, b))
		}
	}
	if msg == nil {
		return &Call{Err: fmt.Errorf("dbus: unsupported call")}
	}
	o.conn.mu.Lock()
	if o.conn.closed {
		o.conn.mu.Unlock()
		return &Call{Err: os.ErrClosed}
	}
	_, err := o.conn.conn.Write(msg)
	o.conn.mu.Unlock()
	if err != nil {
		return &Call{Err: err}
	}
	reply, err := o.conn.waitReply(serial)
	if err != nil {
		return &Call{Err: err}
	}
	if reply.Type == msgError {
		return &Call{Err: fmt.Errorf("dbus error")}
	}
	return &Call{Reply: reply}
}

type Call struct {
	Reply *parsedMsg
	Err   error
}

func (c *Call) Store(dst ...any) error {
	if c.Err != nil {
		return c.Err
	}
	if c.Reply == nil || len(dst) == 0 {
		return nil
	}
	if len(c.Reply.Body) == 0 {
		return nil
	}
	switch p := dst[0].(type) {
	case *map[ObjectPath]map[string]map[string]Variant:
		m, err := DecodeGetManagedObjects(c.Reply.Body)
		if err != nil {
			return err
		}
		*p = m
		return nil
	case *Variant:
		v, err := DecodeBodyVariant(c.Reply.Body)
		if err != nil {
			return err
		}
		*p = v
		return nil
	}
	return fmt.Errorf("dbus: Store not implemented for %T", dst[0])
}
