package server

import (
	"testing"
)

// Intenta cancelar un ID inexistente; muchos servidores devuelven 404 (otros 400).
func Test_Jobs_Cancel_NotFound(t *testing.T) {
	r := hit(t, "GET /jobs/cancel?id=__no_such_id__ HTTP/1.0\r\n")

	c := codeOf(r)
	// Aceptamos 404 (no existe) o 400 (petición inválida en algunas implementaciones).
	okOrSkip(t, c == 404 || c == 400, "cancel de ID inexistente no devolvió 404/400")
}
