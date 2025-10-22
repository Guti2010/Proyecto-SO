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

/************ /isprime (cancelable) ************/
func IsPrimeJSONCtx(ctx context.Context, params map[string]string) resp.Result {
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
			out["is_prime"] = prime
		}
	}
	out["elapsed_ms"] = time.Since(start).Milliseconds()
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

/************ /factor (cancelable) ************/
func FactorJSONCtx(ctx context.Context, params map[string]string) resp.Result {
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
	for d := int64(3); d <= n/d; d += 2 {
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

/************ /pi (cancelable) ************/
func PiJSONCtx(ctx context.Context, params map[string]string) resp.Result {
	const maxDigits = 10000

	d, err := strconv.Atoi(params["digits"])
	if err != nil || d < 1 {
		return resp.BadReq("digits", "digits must be integer >= 1")
	}
	if d > maxDigits {
		d = maxDigits
	}
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
		// (si reactivas spigot, pásale ctx igual que a chudnovsky)
		s, iters, truncated = piChudnovskyCtx(ctx, d, deadline)
	default: // chudnovsky
		s, iters, truncated = piChudnovskyCtx(ctx, d, deadline)
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

// Chudnovsky con checks de cancelación
func piChudnovskyCtx(ctx context.Context, d int, deadline time.Time) (string, int, bool) {
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

	// threshold = 10^{-d}
	pow10 := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d)), nil)
	tenPow := new(big.Float).SetPrec(bits).SetInt(pow10)
	threshold := new(big.Float).SetPrec(bits).Quo(one, tenPow)

	for {
		if k&1023 == 0 {
			select {
			case <-ctx.Done():
				return "", k, true
			default:
			}
			if !deadline.IsZero() && time.Now().After(deadline) {
				break
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

/************ /mandelbrot (cancelable) ************/
func MandelbrotJSONCtx(ctx context.Context, params map[string]string) resp.Result {
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
				if iter&255 == 0 {
					select {
					case <-ctx.Done():
						return resp.Unavail("canceled", "job canceled")
					default:
					}
				}
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

/************ /matrixmul (cancelable) ************/
func MatrixMulHashCtx(ctx context.Context, params map[string]string) resp.Result {
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
		if i&(n-1) == 0 { // cada ~n elementos
			select {
			case <-ctx.Done():
				return resp.Unavail("canceled", "job canceled")
			default:
			}
		}
		A[i] = int64(rng.Intn(7) - 3)
		B[i] = int64(rng.Intn(7) - 3)
	}

	C := make([]int64, n*n)
	for i := 0; i < n; i++ {
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

	out := map[string]any{
		"size":          n,
		"seed":          seed,
		"result_sha256": sum,
		"elapsed_ms":    time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}
