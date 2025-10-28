package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"os"
	"path/filepath"
)

/********** helpers **********/

func mustJSON[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\ninput: %q", err, s)
	}
	return v
}

func ctxBg() context.Context { return context.Background() }

/********** IsPrimeJSONCtx **********/

func TestIsPrimeJSONCtx_Division_Method(t *testing.T) {
	t.Parallel()
	type out struct {
		N       int64  `json:"n"`
		IsPrime bool   `json:"is_prime"`
		Method  string `json:"method"`
	}

	// Prime
	r1 := IsPrimeJSONCtx(ctxBg(), map[string]string{"n": "97", "method": "division"})
	if r1.Status != 200 || !r1.JSON {
		t.Fatalf("status/json: %+v", r1)
	}
	o1 := mustJSON[out](t, r1.Body)
	if !o1.IsPrime || o1.Method != "division" || o1.N != 97 {
		t.Fatalf("payload: %+v", o1)
	}

	// Composite
	r2 := IsPrimeJSONCtx(ctxBg(), map[string]string{"n": "100", "method": "division"})
	o2 := mustJSON[out](t, r2.Body)
	if o2.IsPrime {
		t.Fatalf("100 is not prime: %+v", o2)
	}
}

func TestIsPrimeJSONCtx_MillerRabin_Default(t *testing.T) {
	t.Parallel()
	type out struct {
		IsPrime bool   `json:"is_prime"`
		Method  string `json:"method"`
	}
	r := IsPrimeJSONCtx(ctxBg(), map[string]string{"n": "101", "method": "miller-rabin"})
	if r.Status != 200 {
		t.Fatalf("status: %+v", r)
	}
	o := mustJSON[out](t, r.Body)
	if !o.IsPrime || o.Method != "miller-rabin" {
		t.Fatalf("payload: %+v", o)
	}
}

func TestIsPrimeJSONCtx_Validation(t *testing.T) {
	t.Parallel()
	if r := IsPrimeJSONCtx(ctxBg(), map[string]string{}); r.Status != 400 {
		t.Fatalf("missing n should 400: %+v", r)
	}
	if r := IsPrimeJSONCtx(ctxBg(), map[string]string{"n": "-2"}); r.Status != 400 {
		t.Fatalf("negative n should 400: %+v", r)
	}
	if r := IsPrimeJSONCtx(ctxBg(), map[string]string{"n": "10", "method": "x"}); r.Status != 400 {
		t.Fatalf("bad method should 400: %+v", r)
	}
}


// Ahora: solo atajos de division 
func TestIsPrimeJSONCtx_Division_Shortcuts(t *testing.T) {
	t.Parallel()
	type out struct{ IsPrime bool `json:"is_prime"` }

	// n<2 -> false
	for _, n := range []string{"0", "1"} {
		r := IsPrimeJSONCtx(ctxBg(), map[string]string{"n": n, "method": "division"})
		if r.Status != 200 { t.Fatalf("status for n=%s: %+v", n, r) }
		if mustJSON[out](t, r.Body).IsPrime {
			t.Fatalf("%s should be composite", n)
		}
	}
	// n==2 || n==3 -> true
	for _, n := range []string{"2", "3"} {
		r := IsPrimeJSONCtx(ctxBg(), map[string]string{"n": n, "method": "division"})
		if !mustJSON[out](t, r.Body).IsPrime {
			t.Fatalf("%s should be prime", n)
		}
	}
	// even >2 -> false
	if r := IsPrimeJSONCtx(ctxBg(), map[string]string{"n": "200", "method": "division"}); mustJSON[out](t, r.Body).IsPrime {
		t.Fatalf("200 must be composite")
	}
}

// Nuevo: cancelación en MR -> devuelve 200 con is_prime=false
func TestIsPrimeJSONCtx_MillerRabin_CancelReturnsFalse(t *testing.T) {
	t.Parallel()
	type out struct{ IsPrime bool `json:"is_prime"` }

	// número grande pero válido en int64
	n := "9223372036854775783" // < 2^63-1

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := IsPrimeJSONCtx(ctx, map[string]string{"n": n, "method": "miller-rabin"})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("status/json: %+v", r)
	}
	if mustJSON[out](t, r.Body).IsPrime {
		t.Fatalf("canceled MR should report false")
	}
}

/********** mrIsPrime64Ctx (no exportado) **********/

func TestMrIsPrime64Ctx_Shortcuts(t *testing.T) {
	t.Parallel()
	// Primo pequeño igual a una base -> true temprano
	if !mrIsPrime64Ctx(context.Background(), 17) {
		t.Fatalf("17 should be prime")
	}
	// Compuesto divisible por primo pequeño -> false temprano
	if mrIsPrime64Ctx(context.Background(), 21) {
		t.Fatalf("21 should be composite")
	}
	// Cancelación: ctx cancelado debe cortar (devuelve false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if mrIsPrime64Ctx(ctx, 18446744073709551557) { // número grande impar
		t.Fatalf("canceled MR should not return true")
	}
}

/********** FactorJSONCtx **********/

func TestFactorJSONCtx_Basic(t *testing.T) {
	t.Parallel()
	type out struct {
		N       int64      `json:"n"`
		Factors [][2]int64 `json:"factors"`
	}
	// 60 = 2^2 * 3 * 5
	r := FactorJSONCtx(ctxBg(), map[string]string{"n": "60"})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("status/json: %+v", r)
	}
	o := mustJSON[out](t, r.Body)
	want := [][2]int64{{2, 2}, {3, 1}, {5, 1}}
	if len(o.Factors) != len(want) {
		t.Fatalf("factors len=%d want=%d (%+v)", len(o.Factors), len(want), o.Factors)
	}
	for i := range want {
		if o.Factors[i] != want[i] {
			t.Fatalf("factors[%d]=%v want %v", i, o.Factors[i], want[i])
		}
	}
}

func TestFactorJSONCtx_PrimeInput(t *testing.T) {
	t.Parallel()
	type out struct{ Factors [][2]int64 }
	r := FactorJSONCtx(ctxBg(), map[string]string{"n": "97"})
	o := mustJSON[out](t, r.Body)
	if len(o.Factors) != 1 || o.Factors[0][0] != 97 || o.Factors[0][1] != 1 {
		t.Fatalf("97 factors: %+v", o.Factors)
	}
}

func TestFactorJSONCtx_Validation(t *testing.T) {
	t.Parallel()
	if r := FactorJSONCtx(ctxBg(), map[string]string{}); r.Status != 400 {
		t.Fatalf("missing n: %+v", r)
	}
	if r := FactorJSONCtx(ctxBg(), map[string]string{"n": "1"}); r.Status != 400 {
		t.Fatalf("n<2: %+v", r)
	}
	if r := FactorJSONCtx(ctxBg(), map[string]string{"n": "x"}); r.Status != 400 {
		t.Fatalf("bad int: %+v", r)
	}
}

/********** PiJSONCtx **********/

func TestPiJSONCtx_Spigot_And_Chudnovsky(t *testing.T) {
	t.Parallel()
	type out struct {
		Digits int    `json:"digits"`
		Method string `json:"method"`
		Pi     string `json:"pi"`
	}
	for _, m := range []string{"spigot", "chudnovsky"} {
		r := PiJSONCtx(ctxBg(), map[string]string{"digits": "8", "method": m})
		if r.Status != 200 || !r.JSON {
			t.Fatalf("[%s] status/json: %+v", m, r)
		}
		o := mustJSON[out](t, r.Body)
		if o.Method != m {
			t.Fatalf("[%s] method mismatch: %+v", m, o)
		}
		if !strings.HasPrefix(o.Pi, "3.") || len(o.Pi) != 2+o.Digits {
			t.Fatalf("[%s] pi format: %q", m, o.Pi)
		}
	}
}

func TestPiJSONCtx_Validation(t *testing.T) {
	t.Parallel()
	if r := PiJSONCtx(ctxBg(), map[string]string{}); r.Status != 400 {
		t.Fatalf("missing digits: %+v", r)
	}
	if r := PiJSONCtx(ctxBg(), map[string]string{"digits": "0"}); r.Status != 400 {
		t.Fatalf("digits<1: %+v", r)
	}
	if r := PiJSONCtx(ctxBg(), map[string]string{"digits": "5", "method": "x"}); r.Status != 400 {
		t.Fatalf("bad method: %+v", r)
	}
}

// NUEVO: piSpigotCtx casos n<=0 y cancelación
func TestPiSpigotCtx_NonPositive_And_Cancel(t *testing.T) {
	t.Parallel()
	// n<=0 -> "3", 0 iters, no truncado
	if s, it, trunc := piSpigotCtx(context.Background(), 0); s != "3" || it != 0 || trunc {
		t.Fatalf("n<=0 => '3', got s=%q it=%d trunc=%v", s, it, trunc)
	}
	// Cancelación temprana
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, _, trunc := piSpigotCtx(ctx, 4000)
	if !trunc || !strings.HasPrefix(s, "3.") {
		t.Fatalf("expected truncated pi after cancel, got %q trunc=%v", s, trunc)
	}
}

/********** MandelbrotJSONCtx **********/

func TestMandelbrotJSONCtx_SmallImage(t *testing.T) {
	t.Parallel()
	type out struct {
		Width   int     `json:"width"`
		Height  int     `json:"height"`
		MaxIter int     `json:"max_iter"`
		Map     [][]int `json:"map"`
	}
	r := MandelbrotJSONCtx(ctxBg(), map[string]string{
		"width": "8", "height": "6", "max_iter": "20",
	})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("status/json: %+v", r)
	}
	o := mustJSON[out](t, r.Body)
	if o.Width != 8 || o.Height != 6 || len(o.Map) != 6 || len(o.Map[0]) != 8 {
		t.Fatalf("dimensions mismatch: %+v", o)
	}
	for y := 0; y < o.Height; y++ {
		for x := 0; x < o.Width; x++ {
			if o.Map[y][x] < 0 || o.Map[y][x] > o.MaxIter {
				t.Fatalf("iter out of range at (%d,%d) = %d", x, y, o.Map[y][x])
			}
		}
	}
}

func TestMandelbrotJSONCtx_Validation_And_Cancel(t *testing.T) {
	t.Parallel()
	if r := MandelbrotJSONCtx(ctxBg(), map[string]string{}); r.Status != 400 {
		t.Fatalf("missing params: %+v", r)
	}
	if r := MandelbrotJSONCtx(ctxBg(), map[string]string{
		"width": "-1", "height": "2", "max_iter": "3",
	}); r.Status != 400 {
		t.Fatalf("non-positive: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := MandelbrotJSONCtx(ctx, map[string]string{
		"width": "64", "height": "64", "max_iter": "50",
	})
	if r.Status != 503 || r.Err == nil {
		t.Fatalf("expected 503 on cancel: %+v", r)
	}
}

/********** MatrixMulHashCtx **********/

func TestMatrixMulHashCtx_Deterministic(t *testing.T) {
	t.Parallel()
	type out struct {
		Size int    `json:"size"`
		Seed int64  `json:"seed"`
		Hash string `json:"result_sha256"`
	}
	r1 := MatrixMulHashCtx(ctxBg(), map[string]string{"size": "3", "seed": "42"})
	r2 := MatrixMulHashCtx(ctxBg(), map[string]string{"size": "3", "seed": "42"})
	if r1.Status != 200 || r2.Status != 200 {
		t.Fatalf("status r1=%+v r2=%+v", r1, r2)
	}
	o1 := mustJSON[out](t, r1.Body)
	o2 := mustJSON[out](t, r2.Body)
	if o1.Hash != o2.Hash || o1.Size != 3 || o2.Size != 3 {
		t.Fatalf("determinism/hash mismatch: %q vs %q", o1.Hash, o2.Hash)
	}
	// Seed diferente => hash diferente (muy probable)
	r3 := MatrixMulHashCtx(ctxBg(), map[string]string{"size": "3", "seed": "43"})
	o3 := mustJSON[out](t, r3.Body)
	if o3.Hash == o1.Hash {
		t.Fatalf("different seed produced same hash: %q", o3.Hash)
	}
}

func TestMatrixMulHashCtx_Validation_And_Cancel(t *testing.T) {
	t.Parallel()
	if r := MatrixMulHashCtx(ctxBg(), map[string]string{}); r.Status != 400 {
		t.Fatalf("missing params: %+v", r)
	}
	if r := MatrixMulHashCtx(ctxBg(), map[string]string{"size": "0", "seed": "1"}); r.Status != 400 {
		t.Fatalf("size<=0: %+v", r)
	}
	if r := MatrixMulHashCtx(ctxBg(), map[string]string{"size": "2", "seed": "x"}); r.Status != 400 {
		t.Fatalf("bad seed: %+v", r)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := MatrixMulHashCtx(ctx, map[string]string{"size": "64", "seed": "7"})
	if r.Status != 503 || r.Err == nil {
		t.Fatalf("expected 503 on cancel: %+v", r)
	}
}

/********** tiempos **********/

func testWipeDataDir() {
	_ = os.MkdirAll(dataDir, 0o755)
	ents, err := os.ReadDir(dataDir)
	if err != nil {
		return
	}
	for _, e := range ents {
		_ = os.RemoveAll(filepath.Join(dataDir, e.Name()))
	}
}

func TestMain(m *testing.M) {
	// Timeout global razonable para evitar cuelgues
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	done := make(chan int, 1)
	go func() { done <- m.Run() }()

	code := 0
	select {
	case code = <-done:
	case <-ctx.Done():
		code = 1
	}

	// SIEMPRE limpiar /app/data al final
	testWipeDataDir()

	os.Exit(code)
}



func TestIsPrimeJSONCtx_MillerRabin_KnownComposite(t *testing.T) {
	t.Parallel()
	type out struct{ IsPrime bool `json:"is_prime"` }

	// Número de Carmichael (compuesto) detectado por MR: 561 = 3*11*17
	r := IsPrimeJSONCtx(ctxBg(), map[string]string{"n": "561", "method": "miller-rabin"})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("status/json: %+v", r)
	}
	if mustJSON[out](t, r.Body).IsPrime {
		t.Fatalf("561 es compuesto; MR debe devolver false")
	}
}

// ====== mrIsPrime64Ctx: camino interno de squaring (no atajo de pequeños) ======

func TestMrIsPrime64Ctx_InnerSquarePath(t *testing.T) {
	t.Parallel()
	// 341 = 11 * 31 (Fermat pseudoprime para base 2, MR lo detecta como compuesto)
	if mrIsPrime64Ctx(context.Background(), 341) {
		t.Fatalf("341 es compuesto; MR debe detectarlo")
	}
}

// ====== FactorJSONCtx: potencia perfecta para cubrir bucle de conteo ======

func TestFactorJSONCtx_PerfectPowerCounts(t *testing.T) {
	t.Parallel()
	type out struct{ Factors [][2]int64 }

	// 81 = 3^4 -> una sola entrada con exponente 4 (ejercita el bucle interno)
	r := FactorJSONCtx(ctxBg(), map[string]string{"n": "81"})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("status/json: %+v", r)
	}
	o := mustJSON[out](t, r.Body)
	if len(o.Factors) != 1 || o.Factors[0][0] != 3 || o.Factors[0][1] != 4 {
		t.Fatalf("factores de 81: %+v (esperado [[3 4]])", o.Factors)
	}
}

// ====== piSpigotCtx: forzar ramas q==9 y q==10 con más dígitos ======

func TestPiSpigotCtx_DeepStates(t *testing.T) {
	t.Parallel()
	// Más dígitos ejercitan casos q==9 y q==10 del spigot
	const d = 200
	s, it, trunc := piSpigotCtx(context.Background(), d)
	if trunc {
		t.Fatalf("no debería truncar con ctx activo")
	}
	if !strings.HasPrefix(s, "3.") || len(s) != 2+d {
		t.Fatalf("formato pi: len=%d s=%q", len(s), s[:min(20, len(s))])
	}
	if it == 0 {
		t.Fatalf("esperábamos iteraciones > 0")
	}
}

func min(a, b int) int {
	if a < b { return a }
	return b
}

func TestIsPrimeJSONCtx_Division_OddComposite(t *testing.T) {
	t.Parallel()
	type out struct{ IsPrime bool `json:"is_prime"` }
	// 99 = 9*11 (impar compuesto) — fuerza el bucle de división por impares
	r := IsPrimeJSONCtx(ctxBg(), map[string]string{"n": "99", "method": "division"})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("status/json: %+v", r)
	}
	if mustJSON[out](t, r.Body).IsPrime {
		t.Fatalf("99 es compuesto")
	}
}

// ====== mrIsPrime64Ctx: primo grande (no está en la lista de "small") ======

func TestMrIsPrime64Ctx_PrimeLarge(t *testing.T) {
	t.Parallel()
	// 1,000,003 es primo y no está en {2,3,5,7,11,13,17,19,23,29,31,37}
	// -> fuerza a recorrer el bucle de bases y el squaring interno.
	if !mrIsPrime64Ctx(context.Background(), 1000003) {
		t.Fatalf("1000003 debería ser primo")
	}
}

// ====== FactorJSONCtx: caso solo factor 2 (sin resto n>1 al final) ======

func TestFactorJSONCtx_OnlyTwos_NoRemainder(t *testing.T) {
	t.Parallel()
	type out struct{ Factors [][2]int64 }
	// 64 = 2^6 → solo una entrada (2,6) y NO debe agregarse factor final
	r := FactorJSONCtx(ctxBg(), map[string]string{"n": "64"})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("status/json: %+v", r)
	}
	o := mustJSON[out](t, r.Body)
	if len(o.Factors) != 1 || o.Factors[0][0] != 2 || o.Factors[0][1] != 6 {
		t.Fatalf("factores de 64: %+v (esperado [[2 6]])", o.Factors)
	}
}


// ---------- FactorJSONCtx: más cobertura (sin resto final, mezcla impares) ----------

func TestFactorJSONCtx_NoRemainderAndMixedOdd(t *testing.T) {
	t.Parallel()
	type out struct{ Factors [][2]int64 }

	// 45 = 3^2 * 5  → tras el bucle termina en n=1 (NO se agrega factor final)
	r1 := FactorJSONCtx(ctxBg(), map[string]string{"n": "45"})
	if r1.Status != 200 || !r1.JSON {
		t.Fatalf("status/json: %+v", r1)
	}
	o1 := mustJSON[out](t, r1.Body)
	want1 := [][2]int64{{3, 2}, {5, 1}}
	if len(o1.Factors) != len(want1) || o1.Factors[0] != want1[0] || o1.Factors[1] != want1[1] {
		t.Fatalf("45 factors: %+v want %v", o1.Factors, want1)
	}

	// 1155 = 3 * 5 * 7 * 11  → varios impares distintos (ejercita saltos de divisor)
	r2 := FactorJSONCtx(ctxBg(), map[string]string{"n": "1155"})
	o2 := mustJSON[out](t, r2.Body)
	want2 := [][2]int64{{3, 1}, {5, 1}, {7, 1}, {11, 1}}
	if len(o2.Factors) != len(want2) || o2.Factors[0] != want2[0] || o2.Factors[1] != want2[1] ||
		o2.Factors[2] != want2[2] || o2.Factors[3] != want2[3] {
		t.Fatalf("1155 factors: %+v want %v", o2.Factors, want2)
	}
}

// ---------- mrIsPrime64Ctx: compuestos “difíciles” y camino largo ----------

func TestMrIsPrime64Ctx_CarmichaelComposite(t *testing.T) {
	t.Parallel()
	// Carmichael clásico: 3215031751 (compuesto) – MR debe marcarlo compuesto
	if mrIsPrime64Ctx(context.Background(), 3215031751) {
		t.Fatalf("3215031751 es compuesto; MR debe detectarlo")
	}
}

func TestMrIsPrime64Ctx_PrimeLarge_LongPath(t *testing.T) {
	t.Parallel()
	// Primo “grande” que no cae en atajos de small primes y recorre squaring
	if !mrIsPrime64Ctx(context.Background(), 1000003) { // 1_000_003 es primo
		t.Fatalf("1000003 debería ser primo")
	}
}

// ---------- piSpigotCtx: cubrir rama de empuje del predigit final ----------

func TestPiSpigotCtx_FinalPredigitPush_ShortN(t *testing.T) {
	t.Parallel()
	// Para n=1, el código suele entrar en la rama que empuja el predigit final:
	s, it, trunc := piSpigotCtx(context.Background(), 1)
	if trunc {
		t.Fatalf("no debe truncar con ctx activo")
	}
	if !strings.HasPrefix(s, "3.") || len(s) != 3 { // "3."+1 dígito
		t.Fatalf("formato n=1: s=%q len=%d", s, len(s))
	}
	if it == 0 {
		t.Fatalf("esperamos iteraciones > 0")
	}
}
