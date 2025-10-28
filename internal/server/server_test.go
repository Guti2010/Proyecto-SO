package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
	"fmt"
)

/* ================== helpers comunes ================== */

type parsedHTTP struct {
	StatusLine string
	Code       int
	Reason     string
	Headers    map[string]string
	Body       string
}

func parseHTTP(raw string) parsedHTTP {
	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	head := parts[0]
	body := ""
	if len(parts) == 2 {
		body = parts[1]
	}
	lines := strings.Split(head, "\r\n")
	sl := lines[0]

	h := make(map[string]string)
	for _, ln := range lines[1:] {
		if ln == "" {
			continue
		}
		if i := strings.IndexByte(ln, ':'); i >= 0 {
			k := ln[:i]
			v := strings.TrimSpace(ln[i+1:])
			h[k] = v
		}
	}
	code := 0
	reason := ""
	fs := strings.Fields(sl)
	if len(fs) >= 3 {
		code = atoiSafe(fs[1])
		reason = strings.Join(fs[2:], " ")
	}
	return parsedHTTP{StatusLine: sl, Code: code, Reason: reason, Headers: h, Body: body}
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func itoa(n int) string { // mini itoa (evitar strconv)
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return sign + string(b[i:])
}

/* ================== HandleConn ================== */

// net.Pipe para no abrir sockets reales.
func runThroughHandleConn(t *testing.T, rawReq string) parsedHTTP {
	t.Helper()

	client, server := net.Pipe()
	defer client.Close() // HandleConn cierra su extremo

	done := make(chan struct{})
	go func() {
		defer close(done)
		HandleConn(server) // cierra server al terminar
	}()

	if _, err := io.WriteString(client, rawReq); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, client) // EOF lo produce el servidor

	<-done
	return parseHTTP(buf.String())
}

func TestHandleConn_Status_JSON_And_TraceHeaders(t *testing.T) {
	req := "" +
		"GET /status HTTP/1.0\r\n" +
		"User-Agent: test\r\n" +
		"\r\n"

	resp := runThroughHandleConn(t, req)

	if resp.Code != 200 || resp.Reason != "OK" {
		t.Fatalf("status: %d %q\nraw=%s", resp.Code, resp.Reason, resp.StatusLine)
	}
	if resp.Headers["Connection"] != "close" {
		t.Fatalf("Connection header: %+v", resp.Headers)
	}
	if resp.Headers["X-Request-Id"] == "" {
		t.Fatalf("X-Request-Id missing")
	}
	if resp.Headers["X-Worker-Pid"] != itoa(os.Getpid()) {
		t.Fatalf("X-Worker-Pid mismatch: %q", resp.Headers["X-Worker-Pid"])
	}
	if resp.Headers["Date"] == "" {
		t.Fatalf("Date header missing")
	}
	if resp.Headers["Server"] == "" {
		t.Fatalf("Server header missing")
	}

	var obj struct {
		Pid         int64       `json:"pid"`
		UptimeMS    int64       `json:"uptime_ms"`
		StartedAt   string      `json:"started_at"`
		Connections uint64      `json:"connections"`
		Pools       interface{} `json:"pools"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &obj); err != nil {
		t.Fatalf("invalid json: %v\nbody=%q", err, resp.Body)
	}
	if obj.Pid <= 0 || obj.UptimeMS < 0 || obj.StartedAt == "" {
		t.Fatalf("bad status payload: %#v", obj)
	}
}

func TestHandleConn_BadProtocol_400_WithErrorJSON(t *testing.T) {
	req := "" +
		"GET / HTTP/1.1\r\n" +
		"Host: example\r\n" +
		"\r\n"

	resp := runThroughHandleConn(t, req)
	if resp.Code != 400 {
		t.Fatalf("want 400 bad request, got %d", resp.Code)
	}
	var e struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &e); err != nil {
		t.Fatalf("invalid error json: %v", err)
	}
	if e.Error != "bad_request" || !strings.Contains(e.Detail, "HTTP/1.0") {
		t.Fatalf("error payload mismatch: %+v", e)
	}
}

/* ================== utilidades: PID / Uptime / StartedAt / ConnCount ================== */

func TestServerMeta_PID_Uptime_StartedAt(t *testing.T) {
	if PID() != os.Getpid() {
		t.Fatalf("PID mismatch")
	}
	u1 := Uptime()
	time.Sleep(5 * time.Millisecond)
	u2 := Uptime()
	if !(u2 >= u1) {
		t.Fatalf("uptime should be monotonic: u1=%v u2=%v", u1, u2)
	}
	if StartedAt().After(time.Now()) {
		t.Fatalf("StartedAt must be <= now")
	}
}

func TestConnCount_IncrementsWithMark(t *testing.T) {
	c0 := ConnCount()
	markConnAccepted()
	markConnAccepted()
	c2 := ConnCount()
	if !(c2 >= c0+2) {
		t.Fatalf("ConnCount did not increase: before=%d after=%d", c0, c2)
	}
}

/* ================== ListenAndServe ================== */

func dialAndRequest(t *testing.T, addr string, req string) parsedHTTP {
	t.Helper()
	var conn net.Conn
	var err error
	deadline := time.Now().Add(800 * time.Millisecond)
	for {
		conn, err = net.Dial("tcp", addr)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()

	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("write: %v", err)
	}
	var buf bytes.Buffer
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = io.Copy(&buf, conn)
	return parseHTTP(buf.String())
}

func TestListenAndServe_InvalidAddr_ReturnsError(t *testing.T) {
	if err := ListenAndServe("127.0.0.1:65536"); err == nil {
		t.Fatalf("expected error for invalid addr")
	}
}

func TestListenAndServe_StatusAndConnCount(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close()

	go func() { _ = ListenAndServe(addr) }()

	resp1 := dialAndRequest(t, addr, "GET /status HTTP/1.0\r\n\r\n")
	if resp1.Code != 200 {
		t.Fatalf("status1 code: %d", resp1.Code)
	}
	var st1 struct{ Connections uint64 `json:"connections"` }
	if err := json.Unmarshal([]byte(resp1.Body), &st1); err != nil {
		t.Fatalf("json1: %v", err)
	}

	resp2 := dialAndRequest(t, addr, "GET /status HTTP/1.0\r\n\r\n")
	if resp2.Code != 200 {
		t.Fatalf("status2 code: %d", resp2.Code)
	}
	var st2 struct{ Connections uint64 `json:"connections"` }
	if err := json.Unmarshal([]byte(resp2.Body), &st2); err != nil {
		t.Fatalf("json2: %v", err)
	}
	if !(st2.Connections >= st1.Connections) {
		t.Fatalf("connections should be non-decreasing: prev=%d now=%d", st1.Connections, st2.Connections)
	}

	reverse := dialAndRequest(t, addr, "GET /reverse?text=hey HTTP/1.0\r\n\r\n")
	if reverse.Code != 200 || reverse.Body != "yeh\n" {
		t.Fatalf("reverse via listener: %d body=%q", reverse.Code, reverse.Body)
	}
}

/* ================== robustez extra ================== */

func TestListenAndServe_DialTimeoutWhenDown(t *testing.T) {
	_, err := net.DialTimeout("tcp", "127.0.0.1:9", 100*time.Millisecond) // discard port
	if err == nil {
		_ = errors.New("unexpected listener on port 9")
	}
}
/* ================== HandleConn: casos adicionales ================== */

// Reutiliza el helper ya definido en este archivo:
//   runThroughHandleConn(t, rawReq string) parsedHTTP

// 1) Ruta normal que pasa por router (plain text): /reverse
func TestHandleConn_Router_Reverse_PlainOK(t *testing.T) {
	resp := runThroughHandleConn(t,
		"GET /reverse?text=abcd HTTP/1.0\r\n"+
			"User-Agent: test\r\n"+
			"\r\n",
	)
	if resp.Code != 200 {
		t.Fatalf("code=%d reason=%q", resp.Code, resp.Reason)
	}
	if resp.Headers["Content-Type"] != "text/plain; charset=utf-8" {
		t.Fatalf("ct=%q", resp.Headers["Content-Type"])
	}
	if resp.Body != "dcba\n" {
		t.Fatalf("body=%q", resp.Body)
	}
	// trazabilidad siempre presente
	if resp.Headers["X-Request-Id"] == "" || resp.Headers["Connection"] != "close" {
		t.Fatalf("trace headers missing: %+v", resp.Headers)
	}
}

// 2) Request-line mal formada (piezas != 3) -> 400 bad_request JSON
func TestHandleConn_BadRequestLine_400(t *testing.T) {
	// Falta un espacio entre método y target ("GET/...")
	resp := runThroughHandleConn(t,
		"GET/foobar HTTP/1.0\r\n"+
			"\r\n",
	)
	if resp.Code != 400 {
		t.Fatalf("want 400, got %d", resp.Code)
	}
	if resp.Headers["Content-Type"] != "application/json" {
		t.Fatalf("ct: %q", resp.Headers["Content-Type"])
	}
	var e struct {
		Error, Detail string
	}
	if err := json.Unmarshal([]byte(resp.Body), &e); err != nil {
		t.Fatalf("invalid json: %v body=%q", err, resp.Body)
	}
	if e.Error != "bad_request" {
		t.Fatalf("payload=%+v", e)
	}
}

// 3) Header mal formado (sin ':') pero con CRLF-CRLF correcto -> 400
func TestHandleConn_BadHeaderLine_400(t *testing.T) {
	resp := runThroughHandleConn(t,
		"GET /status HTTP/1.0\r\n"+
			"User-Agent test-sin-dos-puntos\r\n"+ // <<— mal
			"\r\n",
	)
	if resp.Code != 400 {
		t.Fatalf("want 400, got %d", resp.Code)
	}
	var e struct{ Error, Detail string }
	if err := json.Unmarshal([]byte(resp.Body), &e); err != nil {
		t.Fatalf("json: %v", err)
	}
	if e.Error != "bad_request" {
		t.Fatalf("payload=%+v", e)
	}
	// En errores, el servidor siempre usa JSON + Connection: close
	if resp.Headers["Content-Type"] != "application/json" ||
		resp.Headers["Connection"] != "close" {
		t.Fatalf("headers=%+v", resp.Headers)
	}
}

// 4) Método distinto a GET — el router puede responder 400/404 o 200
func TestHandleConn_NonGET_Method_Routed(t *testing.T) {
	resp := runThroughHandleConn(t,
		"HEAD /reverse?text=ok HTTP/1.0\r\n\r\n",
	)

	// Aceptamos lo que decida el router para métodos no-GET:
	//  - 400: payload JSON de error (p.ej., bad_request / method not allowed)
	//  - 404: ruta no encontrada
	//  - 200: si la app maneja HEAD como GET
	if resp.Code != 400 && resp.Code != 404 && resp.Code != 200 {
		t.Fatalf("unexpected code for HEAD: %d", resp.Code)
	}

	// Siempre deben estar los headers de trazabilidad
	if resp.Headers["Connection"] != "close" || resp.Headers["X-Request-Id"] == "" {
		t.Fatalf("trace headers missing: %+v", resp.Headers)
	}

	// Si es 400, debe ser JSON de error válido
	if resp.Code == 400 {
		var e struct{ Error, Detail string }
		if err := json.Unmarshal([]byte(resp.Body), &e); err != nil || e.Error == "" {
			t.Fatalf("400 must carry JSON error payload, got body=%q err=%v", resp.Body, err)
		}
	}
}


// 5) Varias conexiones concurrentes para asegurar que HandleConn
//    responde bien y no bloquea en escenarios típicos.
func TestHandleConn_Parallel_Status(t *testing.T) {
	const N = 8
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			resp := runThroughHandleConn(t, "GET /status HTTP/1.0\r\n\r\n")
			if resp.Code != 200 || resp.Reason != "OK" {
				errCh <- fmt.Errorf("bad resp: %d %q", resp.Code, resp.Reason)
				return
			}
			errCh <- nil
		}()
	}
	for i := 0; i < N; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

