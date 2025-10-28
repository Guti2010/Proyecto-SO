package sched

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"strconv"
	"sync/atomic"
	"time"

	"so-http10-demo/internal/resp"
)

// TaskFunc ejecuta el trabajo asociado al comando.
type TaskFunc func(ctx context.Context, params map[string]string) resp.Result

// work representa una unidad que viaja por la cola del pool.
type work struct {
	id       string
	ctx      context.Context
	params   map[string]string
	enqueued time.Time
	done     chan resp.Result
}

// ---- estadísticos (Welford) ----
type stat struct {
	mu   sync.Mutex
	n    int64
	mean float64
	m2   float64
}

func (s *stat) add(x float64) {
	s.mu.Lock()
	s.n++
	delta := x - s.mean
	s.mean += delta / float64(s.n)
	delta2 := x - s.mean
	s.m2 += delta * delta2
	s.mu.Unlock()
}

func (s *stat) snapshot() (count int64, mean, std float64) {
	s.mu.Lock()
	count = s.n
	mean = s.mean
	if s.n > 1 {
		variance := s.m2 / float64(s.n-1)
		if variance > 0 {
			std = math.Sqrt(variance)
		}
	}
	s.mu.Unlock()
	return
}

// ---- Pool con 3 colas por prioridad ----
type Pool struct {
	name   string
	fn     TaskFunc

	// Prioridades: high, normal, low
	qHigh chan work
	qNorm chan work
	qLow  chan work

	total  int
	busy   int64 // workers ejecutando
	mu     sync.Mutex
	start  sync.Once
	closed bool

	// Métricas acumuladas
	submitted uint64 // trabajos encolados
	completed uint64 // trabajos finalizados
	rejected  uint64 // no encolados por backpressure
	waitStat  stat   // espera (ms)
	runStat   stat   // ejecución (ms)
}

// NewPool crea un pool con workers y capacidad total, repartida en 1:2:1 (high:norm:low).
func NewPool(name string, fn TaskFunc, workers, capacity int) *Pool {
	if workers <= 0 {
		workers = 1
	}
	if capacity <= 0 {
		capacity = 1
	}
	// reparto simple 1:2:1
	ch := imax(1, capacity/4)
	cn := imax(1, capacity/2)
	cl := imax(1, capacity-ch-cn)
	return &Pool{
		name:  name,
		fn:    fn,
		qHigh: make(chan work, ch),
		qNorm: make(chan work, cn),
		qLow:  make(chan work, cl),
		total: workers,
	}
}

func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Close cierra la cola y marca el pool como cerrado.
func (p *Pool) Close() {
	p.mu.Lock()
	if !p.closed {
		close(p.qHigh)
		close(p.qNorm)
		close(p.qLow)
		p.closed = true
	}
	p.mu.Unlock()
}

// SubmitAndWaitCtx encola con prioridad (params["prio"]) y espera resultado/timeout/cancel.
func (p *Pool) SubmitAndWaitCtx(ctx context.Context, id string, params map[string]string, timeout time.Duration) (resp.Result, bool) {
	if p.closed {
		return resp.Unavail("closed", "pool closed"), true
	}

	w := work{
		id:       id,
		ctx:      ctx,
		params:   params,
		enqueued: time.Now(),
		done:     make(chan resp.Result, 1),
	}

	// elige cola por prioridad (default: normal)
	var ch chan work
	switch params["prio"] {
	case "high":
		ch = p.qHigh
	case "low":
		ch = p.qLow
	default:
		ch = p.qNorm
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// intento de encolado con timeout / cancel
	select {
	case ch <- w:
		atomic.AddUint64(&p.submitted, 1)
	case <-timer.C:
		atomic.AddUint64(&p.rejected, 1)
		return resp.Unavail("backpressure", `{"retry_after_ms":100}`), false
	case <-ctx.Done():
		return resp.Unavail("canceled", "job canceled"), true
	}

	// esperar resultado / timeout / cancel de ejecución
	timer.Reset(timeout)
	select {
	case r := <-w.done:
		return r, true
	case <-timer.C:
		return resp.Unavail("timeout", "execution timed out"), true
	case <-ctx.Done():
		return resp.Unavail("canceled", "job canceled"), true
	}
}

// SubmitAndWait helper para rutas síncronas (sin cancel externo).
func (p *Pool) SubmitAndWait(params map[string]string, timeout time.Duration) (resp.Result, bool) {
	return p.SubmitAndWaitCtx(context.Background(), "", params, timeout)
}

// Start lanza los workers (preferencia: high > norm > low).
// Start lanza los workers (preferencia: high > norm > low).
func (p *Pool) Start() {
	p.start.Do(func() {
		for i := 0; i < p.total; i++ {
			workerID := i // capturar el índice
			go func() {
				workerTag := p.name + "#" + strconv.Itoa(workerID)

				for {
					var (
						w  work
						ok bool
					)

					// 1) intenta alta (no bloqueante)
					select {
					case w, ok = <-p.qHigh:
						if !ok {
							// qHigh cerrada: sigue con otras colas
							w = work{}
						}
					default:
						// 2) intenta normal (no bloqueante)
						select {
						case w, ok = <-p.qNorm:
							if !ok {
								w = work{}
							}
						default:
							// 3) bloquea esperando cualquiera, con preferencia
							select {
							case w, ok = <-p.qHigh:
								if !ok {
									w = work{}
								}
							case w, ok = <-p.qNorm:
								if !ok {
									w = work{}
								}
							case w, ok = <-p.qLow:
								if !ok {
									w = work{}
								}
							}
						}
					}

					// Si todas las colas están cerradas y el pool está marcado cerrado, salimos.
					if (w.params == nil && w.done == nil) && p.closed {
						return
					}
					// Si no llegó nada útil (p.ej. una cola cerrada devolvió cero valor), continúa.
					if w.done == nil {
						continue
					}

					// Cancelado antes de ejecutar
					select {
					case <-w.ctx.Done():
						w.done <- resp.Unavail("canceled", "job canceled before run")
						close(w.done)
						continue
					default:
					}

					atomic.AddInt64(&p.busy, 1)
					wait := time.Since(w.enqueued)
					start := time.Now()

					// Ejecuta respetando contexto (handlers deben consultar ctx periódicamente)
					res := p.fn(w.ctx, w.params)

					run := time.Since(start)
					atomic.AddInt64(&p.busy, -1)
					atomic.AddUint64(&p.completed, 1)

					// métricas en ms
					p.waitStat.add(float64(wait) / 1e6)
					p.runStat.add(float64(run) / 1e6)

					// Adjunta X-Worker-Id sin depender de helpers
					if res.Headers == nil {
						res.Headers = map[string]string{}
					}
					res.Headers["X-Worker-Id"] = workerTag

					w.done <- res
					close(w.done)
				}
			}()
		}
	})
}

// metrics devuelve un snapshot serializable para /metrics.
func (p *Pool) metrics() map[string]any {
	sub := atomic.LoadUint64(&p.submitted)
	comp := atomic.LoadUint64(&p.completed)
	rej := atomic.LoadUint64(&p.rejected)
	busy := atomic.LoadInt64(&p.busy)

	_, meanWait, stdWait := p.waitStat.snapshot()
	_, meanRun, stdRun := p.runStat.snapshot()

	qlen := len(p.qHigh) + len(p.qNorm) + len(p.qLow)
	qcap := cap(p.qHigh) + cap(p.qNorm) + cap(p.qLow)

	return map[string]any{
		"queue_len": qlen,
		"queue_cap": qcap,
		"priority_queues": map[string]any{
			"high": map[string]int{"len": len(p.qHigh), "cap": cap(p.qHigh)},
			"norm": map[string]int{"len": len(p.qNorm), "cap": cap(p.qNorm)},
			"low":  map[string]int{"len": len(p.qLow),  "cap": cap(p.qLow)},
		},
		"workers": map[string]any{
			"total": p.total,
			"busy":  busy,
			"idle":  p.total - int(busy),
		},
		"submitted": sub,
		"completed": comp,
		"rejected":  rej,
		"latency_ms": map[string]any{
			"wait": map[string]float64{"avg": meanWait, "std": stdWait},
			"run":  map[string]float64{"avg": meanRun,  "std": stdRun},
		},
	}
}

// ---- Manager ----
type Manager struct {
	mu    sync.RWMutex
	pools map[string]*Pool
}

func NewManager() *Manager {
	return &Manager{pools: make(map[string]*Pool)}
}

func (m *Manager) Register(name string, p *Pool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pools[name]; ok {
		return errors.New("pool already exists")
	}
	m.pools[name] = p
	p.Start()
	return nil
}

func (m *Manager) Pool(name string) (*Pool, bool) {
	m.mu.RLock()
	p, ok := m.pools[name]
	m.mu.RUnlock()
	return p, ok
}

func (m *Manager) MetricsJSON() string {
	m.mu.RLock()
	out := make(map[string]any, len(m.pools))
	for name, p := range m.pools {
		out[name] = p.metrics()
	}
	m.mu.RUnlock()
	b, _ := json.Marshal(out)
	return string(b)
}
