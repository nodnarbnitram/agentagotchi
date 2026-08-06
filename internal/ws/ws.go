package ws

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	opText   = 0x1
	opClose  = 0x8
	opPing   = 0x9
	opPong   = 0xA
	maxFrame = 1 << 20
)

type Conn struct {
	net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
}

func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !headerContains(r.Header, "Connection", "upgrade") ||
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("not a websocket upgrade")
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, errors.New("unsupported websocket version")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, errors.New("missing websocket key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("http server does not support hijacking")
	}
	raw, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	accept := websocketAccept(key)
	if _, err := fmt.Fprintf(rw,
		"HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
		raw.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		raw.Close()
		return nil, err
	}
	return &Conn{Conn: raw, reader: rw.Reader}, nil
}

func (c *Conn) ReadJSON(v any) error {
	payload, err := c.ReadText()
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, v); err != nil {
		return fmt.Errorf("decode websocket JSON: %w", err)
	}
	return nil
}

// ReadText reads one complete text frame while servicing control frames.
func (c *Conn) ReadText() ([]byte, error) {
	for {
		opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case opText:
			return payload, nil
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return nil, err
			}
		case opClose:
			_ = c.writeFrame(opClose, nil)
			return nil, io.EOF
		case opPong:
			continue
		default:
			return nil, fmt.Errorf("unsupported websocket opcode %d", opcode)
		}
	}
}

func (c *Conn) WriteJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(opText, b)
}

// WriteText writes already-encoded protocol bytes without re-marshalling.
func (c *Conn) WriteText(payload []byte) error { return c.writeFrame(opText, payload) }

func (c *Conn) Ping() error {
	return c.writeFrame(opPing, []byte("pet"))
}

func (c *Conn) readFrame() (byte, []byte, error) {
	_ = c.SetReadDeadline(time.Now().Add(45 * time.Second))
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.reader, header); err != nil {
		return 0, nil, err
	}
	if header[0]&0x80 == 0 {
		return 0, nil, errors.New("fragmented websocket frames are not supported")
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	if !masked {
		return 0, nil, errors.New("client websocket frame is not masked")
	}
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		buf := make([]byte, 2)
		if _, err := io.ReadFull(c.reader, buf); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(buf))
	case 127:
		buf := make([]byte, 8)
		if _, err := io.ReadFull(c.reader, buf); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(buf)
	}
	if length > maxFrame {
		return 0, nil, errors.New("websocket frame exceeds limit")
	}
	mask := make([]byte, 4)
	if _, err := io.ReadFull(c.reader, mask); err != nil {
		return 0, nil, err
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return opcode, payload, nil
}

func (c *Conn) writeFrame(opcode byte, payload []byte) error {
	if len(payload) > maxFrame {
		return errors.New("websocket frame exceeds limit")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	var frame bytes.Buffer
	frame.WriteByte(0x80 | opcode)
	switch {
	case len(payload) < 126:
		frame.WriteByte(byte(len(payload)))
	case len(payload) <= 0xffff:
		frame.WriteByte(126)
		_ = binary.Write(&frame, binary.BigEndian, uint16(len(payload)))
	default:
		frame.WriteByte(127)
		_ = binary.Write(&frame, binary.BigEndian, uint64(len(payload)))
	}
	frame.Write(payload)
	_, err := c.Write(frame.Bytes())
	return err
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func headerContains(h http.Header, name, token string) bool {
	for _, value := range h.Values(name) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
