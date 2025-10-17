package test

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"

	"so-http10-demo/internal/http10"
)

// ----- helpers -----
type errAtReader struct {
	data []byte
	n    int
}

func (e *errAtReader) Read(p []byte) (int, error) {
	if len(e.data) == 0 {
		return 0, io.EOF
	}
	if e.n >= 0 && len(e.data) > e.n {
		copy(p, e.data[:e.n])
		e.data = e.data[e.n:]
		e.n = -1
		return 0, errors.New("boom")
	}
	copied := copy(p, e.data)
	e.data = e.data[copied:]
	if len(e.data) == 0 {
		return copied, io.EOF
	}
	return copied, nil
}

// ----- tests -----

func TestParseRequest_OK_Basic(t *testing.T) {
	raw := "GET /p?q=1 HTTP/1.0\r\nHost: x\r\n\r\n"
	req, err := http10.ParseRequest(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Method != "GET" || req.Target != "/p?q=1" || req.Proto != "HTTP/1.0" {
		t.Fatalf("bad parse: %+v", req)
	}
	if req.Header["host"] != "x" {
		t.Fatalf("header miss: %+v", req.Header)
	}
}

func TestParseRequest_HTTP11Rejected(t *testing.T) {
	raw := "GET / HTTP/1.1\r\n\r\n"
	if _, err := http10.ParseRequest(bufio.NewReader(strings.NewReader(raw))); err == nil {
		t.Fatalf("expected error for HTTP/1.1")
	}
}

func TestParseRequest_MissingCRLF(t *testing.T) {
	raw := "GET / HTTP/1.0\n\n"
	if _, err := http10.ParseRequest(bufio.NewReader(strings.NewReader(raw))); err == nil {
		t.Fatalf("expected error for missing CRLF")
	}
}

func TestParseRequest_BadHeader_NoColon(t *testing.T) {
	raw := "GET / HTTP/1.0\r\nBadHeader\r\n\r\n"
	if _, err := http10.ParseRequest(bufio.NewReader(strings.NewReader(raw))); err == nil {
		t.Fatalf("expected error for malformed header")
	}
}

func TestParseRequest_HeaderWithoutCRLF(t *testing.T) {
	raw := "GET / HTTP/1.0\r\nBad: header-without-crlf"
	if _, err := http10.ParseRequest(bufio.NewReader(strings.NewReader(raw))); err == nil {
		t.Fatalf("expected error for header without CRLF")
	}
}

func TestParseRequest_EOFBeforeBlankLine(t *testing.T) {
	raw := "GET / HTTP/1.0\r\nHost: x\r\n" // falta CRLF de cierre de headers
	if _, err := http10.ParseRequest(bufio.NewReader(strings.NewReader(raw))); err == nil {
		t.Fatalf("expected error for EOF before blank line")
	}
}

func TestParseRequest_RequestLineFieldCountWrong(t *testing.T) {
	raw := "GET /onlytwo\r\n\r\n" // sólo 2 campos
	if _, err := http10.ParseRequest(bufio.NewReader(strings.NewReader(raw))); err == nil {
		t.Fatalf("expected error for bad request-line field count")
	}
	raw = "GET   /   HTTP/1.0\r\n\r\n" // split no exacto
	if _, err := http10.ParseRequest(bufio.NewReader(strings.NewReader(raw))); err == nil {
		t.Fatalf("expected error for malformed request-line with extra spaces")
	}
}

func TestParseRequest_ErrorWhileReadingHeaders_NonEOF(t *testing.T) {
	raw := "GET / HTTP/1.0\r\nHeader: x\r\n"
	rd := &errAtReader{data: []byte(raw), n: len(raw) - 1}
	if _, err := http10.ParseRequest(bufio.NewReader(rd)); err == nil {
		t.Fatalf("expected non-EOF error during headers")
	}
}

func TestParseRequest_MultiHeaders_Normalized(t *testing.T) {
	raw := "GET / HTTP/1.0\r\nHost: ExAmPlE\r\nX-Id: 1\r\n\r\n"
	req, err := http10.ParseRequest(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if req.Header["host"] != "ExAmPlE" || req.Header["x-id"] != "1" {
		t.Fatalf("normalization failed: %#v", req.Header)
	}
}
