package resp

// Result define el contrato de salida del router.
// Si JSON es true, Body contiene un JSON serializado.
// Si Err != nil, el server enviará {"error","detail"} con Status.
type Result struct {
	Status int
	Body   string
	JSON   bool
	Err    *struct{ Code, Detail string }
}

// Constructores auxiliares para mantener consistencia en todo el árbol.
func PlainOK(body string) Result          { return Result{200, body, false, nil} }
func JSONOK(json string) Result           { return Result{200, json, true, nil} }
func BadReq(code, detail string) Result   { return Result{400, "", true, &struct{ Code, Detail string }{code, detail}} }
func NotFound(code, detail string) Result { return Result{404, "", true, &struct{ Code, Detail string }{code, detail}} }
func Conflict(code, detail string) Result { return Result{409, "", true, &struct{ Code, Detail string }{code, detail}} }
func TooMany(code, detail string) Result  { return Result{429, "", true, &struct{ Code, Detail string }{code, detail}} }
func IntErr(code, detail string) Result   { return Result{500, "", true, &struct{ Code, Detail string }{code, detail}} }
func Unavail(code, detail string) Result  { return Result{503, "", true, &struct{ Code, Detail string }{code, detail}} }
