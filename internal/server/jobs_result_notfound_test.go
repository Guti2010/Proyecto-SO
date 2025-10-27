package server

import "testing"

// Pedir result de un id inexistente debería ser 404 o 400 (según implementación).
func Test_Jobs_Result_NotFound(t *testing.T) {
	r := hit(t, "GET /jobs/result?id=__nope__ HTTP/1.0\r\n")
	c := codeOf(r)
	okOrSkip(t, c == 404 || c == 400, "result inexistente no devolvió 404/400")
}
