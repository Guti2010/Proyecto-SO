package sched

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"so-http10-demo/internal/resp"
)

// TaskFunc ejecuta el trabajo asociado al comando.
type TaskFunc func(ctx context.Context, params map[string]string) resp.Result

// work representa una unidad que viaja por la cola del pool.
type work struct {
	params   map[string]string
	enqueued time.Time
	done     chan resp.Result
}

// stat acumula media y varianza (Welford) de forma numéricamente estable.
type stat struct {
	mu   sync.Mutex
	n    int64
	mean float64
	m2   float64
}

func (s *stat) add(x float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	delta := x - s.mean
	s.mean += delta / float64(s.n)
	delta2 := x - s.mean
	s.m2 += delta * delta2
}

func (s *stat) snapshot() (count int64, mean, std float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count = s.n
	mean = s.mean
	if s.n > 1 {
		variance := s.m2 / float64(s.n-1) // varianza muestral
		if variance > 0 {
			std = math.Sqrt(variance)
		}
	}
	return
}

// Pool gestiona una cola y N workers, acumulando métricas en tiempo real.
type Pool struct {
	name   string
	fn     TaskFunc
	q      chan work
	total  int
	busy   int64 // workers ejecutando
	mu     sync.Mutex
	start  sync.Once
	closed bool

	// Métricas acumuladas
	submitted uint64 // trabajos que lograron encolarse
	completed uint64 // trabajos finalizados (respuesta enviada)
	rejected  uint64 // trabajos no encolados por backpressure
	waitStat  stat   // espera en cola (ms)
	runStat   stat   // tiempo de ejecución (ms)
}

// NewPool crea un pool con el número de workers y capacidad de cola indicados.
func NewPool(name string, fn TaskFunc, workers, capacity int) *Pool {
	if workers <= 0 {
		workers = 1
	}
	if capacity <= 0 {
		capacity = 1
	}
	return &Pool{name: name, fn: fn, q: make(chan work, capacity), total: workers}
}

// Start lanza los workers si aún no estaban en ejecución.
func (p *Pool) Start() {
	p.start.Do(func() {
		for i := 0; i < p.total; i++ {
			go func() {
				for w := range p.q {
					atomic.AddInt64(&p.busy, 1)

					wait := time.Since(w.enqueued) // duración en cola
					start := time.Now()
					res := p.fn(context.Background(), w.params)
					run := time.Since(start) // duración de ejecución

					atomic.AddInt64(&p.busy, -1)
					atomic.AddUint64(&p.completed, 1)
					// Normalizamos a milisegundos porque así se piden las métricas.
					p.waitStat.add(float64(wait) / 1e6)
					p.runStat.add(float64(run) / 1e6)

					w.done <- res
					close(w.done)
				}
			}()
		}
	})
}

// Close cierra la cola y marca el pool como cerrado.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	close(p.q)
	p.closed = true
}

// SubmitAndWait intenta encolar y esperar la respuesta.
// Si no logra encolar dentro de 'timeout', devuelve (503, false).
// Si encola, esperará hasta 'timeout' adicional por la ejecución (timeout de ejecución).
func (p *Pool) SubmitAndWait(params map[string]string, timeout time.Duration) (resp.Result, bool) {
	if p.closed {
		return resp.Unavail("closed", "pool closed"), true
	}

	w := work{
		params:   params,
		enqueued: time.Now(),
		done:     make(chan resp.Result, 1),
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Intento de ENCOLAR con timeout de encolado.
	select {
	case p.q <- w:
		atomic.AddUint64(&p.submitted, 1)
	case <-timer.C:
		atomic.AddUint64(&p.rejected, 1)
		return resp.Unavail("backpressure", `{"retry_after_ms":100}`), false
	}

	// Ya en cola: espera resultado o timeout de ejecución.
	timer.Reset(timeout)
	select {
	case r := <-w.done:
		return r, true
	case <-timer.C:
		return resp.Unavail("timeout", "execution timed out"), true
	}
}

// metrics devuelve un snapshot serializable para /metrics.
func (p *Pool) metrics() map[string]any {
	sub := atomic.LoadUint64(&p.submitted)
	comp := atomic.LoadUint64(&p.completed)
	rej := atomic.LoadUint64(&p.rejected)
	busy := atomic.LoadInt64(&p.busy)

	_, meanWait, stdWait := p.waitStat.snapshot()
	_, meanRun, stdRun := p.runStat.snapshot()

	return map[string]any{
		"queue_len": len(p.q),
		"queue_cap": cap(p.q),
		"workers": map[string]any{
			"total": p.total,
			"busy":  busy,
			"idle":  p.total - int(busy),
		},
		"submitted": sub,
		"completed": comp,
		"rejected":  rej,
		"latency_ms": map[string]any{
			"wait": map[string]any{"avg": meanWait, "std": stdWait},
			"run":  map[string]any{"avg": meanRun,  "std": stdRun},
		},
	}
}

// Manager coordina múltiples pools por nombre y expone métricas agregadas.
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
	defer m.mu.RUnlock()
	p, ok := m.pools[name]
	return p, ok
}

func (m *Manager) MetricsJSON() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]any, len(m.pools))
	for name, p := range m.pools {
		out[name] = p.metrics()
	}
	b, _ := json.Marshal(out)
	return string(b)
}
