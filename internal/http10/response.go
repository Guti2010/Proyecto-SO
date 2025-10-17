package http10

import (
	"fmt"
	"io"
	"maps"
	"time"
)

// write compone una respuesta HTTP/1.0 incluyendo Content-Length y Connection: close.
// Acepta cabeceras adicionales (p. ej., trazabilidad) que se mezclan con las estándar.
func write(w io.Writer, status int, contentType string, body string, extra map[string]string) {
	headers := map[string]string{
		"Date":           time.Now().UTC().Format(time.RFC1123),
		"Content-Type":   contentType,
		"Content-Length": fmt.Sprintf("%d", len(body)),
		"Connection":     "close",
		"Server":         "so-http10/0.2",
	}
	if extra != nil {
		maps.Copy(headers, extra)
	}

	io.WriteString(w, fmt.Sprintf("HTTP/1.0 %d %s\r\n", status, statusText(status)))
	for k, v := range headers {
		io.WriteString(w, fmt.Sprintf("%s: %s\r\n", k, v))
	}
	io.WriteString(w, "\r\n")
	io.WriteString(w, body)
}

// WritePlainH escribe una respuesta de texto plano con cabeceras extra.
func WritePlainH(w io.Writer, status int, body string, extra map[string]string) {
	write(w, status, "text/plain; charset=utf-8", body, extra)
}

// WriteJSONH escribe una respuesta JSON (string ya serializado) con cabeceras extra.
func WriteJSONH(w io.Writer, status int, json string, extra map[string]string) {
	write(w, status, "application/json", json, extra)
}

// WriteErrorJSON serializa un payload uniforme de error:
// {"error":"<code>","detail":"<detalle>"} con el status indicado.
func WriteErrorJSON(w io.Writer, status int, code, detail string, extra map[string]string) {
	payload := fmt.Sprintf("{\"error\":\"%s\",\"detail\":\"%s\"}", code, escapeJSON(detail))
	WriteJSONH(w, status, payload, extra)
}

// escapeJSON escapa comillas dobles del detail para mantener JSON válido.
// (Este servidor no depende de librerías de alto nivel by design.)
func escapeJSON(s string) string {
	out := ""
	for _, r := range s {
		if r == '"' {
			out += "\\\""
		} else {
			out += string(r)
		}
	}
	return out
}

func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 400:
		return "Bad Request"
	case 404:
		return "Not Found"
	case 409:
		return "Conflict"
	case 429:
		return "Too Many Requests"
	case 500:
		return "Internal Server Error"
	case 503:
		return "Service Unavailable"
	default:
		return "OK"
	}
}
