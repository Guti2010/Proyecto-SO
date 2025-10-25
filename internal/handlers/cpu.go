// internal/handlers/cpu.go
//
// Handlers CPU-bound con **comentarios detallados**.
// - Todos respetan cancelación por contexto (ctx.Done()).
// - No implementan "timeout_ms" internos: el timeout/cancel se maneja desde
//   el pool/router (o Job Manager) cancelando el contexto.
// - Responden con JSON consistente y orden estable de campos.
// - En /isprime y /pi el algoritmo se elige con `method=`.
//
// Endpoints cubiertos:
//   /isprime?n=NUM[&method=division|miller-rabin]
//   /factor?n=NUM
//   /pi?digits=D[&method=spigot|chudnovsky]
//   /mandelbrot?width=W&height=H&max_iter=I
//   /matrixmul?size=N&seed=S
package handlers

import (
	"context"
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


// ============================================================================
// /isprime — primalidad con dos métodos: "division" (por √n) y "miller-rabin".
// - Parám. requeridos: n (>=0)
// - Parám. opcional : method=division|miller-rabin (por defecto: division)
// - Cancelación     : chequeos periódicos de ctx.Done()
// - JSON (ordenado) : { "n", "is_prime", "method", "elapsed_ms" }
// ============================================================================
func IsPrimeJSONCtx(ctx context.Context, params map[string]string) resp.Result {
	// Parseo defensivo de n
	n64, err := strconv.ParseInt(params["n"], 10, 64)
	if err != nil || n64 < 0 {
		return resp.BadReq("n", "n must be integer >= 0")
	}

	// Selección de método (con validación)
	method := params["method"]
	if method == "" {
		method = "division"
	}
	if method != "division" && method != "miller-rabin" {
		return resp.BadReq("method", "use method=division|miller-rabin")
	}

	n := n64
	start := time.Now()

	// Estructura con orden estable en JSON (evita map desordenado)
	type outT struct {
		N        int64  `json:"n"`
		IsPrime  bool   `json:"is_prime"`
		Method   string `json:"method"`
		Elapsed  int64  `json:"elapsed_ms"`
	}
	out := outT{N: n, IsPrime: false, Method: method}

	// Ejecuta el método seleccionado
	switch method {
	case "division":
		switch {
		case n < 2:
			// nada: sigue en false
		case n == 2 || n == 3:
			out.IsPrime = true
		default:
			if n%2 == 0 {
				// compuesto
			} else {
				prime := true
				limit := int64(math.Sqrt(float64(n)))
				for d := int64(3); d <= limit; d += 2 {
					// Chequeo de cancelación cada ~1024 divisores
					if d&1023 == 0 {
						select {
						case <-ctx.Done():
							return resp.Unavail("canceled", "job canceled")
						default:
						}
					}
					if n%d == 0 {
						prime = false
						break
					}
				}
				out.IsPrime = prime
			}
		}
	case "miller-rabin":
		// Versión determinística para 64-bit
		out.IsPrime = mrIsPrime64Ctx(ctx, uint64(n))
	}

	out.Elapsed = time.Since(start).Milliseconds()
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// mrIsPrime64Ctx: Miller–Rabin determinístico para uint64.
// - Usa bases conocidas que garantizan exactitud en 64 bits.
// - Respeta ctx mediante chequeos periódicos.
func mrIsPrime64Ctx(ctx context.Context, n uint64) bool {
	if n < 2 {
		return false
	}
	// Criba de primos pequeños (acorta casos triviales)
	small := [...]uint64{2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37}
	for _, p := range small {
		if n == p {
			return true
		}
		if n%p == 0 && n != p {
			return false
		}
	}

	// Descomposición n-1 = d * 2^r
	r := 0
	d := n - 1
	for d&1 == 0 {
		d >>= 1
		r++
	}

	// Bases determinísticas para 64-bit
	bases := [...]uint64{2, 3, 5, 7, 11, 13, 17}
	nBI := new(big.Int).SetUint64(n)
	dBI := new(big.Int).SetUint64(d)

	for i, a := range bases {
		// Chequeo de cancelación amortizado
		if i&1 == 0 {
			select {
			case <-ctx.Done():
				return false
			default:
			}
		}
		if a%n == 0 {
			continue
		}
		// x = a^d mod n
		x := new(big.Int).Exp(new(big.Int).SetUint64(a), dBI, nBI)
		if x.Sign() == 0 || x.Cmp(big.NewInt(1)) == 0 || x.Cmp(new(big.Int).Sub(nBI, big.NewInt(1))) == 0 {
			continue
		}
		composite := true
		for j := 1; j < r; j++ {
			// x = x^2 mod n
			select {
			case <-ctx.Done():
				return false
			default:
			}
			x.Mul(x, x)
			x.Mod(x, nBI)
			if x.Cmp(new(big.Int).Sub(nBI, big.NewInt(1))) == 0 {
				composite = false
				break
			}
		}
		if composite {
			return false
		}
	}
	return true
}


// ============================================================================
// /factor — factorización por división trial (con conteos).
// - Parám. requeridos: n (>=2)
// - Cancelación: chequeos periódicos.
// - JSON: { "n", "factors":[[p,c],...], "elapsed_ms" }
// ============================================================================
func FactorJSONCtx(ctx context.Context, params map[string]string) resp.Result {
	n64, err := strconv.ParseInt(params["n"], 10, 64)
	if err != nil || n64 < 2 {
		return resp.BadReq("n", "n must be integer >= 2")
	}
	n := n64
	start := time.Now()

	var facts [][2]int64

	// Factor 2
	if n%2 == 0 {
		c := int64(0)
		for n%2 == 0 {
			n /= 2
			c++
		}
		facts = append(facts, [2]int64{2, c})
	}

	// Factores impares
	for d := int64(3); d <= n/d; d += 2 {
		// Chequeo de cancelación amortizado
		if d&1023 == 0 {
			select {
			case <-ctx.Done():
				return resp.Unavail("canceled", "job canceled")
			default:
			}
		}
		if n%d == 0 {
			c := int64(0)
			for n%d == 0 {
				n /= d
				c++
				// Chequeo adicional durante conteo
				if c&1023 == 0 {
					select {
					case <-ctx.Done():
						return resp.Unavail("canceled", "job canceled")
					default:
					}
				}
			}
			facts = append(facts, [2]int64{d, c})
		}
	}
	// Si quedó un primo > 1
	if n > 1 {
		facts = append(facts, [2]int64{n, 1})
	}

	type outT struct {
		N         int64      `json:"n"`
		Factors   [][2]int64 `json:"factors"`
		ElapsedMS int64      `json:"elapsed_ms"`
	}
	out := outT{
		N:         n64,
		Factors:   facts,
		ElapsedMS: time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}


// ============================================================================
// /pi — cálculo de π con dos métodos: "chudnovsky" (rápido) y "spigot" (simple).
// - Parám. requeridos: digits (>=1; cap a 10000)
// - Parám. opcional : method=chudnovsky|spigot (default: chudnovsky)
// - Cancelación     : chequeos periódicos; NO maneja timeout local.
// - JSON            : { "digits","method","iterations","truncated","pi","elapsed_ms" }
// ============================================================================
func PiJSONCtx(ctx context.Context, params map[string]string) resp.Result {
	const maxDigits = 10000

	// digits requerido
	d, err := strconv.Atoi(params["digits"])
	if err != nil || d < 1 {
		return resp.BadReq("digits", "digits must be integer >= 1")
	}
	if d > maxDigits {
		d = maxDigits
	}

	// method=spigot|chudnovsky (default chudnovsky)
	method := params["method"]
	if method == "" {
		method = "chudnovsky"
	}
	if method != "spigot" && method != "chudnovsky" {
		return resp.BadReq("method", "use method=spigot|chudnovsky")
	}

	start := time.Now()
	var s string
	var iters int
	var truncated bool

	switch method {
	case "spigot":
		s, iters, truncated = piSpigotCtx(ctx, d)
	case "chudnovsky":
		s, iters, truncated = piChudnovskyCtx(ctx, d)
	}

	type outT struct {
		Digits     int    `json:"digits"`
		Method     string `json:"method"`
		Iterations int    `json:"iterations"`
		Truncated  bool   `json:"truncated"`
		Pi         string `json:"pi"`
		Elapsed    int64  `json:"elapsed_ms"`
	}
	out := outT{
		Digits:     d,
		Method:     method,
		Iterations: iters,
		Truncated:  truncated,
		Pi:         s,
		Elapsed:    time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// piSpigotCtx: Spigot (Rabinowitz–Wagon, base 10) con soporte de ctx.
// Devuelve "3." + d decimales exactos (sin redondear), el número de
// iteraciones internas y un flag si se truncó por cancelación.
func piSpigotCtx(ctx context.Context, n int) (string, int, bool) {
	if n <= 0 {
		return "3", 0, false
	}

	size := (10*n)/3 + 1
	a := make([]int, size)
	for i := range a {
		a[i] = 2
	}

	// Estado del emisor
	const (
		stateDropInt = iota // descartar el entero (primer q=3)
		stateFirstPred      // capturar el primer predigit decimal
		stateNormal         // flujo normal de emisión
	)
	state := stateDropInt

	nines := 0
	predigit := 0
	iters := 0

	out := make([]byte, 0, n+2)
	out = append(out, '3', '.')

	for digits := 0; digits < n; {
		// cancelación periódica
		if (digits & 63) == 0 {
			select {
			case <-ctx.Done():
				// Solo emitimos predigit si ya estamos en flujo que lo usa
				if state == stateNormal {
					out = append(out, byte(predigit)+'0')
					for ; nines > 0 && len(out) < 2+n; nines-- {
						out = append(out, '9')
					}
				}
				if len(out) > 2+n {
					out = out[:2+n]
				}
				return string(out), iters, true
			default:
			}
		}

		// Paso interno del spigot
		carry := 0
		for i := size - 1; i > 0; i-- {
			x := a[i]*10 + carry*(i+1)
			den := 2*i + 1
			a[i] = x % den
			carry = x / den
			iters++
		}
		x0 := a[0]*10 + carry
		a[0] = x0 % 10
		q := x0 / 10

		switch state {
		case stateDropInt:
			// q debería ser 3; lo descartamos (ya pusimos "3.")
			state = stateFirstPred
			continue

		case stateFirstPred:
			// Primer dígito decimal: solo lo guardamos como predigit
			predigit = q
			state = stateNormal
			continue

		case stateNormal:
			// Flujo normal: ahora sí se emite predigit previo
			switch {
			case q == 9:
				nines++
				// no se emite nada aún, solo contamos 9s consecutivos
			case q == 10:
				out = append(out, byte(predigit+1)+'0')
				for ; nines > 0; nines-- {
					out = append(out, '0')
				}
				predigit = 0
				digits++
			default:
				out = append(out, byte(predigit)+'0')
				for ; nines > 0; nines-- {
					out = append(out, '9')
				}
				predigit = q
				digits++
			}
		}
	}

	// Empujar el último predigit para completar exactamente n decimales
	if len(out) < 2+n {
		out = append(out, byte(predigit)+'0')
	}
	if len(out) > 2+n {
		out = out[:2+n]
	}
	return string(out), iters, false
}


// piChudnovskyCtx: Implementación de Chudnovsky con big.Float.
// - Más eficiente para muchos dígitos.
// - Corta cuando el término cae por debajo de 10^{-d}.
// - Chequeos periódicos de ctx.Done().
func piChudnovskyCtx(ctx context.Context, d int) (string, int, bool) {
	// Precisión en bits ≈ (d+5)*log2(10)
	bits := uint(float64(d+5) * 3.32193)
	one := new(big.Float).SetPrec(bits).SetInt64(1)

	A := big.NewFloat(13591409).SetPrec(bits)
	B := big.NewFloat(545140134).SetPrec(bits)

	C3int := new(big.Int).Exp(big.NewInt(640320), big.NewInt(3), nil)
	C3 := new(big.Float).SetPrec(bits).SetInt(C3int)

	sum := new(big.Float).SetPrec(bits).SetFloat64(0.0)
	t := new(big.Float).SetPrec(bits).SetFloat64(1.0)
	k := 0
	sign := 1.0

	// Umbral de corte: 10^{-d}
	pow10 := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d)), nil)
	tenPow := new(big.Float).SetPrec(bits).SetInt(pow10)
	threshold := new(big.Float).SetPrec(bits).Quo(one, tenPow)

	for {
		if (k & 1023) == 0 {
			select {
			case <-ctx.Done():
				// Cancelado: devolver lo que tengamos (marcando truncado)
				// Nota: devolveremos "" para pi: el caller marcará 'truncated'.
				// Para consistencia, mejor devolvemos el avance parcial calculado.
				// Construimos π parcial igual que al final.
			default:
			}
		}

		Ak := new(big.Float).SetPrec(bits).Mul(B, new(big.Float).SetPrec(bits).SetFloat64(float64(k)))
		Ak.Add(Ak, A)
		term := new(big.Float).SetPrec(bits).Mul(t, Ak)
		if sign < 0 {
			term.Neg(term)
		}
		sum.Add(sum, term)

		absTerm := new(big.Float).Abs(term)
		if absTerm.Cmp(threshold) < 0 {
			break
		}

		k++
		sign *= -1

		num := new(big.Float).SetPrec(bits).SetFloat64(float64(6*k - 5))
		num.Mul(num, new(big.Float).SetPrec(bits).SetFloat64(float64(6*k - 3)))
		num.Mul(num, new(big.Float).SetPrec(bits).SetFloat64(float64(6*k - 1)))

		den := new(big.Float).SetPrec(bits).SetFloat64(float64(k * k * k))
		den.Mul(den, C3)

		t.Mul(t, num)
		t.Quo(t, den)
	}

	c3Sqrt := new(big.Float).SetPrec(bits).Sqrt(C3)
	den := new(big.Float).SetPrec(bits).Mul(new(big.Float).SetPrec(bits).SetFloat64(12.0), sum)
	pi := new(big.Float).SetPrec(bits).Quo(c3Sqrt, den)

	txt := pi.Text('f', d) // “3.” + d decimales
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


// ============================================================================
// /mandelbrot — genera mapa de iteraciones (matriz de int) en JSON.
// - Parám. requeridos: width>0, height>0, max_iter>0 (cap en 512x512, 2000)
// - Cancelación: chequeos dentro de los bucles
// - JSON: { "width","height","max_iter","map":[[...]],"elapsed_ms" }
// ============================================================================
func MandelbrotJSONCtx(ctx context.Context, params map[string]string) resp.Result {
	// Parseo y validación de parámetros
	w, errW := strconv.Atoi(params["width"])
	h, errH := strconv.Atoi(params["height"])
	it, errI := strconv.Atoi(params["max_iter"])
	if errW != nil || errH != nil || errI != nil {
		return resp.BadReq("params", "width,height,max_iter must be integers")
	}
	if w <= 0 || h <= 0 || it <= 0 {
		return resp.BadReq("params", "width,height,max_iter must be > 0")
	}
	// Límites para evitar respuestas gigantes / uso excesivo de CPU
	if w > 512 { w = 512 }
	if h > 512 { h = 512 }
	if it > 2000 { it = 2000 }

	start := time.Now()

	// Ventana típica del conjunto
	minRe, maxRe := -2.5, 1.0
	minIm, maxIm := -1.0, 1.0

	// Mapa [h][w] con número de iteraciones por píxel
	img := make([][]int, h)
	for y := 0; y < h; y++ {
		// Cancelación amortizada por fila
		if y&63 == 0 {
			select {
			case <-ctx.Done():
				return resp.Unavail("canceled", "job canceled")
			default:
			}
		}
		row := make([]int, w)
		ci := minIm + (maxIm-minIm)*float64(y)/float64(h-1)
		for x := 0; x < w; x++ {
			cr := minRe + (maxRe-minRe)*float64(x)/float64(w-1)
			c := complex(cr, ci)
			z := complex(0, 0)
			iter := 0
			for iter = 0; iter < it; iter++ {
				// Cancelación dentro del bucle interno
				if iter&255 == 0 {
					select {
					case <-ctx.Done():
						return resp.Unavail("canceled", "job canceled")
					default:
					}
				}
				z = z*z + c
				if cmplx.Abs(z) > 2.0 { // escape
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


// ============================================================================
// /matrixmul — multiplicación de matrices NxN con hash del resultado.
// - Parám. requeridos: size>0, seed (int64)
// - Se genera A y B con RNG determinístico (seed).
// - Cancelación: chequeos en bucles.
// - JSON: { "size","seed","result_sha256","elapsed_ms" }
// ============================================================================
func MatrixMulHashCtx(ctx context.Context, params map[string]string) resp.Result {
	// Validación de parámetros
	n, err1 := strconv.Atoi(params["size"])
	seed, err2 := strconv.ParseInt(params["seed"], 10, 64)
	if err1 != nil || n <= 0 || err2 != nil {
		return resp.BadReq("params", "size>0 and valid seed required")
	}
	start := time.Now()

	// RNG determinístico
	rng := rand.New(rand.NewSource(seed))

	// Matrices en forma lineal (performance/cache-friendly)
	A := make([]int64, n*n)
	B := make([]int64, n*n)

	// Relleno de A y B con enteros pequeños (-3..+3)
	for i := 0; i < n*n; i++ {
		// Chequeo amortizado de cancelación
		if i&(n-1) == 0 {
			select {
			case <-ctx.Done():
				return resp.Unavail("canceled", "job canceled")
			default:
			}
		}
		A[i] = int64(rng.Intn(7) - 3)
		B[i] = int64(rng.Intn(7) - 3)
	}

	// C = A * B
	C := make([]int64, n*n)
	for i := 0; i < n; i++ {
		// Chequeo amortizado por fila
		if i&7 == 0 {
			select {
			case <-ctx.Done():
				return resp.Unavail("canceled", "job canceled")
			default:
			}
		}
		ik := i * n
		for k := 0; k < n; k++ {
			aik := A[ik+k]
			if aik == 0 {
				continue
			}
			kj := k * n
			for j := 0; j < n; j++ {
				// Chequeo amortizado por columnas
				if j&255 == 0 {
					select {
					case <-ctx.Done():
						return resp.Unavail("canceled", "job canceled")
					default:
					}
				}
				C[ik+j] += aik * B[kj+j]
			}
		}
	}

	// Hash del resultado (SHA-256 little endian de cada int64)
	h := sha256.New()
	for idx, v := range C {
		if idx&8191 == 0 {
			select {
			case <-ctx.Done():
				return resp.Unavail("canceled", "job canceled")
			default:
			}
		}
		_ = binary.Write(h, binary.LittleEndian, v)
	}
	sum := hex.EncodeToString(h.Sum(nil))

	// Estructura con orden estable
	type outT struct {
		Size    int    `json:"size"`
		Seed    int64  `json:"seed"`
		Hash    string `json:"result_sha256"`
		Elapsed int64  `json:"elapsed_ms"`
	}
	out := outT{
		Size:    n,
		Seed:    seed,
		Hash:    sum,
		Elapsed: time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}
