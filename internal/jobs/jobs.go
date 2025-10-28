package jobs

import (
    "bufio"
    "context"
    "encoding/json"
    "errors"
    "os"
    "path/filepath"
    "sync"
    "time"

    "so-http10-demo/internal/resp"
    "so-http10-demo/internal/sched"
    "so-http10-demo/internal/util"
)

// Estados requeridos por el enunciado.
type Status string

const (
	StatusQueued   Status = "queued"
	StatusRunning  Status = "running"
	StatusDone     Status = "done"
	StatusFailed   Status = "failed"
	StatusTimeout  Status = "timeout"
	StatusCanceled Status = "canceled"
)

// Job almacena metadatos y, cuando finaliza, el Result.
type Job struct {
    ID         string            `json:"id"`
    Task       string            `json:"task"`
    Params     map[string]string `json:"params,omitempty"`
    Status     Status            `json:"status"`
    EnqueuedAt time.Time         `json:"enqueued_at"`
    StartedAt  *time.Time        `json:"started_at,omitempty"`
    EndedAt    *time.Time        `json:"ended_at,omitempty"`
    Result     *resp.Result      `json:"result,omitempty"`

    Progress *int   `json:"progress,omitempty"`
    ETAMs    *int64 `json:"eta_ms,omitempty"`

    // Cancelación cooperativa
    cancel context.CancelFunc `json:"-"`
}


// Manager mantiene el registro de jobs y usa el sched.Manager para ejecutarlos.
type Manager struct {
	sched   *sched.Manager
	jobsDir string      // directorio para journal (por defecto /app/data)
	journal string      // ruta del journal JSONL
	mu      sync.RWMutex
	jobs    map[string]*Job

	ttl   time.Duration
	stopC chan struct{}
}

// NewManager crea un Job Manager con TTL de limpieza y persiste en /app/data.
func NewManager(s *sched.Manager, ttl time.Duration) *Manager {
	jdir := "/app/data"
	m := &Manager{
		sched:   s,
		jobsDir: jdir,
		journal: filepath.Join(jdir, "jobs.journal"),
		jobs:    make(map[string]*Job),
		ttl:     ttl,
		stopC:   make(chan struct{}),
	}
	_ = os.MkdirAll(m.jobsDir, 0o755)
	m.loadJournal()
	go m.gcLoop()
	return m
}

// Close detiene el GC.
func (m *Manager) Close() { close(m.stopC) }

// ---------- Journal (persistencia efímera) ----------

type journalRecord struct {
	Type string `json:"type"` // "upsert" | "delete"
	Job  *Job   `json:"job,omitempty"`
	ID   string `json:"id,omitempty"`
}

func (m *Manager) appendJournal(rec journalRecord) {
	f, err := os.OpenFile(m.journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	enc, _ := json.Marshal(rec)
	_, _ = f.Write(append(enc, '\n'))
}

func (m *Manager) loadJournal() {
	f, err := os.Open(m.journal)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec journalRecord
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		switch rec.Type {
		case "upsert":
			if rec.Job != nil {
				j := *rec.Job
				// Re-hidratación: si estaba queued/running al apagarse, márcalo failed.
				if j.Status == StatusQueued || j.Status == StatusRunning {
					now := time.Now()
					j.Status = StatusFailed
					j.EndedAt = &now
					msg := resp.IntErr("restart", "job interrupted by restart")
					j.Result = &msg
				}
				m.jobs[j.ID] = &j
			}
		case "delete":
			delete(m.jobs, rec.ID)
		}
	}
}

// ---------- GC (limpieza de finalizados por TTL) ----------

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
		if (j.Status == StatusDone || j.Status == StatusFailed || j.Status == StatusTimeout || j.Status == StatusCanceled) &&
			j.EndedAt != nil && j.EndedAt.Before(cut) {
			delete(m.jobs, id)
			m.appendJournal(journalRecord{Type: "delete", ID: id})
		}
	}
}

// ---------- API pública de Jobs ----------

// Submit encola la ejecución en el Pool del "task" y devuelve ID.
// Si el pool no existe, devuelve "".
func (m *Manager) Submit(task string, params map[string]string, execTimeout time.Duration) string {
    if _, ok := m.sched.Pool(task); !ok {
        return ""
    }

    id := util.NewReqID()
    now := time.Now()

    // contexto + cancel por job
    ctx, cancel := context.WithCancel(context.Background())

    job := &Job{
        ID:         id,
        Task:       task,
        Params:     params,
        Status:     StatusQueued,
        EnqueuedAt: now,
        cancel:     cancel,
    }
    m.mu.Lock()
    m.jobs[id] = job
    m.mu.Unlock()
    m.appendJournal(journalRecord{Type: "upsert", Job: job})

    // Ejecuta en background.
    go func() {
        p, _ := m.sched.Pool(task)

        // Si fue cancelado antes de arrancar, cerrar como canceled.
        select {
        case <-ctx.Done():
            end := time.Now()
            m.mu.Lock()
            job.Status = StatusCanceled
            job.EndedAt = &end
            m.mu.Unlock()
            m.appendJournal(journalRecord{Type: "upsert", Job: job})
            return
        default:
        }

        start := time.Now()
        m.mu.Lock()
        job.StartedAt = &start
        job.Status = StatusRunning
        m.mu.Unlock()
        m.appendJournal(journalRecord{Type: "upsert", Job: job})

        // Ejecuta respetando el contexto (scheduler debe pasar ctx a la TaskFunc)
        res, enq := p.SubmitAndWaitCtx(ctx, id, params, execTimeout)
        end := time.Now()

        m.mu.Lock()
        defer m.mu.Unlock()
        job.EndedAt = &end
        job.Result = &res

        switch {
        case !enq:
            job.Status = StatusFailed // backpressure al encolar
        case res.Err != nil && res.Err.Code == "canceled":
            job.Status = StatusCanceled
        case res.Err != nil && res.Err.Code == "timeout":
            job.Status = StatusTimeout
        case res.Status >= 200 && res.Status < 300:
            job.Status = StatusDone
        default:
            job.Status = StatusFailed
        }
        m.appendJournal(journalRecord{Type: "upsert", Job: job})
    }()

    return id
}

// Cancel intenta cancelar: si está queued → canceled; si running/done → not_cancelable.
func (m *Manager) Cancel(id string) (string, bool) {
    m.mu.Lock()
    defer m.mu.Unlock()
    j, ok := m.jobs[id]
    if !ok {
        return "not_found", false
    }

    switch j.Status {
    case StatusDone, StatusFailed, StatusTimeout, StatusCanceled:
        return "not_cancelable", true

    case StatusQueued:
        if j.cancel != nil {
            j.cancel() // evita que arranque
        }
        now := time.Now()
        j.Status = StatusCanceled
        j.EndedAt = &now
        m.appendJournal(journalRecord{Type: "upsert", Job: j})
        return "canceled", true

    case StatusRunning:
        if j.cancel != nil {
            j.cancel() // el handler debe respetar ctx.Done() y salir con "canceled"
            // aquí devolvemos "canceled" (solicitud aceptada); el estado
            // pasará a CANCELED cuando el handler termine y Submit lo fije.
            return "canceled", true
        }
        return "not_cancelable", true

    default:
        return "not_cancelable", true
    }
}


// SnapshotJSON devuelve el estado del job con progress/eta si es posible.
func (m *Manager) SnapshotJSON(id string) (string, bool) {
	m.mu.RLock()
	j, ok := m.jobs[id]
	m.mu.RUnlock()
	if !ok {
		return "", false
	}
	cp := *j
	cp.Progress, cp.ETAMs = deriveProgressETA(&cp)
	b, _ := json.Marshal(cp)
	return string(b), true
}

// ResultJSON devuelve el JSON del resultado si el job terminó.
func (m *Manager) ResultJSON(id string) (string, bool, error) {
    m.mu.RLock()
    j, ok := m.jobs[id]
    m.mu.RUnlock()
    if !ok {
        return "", false, nil
    }
    if j.Status == StatusDone || j.Status == StatusFailed || j.Status == StatusTimeout || j.Status == StatusCanceled {
        // construir respuesta con error si existe
        out := map[string]any{
            "status": string(j.Status),
        }
        if j.Result != nil {
            // cuerpo del comando (si lo hubo)
            if j.Result.Body != "" {
                out["result"] = j.Result.Body
            }
            if j.Result.Err != nil && j.Result.Err.Detail != "" {
                out["error"] = j.Result.Err.Detail
            }
        }
        b, _ := json.Marshal(out)
        return string(b), true, nil
    }
    return "", true, errors.New("not_ready")
}

// ListJSON lista jobs activos y recientes.
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

// ---------- util de progreso/ETA ----------

// deriveProgressETA intenta estimar progreso para tareas conocidas.
// Para "sleep": usa seconds; para otras: nil.
func deriveProgressETA(j *Job) (*int, *int64) {
	if j.Status != StatusRunning || j.StartedAt == nil {
		return nil, nil
	}
	switch j.Task {
	case "sleep":
		secStr := j.Params["seconds"]
		d, err := time.ParseDuration(secStr + "s")
		if err != nil || d <= 0 {
			return nil, nil
		}
		el := time.Since(*j.StartedAt)
		if el < 0 {
			el = 0
		}
		if el >= d {
			p := 100
			eta := int64(0)
			return &p, &eta
		}
		pct := int(float64(el) / float64(d) * 100.0)
		remain := d - el
		eta := remain.Milliseconds()
		return &pct, &eta
	default:
		return nil, nil
	}
}

