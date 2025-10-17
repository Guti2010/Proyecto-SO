package test

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"so-http10-demo/internal/http10"
)

func TestWritePlainAndErrorJSON(t *testing.T) {
	var b bytes.Buffer
	http10.WritePlainH(&b, 200, "hola\n", map[string]string{"X-Request-Id": "abc"})
	r := bufio.NewReader(&b)
	if s, _ := r.ReadString('\n'); s != "HTTP/1.0 200 OK\r\n" { t.Fatalf("status: %q", s) }

	hasCT, hasCL, hasConn, hasReq := false, false, false, false
	for {
		h, _ := r.ReadString('\n'); if h == "\r\n" { break }
		l := strings.ToLower(h)
		hasCT = hasCT || strings.HasPrefix(l, "content-type: text/plain")
		hasCL = hasCL || strings.HasPrefix(l, "content-length: 5")
		hasConn = hasConn || strings.HasPrefix(l, "connection: close")
		hasReq = hasReq || strings.HasPrefix(l, "x-request-id: abc")
	}
	if !(hasCT && hasCL && hasConn && hasReq) { t.Fatal("missing headers") }
	if body, _ := r.ReadString('\n'); body != "hola\n" { t.Fatalf("body: %q", body) }

	// error json
	b.Reset()
	http10.WriteErrorJSON(&b, 400, "bad", "detail", nil)
	r = bufio.NewReader(&b)
	if s, _ := r.ReadString('\n'); s != "HTTP/1.0 400 Bad Request\r\n" { t.Fatalf("status: %q", s) }
}

func TestStatusTextBranches(t *testing.T) {
	check := func(code int, expect string) {
		var b bytes.Buffer
		http10.WritePlainH(&b, code, "", nil)
		br := bufio.NewReader(&b)
		if s, _ := br.ReadString('\n'); s != expect { t.Fatalf("want %q got %q", expect, s) }
	}
	check(404, "HTTP/1.0 404 Not Found\r\n")
	check(409, "HTTP/1.0 409 Conflict\r\n")
	check(429, "HTTP/1.0 429 Too Many Requests\r\n")
	check(500, "HTTP/1.0 500 Internal Server Error\r\n")
	check(503, "HTTP/1.0 503 Service Unavailable\r\n")
}

func TestStatusText_DefaultBranch(t *testing.T) {
	var b bytes.Buffer
	http10.WritePlainH(&b, 201, "", nil) // 201 no está en switch → "OK" por default
	br := bufio.NewReader(&b)
	if s, _ := br.ReadString('\n'); s != "HTTP/1.0 201 OK\r\n" {
		t.Fatalf("default statusText: %q", s)
	}
}

func TestEscapeJSON_QuotesInDetail(t *testing.T) {
	var b bytes.Buffer
	http10.WriteErrorJSON(&b, 400, "bad", `det"ail`, nil)
	br := bufio.NewReader(&b)
	_, _ = br.ReadString('\n') // status
	// consume headers
	for {
		h, _ := br.ReadString('\n')
		if h == "\r\n" { break }
	}
	body, _ := br.ReadString('\n')
	if !strings.Contains(body, `\"`) {
		t.Fatalf("expected escaped quote in body: %q", body)
	}
}

func TestEscapeJSON_DoubleQuotesEscaped(t *testing.T) {
	var b bytes.Buffer
	http10.WriteErrorJSON(&b, 400, "bad", `x"y"z`, nil)
	br := bufio.NewReader(&b)
	_, _ = br.ReadString('\n') // status
	for {
		h, _ := br.ReadString('\n')
		if h == "\r\n" { break }
	}
	body, _ := br.ReadString('\n')
	if strings.Count(body, `\"`) < 2 {
		t.Fatalf("expected two escaped quotes in %q", body)
	}
}
