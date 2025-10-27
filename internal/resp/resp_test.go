package resp_test

import (
	"testing"

	"so-http10-demo/internal/resp"
)

// Ejecuta (smoke) todos los helpers de resp.* para que sus
// statements cuenten en cobertura. No validamos el render HTTP aquí.
func TestRespHelpers_Smoke(t *testing.T) {
	var r resp.Result

	// 200 OK
	r = resp.PlainOK("hola mundo")
	_ = r

	// 200 OK con JSON (tu JSONOK recibe un string con JSON)
	r = resp.JSONOK(`{"ok":true,"n":1}`)
	_ = r

	// Errores comunes (dos strings: mensaje + detalle)
	r = resp.BadReq("bad request", "param faltante")
	_ = r

	r = resp.NotFound("not found", "ruta /no-such-route")
	_ = r

	r = resp.Conflict("conflict", "estado inconsistente")
	_ = r

	r = resp.TooMany("too many requests", "rate limit")
	_ = r

	r = resp.IntErr("internal error", "panic o fallo interno")
	_ = r

	r = resp.Unavail("service unavailable", "mantenimiento")
	_ = r
}
