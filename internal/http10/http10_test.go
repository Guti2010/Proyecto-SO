package http10

import (
	"bufio"
	"errors"
	"io"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ---------- helpers ----------
type parsedResp struct {
	StatusLine string
	StatusCode int
	Reason     string
	Headers    map[string]string
	Body       string
}

func parseHTTP(raw string) parsedResp {
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
		col := strings.Index(ln, ":")
		if col < 0 {
			continue
		}
		k := ln[:col]
		v := strings.TrimSpace(ln[col+1:])
		h[k] = v
	}

	// "HTTP/1.0 200 OK"
	code := 0
	reason := ""
	if f := strings.Fields(sl); len(f) >= 3 {
		// f[1] es el código, f[2:] el reason
		// evita fallo si no es int
		if n, err := strconvAtoiSafe(f[1]); err == nil {
			code = n
		}
		reason = strings.Join(f[2:], " ")
	}

	return parsedResp{
		StatusLine: sl,
		StatusCode: code,
		Reason:     reason,
		Headers:    h,
		Body:       body,
	}
}

func strconvAtoiSafe(s string) (int, error) {
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, &strconvErr{s}
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

type strconvErr struct{ s string }
func (e *strconvErr) Error() string { return "bad int: " + e.s }

// ---------- SplitTarget ----------
func TestSplitTarget_Variants(t *testing.T) {
	cases := []struct {
		in         string
		wantPath   string
		wantQuery  string
	}{
		{"/hello?x=1&y=2", "/hello", "x=1&y=2"},
		{"/solo", "/solo", ""},
		{"/with-empty?", "/with-empty", ""},
		{"?onlyq=a=1", "", "onlyq=a=1"}, // path vacío con query
		{"", "", ""},
		{"/multi?one=1?two=2", "/multi", "one=1?two=2"}, // solo corta en el primer '?'
	}
	for _, tc := range cases {
		p, q := SplitTarget(tc.in)
		if p != tc.wantPath || q != tc.wantQuery {
			t.Fatalf("SplitTarget(%q) -> (%q,%q) want (%q,%q)",
				tc.in, p, q, tc.wantPath, tc.wantQuery)
		}
	}
}

// ---------- ParseQuery ----------
func TestParseQuery_Variants(t *testing.T) {
	m := ParseQuery("a=1&b=2")
	if m["a"] != "1" || m["b"] != "2" {
		t.Fatalf("basic: %+v", m)
	}
	// sin valor
	m = ParseQuery("a&b=2")
	if m["a"] != "" || m["b"] != "2" {
		t.Fatalf("no value: %+v", m)
	}
	// clave duplicada => último gana (por asignación sobre el mismo mapa)
	m = ParseQuery("k=1&k=2")
	if m["k"] != "2" {
		t.Fatalf("last wins: %+v", m)
	}
	// vacíos saltados
	m = ParseQuery("&&x=7&&")
	if m["x"] != "7" || len(m) != 1 {
		t.Fatalf("empty segments: %+v", m)
	}
	// vacío => mapa vacío
	m = ParseQuery("")
	if len(m) != 0 {
		t.Fatalf("empty query should be empty map: %+v", m)
	}
}

// ---------- write / WritePlainH / WriteJSONH / WriteErrorJSON ----------
func TestWritePlainH_Basics_And_ExtraOverride(t *testing.T) {
	var buf bytes.Buffer
	body := "hola mundo\n"
	extra := map[string]string{
		"X-Trace": "abc-123",
		"Server":  "override/1.0", // debe sobreescribir
	}
	WritePlainH(&buf, 200, body, extra)

	resp := parseHTTP(buf.String())

	if !strings.HasPrefix(resp.StatusLine, "HTTP/1.0 200 ") {
		t.Fatalf("status line: %q", resp.StatusLine)
	}
	if resp.Reason != "OK" {
		t.Fatalf("reason: %q", resp.Reason)
	}
	// Headers esenciales
	if ct := resp.Headers["Content-Type"]; ct != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type: %q", ct)
	}
	if cl := resp.Headers["Content-Length"]; cl != strconvItoa(len(body)) {
		t.Fatalf("Content-Length: %q want %d", cl, len(body))
	}
	if resp.Headers["Connection"] != "close" {
		t.Fatalf("Connection header missing/incorrect: %+v", resp.Headers)
	}
	if resp.Headers["Server"] != "override/1.0" {
		t.Fatalf("Server must be overridden: %q", resp.Headers["Server"])
	}
	if resp.Headers["X-Trace"] != "abc-123" {
		t.Fatalf("extra header missing")
	}
	// Date RFC1123
	if d := resp.Headers["Date"]; d == "" {
		t.Fatalf("Date header missing")
	} else if _, err := time.Parse(time.RFC1123, d); err != nil {
		t.Fatalf("Date not RFC1123: %q (%v)", d, err)
	}
	// Cuerpo
	if resp.Body != body {
		t.Fatalf("body mismatch: %q", resp.Body)
	}
}

func TestWriteJSONH_ContentType_And_Body(t *testing.T) {
	var buf bytes.Buffer
	payload := `{"x":1,"y":"ok"}`
	WriteJSONH(&buf, 200, payload, nil)

	pr := parseHTTP(buf.String())
	if pr.Headers["Content-Type"] != "application/json" {
		t.Fatalf("wrong content-type: %q", pr.Headers["Content-Type"])
	}
	if pr.Body != payload {
		t.Fatalf("body mismatch: %q", pr.Body)
	}
	// Content-Length correcto
	if pr.Headers["Content-Length"] != strconvItoa(len(payload)) {
		t.Fatalf("content-length mismatch: %v", pr.Headers["Content-Length"])
	}
}

func TestWriteErrorJSON_EscapesAndFormat(t *testing.T) {
	var buf bytes.Buffer
	WriteErrorJSON(&buf, 400, "bad_input", `detalle con "comillas"`, map[string]string{
		"X-Err": "1",
	})

	pr := parseHTTP(buf.String())
	if !strings.HasPrefix(pr.StatusLine, "HTTP/1.0 400 ") {
		t.Fatalf("status: %q", pr.StatusLine)
	}
	if pr.Headers["Content-Type"] != "application/json" {
		t.Fatalf("ct: %q", pr.Headers["Content-Type"])
	}
	if pr.Headers["X-Err"] != "1" {
		t.Fatalf("extra header missing")
	}
	// JSON válido y comillas escapadas
	var obj struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(pr.Body), &obj); err != nil {
		t.Fatalf("invalid json body: %v\nraw=%q", err, pr.Body)
	}
	if obj.Error != "bad_input" || obj.Detail != `detalle con "comillas"` {
		t.Fatalf("payload mismatch: %+v", obj)
	}
}

// ---------- statusText (códigos conocidos + fallback) ----------
func TestStatusText_AllKnownCodes(t *testing.T) {
	cases := map[int]string{
		200: "OK",
		400: "Bad Request",
		404: "Not Found",
		409: "Conflict",
		429: "Too Many Requests",
		500: "Internal Server Error",
		503: "Service Unavailable",
	}
	for code, want := range cases {
		if got := statusText(code); got != want {
			t.Fatalf("statusText(%d) = %q; want %q", code, got, want)
		}
	}
	// desconocidos deben caer en "OK" según la implementación actual
	unknowns := []int{418, 207, 451, 0, -1, 777}
	for _, code := range unknowns {
		if got := statusText(code); got != "OK" {
			t.Fatalf("statusText(%d) fallback = %q; want %q", code, got, "OK")
		}
	}
}

// ---------- WriteJSONH: UTF-8 y Content-Length ----------
func TestWriteJSONH_UTF8_ContentLength(t *testing.T) {
	var buf bytes.Buffer
	// Contenido con UTF-8 (len debe contar bytes, no runes)
	payload := `{"msg":"¡hola, mundo! ñ"}`
	WriteJSONH(&buf, 200, payload, map[string]string{"X-Test": "1"})

	pr := parseHTTP(buf.String())
	if pr.Headers["Content-Type"] != "application/json" {
		t.Fatalf("wrong content-type: %q", pr.Headers["Content-Type"])
	}
	if pr.Headers["X-Test"] != "1" {
		t.Fatalf("missing extra header")
	}
	if pr.Body != payload {
		t.Fatalf("body mismatch: %q", pr.Body)
	}
	if cl := pr.Headers["Content-Length"]; cl != strconvItoa(len(payload)) {
		t.Fatalf("Content-Length mismatch: %q want %d", cl, len(payload))
	}
}

// ---------- WriteErrorJSON: cuerpo exacto y Content-Length ----------
func TestWriteErrorJSON_ExactBody_And_Length(t *testing.T) {
	var buf bytes.Buffer
	code, detail := "bad_input", `detalle con "comillas"`
	expected := `{"error":"bad_input","detail":"detalle con \"comillas\""}`

	WriteErrorJSON(&buf, 400, code, detail, nil)
	pr := parseHTTP(buf.String())

	if !strings.HasPrefix(pr.StatusLine, "HTTP/1.0 400 ") {
		t.Fatalf("status: %q", pr.StatusLine)
	}
	if pr.Headers["Content-Type"] != "application/json" {
		t.Fatalf("ct: %q", pr.Headers["Content-Type"])
	}
	if pr.Body != expected {
		t.Fatalf("exact body mismatch:\n got: %q\nwant: %q", pr.Body, expected)
	}
	if pr.Headers["Content-Length"] != strconvItoa(len(expected)) {
		t.Fatalf("length mismatch: %v vs %d", pr.Headers["Content-Length"], len(expected))
	}
}


// ---------- mini strconv helpers (para no importar strconv) ----------
func strconvItoa(n int) string {
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
        b[i] = byte('0' + (n % 10))
		n /= 10
	}
	return sign + string(b[i:])
}

// ---------- ParseRequest ----------
func TestParseRequest_Valid_BodyLeftover(t *testing.T) {
	raw := "" +
		"GET /hello?x=1 HTTP/1.0\r\n" +
		"Host: example.com\r\n" +
		"User-Agent: Go-Test\r\n" +
		"X-Trace: 123\r\n" +
		"\r\n" +
		"BODY-IGNORED"
	r := bufio.NewReader(strings.NewReader(raw))

	req, err := ParseRequest(r)
	if err != nil {
		t.Fatalf("ParseRequest err: %v", err)
	}
	if req.Method != "GET" || req.Target != "/hello?x=1" || req.Proto != "HTTP/1.0" {
		t.Fatalf("req line mismatch: %+v", req)
	}
	// headers normalizados a lower-case
	if req.Header["host"] != "example.com" {
		t.Fatalf("host: %q", req.Header["host"])
	}
	if req.Header["user-agent"] != "Go-Test" {
		t.Fatalf("ua: %q", req.Header["user-agent"])
	}
	if req.Header["x-trace"] != "123" {
		t.Fatalf("x-trace: %q", req.Header["x-trace"])
	}
	// el cuerpo queda sin consumir por el parser
	rest, _ := io.ReadAll(r)
	if string(rest) != "BODY-IGNORED" {
		t.Fatalf("leftover body mismatch: %q", string(rest))
	}
}

func TestParseRequest_DuplicateHeader_LastWins(t *testing.T) {
	raw := "" +
		"GET / HTTP/1.0\r\n" +
		"X-Dup: one\r\n" +
		"X-Dup: two\r\n" + // último gana
		"\r\n"
	r := bufio.NewReader(strings.NewReader(raw))
	req, err := ParseRequest(r)
	if err != nil {
		t.Fatalf("ParseRequest err: %v", err)
	}
	if req.Header["x-dup"] != "two" {
		t.Fatalf("duplicate header last-wins failed: %+v", req.Header)
	}
}

func TestParseRequest_BadCRLF_InRequestLine(t *testing.T) {
	// Falta \r antes de \n en la request-line
	raw := "GET / HTTP/1.0\nHost: x\r\n\r\n"
	_, err := ParseRequest(bufio.NewReader(strings.NewReader(raw)))
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("want ErrBadRequest, got %v", err)
	}
}

func TestParseRequest_BadProto(t *testing.T) {
	raw := "GET / HTTP/1.1\r\n\r\n"
	_, err := ParseRequest(bufio.NewReader(strings.NewReader(raw)))
	if !errors.Is(err, ErrBadProto) {
		t.Fatalf("want ErrBadProto, got %v", err)
	}
}

func TestParseRequest_HeaderMissingColon(t *testing.T) {
	raw := "" +
		"GET / HTTP/1.0\r\n" +
		"BadHeader\r\n" + // sin ':'
		"\r\n"
	_, err := ParseRequest(bufio.NewReader(strings.NewReader(raw)))
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("want ErrBadRequest, got %v", err)
	}
}

func TestParseRequest_HeaderNoCRLF(t *testing.T) {
	// Header termina en '\n' pero no en "\r\n"
	raw := "" +
		"GET / HTTP/1.0\r\n" +
		"Host: example.com\n" + // <- mal
		"\r\n"
	_, err := ParseRequest(bufio.NewReader(strings.NewReader(raw)))
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("want ErrBadRequest, got %v", err)
	}
}

func TestParseRequest_EOFBeforeBlankLine(t *testing.T) {
	// Falta la línea en blanco final que cierra headers
	raw := "" +
		"GET / HTTP/1.0\r\n" +
		"Host: example.com\r\n"
	_, err := ParseRequest(bufio.NewReader(strings.NewReader(raw)))
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("want ErrBadRequest, got %v", err)
	}
}

func TestParseRequest_BadRequestLineParts(t *testing.T) {
	// Solo 2 partes -> debe fallar
	raw := "GET /only-two-parts\r\n"
	_, err := ParseRequest(bufio.NewReader(strings.NewReader(raw)))
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("want ErrBadRequest, got %v", err)
	}
}

func TestParseRequest_EmptyReader_PropagatesEOF(t *testing.T) {
	// Sin datos: el primer ReadString('\n') devuelve EOF y se propaga (no ErrBadRequest)
	_, err := ParseRequest(bufio.NewReader(strings.NewReader("")))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want io.EOF, got %v", err)
	}
}
