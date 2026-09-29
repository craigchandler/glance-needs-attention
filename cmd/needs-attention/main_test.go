package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServerClosesIncompleteHeaders(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newServer(listener.Addr().String(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		if err := <-done; err != http.ErrServerClosed {
			t.Errorf("Serve: %v", err)
		}
	})
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(8 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(conn, "GET /health HTTP/1.1\r\nHost: localhost\r\nX-Incomplete: "); err != nil {
		t.Fatal(err)
	}
	_, err = bufio.NewReader(conn).ReadByte()
	if err == nil {
		t.Fatal("unexpected response to incomplete headers")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("server did not close incomplete headers before the client deadline")
	}
}
