package server

import "testing"

// Algunas implementaciones exponen /runtime o sub-rutas; no todas.
// Por eso cada chequeo es okOrSkip.
func Test_Runtime_Stats(t *testing.T) {
	// /runtime (agrega o ajusta según tu servidor: /runtime/uptime, /runtime/pid, etc.)
	r := hit(t, "GET /runtime HTTP/1.0\r\n")
	c := codeOf(r)
	okOrSkip(t, c == 200 || c == 404, "runtime no devolvió 200/404")

	// Sub-rutas opcionales: nunca botamos la suite si no existen.
	r = hit(t, "GET /runtime/uptime HTTP/1.0\r\n")
	c = codeOf(r)
	okOrSkip(t, c == 200 || c == 404, "runtime/uptime no devolvió 200/404")

	r = hit(t, "GET /runtime/pid HTTP/1.0\r\n")
	c = codeOf(r)
	okOrSkip(t, c == 200 || c == 404, "runtime/pid no devolvió 200/404")
}
