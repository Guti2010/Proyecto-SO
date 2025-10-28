package sched

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"testing"
	"time"

	"so-http10-demo/internal/resp"
)

/* ================= helpers ================= */

func waitUntil(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

/* ================= stat / imax ================= */

func TestStatAddSnapshot(t *testing.T) {
	var s stat
	s.add(1)
	s.add(2)
	s.add(3)
	n, mean, std := s.snapshot()
	if n != 3 {
		t.Fatalf("n=3, got %d", n)
	}
	if math.Abs(mean-2.0) > 1e-9 {
		t.Fatalf("mean=2, got %v", mean)
	}
	if math.Abs(std-1.0) > 1e-9 {
		t.Fatalf("std=1, got %v", std)
	}
}

func TestIMax(t *testing.T) {
	if imax(2, 1) != 2 {
		t.Fatal("imax(2,1) != 2")
	}
	if imax(1, 3) != 3 {
		t.Fatal("imax(1,3) != 3")
	}
}

/* ================= NewPool / Close / Start ================= */

func TestNewPoolDistributionAndDefaults(t *testing.T) {
	// capacity <= 0 -> 1; workers <= 0 -> 1
	p := NewPool("x", func(ctx context.Context, _ map[string]string) resp.Result { return resp.PlainOK("ok") }, 0, 0)
	if p.total != 1 {
		t.Fatalf("workers default 1, got %d", p.total)
	}
	if cap(p.qHigh) < 1 || cap(p.qNorm) < 1 || cap(p.qLow) < 1 {
		t.Fatalf("all queues must have at least cap=1: %d %d %d", cap(p.qHigh), cap(p.qNorm), cap(p.qLow))
	}

	// reparto 1:2:1 para una capacidad mayor
	p2 := NewPool("y", func(ctx context.Context, _ map[string]string) resp.Result { return resp.PlainOK("ok") }, 2, 8)
	if cap(p2.qHigh) != 2 || cap(p2.qNorm) != 4 || cap(p2.qLow) != 2 {
		t.Fatalf("esperado 2/4/2, got %d/%d/%d", cap(p2.qHigh), cap(p2.qNorm), cap(p2.qLow))
	}
}

func TestCloseIdempotent(t *testing.T) {
	p := NewPool("c", func(ctx context.Context, _ map[string]string) resp.Result { return resp.PlainOK("ok") }, 1, 1)
	p.Close()
	// no debe paniquear en segundo close
	p.Close()
	if !p.closed {
		t.Fatalf("pool debe estar cerrado")
	}
}

func TestStartCalledOnce(t *testing.T) {
	p := NewPool("once", func(ctx context.Context, _ map[string]string) resp.Result { time.Sleep(5 * time.Millisecond); return resp.PlainOK("ok") }, 1, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); p.Start() }()
	go func() { defer wg.Done(); p.Start() }()
	wg.Wait()

	// Enviar un trabajo para verificar que hay al menos un worker vivo
	ctx := context.Background()
	res, enq := p.SubmitAndWaitCtx(ctx, "a", nil, 500*time.Millisecond)
	if !enq || res.Status != 200 {
		t.Fatalf("submit after Start => %v, enq=%v", res, enq)
	}
}

/* ================= Prioridad high > low ================= */

func TestPriorityHighBeatsLowOnStart(t *testing.T) {
	started := make(chan string, 2)

	p := NewPool("prio", func(ctx context.Context, params map[string]string) resp.Result {
		which := params["which"]
		started <- which
		return resp.PlainOK(which)
	}, 1, 8)

	// Encolamos directo en las colas antes de arrancar el worker
	wh := work{
		id:       "H",
		ctx:      context.Background(),
		params:   map[string]string{"which": "high"},
		enqueued: time.Now(),
		done:     make(chan resp.Result, 1),
	}
	wl := work{
		id:       "L",
		ctx:      context.Background(),
		params:   map[string]string{"which": "low"},
		enqueued: time.Now(),
		done:     make(chan resp.Result, 1),
	}
	// Importante: poner AMBOS antes de arrancar
	p.qLow <- wl
	p.qHigh <- wh

	// Ahora arrancamos
	p.Start()

	// Debe arrancar "high" primero
	first := <-started
	second := <-started
	if first != "high" || second != "low" {
		t.Fatalf("esperado high luego low, got %q then %q", first, second)
	}
}

/* ================= SubmitAndWaitCtx rutas ================= */

func TestSubmitAndWaitCtx_PoolClosed(t *testing.T) {
	p := NewPool("closed", func(ctx context.Context, _ map[string]string) resp.Result { return resp.PlainOK("ok") }, 1, 1)
	p.Close()
	r, enq := p.SubmitAndWaitCtx(context.Background(), "id", nil, 50*time.Millisecond)
	if !enq || r.Err == nil || r.Err.Code != "closed" {
		t.Fatalf("esperado closed,true; got enq=%v res=%#v", enq, r)
	}
}

func TestSubmitAndWaitCtx_BackpressureReject(t *testing.T) {
	// No arrancamos el worker; llenamos la cola norm y pedimos encolado con timeout corto
	p := NewPool("bp", func(ctx context.Context, _ map[string]string) resp.Result { return resp.PlainOK("ok") }, 1, 1)

	// Llenar qNorm con un trabajo "falso" (no será consumido)
	p.qNorm <- work{
		id:       "fill",
		ctx:      context.Background(),
		params:   map[string]string{},
		enqueued: time.Now(),
		done:     make(chan resp.Result, 1),
	}

	r, enq := p.SubmitAndWaitCtx(context.Background(), "id2", map[string]string{}, 10*time.Millisecond)
	if enq || r.Err == nil || r.Err.Code != "backpressure" {
		t.Fatalf("esperado backpressure,false; got enq=%v res=%#v", enq, r)
	}

	// Verifica contador rejected
	m := p.metrics()
	if m["rejected"].(uint64) == 0 {
		t.Fatalf("rejected no incrementó")
	}
}

func TestSubmitAndWaitCtx_CancelBeforeEnqueue(t *testing.T) {
	p := NewPool("preenqcancel", func(ctx context.Context, _ map[string]string) resp.Result { return resp.PlainOK("ok") }, 1, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancela antes de encolar

	r, enq := p.SubmitAndWaitCtx(ctx, "id", nil, 100*time.Millisecond)
	if !enq || r.Err == nil || r.Err.Code != "canceled" {
		t.Fatalf("esperado canceled,true antes de encolar; got enq=%v res=%#v", enq, r)
	}
}

func TestSubmitAndWaitCtx_SuccessAndMetricsAndHeader(t *testing.T) {
	p := NewPool("runok", func(ctx context.Context, params map[string]string) resp.Result {
		return resp.PlainOK("ok")
	}, 1, 2)
	p.Start()

	r, enq := p.SubmitAndWaitCtx(context.Background(), "id", map[string]string{}, 500*time.Millisecond)
	if !enq || r.Status != 200 {
		t.Fatalf("run ok => enq=%v res=%#v", enq, r)
	}
	if r.Headers["X-Worker-Id"] == "" {
		t.Fatalf("X-Worker-Id no seteado")
	}
	m := p.metrics()
	if m["submitted"].(uint64) != 1 || m["completed"].(uint64) < 1 {
		t.Fatalf("counters inesperados: %+v", m)
	}
}

func TestSubmitAndWaitCtx_ExecutionTimeout(t *testing.T) {
	p := NewPool("runto", func(ctx context.Context, params map[string]string) resp.Result {
		time.Sleep(100 * time.Millisecond) // más lento que el timeout
		return resp.PlainOK("late")
	}, 1, 1)
	p.Start()

	r, enq := p.SubmitAndWaitCtx(context.Background(), "id", map[string]string{}, 20*time.Millisecond)
	if !enq || r.Err == nil || r.Err.Code != "timeout" {
		t.Fatalf("esperado timeout,true; got enq=%v res=%#v", enq, r)
	}
}

func TestSubmitAndWaitCtx_PreRunCancelFromWorkerPath(t *testing.T) {
	// Encolamos un trabajo con ctx ya cancelado, sin worker activo; luego Start.
	// El worker tomará el job y disparará la rama "job canceled before run".
	p := NewPool("preruncancel", func(ctx context.Context, _ map[string]string) resp.Result {
		return resp.PlainOK("should-not-run")
	}, 1, 2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := work{
		id:       "w1",
		ctx:      ctx,
		params:   map[string]string{},
		enqueued: time.Now(),
		done:     make(chan resp.Result, 1),
	}
	p.qNorm <- w

	// Arranca el worker y espera el resultado
	p.Start()
	var res resp.Result
	select {
	case res = <-w.done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout esperando respuesta del worker")
	}

	if res.Err == nil || res.Err.Code != "canceled" {
		t.Fatalf("esperado canceled desde rama worker, got %#v", res)
	}
}

func TestSubmitAndWait_Helper(t *testing.T) {
	p := NewPool("helper", func(ctx context.Context, _ map[string]string) resp.Result { return resp.PlainOK("ok") }, 1, 1)
	p.Start()
	r, enq := p.SubmitAndWait(map[string]string{}, 200*time.Millisecond)
	if !enq || r.Status != 200 {
		t.Fatalf("SubmitAndWait => enq=%v res=%#v", enq, r)
	}
}

/* ================= metrics() shape ================= */

func TestMetricsShapeAndBusy(t *testing.T) {
	p := NewPool("metrics", func(ctx context.Context, _ map[string]string) resp.Result {
		time.Sleep(30 * time.Millisecond)
		return resp.PlainOK("ok")
	}, 1, 4)
	p.Start()

	// Mientras corre, busy debería ser 1 en algún momento
	started := make(chan struct{}, 1)
	go func() {
		started <- struct{}{}
		p.SubmitAndWaitCtx(context.Background(), "id", nil, 500*time.Millisecond)
	}()

	<-started
	okBusy := waitUntil(200*time.Millisecond, func() bool {
		m := p.metrics()
		w := m["workers"].(map[string]any)
		return w["busy"].(int64) >= 1
	})
	if !okBusy {
		t.Fatal("busy nunca fue >=1")
	}

	// Al finalizar, submitted/completed deben ser >=1 y rejected 0
	okDone := waitUntil(800*time.Millisecond, func() bool {
		m := p.metrics()
		return m["submitted"].(uint64) >= 1 && m["completed"].(uint64) >= 1
	})
	if !okDone {
		t.Fatal("counters no se actualizaron a tiempo")
	}

	m := p.metrics()
	if m["rejected"].(uint64) != 0 {
		t.Fatalf("rejected debe ser 0, got %v", m["rejected"])
	}

	// JSON de manager para pools
	mgr := NewManager()
	if err := mgr.Register("metrics", p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	var decoded map[string]any
	if e := json.Unmarshal([]byte(mgr.MetricsJSON()), &decoded); e != nil {
		t.Fatalf("metrics JSON inválido: %v", e)
	}
	if _, ok := decoded["metrics"]; ok {
		// no se espera una clave "metrics" en el toplevel; debe ser el nombre del pool
	}
}

/* ================= Manager ================= */

func TestManagerRegisterPoolLookupAndDup(t *testing.T) {
	mgr := NewManager()

	p1 := NewPool("a", func(ctx context.Context, _ map[string]string) resp.Result { return resp.PlainOK("ok") }, 1, 1)
	if err := mgr.Register("a", p1); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	// Duplicado
	if err := mgr.Register("a", NewPool("X", func(ctx context.Context, _ map[string]string) resp.Result { return resp.PlainOK("ok") }, 1, 1)); err == nil {
		t.Fatalf("Register duplicado debería fallar")
	}

	// Pool lookup
	if _, ok := mgr.Pool("a"); !ok {
		t.Fatalf("Pool a debería existir")
	}
	if _, ok := mgr.Pool("nope"); ok {
		t.Fatalf("Pool nope no debería existir")
	}

	// Métricas JSON debe incluir el pool "a"
	js := mgr.MetricsJSON()
	var mm map[string]any
	if err := json.Unmarshal([]byte(js), &mm); err != nil {
		t.Fatalf("MetricsJSON inválido: %v", err)
	}
	if _, ok := mm["a"]; !ok {
		t.Fatalf("no aparece 'a' en MetricsJSON: %v", js)
	}
}

/* ================= sanity: counters mutate where expected ================= */

func TestCountersMutateWhereExpected(t *testing.T) {
	p := NewPool("cnt", func(ctx context.Context, _ map[string]string) resp.Result {
		return resp.PlainOK("ok")
	}, 1, 1)

	// backpressure incrementa rejected
	p.qNorm <- work{id: "fill", ctx: context.Background(), params: map[string]string{}, enqueued: time.Now(), done: make(chan resp.Result, 1)}
	_, enq := p.SubmitAndWaitCtx(context.Background(), "id", nil, 5*time.Millisecond)
	if enq {
		t.Fatalf("esperado enq=false por backpressure")
	}
	m1 := p.metrics()
	if m1["rejected"].(uint64) != 1 {
		t.Fatalf("rejected=1, got %v", m1["rejected"])
	}

	// ahora éxito: submitted y completed deben aumentar
	// drenar la cola para que pueda encolar (arrancamos y consumimos el fill)
	p.Start()
	// El worker leerá el fill y quedará libre
	ok := waitUntil(200*time.Millisecond, func() bool {
		// una vez que haya leído el fill, la cola quedará vacía
		return len(p.qNorm) == 0
	})
	if !ok {
		t.Fatal("worker no drenó cola inicial a tiempo")
	}

	r, enq2 := p.SubmitAndWaitCtx(context.Background(), "id2", nil, 300*time.Millisecond)
	if !enq2 || r.Status != 200 {
		t.Fatalf("esperado éxito, got enq=%v res=%#v", enq2, r)
	}
	m2 := p.metrics()
	if m2["submitted"].(uint64) < 1 || m2["completed"].(uint64) < 1 {
		t.Fatalf("submitted/completed no crecieron: %+v", m2)
	}
}

/* ================= pre-run cancel via SubmitAndWaitCtx (enqueued, then cancel by ctx.Done branch) ================= */

func TestSubmitAndWaitCtx_WaitCancelBranch(t *testing.T) {
	// Cubre la rama de cancelación en la espera (select del caller), no la del worker.
	p := NewPool("waitcancel", func(ctx context.Context, _ map[string]string) resp.Result {
		// no devuelve nada rápido; bloquea bastante
		select {
		case <-ctx.Done():
			return resp.Unavail("canceled", "ctx canceled")
		case <-time.After(500 * time.Millisecond):
			return resp.PlainOK("late")
		}
	}, 1, 2)

	// No arrancar aún: encolamos y cancelamos inmediatamente después
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var got resp.Result
	var enq bool
	go func() {
		defer close(done)
		got, enq = p.SubmitAndWaitCtx(ctx, "id", nil, 2*time.Second)
	}()

	// pequeña espera para permitir que encole
	time.Sleep(10 * time.Millisecond)
	cancel()

	<-done
	if !enq || got.Err == nil || got.Err.Code != "canceled" {
		t.Fatalf("esperado canceled,true en rama de espera; got enq=%v res=%#v", enq, got)
	}
}

func TestStart_ClosedHighQueue_StarvesAndTimesOut(t *testing.T) {
	// Si qHigh está cerrada, el worker entra en un ciclo de 'continue' y
	// no procesa low/norm -> el submit low debe terminar en timeout.
	p := NewPool("highClosed", func(ctx context.Context, params map[string]string) resp.Result {
		// No debería ejecutarse en este escenario
		return resp.PlainOK("unexpected")
	}, 1, 4)

	// Cerramos SOLO qHigh (pool sigue abierto).
	close(p.qHigh)

	p.Start()

	// Enviamos un job LOW; por la inanición, debe timeoutear.
	r, enq := p.SubmitAndWaitCtx(
		context.Background(), "id-low",
		map[string]string{"prio": "low"},
		250*time.Millisecond,
	)
	if !enq || r.Err == nil || r.Err.Code != "timeout" {
		t.Fatalf("esperado timeout por inanición cuando qHigh está cerrada; enq=%v res=%#v", enq, r)
	}

	// Verificamos contadores coherentes con la inanición (se encoló pero no se completó).
	m := p.metrics()
	if m["submitted"].(uint64) != 1 {
		t.Fatalf("submitted esperado 1, got %v", m["submitted"])
	}
	if m["completed"].(uint64) != 0 {
		t.Fatalf("completed esperado 0 por inanición, got %v", m["completed"])
	}
}


