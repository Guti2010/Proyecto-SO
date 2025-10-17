package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// start se usa para calcular uptime en /status.
var start = time.Now()

// HelpText lista concisa de rutas básicas disponibles.
func HelpText() string {
	return strings.TrimSpace(`
/               -> hola mundo
/status         -> json de estado
/timestamp      -> json (unix, utc)
/reverse?text=  -> invierte texto
/toupper?text=  -> mayúsculas
/hash?text=     -> sha256 json
/random?count=n&min=a&max=b -> n enteros
/fibonacci?num=N -> N-ésimo iterativo
/createfile?name=filename&content=txt&repeat=x
/deletefile?name=filename
/help`) + "\n"
}

// StatusJSON expone salud básica del proceso.
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
