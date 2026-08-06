// Client-side WebSocket dialing for the upstream Edge→Home connection.
// Plaintext ws:// is accepted only for loopback local development; remote
// upstreams remain fail-closed on WSS.
package edge

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type bufioReader = bufio.Reader

func dialWSS(ctx context.Context, rawURL, token string, tlsConfig *tls.Config) (net.Conn, *bufio.Reader, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	if u.Scheme != "wss" && u.Scheme != "ws" {
		return nil, nil, fmt.Errorf("upstream URL must be wss://, got %q", u.Scheme)
	}
	if u.Scheme == "ws" && !isLoopbackHost(u.Hostname()) {
		return nil, nil, fmt.Errorf("plaintext upstream URL is only allowed for loopback hosts, got %q", u.Hostname())
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "ws" {
			port = "80"
		} else {
			port = "443"
		}
	}
	host := net.JoinHostPort(u.Hostname(), port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	if u.Scheme == "ws" {
		conn, err = dialer.DialContext(ctx, "tcp", host)
	} else {
		conn, err = tlsConfigDial(ctx, dialer, host, tlsConfig, u.Hostname())
	}
	if err != nil {
		return nil, nil, err
	}
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		conn.Close()
		return nil, nil, err
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	_, err = fmt.Fprintf(conn,
		"GET %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n\r\n",
		path, u.Host, token, base64.StdEncoding.EncodeToString(key))
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, nil, &HandshakeError{StatusCode: response.StatusCode}
	}
	return conn, reader, nil
}

// HandshakeError is a non-101 upstream handshake response; StatusCode lets
// callers distinguish revoked credentials (401) from other failures.
type HandshakeError struct {
	StatusCode int
}

func (e *HandshakeError) Error() string {
	return fmt.Sprintf("upstream handshake status %d", e.StatusCode)
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSuffix(host, ".")) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func tlsConfigDial(ctx context.Context, dialer *net.Dialer, host string, tlsConfig *tls.Config, serverName string) (net.Conn, error) {
	raw, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	cfg := tlsConfig.Clone()
	if cfg.ServerName == "" {
		cfg.ServerName = serverName
	}
	tlsConn := tls.Client(raw, cfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return tlsConn, nil
}

// wsClientWrite sends one masked text frame (client frames must be masked).
func wsClientWrite(conn net.Conn, payload []byte) error {
	if len(payload) > 1<<20 {
		return fmt.Errorf("client frame exceeds limit")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	var frame []byte
	frame = append(frame, 0x81)
	switch {
	case len(payload) < 126:
		frame = append(frame, 0x80|byte(len(payload)))
	case len(payload) <= 0xffff:
		frame = append(frame, 0x80|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		frame = append(frame, 0x80|127)
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(payload)))
		frame = append(frame, n[:]...)
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	_, err := conn.Write(frame)
	return err
}

// wsClientRead reads one unmasked server text frame, answering pings.
func wsClientRead(reader *bufio.Reader) ([]byte, error) {
	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(reader, header); err != nil {
			return nil, err
		}
		if header[0]&0x80 == 0 {
			return nil, fmt.Errorf("fragmented frames unsupported")
		}
		opcode := header[0] & 0x0f
		if header[1]&0x80 != 0 {
			return nil, fmt.Errorf("server frame unexpectedly masked")
		}
		length := uint64(header[1] & 0x7f)
		switch length {
		case 126:
			var n [2]byte
			if _, err := io.ReadFull(reader, n[:]); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(n[:]))
		case 127:
			var n [8]byte
			if _, err := io.ReadFull(reader, n[:]); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(n[:])
		}
		if length > 1<<20 {
			return nil, fmt.Errorf("server frame exceeds limit")
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, err
		}
		switch opcode {
		case 0x1: // text
			return payload, nil
		case 0x8: // close
			return nil, io.EOF
		case 0x9, 0xA: // ping/pong: ping replies handled by server read deadline
			continue
		default:
			return nil, fmt.Errorf("unsupported opcode %d", opcode)
		}
	}
}
