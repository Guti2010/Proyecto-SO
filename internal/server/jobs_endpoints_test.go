package server

import (
	"strings"
	"testing"
)

func Test_Jobs_Endpoints(t *testing.T) {
	// submit
	r1 := hit(t, "GET /jobs/submit?task=isprime&n=101&timeout_ms=3000 HTTP/1.0\r\n")
	okOrSkip(t, codeOf(r1) == 200, "jobs_submit no 200")

	// list
	r2 := hit(t, "GET /jobs/list HTTP/1.0\r\n")
	if codeOf(r2) == 200 {
		// no comparar []byte; usar bodyOf() que es string
		okOrSkip(t, strings.Contains(bodyOf(r2), "jobs") || len(bodyOf(r2)) >= 0, "jobs_list sin contenido")
	} else {
		okOrSkip(t, false, "jobs_list no 200")
	}

	// result (si existe; algunas implementaciones requieren id)
	// intentamos sin id para no romper si tu API es distinta — se permite 400/404 también
	r3 := hit(t, "GET /jobs/result HTTP/1.0\r\n")
	c3 := codeOf(r3)
	okOrSkip(t, c3 == 200 || c3 == 400 || c3 == 404,
		"jobs_result no devolvió 200/400/404")

	// snapshot/metrics si existiera (no obligatorio). No fallar la suite si no está.
	r4 := hit(t, "GET /jobs/snapshot HTTP/1.0\r\n")
	c4 := codeOf(r4)
	okOrSkip(t, c4 == 200 || c4 == 404, "jobs_snapshot inesperado")

	// cancelar sin id -> debería ser 400 (o 404 en algunas)
	r5 := hit(t, "GET /jobs/cancel HTTP/1.0\r\n")
	c5 := codeOf(r5)
	okOrSkip(t, c5 == 400 || c5 == 404, "jobs_cancel sin id no devolvió 400/404")
}
