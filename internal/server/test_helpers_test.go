// internal/server/test_helpers.go
package server

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// okOrSkip soporta bool ó string como segundo argumento:
//
//   okOrSkip(t, condBool, "mensaje opcional")
//   okOrSkip(t, "nombre-del-endpoint")    // salta con "nombre-del-endpoint not available"
//
func okOrSkip(t *testing.T, v any, msg ...string) {
	t.Helper()
	switch x := v.(type) {
	case bool:
		if !x {
			m := "skipped"
			if len(msg) > 0 {
				m = msg[0]
			}
			t.Skip(m)
		}
	case string:
		// llamadas que hacían okOrSkip(t, "help"), etc.
		t.Skipf("%s not available", x)
	default:
		// por compatibilidad: no saltar
	}
}

// hit envía una petición HTTP/1.0 al handler del servidor usando net.Pipe
// y devuelve la respuesta cruda (incluyendo headers).
func hit(t *testing.T, req string) []byte {
	t.Helper()

	// Asegura doble CRLF al final de la request
	if !strings.HasSuffix(req, "\r\n\r\n") {
		req += "\r\n\r\n"
	}

	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })

	// Lado "servidor"
	done := make(chan struct{})
	go func() {
		// deadline para que no quede colgado
		_ = c1.SetDeadline(time.Now().Add(5 * time.Second))
		HandleConn(c1)
		close(done)
	}()

	// Lado "cliente" escribe la request
	if _, err := io.WriteString(c2, req); err != nil {
		t.Fatalf("write request: %v", err)
	}

	// lee todo el response
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, c2); err != nil && !errorsIsClosed(err) {
		t.Fatalf("read response: %v", err)
	}
	<-done

	return buf.Bytes()
}

func errorsIsClosed(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "use of closed network connection") || strings.Contains(s, "closed pipe")
}

// bodyOf extrae el cuerpo del response (lo que viene después del \r\n\r\n).
func bodyOf(r []byte) string {
	i := bytes.Index(r, []byte("\r\n\r\n"))
	if i < 0 {
		return ""
	}
	return string(r[i+4:])
}

// codeOf retorna el status code (ej. 200, 404) de "HTTP/1.0 200 ..." en la primera línea.
func codeOf(r []byte) int {
	br := bufio.NewReader(bytes.NewReader(r))
	line, _ := br.ReadString('\n') // primera línea
	parts := strings.Fields(line)
	if len(parts) >= 2 {
		if n := parseInt(parts[1]); n > 0 {
			return n
		}
	}
	return 0
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// must200 falla el test si el response no trae 200.
func must200(t *testing.T, name string, r []byte) {
	t.Helper()
	if codeOf(r) != 200 {
		t.Fatalf("%s: want HTTP/1.0 200, got: %s", name, string(r))
	}
}
