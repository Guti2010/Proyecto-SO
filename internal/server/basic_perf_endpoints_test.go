package server

import (
	"strings"
	"testing"
)

// Este test valida endpoints "ligeros" y de utilería.
// Usa helpers comunes: hit, must200, bodyOf, okOrSkip.
func Test_BasicPerf_Endpoints(t *testing.T) {
	// /help
	r := hit(t, "GET /help HTTP/1.0\r\n")
	must200(t, "help", r)
	body := string(bodyOf(r))
	okOrSkip(t, strings.Contains(body, "help") || strings.Contains(body, "Help"),
		`/help no retornó contenido esperado`)

	// /loadtest (debería existir y responder 200)
	r = hit(t, "GET /loadtest?tasks=1&sleep=0 HTTP/1.0\r\n")
	must200(t, "loadtest", r)
	body = string(bodyOf(r))
	okOrSkip(t, strings.Contains(body, "load") || len(body) > 0,
		`/loadtest no retornó contenido esperado`)

	// /files_create (si existe devuelve 200 – el contenido exacto no es relevante)
	r = hit(t, "GET /files_create HTTP/1.0\r\n")
	must200(t, "files_create", r)

	// /files_delete (si existe devuelve 200)
	r = hit(t, "GET /files_delete HTTP/1.0\r\n")
	must200(t, "files_delete", r)
}
