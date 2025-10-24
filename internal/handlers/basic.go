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

// start se usa para calcular uptime en /status.
var start = time.Now()

// HelpText lista concisa de rutas disponibles (HTTP/1.0, sólo GET).
func HelpText() string {
	return strings.TrimSpace(`
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
`) + "\n"
}

// StatusJSON expone salud básica del proceso (helper simple).
func StatusJSON() string {
	type status struct {
		UptimeSec int64  `json:"uptime_sec"`
		Server    string `json:"server"`
	}
	b, _ := json.Marshal(status{
		UptimeSec: int64(time.Since(start).Seconds()),
		Server:    "so-http10/0.2",
	})
	return string(b)
}

// TimestampJSON devuelve epoch unix y fecha UTC ISO 8601.
func TimestampJSON() string {
	now := time.Now().UTC()
	out := map[string]any{
		"unix": now.Unix(),
		"utc":  now.Format(time.RFC3339),
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// Reverse invierte runas (funciona con UTF-8) y agrega salto de línea.
func Reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r) + "\n"
}

// HashJSON calcula SHA-256 del texto y devuelve JSON con el hex.
func HashJSON(text string) string {
	sum := sha256.Sum256([]byte(text))
	b, _ := json.Marshal(map[string]string{
		"algo": "sha256",
		"hex":  hex.EncodeToString(sum[:]),
	})
	return string(b)
}

// RandomJSON genera n enteros uniformes en [min,max] con límites defensivos.
func RandomJSON(n, min, max int) string {
	if n <= 0 {
		n = 1
	}
	if n > 1000 {
		n = 1000
	}
	if max < min {
		max, min = min, max
	}
	rand.Seed(time.Now().UnixNano())
	arr := make([]int, n)
	for i := 0; i < n; i++ {
		if max == min {
			arr[i] = min
		} else {
			arr[i] = rand.Intn(max-min+1) + min
		}
	}
	b, _ := json.Marshal(map[string]any{"values": arr})
	return string(b)
}

// FibonacciText calcula el N-ésimo Fibonacci de forma iterativa.
func FibonacciText(n int) string {
	if n < 0 {
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

// ToUpper convierte el texto a mayúsculas y agrega salto de línea.
func ToUpper(s string) string {
	return strings.ToUpper(s) + "\n"
}

// -----------------------------------------------------------------------------
// Wrappers con validación que devuelven resp.Result (errores JSON coherentes).
// -----------------------------------------------------------------------------

// /help
func HelpTextRes() resp.Result {
	return resp.PlainOK(HelpText())
}

// /timestamp
func TimestampJSONRes() resp.Result {
	return resp.JSONOK(TimestampJSON())
}

// /reverse?text=...
func ReverseJSON(params map[string]string) resp.Result {
	txt, ok := params["text"]
	if !ok {
		return resp.BadReq("missing_param", "text is required")
	}
	return resp.PlainOK(Reverse(txt))
}

// /toupper?text=...
func ToUpperJSON(params map[string]string) resp.Result {
	txt, ok := params["text"]
	if !ok {
		return resp.BadReq("missing_param", "text is required")
	}
	return resp.PlainOK(ToUpper(txt))
}

// /hash?text=...
func HashTextJSON(params map[string]string) resp.Result {
	txt, ok := params["text"]
	if !ok {
		return resp.BadReq("missing_param", "text is required")
	}
	return resp.JSONOK(HashJSON(txt))
}

// /random?count=n&min=a&max=b
func RandomJSONRes(params map[string]string) resp.Result {
	var (
		count int
		min   int
		max   int
		err   error
	)

	if v, ok := params["count"]; ok {
		if count, err = strconv.Atoi(v); err != nil {
			return resp.BadReq("count", "count must be integer")
		}
	} else {
		count = 1
	}
	if v, ok := params["min"]; ok {
		if min, err = strconv.Atoi(v); err != nil {
			return resp.BadReq("min", "min must be integer")
		}
	}
	if v, ok := params["max"]; ok {
		if max, err = strconv.Atoi(v); err != nil {
			return resp.BadReq("max", "max must be integer")
		}
	}

	return resp.JSONOK(RandomJSON(count, min, max))
}

// /fibonacci?num=N
func FibonacciTextRes(params map[string]string) resp.Result {
	v, ok := params["num"]
	if !ok {
		return resp.BadReq("missing_param", "num is required")
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return resp.BadReq("num", "num must be integer >= 0")
	}
	return resp.PlainOK(FibonacciText(n))
}
