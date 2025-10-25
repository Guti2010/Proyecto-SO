package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"so-http10-demo/internal/resp"
)

// ===============================================================
//  Handlers básicos (sin bloqueo prolongado)
//  - Cada handler exportado devuelve resp.Result (con código HTTP,
//    body y formato) y valida parámetros.
//  - La lógica “pura” está en funciones core no exportadas,
//    fáciles de testear y reusar.
// ===============================================================

// -------------------------------------------------
// Helpers "core" (puros) — NO exportados
//   * No hacen validaciones ni devuelven resp.Result.
//   * No conocen de HTTP ni de errores de usuario.
// -------------------------------------------------

// boot se usa si alguna vez quieres reportar uptime aquí.
// (Actualmente /status vive fuera de este archivo.)
var boot = time.Now()

// timestampCore construye un JSON con epoch Unix y fecha UTC.
// No valida nada ni conoce de HTTP.
func timestampCore() string {
	now := time.Now().UTC()
	out := map[string]any{
		"unix": now.Unix(),
		"utc":  now.Format(time.RFC3339),
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// reverseCore invierte el texto como runas (UTF-8 seguro) y agrega "\n".
func reverseCore(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r) + "\n"
}

// toUpperCore convierte a MAYÚSCULAS y agrega "\n".
func toUpperCore(s string) string {
	return strings.ToUpper(s) + "\n"
}

// hashCore calcula SHA-256 del texto y lo devuelve como JSON {algo, hex}.
func hashCore(text string) string {
	sum := sha256.Sum256([]byte(text))
	b, _ := json.Marshal(map[string]string{
		"algo": "sha256",
		"hex":  hex.EncodeToString(sum[:]),
	})
	return string(b)
}

// randomCore genera n enteros uniformes en [min, max] y los devuelve en JSON.
// PRECONDICIONES (garantizadas por el wrapper):
//   - n >= 1
//   - min <= max
func randomCore(n, min, max int) string {
	rand.Seed(time.Now().UnixNano())
	arr := make([]int, n)
	span := max - min + 1
	for i := 0; i < n; i++ {
		arr[i] = rand.Intn(span) + min
	}
	b, _ := json.Marshal(map[string]any{"values": arr})
	return string(b)
}

// fibonacciCore devuelve el N-ésimo Fibonacci como string con "\n".
// Complejidad O(n) y espacio O(1).
// PRECONDICIÓN: n >= 0 (el wrapper valida).
func fibonacciCore(n int) string {
	if n < 0 {
		// Mensaje defensivo si alguien llama core sin validar.
		return "error: num debe ser >=0\n"
	}
	if n == 0 {
		return "0\n"
	}
	if n == 1 {
		return "1\n"
	}
	a, b := 0, 1
	for i := 2; i <= n; i++ {
		a, b = b, a+b
	}
	return fmt.Sprintf("%d\n", b)
}

// -------------------------------------------------
// API principal (exportada) — lo que llama el router
//   * Siempre valida parámetros.
//   * Devuelve resp.Result con códigos y mensajes coherentes.
// -------------------------------------------------

// Help devuelve el listado de rutas disponibles.
// Formato: texto plano (200).
func Help() resp.Result {
	return resp.PlainOK(strings.TrimSpace(`
/                      -> hola mundo
/help                  -> este listado
/status                -> estado del proceso + pools (pid, uptime, conns, colas, workers)
/metrics               -> métricas por pool (latencias, colas, workers, contadores)

/fibonacci?num=N       -> N-ésimo (iterativo)
/reverse?text=abc      -> invierte texto
/toupper?text=abc      -> a MAYÚSCULAS
/random?count=n&min=a&max=b -> n enteros aleatorios
/timestamp             -> JSON con epoch/UTC
/hash?text=abc         -> SHA-256 (hex)

/createfile?name=FILE&content=txt&repeat=x
/deletefile?name=FILE

# Pools / simulación
/sleep?seconds=s
/simulate?seconds=s&task=sleep|spin
/loadtest?tasks=n&sleep=s

# CPU-bound
/isprime?n=NUM
/factor?n=NUM
/pi?digits=D[&algo=spigot|chudnovsky][&timeout_ms=T]
/mandelbrot?width=W&height=H&max_iter=I
/matrixmul?size=N&seed=S

# IO-bound
/wordcount?name=FILE
/grep?name=FILE&pattern=REGEX
/hashfile?name=FILE[&algo=sha256]
/sortfile?name=FILE[&algo=merge|quick][&chunksize=N]
/compress?name=FILE[&codec=gzip]   (xz no disponible)

/jobs/submit?task=TASK&<params>[&timeout_ms=MS][&prio=low|normal|high]
/jobs/status?id=JOBID
/jobs/result?id=JOBID
/jobs/cancel?id=JOBID
/jobs/list
`) + "\n")
}

// Timestamp devuelve JSON con epoch y UTC.
// 200 + JSON; no requiere parámetros.
func Timestamp(_ map[string]string) resp.Result {
	return resp.JSONOK(timestampCore())
}

// Reverse invierte el texto recibido en ?text=... (UTF-8 seguro).
// Errores:
//   - 400 missing_param si falta text.
func Reverse(params map[string]string) resp.Result {
	txt, ok := params["text"]
	if !ok {
		return resp.BadReq("missing_param", "text is required")
	}
	return resp.PlainOK(reverseCore(txt))
}

// ToUpper convierte a MAYÚSCULAS el parámetro ?text=...
// Errores:
//   - 400 missing_param si falta text.
func ToUpper(params map[string]string) resp.Result {
	txt, ok := params["text"]
	if !ok {
		return resp.BadReq("missing_param", "text is required")
	}
	return resp.PlainOK(toUpperCore(txt))
}

// Hash calcula SHA-256 del parámetro ?text=... y devuelve JSON con {algo, hex}.
// Errores:
//   - 400 missing_param si falta text.
func Hash(params map[string]string) resp.Result {
	txt, ok := params["text"]
	if !ok {
		return resp.BadReq("missing_param", "text is required")
	}
	return resp.JSONOK(hashCore(txt))
}

// Random genera count enteros en el rango [min, max].
// Reglas y errores:
//   - count requerido, entero >= 1 → 400 si no.
//   - min requerido, entero        → 400 si no.
//   - max requerido, entero        → 400 si no.
//   - min <= max                   → 400 "range" si no.
// 200 + JSON {values:[...] } si todo OK.
func Random(params map[string]string) resp.Result {
	cStr, ok := params["count"]
	if !ok {
		return resp.BadReq("count", "count is required (integer >= 1)")
	}
	count, err := strconv.Atoi(cStr)
	if err != nil || count < 1 {
		return resp.BadReq("count", "must be integer >= 1")
	}

	minStr, ok := params["min"]
	if !ok {
		return resp.BadReq("min", "min is required (integer)")
	}
	min, err := strconv.Atoi(minStr)
	if err != nil {
		return resp.BadReq("min", "min must be integer")
	}

	maxStr, ok := params["max"]
	if !ok {
		return resp.BadReq("max", "max is required (integer)")
	}
	max, err := strconv.Atoi(maxStr)
	if err != nil {
		return resp.BadReq("max", "max must be integer")
	}
	if min > max {
		return resp.BadReq("range", "min must be <= max")
	}

	return resp.JSONOK(randomCore(count, min, max))
}

// Fibonacci devuelve el n-ésimo número de Fibonacci como texto terminado en "\n".
// Reglas y errores:
//   - num requerido, entero >= 0 → 400 si no.
// 200 + texto plano si OK.
func Fibonacci(params map[string]string) resp.Result {
	v, ok := params["num"]
	if !ok {
		return resp.BadReq("missing_param", "num is required")
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return resp.BadReq("num", "num must be integer >= 0")
	}
	return resp.PlainOK(fibonacciCore(n))
}

// Submit es un “hook” que el router debe asignar.
// Debe encolar en el pool y esperar con timeout.
// Devuelve (resultado, encolado?). Si encolado=false → backpressure (503).
var Submit func(task string, params map[string]string, timeout time.Duration) (resp.Result, bool)

// Sleep: /sleep?seconds=s  (valida y ejecuta vía pool "sleep")
func Sleep(params map[string]string) resp.Result {
	secStr, ok := params["seconds"]
	if !ok {
		return resp.BadReq("seconds", "seconds is required (integer >= 0)")
	}
	sec, err := strconv.Atoi(secStr)
	if err != nil || sec < 0 {
		return resp.BadReq("seconds", "must be integer >= 0")
	}

	if Submit == nil {
		// Fallback por si no fue inyectado (no debería pasar en tu server)
		return resp.IntErr("submit_not_set", "internal submit hook not set")
	}
	r, _ := Submit("sleep", map[string]string{"seconds": secStr}, 15*time.Second)
	return r
}

// Simulate: /simulate?seconds=s&task=sleep|spin
func Simulate(params map[string]string) resp.Result {
	task, ok := params["task"]
	if !ok {
		return resp.BadReq("task", "task is required (sleep|spin)")
	}
	if task != "sleep" && task != "spin" {
		return resp.BadReq("task", "use task=sleep|spin")
	}
	secStr, ok := params["seconds"]
	if !ok {
		return resp.BadReq("seconds", "seconds is required (integer >= 0)")
	}
	if s, err := strconv.Atoi(secStr); err != nil || s < 0 {
		return resp.BadReq("seconds", "must be integer >= 0")
	}

	if Submit == nil {
		return resp.IntErr("submit_not_set", "internal submit hook not set")
	}
	r, _ := Submit(task, map[string]string{"seconds": secStr}, 30*time.Second)
	return r
}

// LoadTest: /loadtest?tasks=n&sleep=x  (lanza n jobs "sleep" de x segundos)
func LoadTest(params map[string]string) resp.Result {
	nStr, ok := params["tasks"]
	if !ok {
		return resp.BadReq("tasks", "tasks is required (integer > 0)")
	}
	n, errN := strconv.Atoi(nStr)
	if errN != nil || n <= 0 {
		return resp.BadReq("tasks", "must be integer > 0")
	}

	secStr, ok := params["sleep"]
	if !ok {
		return resp.BadReq("sleep", "sleep is required (integer >= 0)")
	}
	if s, err := strconv.Atoi(secStr); err != nil || s < 0 {
		return resp.BadReq("sleep", "must be integer >= 0")
	}

	if Submit == nil {
		return resp.IntErr("submit_not_set", "internal submit hook not set")
	}

	okCount := 0
	for i := 0; i < n; i++ {
		if r, enq := Submit("sleep", map[string]string{"seconds": secStr}, 10*time.Second); enq && r.Status == 200 {
			okCount++
		}
	}
	return resp.PlainOK("ok " + strconv.Itoa(okCount) + "/" + strconv.Itoa(n) + "\n")
}
