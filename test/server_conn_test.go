package test

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"so-http10-demo/internal/server"
)

func TestServer_HandleConn_OK_404_400(t *testing.T) {
	// 200 OK + trazabilidad
	{
		s, c := net.Pipe(); defer s.Close(); defer c.Close()
		go server.HandleConn(s)
		_, _ = c.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
		br := bufio.NewReader(c)
		if l, _ := br.ReadString('\n'); l != "HTTP/1.0 200 OK\r\n" { t.Fatalf("status: %q", l) }
		hasReq, hasPid := false, false
		for { h, _ := br.ReadString('\n'); if h == "\r\n" { break }
			ll := strings.ToLower(h); hasReq = hasReq || strings.HasPrefix(ll, "x-request-id:"); hasPid = hasPid || strings.HasPrefix(ll, "x-worker-pid:")
		}
		if !hasReq || !hasPid { t.Fatal("missing trace headers") }
	}

	// 404 con JSON
	{
		s, c := net.Pipe(); defer s.Close(); defer c.Close()
		go server.HandleConn(s)
		_, _ = c.Write([]byte("GET /nope HTTP/1.0\r\n\r\n"))
		br := bufio.NewReader(c)
		if l, _ := br.ReadString('\n'); l != "HTTP/1.0 404 Not Found\r\n" { t.Fatalf("status: %q", l) }
		for { h, _ := br.ReadString('\n'); if h == "\r\n" { break } }
		body, _ := br.ReadString('\n')
		if err := json.Unmarshal([]byte(body), &map[string]any{}); err != nil { t.Fatalf("json: %v %q", err, body) }
	}

	// 400 por CRLF incorrecto
	{
		s, c := net.Pipe(); defer s.Close(); defer c.Close()
		go server.HandleConn(s)
		_, _ = c.Write([]byte("GET / HTTP/1.0\n\n"))
		br := bufio.NewReader(c)
		if l, _ := br.ReadString('\n'); l != "HTTP/1.0 400 Bad Request\r\n" { t.Fatalf("status: %q", l) }
	}
}


func TestServer_JSONSuccessBranch_Status(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close(); defer c.Close()

	go server.HandleConn(s)
	_, _ = c.Write([]byte("GET /status HTTP/1.0\r\n\r\n"))

	br := bufio.NewReader(c)
	if line, _ := br.ReadString('\n'); line != "HTTP/1.0 200 OK\r\n" {
		t.Fatalf("status: %q", line)
	}
	// salta headers
	for {
		h, _ := br.ReadString('\n')
		if h == "\r\n" { break }
	}
	// cuerpo debe ser JSON parseable
	body, _ := br.ReadString('\n')
	if err := json.Unmarshal([]byte(body), &map[string]any{}); err != nil {
		t.Fatalf("status body json: %v %q", err, body)
	}
}
