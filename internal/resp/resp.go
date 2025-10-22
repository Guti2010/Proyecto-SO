package resp

// ErrObj es el error estándar que serializamos en JSON.
type ErrObj struct {
	Code   string `json:"error"`
	Detail string `json:"detail"`
}

// Result es el contrato de salida del router.
// Si JSON=true, Body ya es un JSON serializado.
// Si Err!=nil, el servidor enviará {"error","detail"} con Status.
type Result struct {
	Status  int
	Body    string
	JSON    bool
	Err     *ErrObj
	Headers map[string]string // headers extra (X-Worker-Id, etc.)
}

// WithHeader devuelve una copia de Result con un header adicional.
func (r Result) WithHeader(k, v string) Result {
	if r.Headers == nil {
		r.Headers = make(map[string]string, 1)
	}
	r.Headers[k] = v
	return r
}

// Constructores coherentes en todo el árbol:

func PlainOK(body string) Result        { return Result{Status: 200, Body: body, JSON: false} }
func JSONOK(json string) Result         { return Result{Status: 200, Body: json, JSON: true} }
func BadReq(code, d string) Result      { return Result{Status: 400, JSON: true, Err: &ErrObj{code, d}} }
func NotFound(code, d string) Result    { return Result{Status: 404, JSON: true, Err: &ErrObj{code, d}} }
func Conflict(code, d string) Result    { return Result{Status: 409, JSON: true, Err: &ErrObj{code, d}} }
func TooMany(code, d string) Result     { return Result{Status: 429, JSON: true, Err: &ErrObj{code, d}} }
func IntErr(code, d string) Result      { return Result{Status: 500, JSON: true, Err: &ErrObj{code, d}} }
func Unavail(code, d string) Result     { return Result{Status: 503, JSON: true, Err: &ErrObj{code, d}} }
