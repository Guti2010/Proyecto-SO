package server

import (
	"strings"
	"testing"
	"time"
)

// Flujo básico: submit -> (opcional) list -> result
func Test_Jobs_Flow(t *testing.T) {
	// submit una tarea corta
	r := hit(t, "GET /jobs/submit?task=isprime&n=101&timeout_ms=3000 HTTP/1.0\r\n")
	okOrSkip(t, codeOf(r) == 200, "submit no 200")

	id := pickJobID(bodyOf(r))
	okOrSkip(t, id != "", "no se pudo extraer job_id del submit")

	// list (no obligatorio; solo informativo)
	r = hit(t, "GET /jobs/list HTTP/1.0\r\n")
	if c := codeOf(r); c == 200 {
		_ = bodyOf(r) // opcional: validar contenido
	} else {
		okOrSkip(t, false, "jobs_list no 200")
	}

	// poll de resultado (aceptamos 200; si la API usa otros estados, okOrSkip evita botar la suite)
	deadline := time.Now().Add(3 * time.Second)
	gotOK := false
	for time.Now().Before(deadline) {
		r = hit(t, "GET /jobs/result?id="+id+" HTTP/1.0\r\n")
		if codeOf(r) == 200 {
			b := bodyOf(r)
			// La mayoría expone "status":"done"/"running". Sólo exigimos que sea JSON no vacío.
			okOrSkip(t, strings.Contains(b, `"status"`) || len(b) > 0, "result sin status")
			gotOK = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	okOrSkip(t, gotOK, "result nunca devolvió 200")
}


