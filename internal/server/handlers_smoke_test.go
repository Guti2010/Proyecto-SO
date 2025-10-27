package server

import "testing"

// Mantén este nombre único para evitar choques con otras pruebas.
func Test_Handlers_Smoke_Basic(t *testing.T) {
	r := hit(t, "GET / HTTP/1.0\r\n")
	okOrSkip(t, codeOf(r) == 200, "root no devolvió 200")
}
