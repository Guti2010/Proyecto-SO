package handlers

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"math/cmplx"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"so-http10-demo/internal/resp"
)

// ---------- /isprime ----------
// Prueba por división hasta √n (trial division).
// Respuesta: {"n":NUM, "is_prime":bool, "method":"trial", "elapsed_ms":...}
func IsPrimeJSON(params map[string]string) resp.Result {
	n64, err := strconv.ParseInt(params["n"], 10, 64)
	if err != nil || n64 < 0 {
		return resp.BadReq("n", "n must be integer >= 0")
	}
	n := n64
	start := time.Now()
	out := map[string]any{
		"n":        n,
		"method":   "trial",
		"is_prime": false,
	}
	switch {
	case n < 2:
	case n == 2 || n == 3:
		out["is_prime"] = true
	default:
		if n%2 == 0 {
			// compuesto
		} else {
			prime := true
			limit := int64(math.Sqrt(float64(n)))
			for d := int64(3); d <= limit; d += 2 {
				if n%d == 0 {
					prime = false
					break
				}
			}
			out["is_prime"] = prime
		}
	}
	out["elapsed_ms"] = time.Since(start).Milliseconds()
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// ---------- /factor ----------
// Factorización por división (2 y luego impares).
// Respuesta: {"n":NUM,"factors":[[p,c],...],"elapsed_ms":...}
func FactorJSON(params map[string]string) resp.Result {
	n64, err := strconv.ParseInt(params["n"], 10, 64)
	if err != nil || n64 < 2 {
		return resp.BadReq("n", "n must be integer >= 2")
	}
	n := n64
	start := time.Now()

	var facts [][2]int64
	if n%2 == 0 {
		c := int64(0)
		for n%2 == 0 {
			n /= 2
			c++
		}
		facts = append(facts, [2]int64{2, c})
	}
	// evitar overflow: usar d <= n/d en vez de d*d <= n
	for d := int64(3); d <= n/d; d += 2 {
		if n%d == 0 {
			c := int64(0)
			for n%d == 0 {
				n /= d
				c++
			}
			facts = append(facts, [2]int64{d, c})
		}
	}
	if n > 1 {
		facts = append(facts, [2]int64{n, 1})
	}

	out := map[string]any{
		"n":          n64,
		"factors":    facts,
		"elapsed_ms": time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// ---------- /pi ----------
// Cálculo de π con control iterativo/tiempo.
// Soporta dos métodos: "spigot" (decimales base 10) y "chudnovsky" (serie rápida).
// Parámetros:
//   - digits=D (decimales deseados; máx seguro 10000)
//   - algo=spigot|chudnovsky (opcional; por defecto chudnovsky)
//   - timeout_ms=T (opcional; si se excede, se corta y se marca truncated=true)
func PiJSON(params map[string]string) resp.Result {
	const maxDigits = 10000

	d, err := strconv.Atoi(params["digits"])
	if err != nil || d < 1 {
		return resp.BadReq("digits", "digits must be integer >= 1")
	}
	if d > maxDigits {
		d = maxDigits
	}

	// por defecto usa chudnovsky (spigot queda desactivado hasta afinarlo)
	algo := params["algo"]
	if algo != "spigot" && algo != "chudnovsky" {
		algo = "chudnovsky"
	}

	timeout := 0 * time.Millisecond
	if ms, err := strconv.Atoi(params["timeout_ms"]); err == nil && ms > 0 {
		timeout = time.Duration(ms) * time.Millisecond
	}
	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	start := time.Now()
	var s string
	var iters int
	var truncated bool

	switch algo {
	case "spigot":
		// TODO: reactivar spigot real cuando esté afinado.
		s, iters, truncated = piChudnovsky(d, deadline)
	case "chudnovsky":
		s, iters, truncated = piChudnovsky(d, deadline)
	}

	out := map[string]any{
		"digits":     d,
		"method":     algo,
		"iterations": iters,
		"truncated":  truncated,
		"pi":         s,
		"elapsed_ms": time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// --- Implementación Spigot (base 10) con stop por deadline ---
// (Actualmente no usada: PiJSON delega a chudnovsky)
func piSpigot(d int, deadline time.Time) (string, int, bool) {
	n := (10*d)/3 + 1
	a := make([]int, n)
	for i := 0; i < n; i++ {
		a[i] = 2
	}

	digits := make([]int, 0, d+1)
	predigit := 0
	nines := 0
	iters := 0

	for j := 0; j < d+1; j++ {
		if !deadline.IsZero() && time.Now().After(deadline) {
			if len(digits) == 0 {
				return "3.", iters, true
			}
			digits = append(digits, predigit)
			var b strings.Builder
			b.Grow(2 + len(digits))
			b.WriteByte(byte('0' + digits[0]))
			b.WriteByte('.')
			limit := d
			if len(digits)-1 < limit {
				limit = len(digits) - 1
			}
			for i := 0; i < limit; i++ {
				b.WriteByte(byte('0' + digits[i+1]))
			}
			return b.String(), iters, true
		}

		carry := 0
		for i := n - 1; i >= 0; i-- {
			x := 10*a[i] + carry
			q := x / (2*i + 1)
			a[i] = x % (2*i + 1)
			carry = q * i
			iters++
		}

		q := (10*a[0] + carry) / 10
		a[0] = (10*a[0] + carry) % 10

		if q == 9 {
			nines++
		} else if q == 10 {
			digits = append(digits, predigit+1)
			for ; nines > 0; nines-- {
				digits = append(digits, 0)
			}
			predigit = 0
		} else {
			digits = append(digits, predigit)
			for ; nines > 0; nines-- {
				digits = append(digits, 9)
			}
			predigit = q
		}
	}

	digits = append(digits, predigit)

	var b strings.Builder
	b.Grow(2 + d)
	b.WriteByte(byte('0' + digits[0]))
	b.WriteByte('.')
	for i := 0; i < d; i++ {
		b.WriteByte(byte('0' + digits[i+1]))
	}
	return b.String(), iters, false
}

// --- Implementación Chudnovsky con big.Float y control de convergencia/deadline ---
// π = 1 / (12 * Σ_{k=0..∞} (-1)^k * (6k)! / [(3k)! (k!)^3] * (13591409 + 545140134k) / 640320^(3k + 3/2))
func piChudnovsky(d int, deadline time.Time) (string, int, bool) {
	// Precisión en bits ~ dígitos * log2(10) ≈ dígitos * 3.32193
	bits := uint(float64(d+5) * 3.32193) // +5 de margen
	one := new(big.Float).SetPrec(bits).SetInt64(1)

	// Constantes exactas
	A := big.NewFloat(13591409).SetPrec(bits)
	B := big.NewFloat(545140134).SetPrec(bits)

	// 640320^3 exacto, sin pasar por float64
	C3int := new(big.Int).Exp(big.NewInt(640320), big.NewInt(3), nil)
	C3 := new(big.Float).SetPrec(bits).SetInt(C3int)

	// Acumulador de la serie Σ (-1)^k * t_k * (A + Bk)
	sum := new(big.Float).SetPrec(bits).SetFloat64(0.0)

	// Relación recursiva para t_k:
	// t_k / t_{k-1} = - (6k-5)(6k-3)(6k-1) / (k^3 * C3)
	t := new(big.Float).SetPrec(bits).SetFloat64(1.0) // t_0 = 1
	k := 0
	sign := 1.0

	// Umbral 10^{-d} evitando underflow
	pow10 := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d)), nil)
	tenPow := new(big.Float).SetPrec(bits).SetInt(pow10)   // 10^d
	threshold := new(big.Float).SetPrec(bits).Quo(one, tenPow) // 1 / 10^d

	for {
		if !deadline.IsZero() && time.Now().After(deadline) {
			break
		}

		// term = t_k * (A + Bk) con signo alterno
		Ak := new(big.Float).SetPrec(bits).Mul(B, new(big.Float).SetPrec(bits).SetFloat64(float64(k)))
		Ak.Add(Ak, A)
		term := new(big.Float).SetPrec(bits).Mul(t, Ak)
		if sign < 0 {
			term.Neg(term)
		}
		sum.Add(sum, term)

		// convergencia: |term| < 10^{-d} ?
		absTerm := new(big.Float).Abs(term)
		if absTerm.Cmp(threshold) < 0 {
			break
		}

		// siguiente t_{k+1}
		k++
		sign *= -1

		// t *= ( (6k-5)(6k-3)(6k-1) ) / ( k^3 * C3 )
		num := new(big.Float).SetPrec(bits).SetFloat64(float64(6*k - 5))
		num.Mul(num, new(big.Float).SetPrec(bits).SetFloat64(float64(6*k - 3)))
		num.Mul(num, new(big.Float).SetPrec(bits).SetFloat64(float64(6*k - 1)))

		den := new(big.Float).SetPrec(bits).SetFloat64(float64(k * k * k))
		den.Mul(den, C3)

		t.Mul(t, num)
		t.Quo(t, den)
	}

	// pi ≈ sqrt(C3) / (12 * sum)  con C3 = 640320^3
	c3Sqrt := new(big.Float).SetPrec(bits).Sqrt(C3)
	den := new(big.Float).SetPrec(bits).Mul(new(big.Float).SetPrec(bits).SetFloat64(12.0), sum)
	pi := new(big.Float).SetPrec(bits).Quo(c3Sqrt, den)

	// "3."+d decimales
	txt := pi.Text('f', d)
	truncated := false
	if idx := strings.IndexByte(txt, '.'); idx >= 0 {
		want := idx + 1 + d
		if want < len(txt) {
			txt = txt[:want]
		} else if want > len(txt) {
			truncated = true
		}
	}
	return txt, k + 1, truncated
}


// ---------- /mandelbrot ----------
// Mapa de iteraciones del conjunto de Mandelbrot.
// Límites: width/height <= 512, max_iter <= 2000.
func MandelbrotJSON(params map[string]string) resp.Result {
	w, errW := strconv.Atoi(params["width"])
	h, errH := strconv.Atoi(params["height"])
	it, errI := strconv.Atoi(params["max_iter"])
	if errW != nil || errH != nil || errI != nil {
		return resp.BadReq("params", "width,height,max_iter must be integers")
	}
	if w <= 0 || h <= 0 || it <= 0 {
		return resp.BadReq("params", "width,height,max_iter must be > 0")
	}
	if w > 512 {
		w = 512
	}
	if h > 512 {
		h = 512
	}
	if it > 2000 {
		it = 2000
	}

	start := time.Now()
	minRe, maxRe := -2.5, 1.0
	minIm, maxIm := -1.0, 1.0

	img := make([][]int, h)
	for y := 0; y < h; y++ {
		row := make([]int, w)
		ci := minIm + (maxIm-minIm)*float64(y)/float64(h-1)
		for x := 0; x < w; x++ {
			cr := minRe + (maxRe-minRe)*float64(x)/float64(w-1)
			c := complex(cr, ci)
			z := complex(0, 0)
			iter := 0
			for iter = 0; iter < it; iter++ {
				z = z*z + c
				if cmplx.Abs(z) > 2.0 {
					break
				}
			}
			row[x] = iter
		}
		img[y] = row
	}

	out := map[string]any{
		"width":      w,
		"height":     h,
		"max_iter":   it,
		"map":        img,
		"elapsed_ms": time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// ---------- /matrixmul ----------
// Multiplica A(NxN)*B(NxN) (PRNG determinístico por seed) y retorna SHA-256 de C.
func MatrixMulHash(params map[string]string) resp.Result {
	n, err1 := strconv.Atoi(params["size"])
	seed, err2 := strconv.ParseInt(params["seed"], 10, 64)
	if err1 != nil || n <= 0 || err2 != nil {
		return resp.BadReq("params", "size>0 and valid seed required")
	}
	start := time.Now()

	rng := rand.New(rand.NewSource(seed))
	A := make([]int64, n*n)
	B := make([]int64, n*n)
	for i := 0; i < n*n; i++ {
		A[i] = int64(rng.Intn(7) - 3) // [-3..3]
		B[i] = int64(rng.Intn(7) - 3)
	}

	C := make([]int64, n*n)
	for i := 0; i < n; i++ {
		ik := i * n
		for k := 0; k < n; k++ {
			aik := A[ik+k]
			if aik == 0 {
				continue
			}
			kj := k * n
			for j := 0; j < n; j++ {
				C[ik+j] += aik * B[kj+j]
			}
		}
	}

	h := sha256.New()
	for _, v := range C {
		_ = binary.Write(h, binary.LittleEndian, v)
	}
	sum := hex.EncodeToString(h.Sum(nil))

	out := map[string]any{
		"size":          n,
		"seed":          seed,
		"result_sha256": sum,
		"elapsed_ms":    time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}
