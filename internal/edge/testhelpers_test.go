package edge

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
)

// readServerTextOrErr is readServerText without the fatal, for drop checks.
func readServerTextOrErr(reader *bufio.Reader) ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	length := uint64(header[1] & 0x7f)
	if length == 126 {
		var n uint16
		if err := binary.Read(reader, binary.BigEndian, &n); err != nil {
			return nil, err
		}
		length = uint64(n)
	} else if length == 127 {
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			return nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// dialTestWebSocketExpectStatus dials and asserts the HTTP status, returning
// the raw connection for close (body already consumed).
func dialTestWebSocketExpectStatus(t *testing.T, serverURL, path, token string, wantStatus int) (net.Conn, error) {
	t.Helper()
	u, _ := url.Parse(serverURL)
	conn, err := tls.Dial("tcp", u.Host, &tls.Config{InsecureSkipVerify: true}) // test server only
	if err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", path, u.Host, token)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if response.StatusCode != wantStatus {
		conn.Close()
		return nil, fmt.Errorf("status = %d, want %d", response.StatusCode, wantStatus)
	}
	return conn, nil
}

func jsonContains(blob []byte, needle string) bool {
	var decoded any
	if err := json.Unmarshal(blob, &decoded); err != nil {
		return false
	}
	reencoded, _ := json.Marshal(decoded)
	return bytes.Contains(reencoded, []byte(needle)) || bytes.Contains(blob, []byte(needle))
}
