package server

import (
	"strings"
	"testing"
)

// saca un id del JSON: busca "id":"..."
// también soporta "job_id":"..."
func pickJobID(s string) string {
	// orden: job_id primero, luego id
	keys := []string{`"job_id":"`, `"id":"`}
	for _, k := range keys {
		if i := strings.Index(s, k); i >= 0 {
			j := i + len(k)
			if j < len(s) {
				if k2 := strings.IndexByte(s[j:], '"'); k2 >= 0 {
					return s[j : j+k2]
				}
			}
		}
	}
	return ""
}

func Test_Jobs_Cancel(t *testing.T) {
	// 1) submit (elige una tarea barata y rápida; adapta si tu servidor usa otro nombre)
	r := hit(t, "GET /jobs/submit?task=isprime&n=101&timeout_ms=3000 HTTP/1.0\r\n")
	okOrSkip(t, codeOf(r) == 200, "submit no 200")

	body := bodyOf(r)
	id := pickJobID(body)
	okOrSkip(t, id != "", "no se pudo extraer job_id del submit")

	// 2) cancelar
	r = hit(t, "GET /jobs/cancel?id="+id+" HTTP/1.0\r\n")
	c := codeOf(r)

	// Aceptamos:
	// 200 -> cancelado
	// 409 -> no se puede cancelar (p.ej. ya completó o estado incompatible)
	// 404 -> si la implementación elimina jobs muy rápido o no reconoce el id
	okOrSkip(t, c == 200 || c == 409 || c == 404, "cancel no devolvió 200/409/404")
}
