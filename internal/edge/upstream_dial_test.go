package edge

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"testing"
)

func TestDialWSSAllowsPlaintextLoopback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		request, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			serverErr <- err
			return
		}
		if request.URL.Path != "/edge/v1" {
			serverErr <- fmt.Errorf("path = %q, want /edge/v1", request.URL.Path)
			return
		}
		if request.Header.Get("Authorization") != "Bearer local-token" {
			serverErr <- fmt.Errorf("authorization header missing")
			return
		}
		_, err = fmt.Fprint(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		serverErr <- err
	}()

	conn, _, err := dialWSS(
		context.Background(),
		"ws://127.0.0.1:"+port+"/edge/v1",
		"local-token",
		&tls.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestDialWSSRejectsPlaintextNonLoopback(t *testing.T) {
	for _, host := range []string{"example.com", "127.0.0.2", "localhost.example"} {
		t.Run(host, func(t *testing.T) {
			_, _, err := dialWSS(context.Background(), "ws://"+host+"/edge/v1", "token", &tls.Config{})
			if err == nil {
				t.Fatal("dialWSS accepted plaintext non-loopback URL")
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for _, host := range []string{"localhost", "LOCALHOST", "127.0.0.1", "::1", "localhost."} {
		if !isLoopbackHost(host) {
			t.Errorf("isLoopbackHost(%q) = false, want true", host)
		}
	}
	for _, host := range []string{"127.0.0.2", "192.168.1.1", "localhost.example", ""} {
		if isLoopbackHost(host) {
			t.Errorf("isLoopbackHost(%q) = true, want false", host)
		}
	}
}
