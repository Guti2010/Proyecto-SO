package jobs

import (
	"encoding/json"
	"sync"
	"time"

	"so-http10-demo/internal/resp"
	"so-http10-demo/internal/sched"
	"so-http10-demo/internal/util"
)

type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusTimeout Status = "timeout"
)

type Job struct {
	ID         string            `json:"id"`
	Task       string            `json:"task"`
	Params     map[string]string `json:"params,omitempty"`
	Status     Status            `json:"status"`
	EnqueuedAt time.Time         `json:"enqueued_at"`
	StartedAt  *time.Time        `json:"started_at,omitempty"`
	EndedAt    *time.Time        `json:"ended_at,omitempty"`
	Result     *resp.Result      `json:"result,omitempty"`
}

// Manager mantiene un registro en memoria de jobs y ejecuta cada job
// en el pool correspondiente de sched.Manager.
type Manager struct {
	sched *sched.Manager

	mu   sync.RWMutex
	jobs map[string]*Job

	ttl   time.Duration
	stopC chan struct{}
}

// NewManager crea un Job Manager con TTL de limpieza para jobs finalizados.
func NewManager(s *sched.Manager, ttl time.Duration) *Manager {
	m := &Manager{
		sched: s,
		jobs:  make(map[string]*Job),
		ttl:   ttl,
		stopC: make(chan struct{}),
	}
	go m.gcLoop()
	return m
}

// Close detiene la goroutine de GC.
func (m *Manager) Close() { close(m.stopC) }

func (m *Manager) gcLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			m.cleanup()
		case <-m.stopC:
			return
		}
	}
}

func (m *Manager) cleanup() {
	cut := time.Now().Add(-m.ttl)
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, j := range m.jobs {
		if (j.Status == StatusDone || j.Status == StatusFailed || j.Status == StatusTimeout) &&
			j.EndedAt != nil && j.EndedAt.Before(cut) {
			delete(m.jobs, id)
		}
	}
}

// Submit crea un job y lo ejecuta en background. Devuelve el ID.
// Si el pool no existe, no crea el job y retorna vacío.
func (m *Manager) Submit(task string, params map[string]string, execTimeout time.Duration) string {
	// valida que exista el pool
	if _, ok := m.sched.Pool(task); !ok {
		return ""
	}

	id := util.NewReqID()
	now := time.Now()
	job := &Job{
		ID:         id,
		Task:       task,
		Params:     params,
		Status:     StatusQueued,
		EnqueuedAt: now,
	}
	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()

	// Ejecuta en background
	go func() {
		p, _ := m.sched.Pool(task)

		// Marcamos como "running" cuando intentamos encolar.
		start := time.Now()
		m.mu.Lock()
		job.StartedAt = &start
		job.Status = StatusRunning
		m.mu.Unlock()

		res, enq := p.SubmitAndWait(params, execTimeout)
		end := time.Now()

		m.mu.Lock()
		defer m.mu.Unlock()
		job.EndedAt = &end
		job.Result = &res
		if !enq {
			// backpressure de encolado
			job.Status = StatusFailed
			return
		}
		// Mapeo de status por conveniencia
		if res.Status == 503 && res.Err != nil {
			// puede ser timeout (execution)
			if res.Err.Code == "timeout" {
				job.Status = StatusTimeout
				return
			}
		}
		if res.Status >= 200 && res.Status < 300 {
			job.Status = StatusDone
		} else {
			job.Status = StatusFailed
		}
	}()

	return id
}

// SnapshotJSON devuelve un JSON con metadatos del job sin mutar el original.
func (m *Manager) SnapshotJSON(id string) (string, bool) {
	m.mu.RLock()
	j, ok := m.jobs[id]
	m.mu.RUnlock()
	if !ok {
		return "", false
	}
	cp := struct {
		ID         string            `json:"id"`
		Task       string            `json:"task"`
		Params     map[string]string `json:"params,omitempty"`
		Status     Status            `json:"status"`
		EnqueuedAt time.Time         `json:"enqueued_at"`
		StartedAt  *time.Time        `json:"started_at,omitempty"`
		EndedAt    *time.Time        `json:"ended_at,omitempty"`
		Result     *resp.Result      `json:"result,omitempty"`
	}{
		ID:         j.ID,
		Task:       j.Task,
		Params:     j.Params,
		Status:     j.Status,
		EnqueuedAt: j.EnqueuedAt,
		StartedAt:  j.StartedAt,
		EndedAt:    j.EndedAt,
		Result:     j.Result,
	}
	b, _ := json.Marshal(cp)
	return string(b), true
}

// ListJSON lista los jobs actuales (activos y finalizados no vencidos).
func (m *Manager) ListJSON() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	type lite struct {
		ID     string `json:"id"`
		Task   string `json:"task"`
		Status Status `json:"status"`
	}
	out := make([]lite, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, lite{ID: j.ID, Task: j.Task, Status: j.Status})
	}
	b, _ := json.Marshal(out)
	return string(b)
}
