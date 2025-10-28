package router

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"path/filepath"
	"os"

	"so-http10-demo/internal/jobs"
	"so-http10-demo/internal/resp"
	"so-http10-demo/internal/sched"
)

/* ---------------- helpers ---------------- */

func resetGlobals(t *testing.T) func() {
	t.Helper()
	oldMgr := manager
	oldJM := jobman

	manager = sched.NewManager()
	jobman = jobs.NewManager(manager, time.Minute)

	// Capturamos el jobman NUEVO para cerrar en cleanup sin hacer double-close
	newJM := jobman

	return func() {
		// Si el test ya lo cerró, Close() volverá a cerrar stopC → panic.
		// Lo envolvemos en recover para ignorar "close of closed channel" en cleanup.
		if newJM != nil {
			func() {
				defer func() { _ = recover() }()
				newJM.Close()
			}()
		}
		manager = oldMgr
		jobman = oldJM
	}
}

func mustRegisterPool(t *testing.T, name string, fn sched.TaskFunc, workers, cap int, start bool) {
	t.Helper()
	p := sched.NewPool(name, fn, workers, cap)
	if start {
		p.Start()
	}
	if err := manager.Register(name, p); err != nil {
		t.Fatalf("Register(%s): %v", name, err)
	}
}

// espera hasta d a que cond() sea true
func waitUntil(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

/* ---------------- tests: getDurEnv ---------------- */

func TestGetDurEnv_DefaultAndValidInvalid(t *testing.T) {
	t.Setenv("ROUTER_TEST_TIMEOUT", "")
	if got := getDurEnv("ROUTER_TEST_TIMEOUT", 42*time.Second); got != 42*time.Second {
		t.Fatalf("default mismatch: %v", got)
	}
	t.Setenv("ROUTER_TEST_TIMEOUT", "150ms")
	if got := getDurEnv("ROUTER_TEST_TIMEOUT", 42*time.Second); got != 150*time.Millisecond {
		t.Fatalf("valid env mismatch: %v", got)
	}
	t.Setenv("ROUTER_TEST_TIMEOUT", "abc")
	if got := getDurEnv("ROUTER_TEST_TIMEOUT", 42*time.Second); got != 42*time.Second {
		t.Fatalf("invalid env should fallback: %v", got)
	}
	t.Setenv("ROUTER_TEST_TIMEOUT", "0s")
	if got := getDurEnv("ROUTER_TEST_TIMEOUT", 42*time.Second); got != 42*time.Second {
		t.Fatalf("non-positive should fallback: %v", got)
	}
}

/* ---------------- tests: submitSync ---------------- */

func TestSubmitSync_NoPool(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	r, enq := submitSync("nope", nil, time.Second)
	if !enq {
		t.Fatalf("enq should be true on no_pool (behavior actual)")
	}
	if r.Err == nil || r.Err.Code != "no_pool" {
		t.Fatalf("expected no_pool error, got %#v", r)
	}
}

func TestSubmitSync_WithPool_OK(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	mustRegisterPool(t, "echo", func(ctx context.Context, _ map[string]string) resp.Result {
		return resp.PlainOK("ok")
	}, 1, 1, true)

	r, enq := submitSync("echo", nil, time.Second)
	if !enq {
		t.Fatalf("expected enq=true")
	}
	if r.Status != 200 || r.Body != "ok" {
		t.Fatalf("unexpected result: %#v", r)
	}
}

/* ---------------- tests: InitPools ---------------- */

func TestInitPools_RegistersKeyPools(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	cfg := map[string]int{
		"workers.sleep": 1, "queue.sleep": 1,
		"workers.spin": 1, "queue.spin": 1,

        "workers.isprime": 1, "queue.isprime": 1,
        "workers.factor": 1, "queue.factor": 1,
        "workers.pi": 1, "queue.pi": 1,
        "workers.mandelbrot": 1, "queue.mandelbrot": 1,
        "workers.matrixmul": 1, "queue.matrixmul": 1,

        "workers.wordcount": 1, "queue.wordcount": 1,
        "workers.grep": 1, "queue.grep": 1,
        "workers.hashfile": 1, "queue.hashfile": 1,
        "workers.sortfile": 1, "queue.sortfile": 1,
        "workers.compress": 1, "queue.compress": 1,
	}
	InitPools(cfg)

	for _, name := range []string{"sleep", "spin", "isprime"} {
		if _, ok := manager.Pool(name); !ok {
			t.Fatalf("pool %q not registered", name)
		}
	}
}

/* ---------------- tests: Dispatch (básicos y validaciones) ---------------- */

func TestDispatch_MethodAndBasics(t *testing.T) {
	// method != GET
	r := Dispatch("POST", "/")
	if r.Status != 400 || r.Err == nil || r.Err.Code != "method" {
		t.Fatalf("expected method error, got %#v", r)
	}

	// "/" hola mundo
	r = Dispatch("GET", "/")
	if r.Status != 200 || r.Body != "hola mundo\n" {
		t.Fatalf("unexpected root: %#v", r)
	}
}

func TestDispatch_Simulate_InvalidTask(t *testing.T) {
	r := Dispatch("GET", "/simulate?task=foo")
	if r.Status != 400 || r.Err == nil || r.Err.Code != "task" {
		t.Fatalf("expected task error, got %#v", r)
	}
}

func TestDispatch_Loadtest_ParamValidation(t *testing.T) {
	r := Dispatch("GET", "/loadtest?tasks=0&sleep=1")
	if r.Status != 400 || r.Err == nil || r.Err.Code != "tasks" {
		t.Fatalf("expected tasks validation error: %#v", r)
	}
	r = Dispatch("GET", "/loadtest?tasks=2&sleep=-1")
	if r.Status != 400 || r.Err == nil || r.Err.Code != "sleep" {
		t.Fatalf("expected sleep validation error: %#v", r)
	}
}

func TestDispatch_JobsSubmit_NoPool(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	// no pools → NotFound no_pool
	r := Dispatch("GET", "/jobs/submit?task=nope")
	if r.Status != 404 || r.Err == nil || r.Err.Code != "no_pool" {
		t.Fatalf("expected 404 no_pool, got %#v", r)
	}
}

func TestDispatch_JobsSubmit_StatusAndResultPaths(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	// registra sleep para que /jobs/submit acepte
	mustRegisterPool(t, "sleep", func(ctx context.Context, p map[string]string) resp.Result {
		// respeta cancelación si llega
		select {
		case <-ctx.Done():
			return resp.Unavail("canceled", "canceled")
		case <-time.After(100 * time.Millisecond):
			return resp.PlainOK("slept")
		}
	}, 1, 1, true)

	// submit con seconds=1
	res := Dispatch("GET", "/jobs/submit?task=sleep&seconds=1")
	if res.Status != 200 || !res.JSON {
		t.Fatalf("submit should return JSON 200, got %#v", res)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(res.Body), &obj); err != nil {
		t.Fatalf("unmarshal submit: %v", err)
	}
	id, _ := obj["job_id"].(string)
	if id == "" {
		t.Fatalf("job_id missing in submit response: %v", obj)
	}

	// status not found cuando id inválido
	st := Dispatch("GET", "/jobs/status?id=does-not-exist")
	if st.Status != 404 || st.Err == nil || st.Err.Code != "not_found" {
		t.Fatalf("status not_found expected, got %#v", st)
	}

	// result not found cuando id inválido
	rnf := Dispatch("GET", "/jobs/result?id=does-not-exist")
	if rnf.Status != 404 || rnf.Err == nil || rnf.Err.Code != "not_found" {
		t.Fatalf("result not_found expected, got %#v", rnf)
	}

	// result bad request cuando falta id
	rbad := Dispatch("GET", "/jobs/result")
	if rbad.Status != 400 || rbad.Err == nil || rbad.Err.Code != "id" {
		t.Fatalf("result id required expected, got %#v", rbad)
	}

	// cancel id faltante
	cc := Dispatch("GET", "/jobs/cancel")
	if cc.Status != 400 || cc.Err == nil || cc.Err.Code != "id" {
		t.Fatalf("cancel id required expected, got %#v", cc)
	}
}

/* ---------------- tests: PoolsSummary y Metrics ---------------- */

func TestPoolsSummaryAndMetrics(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	// pool simple
	mustRegisterPool(t, "echo", func(ctx context.Context, _ map[string]string) resp.Result {
		return resp.PlainOK("ok")
	}, 1, 1, true)

	// /metrics debe ser JSON válido
	r := Dispatch("GET", "/metrics")
	if r.Status != 200 || !r.JSON || r.Body == "" {
		t.Fatalf("metrics JSON expected, got %#v", r)
	}

	// PoolsSummary forma básica
	ps := PoolsSummary()
	v, ok := ps["echo"]
	if !ok {
		t.Fatalf("echo not present in PoolsSummary: %#v", ps)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("value not a map: %#v", v)
	}
	if _, ok := m["queue_len"]; !ok {
		t.Fatalf("queue_len missing")
	}
	if _, ok := m["queue_cap"]; !ok {
		t.Fatalf("queue_cap missing")
	}
	w, ok := m["workers"].(map[string]any)
	if !ok {
		t.Fatalf("workers missing/invalid: %#v", m)
	}
	if _, ok := w["total"]; !ok {
		t.Fatalf("workers.total missing")
	}
	if _, ok := w["busy"]; !ok {
		t.Fatalf("workers.busy missing")
	}
	if _, ok := w["idle"]; !ok {
		t.Fatalf("workers.idle missing")
	}
}

/* ---------------- tests: Close ---------------- */

func TestClose_NoPanic(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	// No debe paniquear aunque cleanup vuelva a cerrar el mismo jobman
	Close()
}

func TestInitPools_ExecuteCPUClosures_Robust(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	cfg := map[string]int{
		// básicos
		"workers.sleep": 1, "queue.sleep": 1,
		"workers.spin":  1, "queue.spin":  1,
		// CPU (activados para ejecutar closures)
		"workers.isprime": 1, "queue.isprime": 1,
		"workers.factor":  1, "queue.factor":  1,
		"workers.pi":      1, "queue.pi":      1,
		"workers.mandelbrot": 1, "queue.mandelbrot": 1,
		"workers.matrixmul":  1, "queue.matrixmul":  1,
		// IO desactivados (evitamos dependencias de archivos)
		"workers.wordcount": 0, "queue.wordcount": 0,
		"workers.grep":      0, "queue.grep": 0,
		"workers.hashfile":  0, "queue.hashfile": 0,
		"workers.sortfile":  0, "queue.sortfile": 0,
		"workers.compress":  0, "queue.compress": 0,
	}
	InitPools(cfg)

	// Ejecuta closures "sleep" y "spin" (cuentan como líneas dentro de InitPools)
	for _, target := range []string{
		"/sleep?seconds=0",
		"/simulate?task=sleep&seconds=0",
		"/simulate?task=spin&ms=1",
	} {
		r := Dispatch("GET", target)
		if r.Status >= 500 {
			t.Fatalf("%s => %#v", target, r)
		}
	}

	// Ejecuta closures CPU. Aceptamos 200 o 400 (según validación del handler),
	// lo importante es que la closure se ejecute (Status != 500).
	for _, target := range []string{
		"/isprime?num=7",   // si tu handler usa 'n', también probamos abajo:
		"/isprime?n=7",
		"/factor?num=12",
		"/factor?n=12",
		"/pi?digits=1",
		"/mandelbrot?w=1&h=1",
		"/matrixmul?n=1",
	} {
		r := Dispatch("GET", target)
		if r.Status >= 500 {
			t.Fatalf("%s => %#v", target, r)
		}
	}
}


func TestDispatch_BasicRoutes_And_JobsFlow(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	// Registramos un pool "sleep" mínimo para que jobman pueda ejecutar algo rápido
	mustRegisterPool(t, "sleep", func(ctx context.Context, p map[string]string) resp.Result {
		// respetar cancel si llega; en caso contrario terminar rápido
		select {
		case <-ctx.Done():
			return resp.Unavail("canceled", "canceled")
		case <-time.After(20 * time.Millisecond):
			return resp.PlainOK("slept")
		}
	}, 1, 1, true)

	// --- Rutas básicas del router ---
	if r := Dispatch("GET", "/help"); r.Status != 200 { t.Fatalf("/help => %v", r) }
	if r := Dispatch("GET", "/timestamp"); r.Status != 200 { t.Fatalf("/timestamp => %v", r) }
	if r := Dispatch("GET", "/reverse?text=abc"); r.Status != 200 { t.Fatalf("/reverse => %v", r) }
	if r := Dispatch("GET", "/toupper?text=abc"); r.Status != 200 { t.Fatalf("/toupper => %v", r) }
	if r := Dispatch("GET", "/hash?text=a"); r.Status != 200 { t.Fatalf("/hash => %v", r) }
	if r := Dispatch("GET", "/random?count=1&min=0&max=0"); r.Status != 200 { t.Fatalf("/random => %v", r) }
	if r := Dispatch("GET", "/fibonacci?num=5"); r.Status != 200 { t.Fatalf("/fibonacci => %v", r) }

	// Not found
	if r := Dispatch("GET", "/no-such-route"); r.Status != 404 { t.Fatalf("not_found => %v", r) }

	// --- Métricas ---
	if r := Dispatch("GET", "/metrics"); r.Status != 200 || !r.JSON {
		t.Fatalf("/metrics => %v", r)
	}

	// --- Jobs: submit → status → result(not_ready) → cancel → list ---
	sub := Dispatch("GET", "/jobs/submit?task=sleep&seconds=1")
	if sub.Status != 200 || !sub.JSON { t.Fatalf("/jobs/submit => %v", sub) }
	var obj map[string]any
	if err := json.Unmarshal([]byte(sub.Body), &obj); err != nil { t.Fatalf("unmarshal submit: %v", err) }
	id, _ := obj["job_id"].(string)
	if id == "" { t.Fatalf("missing job_id in submit") }

	// status válido
	st := Dispatch("GET", "/jobs/status?id="+id)
	if st.Status != 200 || !st.JSON { t.Fatalf("/jobs/status => %v", st) }

	// result not_ready (mientras corre)
	res := Dispatch("GET", "/jobs/result?id="+id)
	if res.Status != 400 || res.Err == nil || res.Err.Code != "not_ready" {
		t.Fatalf("/jobs/result not_ready => %v", res)
	}

	// cancel aceptar
	cx := Dispatch("GET", "/jobs/cancel?id="+id)
	if cx.Status != 200 || !cx.JSON { t.Fatalf("/jobs/cancel => %v", cx) }

	// list
	lj := Dispatch("GET", "/jobs/list")
	if lj.Status != 200 || !lj.JSON { t.Fatalf("/jobs/list => %v", lj) }

	// (opcional) esperar que termine cancelado para no dejar goroutine colgando
	_ = waitUntil(800*time.Millisecond, func() bool {
		js := Dispatch("GET", "/jobs/status?id="+id)
		var v map[string]any
		_ = json.Unmarshal([]byte(js.Body), &v)
		return v["status"] == string(jobs.StatusCanceled)
	})
}

func TestDispatch_CPUAndIORoutes_WithStubPools(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	// Registrar stubs muy rápidos para todas las rutas CPU/IO
	names := []string{
		"isprime", "factor", "pi", "mandelbrot", "matrixmul",
		"wordcount", "grep", "hashfile", "sortfile", "compress",
	}
	for _, n := range names {
		mustRegisterPool(t, n, func(ctx context.Context, p map[string]string) resp.Result {
			return resp.PlainOK(n + "-ok")
		}, 1, 1, true)
	}

	// CPU
	if r := Dispatch("GET", "/isprime?num=7"); r.Status != 200 { t.Fatalf("/isprime => %v", r) }
	if r := Dispatch("GET", "/factor?num=12"); r.Status != 200 { t.Fatalf("/factor => %v", r) }
	if r := Dispatch("GET", "/pi?digits=1"); r.Status != 200 { t.Fatalf("/pi => %v", r) }
	if r := Dispatch("GET", "/mandelbrot?w=1&h=1"); r.Status != 200 { t.Fatalf("/mandelbrot => %v", r) }
	if r := Dispatch("GET", "/matrixmul?n=1"); r.Status != 200 { t.Fatalf("/matrixmul => %v", r) }

	// IO
	if r := Dispatch("GET", "/wordcount"); r.Status != 200 { t.Fatalf("/wordcount => %v", r) }
	if r := Dispatch("GET", "/grep"); r.Status != 200 { t.Fatalf("/grep => %v", r) }
	if r := Dispatch("GET", "/hashfile"); r.Status != 200 { t.Fatalf("/hashfile => %v", r) }
	if r := Dispatch("GET", "/sortfile"); r.Status != 200 { t.Fatalf("/sortfile => %v", r) }
	if r := Dispatch("GET", "/compress"); r.Status != 200 { t.Fatalf("/compress => %v", r) }
}

func TestInitPools_AllClosures_Robust_CPU(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	// Activamos sleep/spin + TODOS los CPU
	cfg := map[string]int{
		"workers.sleep": 1, "queue.sleep": 1,
		"workers.spin":  1, "queue.spin":  1,

		"workers.isprime": 1, "queue.isprime": 1,
		"workers.factor":  1, "queue.factor":  1,
		"workers.pi":      1, "queue.pi":      1,
		"workers.mandelbrot": 1, "queue.mandelbrot": 1,
		"workers.matrixmul":  1, "queue.matrixmul":  1,

		// IO desactivados aquí (los cubrimos en otra prueba)
		"workers.wordcount": 0, "queue.wordcount": 0,
		"workers.grep":      0, "queue.grep": 0,
		"workers.hashfile":  0, "queue.hashfile": 0,
		"workers.sortfile":  0, "queue.sortfile": 0,
		"workers.compress":  0, "queue.compress": 0,
	}
	InitPools(cfg)

	// Ejecuta closures básicas (sleep/spin) y de CPU; aceptamos != 5xx.
	targets := []string{
		"/sleep?seconds=0",
		"/simulate?task=sleep&seconds=0",
		"/simulate?task=spin&ms=1",

		"/isprime?num=7",
		"/isprime?n=7",
		"/factor?num=12",
		"/factor?n=12",
		"/pi?digits=1",
		"/mandelbrot?w=1&h=1",
		"/matrixmul?n=1",
	}
	for _, tg := range targets {
		r := Dispatch("GET", tg)
		if r.Status >= 500 {
			t.Fatalf("%s => %#v", tg, r)
		}
	}
}

func TestInitPools_AllClosures_Robust_IO(t *testing.T) {
	cleanup := resetGlobals(t)
	defer cleanup()

	// Activamos sleep (lo usa loadtest) + TODOS los IO
	cfg := map[string]int{
		"workers.sleep": 1, "queue.sleep": 1,
		"workers.spin":  0, "queue.spin":  0,

		// CPU off aquí
		"workers.isprime": 0, "queue.isprime": 0,
		"workers.factor":  0, "queue.factor":  0,
		"workers.pi":      0, "queue.pi":      0,
		"workers.mandelbrot": 0, "queue.mandelbrot": 0,
		"workers.matrixmul":  0, "queue.matrixmul":  0,

		// IO on
		"workers.wordcount": 1, "queue.wordcount": 1,
		"workers.grep":      1, "queue.grep":      1,
		"workers.hashfile":  1, "queue.hashfile":  1,
		"workers.sortfile":  1, "queue.sortfile":  1,
		"workers.compress":  1, "queue.compress":  1,
	}
	InitPools(cfg)

	// Archivos temporales para las rutas que típicamente piden path
	td := t.TempDir()
	fileA := filepath.Join(td, "a.txt")
	fileB := filepath.Join(td, "b.txt")
	content := []byte("a\nb\na\nc\n")
	if err := os.WriteFile(fileA, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// fileB se deja sin crear para probar 400/404 en algunas rutas (≠ 500)

	// Disparamos TODAS las IO closures; aceptamos cualquier estado < 500.
	targets := []string{
		// wordcount/grep: probamos variantes con text y con path
		"/wordcount?text=a+b+a",
		"/wordcount?path=" + fileA,
		"/grep?pattern=a&text=aaa",
		"/grep?pattern=a&path=" + fileA,

		// hashfile/sortfile/compress: con path válido
		"/hashfile?path=" + fileA,
		"/sortfile?path=" + fileA,     // si requiere destino adicional, devolverá 400 (ok)
		"/compress?path=" + fileA,     // puede pedir algo como algo=zip; 400 también vale

		// y algunas con path inexistente para forzar validaciones (≠500)
		"/hashfile?path=" + fileB,
		"/sortfile?path=" + fileB,
		"/compress?path=" + fileB,

		// loadtest (usa sleep del bloque IO/Simulación)
		"/loadtest?tasks=2&sleep=0",
	}
	for _, tg := range targets {
		r := Dispatch("GET", tg)
		if r.Status >= 500 {
			t.Fatalf("%s => %#v", tg, r)
		}
	}
}
