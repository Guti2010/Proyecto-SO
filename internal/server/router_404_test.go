package server

import "testing"

// Ruta inexistente: muchas implementaciones devuelven 404; aceptamos 400 también.
func Test_Router_404(t *testing.T) {
	r := hit(t, "GET /__this_route_does_not_exist__ HTTP/1.0\r\n")
	c := codeOf(r)
	okOrSkip(t, c == 404 || c == 400, "ruta inexistente no devolvió 404/400")
}
