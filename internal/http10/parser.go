package http10

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// Request modela el mínimo necesario de una petición HTTP/1.0.
// Nota: headers se normalizan a lower-case y no soporta claves repetidas.
type Request struct {
	Method string
	Target string
	Proto  string
	Header map[string]string
}

var (
	// ErrBadRequest cubre malformaciones: líneas sin CRLF, request-line inválida,
	// encabezados sin ":", fin de headers incorrecto, etc.
	ErrBadRequest = errors.New("malformed request (CRLF/fields)")
	// ErrBadProto se usa cuando la versión no es HTTP/1.0.
	ErrBadProto = errors.New("unsupported protocol (HTTP/1.0 only)")
)

// ParseRequest lee una petición HTTP/1.0 estricta desde r.
// Formato requerido:
//   request-line: "METHOD SP target SP HTTP/1.0 CRLF"
//   0..N header-lines terminadas en CRLF
//   línea en blanco CRLF que cierra los headers
func ParseRequest(r *bufio.Reader) (*Request, error) {
	// request-line
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(line, "\r\n") {
		return nil, ErrBadRequest
	}
	parts := strings.Split(strings.TrimRight(line, "\r\n"), " ")
	if len(parts) != 3 {
		return nil, ErrBadRequest
	}
	method, target, proto := parts[0], parts[1], parts[2]
	if proto != "HTTP/1.0" {
		return nil, ErrBadProto
	}

	// headers
	h := map[string]string{}
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil, ErrBadRequest
			}
			return nil, err
		}
		if l == "\r\n" {
			break // fin de headers
		}
		if !strings.HasSuffix(l, "\r\n") {
			return nil, ErrBadRequest
		}
		l = strings.TrimRight(l, "\r\n")
		kv := strings.SplitN(l, ":", 2)
		if len(kv) != 2 {
			return nil, ErrBadRequest
		}
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		val := strings.TrimSpace(kv[1])
		h[key] = val
	}

	return &Request{Method: method, Target: target, Proto: proto, Header: h}, nil
}
