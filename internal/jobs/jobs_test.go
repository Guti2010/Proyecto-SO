package jobs

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	"context"

	"so-http10-demo/internal/resp"
	"so-http10-demo/internal/sched"
)

/* ------------ helpers ------------ */

func newMgrForTest(t *testing.T) *Manager {
	t.Helper()
	td := t.TempDir()
	m := &Manager{
		sched:   (*sched.Manager)(nil), // sin scheduler real en estas pruebas
		jobsDir: td,
		journal: filepath.Join(td, "jobs.journal"),
		jobs:    make(map[string]*Job),
		ttl:     50 * time.Millisecond,
		stopC:   make(chan struct{}),
	}
	_ = os.MkdirAll(m.jobsDir, 0o755)
	return m
}

func writeJournalLine(t *testing.T, path string, rec journalRecord) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	defer f.Close()
	enc, _ := json.Marshal(rec)
	if _, err := f.Write(append(enc, '\n')); err != nil {
		t.Fatalf("write journal: %v", err)
	}
}

// lee todas las líneas del journal como []string
func readAllLines(t *testing.T, path string) []string {
    t.Helper()
    f, err := os.Open(path)
    if err != nil {
        return nil
    }
    defer f.Close()
    var out []string
    sc := bufio.NewScanner(f)
    for sc.Scan() {
        out = append(out, sc.Text())
    }
    return out
}

// escribe una línea cruda (para probar casos corruptos o tipos desconocidos)
func writeRawLine(t *testing.T, path, raw string) {
    t.Helper()
    f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
    if err != nil {
        t.Fatalf("open journal: %v", err)
    }
    defer f.Close()
    if _, err := f.Write([]byte(raw + "\n")); err != nil {
        t.Fatalf("write journal: %v", err)
    }
}



/* ----- helpers específicas para probar NewManager en /app/data ----- */

const appDataDir = "/app/data"
const journalName = "jobs.journal"

type journalBackup struct {
	existed    bool
	backupPath string
}

func ensureAppDataWritable(t *testing.T) (journalPath string, cleanup func()) {
	t.Helper()

	if err := os.MkdirAll(appDataDir, 0o755); err != nil {
		t.Skipf("no se pudo crear %s (%v) — se omite prueba dependiente del FS", appDataDir, err)
	}
	journalPath = filepath.Join(appDataDir, journalName)

	// respaldo si ya existe
	var bk journalBackup
	if _, err := os.Stat(journalPath); err == nil {
		tmp := journalPath + ".bak." + time.Now().Format("20060102T150405.000000000")
		if err := os.Rename(journalPath, tmp); err != nil {
			t.Skipf("no se pudo respaldar journal existente (%v) — se omite por seguridad", err)
		}
		bk = journalBackup{existed: true, backupPath: tmp}
	}

	cleanup = func() {
		// borrar lo creado por la prueba
		_ = os.Remove(journalPath)
		// restaurar respaldo si había
		if bk.existed {
			_ = os.Rename(bk.backupPath, journalPath)
		}
	}
	return journalPath, cleanup
}

// crea un sched.Manager real con un pool registrado
func mkSchedWithPool(t *testing.T, name string, fn sched.TaskFunc, workers, capacity int, start bool) *sched.Manager {
    t.Helper()
    sm := sched.NewManager()
    p := sched.NewPool(name, fn, workers, capacity)
    if start {
        p.Start()
    }
    if err := sm.Register(name, p); err != nil {
        t.Fatalf("Register pool: %v", err)
    }
    return sm
}

// espera hasta que el job cumpla la condición o venza el timeout
func waitUntil(t *testing.T, d time.Duration, check func() bool) bool {
    t.Helper()
    deadline := time.Now().Add(d)
    for time.Now().Before(deadline) {
        if check() {
            return true
        }
        time.Sleep(10 * time.Millisecond)
    }
    return false
}


/* ------------ tests ------------ */

func TestLoadJournal_RehydrateQueuedAndRunningToFailed(t *testing.T) {
	m := newMgrForTest(t)

	// Simulamos dos registros: uno queued y otro running.
	writeJournalLine(t, m.journal, journalRecord{
		Type: "upsert",
		Job: &Job{
			ID:     "a",
			Task:   "sleep",
			Status: StatusQueued,
		},
	})
	writeJournalLine(t, m.journal, journalRecord{
		Type: "upsert",
		Job: &Job{
			ID:     "b",
			Task:   "sleep",
			Status: StatusRunning,
		},
	})

	m.loadJournal()

	ja, ok := m.jobs["a"]
	if !ok {
		t.Fatalf("job a no rehidratado")
	}
	if ja.Status != StatusFailed {
		t.Fatalf("job a esperado failed, got %s", ja.Status)
	}
	if ja.EndedAt == nil {
		t.Fatalf("job a sin EndedAt tras rehidratación")
	}
	if ja.Result == nil || ja.Result.Err == nil || ja.Result.Err.Code != "restart" {
		t.Fatalf("job a debería contener IntErr('restart')")
	}

	jb, ok := m.jobs["b"]
	if !ok {
		t.Fatalf("job b no rehidratado")
	}
	if jb.Status != StatusFailed {
		t.Fatalf("job b esperado failed, got %s", jb.Status)
	}
	if jb.EndedAt == nil {
		t.Fatalf("job b sin EndedAt tras rehidratación")
	}
	if jb.Result == nil || jb.Result.Err == nil || jb.Result.Err.Code != "restart" {
		t.Fatalf("job b debería contener IntErr('restart')")
	}
}

func TestCancel_Queued(t *testing.T) {
	m := newMgrForTest(t)
	called := false
	cancel := func() { called = true }

	j := &Job{
		ID:     "q1",
		Task:   "sleep",
		Status: StatusQueued,
		cancel: cancel,
	}
	m.jobs[j.ID] = j

	msg, ok := m.Cancel(j.ID)
	if !ok || msg != "canceled" {
		t.Fatalf("cancel queued => %v, %v", msg, ok)
	}
	if j.Status != StatusCanceled {
		t.Fatalf("esperado canceled, got %s", j.Status)
	}
	if !called {
		t.Fatalf("cancel func no fue llamada")
	}
	if j.EndedAt == nil {
		t.Fatalf("EndedAt debería estar seteado")
	}
}

func TestCancel_Running_DoesNotFlipStatusImmediately(t *testing.T) {
	m := newMgrForTest(t)
	called := false
	cancel := func() { called = true }

	now := time.Now()
	j := &Job{
		ID:        "r1",
		Task:      "sleep",
		Status:    StatusRunning,
		StartedAt: &now,
		cancel:    cancel,
	}
	m.jobs[j.ID] = j

	msg, ok := m.Cancel(j.ID)
	if !ok || msg != "canceled" {
		t.Fatalf("cancel running => %v, %v", msg, ok)
	}
	if !called {
		t.Fatalf("cancel func no fue llamada")
	}
	// El estado se actualiza cuando termina la goroutine de Submit.
	if j.Status != StatusRunning {
		t.Fatalf("running no debe cambiar aún, got %s", j.Status)
	}
}

func TestSnapshotJSON_SleepProgress(t *testing.T) {
	m := newMgrForTest(t)
	start := time.Now().Add(-400 * time.Millisecond)
	j := &Job{
		ID:        "s1",
		Task:      "sleep",
		Params:    map[string]string{"seconds": "1"},
		Status:    StatusRunning,
		StartedAt: &start,
	}
	m.jobs[j.ID] = j

	js, ok := m.SnapshotJSON(j.ID)
	if !ok {
		t.Fatalf("SnapshotJSON: id no encontrado")
	}

	var out struct {
		ID       string `json:"id"`
		Task     string `json:"task"`
		Status   Status `json:"status"`
		Progress *int   `json:"progress"`
		ETAms    *int64 `json:"eta_ms"`
	}
	if err := json.Unmarshal([]byte(js), &out); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if out.Progress == nil || *out.Progress <= 0 || *out.Progress >= 100 {
		t.Fatalf("progress inválido: %#v", out.Progress)
	}
	if out.ETAms == nil || *out.ETAms <= 0 {
		t.Fatalf("eta_ms inválido: %#v", out.ETAms)
	}
}

func TestResultJSON_ReadyAndNotReady(t *testing.T) {
	m := newMgrForTest(t)

	// Caso listo (Done) con body
	done := &Job{
		ID:     "d1",
		Task:   "x",
		Status: StatusDone,
		Result: &resp.Result{Status: 200, Body: "ok"},
	}
	m.jobs[done.ID] = done

	s, ok, err := m.ResultJSON(done.ID)
	if !ok || err != nil {
		t.Fatalf("ResultJSON listo => ok=%v err=%v", ok, err)
	}
	var obj map[string]any
	if e := json.Unmarshal([]byte(s), &obj); e != nil {
		t.Fatalf("unmarshal result: %v", e)
	}
	if obj["status"] != "done" || obj["result"] != "ok" {
		t.Fatalf("result JSON inesperado: %v", obj)
	}

	// Caso not_ready
	running := &Job{
		ID:     "r2",
		Task:   "x",
		Status: StatusRunning,
	}
	m.jobs[running.ID] = running

	s, ok, err = m.ResultJSON(running.ID)
	if !ok {
		t.Fatalf("ResultJSON running debe encontrar id")
	}
	if err == nil || err.Error() != "not_ready" {
		t.Fatalf("esperado not_ready, got: %v", err)
	}
	if s != "" {
		t.Fatalf("cuando no está listo no debe devolver payload, got: %q", s)
	}

	// Caso not found
	s, ok, err = m.ResultJSON("nope")
	if ok || err != nil || s != "" {
		t.Fatalf("not found => ok=false, err=nil, s=\"\", got ok=%v err=%v s=%q", ok, err, s)
	}
}

func TestCleanupTTL_RemovesExpired(t *testing.T) {
	m := newMgrForTest(t)
	// TTL de 50ms (por default en helper). Marcamos un job finalizado hace 2s.
	end := time.Now().Add(-2 * time.Second)
	j := &Job{
		ID:      "old",
		Task:    "x",
		Status:  StatusDone,
		EndedAt: &end,
	}
	m.jobs[j.ID] = j

	m.cleanup()

	if _, ok := m.jobs["old"]; ok {
		t.Fatalf("cleanup no eliminó job expirado")
	}
}

func TestListJSON(t *testing.T) {
	m := newMgrForTest(t)
	m.jobs["a"] = &Job{ID: "a", Task: "sleep", Status: StatusQueued}
	m.jobs["b"] = &Job{ID: "b", Task: "work", Status: StatusFailed}

	js := m.ListJSON()
	var arr []struct {
		ID     string `json:"id"`
		Task   string `json:"task"`
		Status Status `json:"status"`
	}
	if err := json.Unmarshal([]byte(js), &arr); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("esperados 2 jobs, got %d", len(arr))
	}
	// Chequeo rápido de contenidos
	foundA, foundB := false, false
	for _, it := range arr {
		if it.ID == "a" && it.Task == "sleep" && it.Status == StatusQueued {
			foundA = true
		}
		if it.ID == "b" && it.Task == "work" && it.Status == StatusFailed {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Fatalf("contenido incorrecto: %+v", arr)
	}
}

func TestCancel_NotFoundAndNotCancelable(t *testing.T) {
	m := newMgrForTest(t)

	// not_found
	msg, ok := m.Cancel("missing")
	if ok || msg != "not_found" {
		t.Fatalf("cancel not_found => %v %v", msg, ok)
	}

	// not_cancelable (ya finalizado)
	now := time.Now()
	m.jobs["x"] = &Job{
		ID:      "x",
		Task:    "t",
		Status:  StatusDone,
		EndedAt: &now,
	}
	msg, ok = m.Cancel("x")
	if !ok || msg != "not_cancelable" {
		t.Fatalf("cancel en finalizado debería ser not_cancelable, got %v %v", msg, ok)
	}
}

func TestSnapshotJSON_NotFound(t *testing.T) {
	m := newMgrForTest(t)
	if s, ok := m.SnapshotJSON("nope"); ok || s != "" {
		t.Fatalf("SnapshotJSON not found => ok=false, s=\"\"; got ok=%v s=%q", ok, s)
	}
}

func TestResultJSON_ErrorFieldWhenPresent(t *testing.T) {
	m := newMgrForTest(t)

	j := &Job{
		ID:     "e1",
		Task:   "t",
		Status: StatusFailed,
		Result: &resp.Result{
			Status: 500,
			Err: &resp.ErrObj{
				Code:   "boom",
				Detail: "explosion",
			},
		},
	}
	m.jobs[j.ID] = j

	s, ok, err := m.ResultJSON(j.ID)
	if !ok || err != nil {
		t.Fatalf("ResultJSON failed => ok=%v err=%v", ok, err)
	}
	var obj map[string]any
	if e := json.Unmarshal([]byte(s), &obj); e != nil {
		t.Fatalf("unmarshal: %v", e)
	}
	if obj["status"] != "failed" {
		t.Fatalf("status esperado failed, got %v", obj["status"])
	}
	if obj["error"] != "explosion" {
		t.Fatalf("error.detail esperado, got %v", obj["error"])
	}
}

/* Sanity: el errors.New("not_ready") es usado tal cual */
func TestNotReadyErrorLiteral(t *testing.T) {
	if errors.New("not_ready").Error() != "not_ready" {
		t.Fatalf("mensaje not_ready cambió y podría romper aserciones")
	}
}

/* ================= Pruebas de NewManager y Close ================= */

func TestNewManager_InitialFieldsAndDir(t *testing.T) {
	journalPath, cleanup := ensureAppDataWritable(t)
	defer cleanup()

	ttl := 123 * time.Millisecond
	s := (*sched.Manager)(nil)

	m := NewManager(s, ttl)
	defer m.Close()

	if m.sched != s {
		t.Fatalf("sched no seteado")
	}
	if m.jobsDir != appDataDir {
		t.Fatalf("jobsDir esperado %s, got %s", appDataDir, m.jobsDir)
	}
	if m.journal != journalPath {
		t.Fatalf("journal esperado %s, got %s", journalPath, m.journal)
	}
	if m.ttl != ttl {
		t.Fatalf("ttl esperado %v, got %v", ttl, m.ttl)
	}
	if m.jobs == nil {
		t.Fatalf("map jobs debe estar inicializado")
	}
	if m.stopC == nil {
		t.Fatalf("stopC debe estar inicializado")
	}
	if st, err := os.Stat(m.jobsDir); err != nil || !st.IsDir() {
		t.Fatalf("jobsDir no existe o no es dir: err=%v", err)
	}
}

func TestNewManager_LoadsJournalAndRehydrates(t *testing.T) {
	journalPath, cleanup := ensureAppDataWritable(t)
	defer cleanup()

	writeJournalLine(t, journalPath, journalRecord{
		Type: "upsert",
		Job: &Job{
			ID:     "rehydrate-1",
			Task:   "sleep",
			Status: StatusQueued,
		},
	})

	m := NewManager((*sched.Manager)(nil), 50*time.Millisecond)
	defer m.Close()

	j, ok := m.jobs["rehydrate-1"]
	if !ok {
		t.Fatalf("job rehydrate-1 no cargado desde journal")
	}
	if j.Status != StatusFailed {
		t.Fatalf("rehidratación: esperado failed, got %s", j.Status)
	}
	if j.EndedAt == nil {
		t.Fatalf("rehidratación: EndedAt debe estar seteado")
	}
	if j.Result == nil || j.Result.Err == nil || j.Result.Err.Code != "restart" {
		t.Fatalf("rehidratación: esperado IntErr('restart'), got %#v", j.Result)
	}
}

func TestClose_ClosesStopChannel(t *testing.T) {
	// No toca el FS.
	m := &Manager{
		sched:   (*sched.Manager)(nil),
		jobsDir: "/does/not/matter",
		journal: "/does/not/matter",
		jobs:    make(map[string]*Job),
		ttl:     10 * time.Millisecond,
		stopC:   make(chan struct{}),
	}
	go m.gcLoop()

	m.Close()

	select {
	case <-m.stopC:
		// ok: canal cerrado
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("stopC no se cerró a tiempo")
	}
}


// ---------- appendJournal: éxito (crea y appendea) ----------
func TestAppendJournal_CreateAndAppend(t *testing.T) {
    m := newMgrForTest(t)

    r1 := journalRecord{Type: "upsert", Job: &Job{ID: "x1", Task: "t1", Status: StatusQueued}}
    r2 := journalRecord{Type: "delete", ID: "x1"}

    // dos escrituras
    m.appendJournal(r1)
    m.appendJournal(r2)

    lines := readAllLines(t, m.journal)
    if len(lines) != 2 {
        t.Fatalf("esperaba 2 líneas en journal, got %d", len(lines))
    }

    var a, b journalRecord
    if err := json.Unmarshal([]byte(lines[0]), &a); err != nil {
        t.Fatalf("unmarshal 1: %v", err)
    }
    if err := json.Unmarshal([]byte(lines[1]), &b); err != nil {
        t.Fatalf("unmarshal 2: %v", err)
    }

    if a.Type != "upsert" || a.Job == nil || a.Job.ID != "x1" {
        t.Fatalf("línea 1 inesperada: %#v", a)
    }
    if b.Type != "delete" || b.ID != "x1" {
        t.Fatalf("línea 2 inesperada: %#v", b)
    }
}

// ---------- appendJournal: ruta de error (no debe panic/afectar) ----------
func TestAppendJournal_ErrorPath_NoPanic(t *testing.T) {
    // hacemos que m.journal apunte a un DIRECTORIO para que OpenFile falle.
    td := t.TempDir()
    badPath := filepath.Join(td, "asdir")
    if err := os.MkdirAll(badPath, 0o755); err != nil {
        t.Fatalf("mkdir: %v", err)
    }

    m := &Manager{
        jobs:    make(map[string]*Job),
        journal: badPath, // <-- path inválido (es un dir)
    }

    // no debe panic
    m.appendJournal(journalRecord{Type: "upsert", Job: &Job{ID: "y1"}})

    // sigue siendo un directorio
    st, err := os.Stat(badPath)
    if err != nil || !st.IsDir() {
        t.Fatalf("se alteró el directorio inesperadamente: err=%v", err)
    }
}

// ---------- loadJournal: cobertura de casos mixtos ----------
func TestLoadJournal_MixedRecords(t *testing.T) {
    m := newMgrForTest(t)

    // 1) línea corrupta
    writeRawLine(t, m.journal, "{not-json")

    // 2) upsert sin job (se ignora)
    writeRawLine(t, m.journal, `{"type":"upsert"}`)

    // 3) upsert queued → debe rehidratarse a failed+restart
    writeJournalLine(t, m.journal, journalRecord{
        Type: "upsert",
        Job:  &Job{ID: "q", Task: "sleep", Status: StatusQueued},
    })

    // 4) upsert running → failed+restart
    writeJournalLine(t, m.journal, journalRecord{
        Type: "upsert",
        Job:  &Job{ID: "r", Task: "sleep", Status: StatusRunning},
    })

    // 5) upsert done → se conserva
    writeJournalLine(t, m.journal, journalRecord{
        Type: "upsert",
        Job:  &Job{ID: "d", Task: "t", Status: StatusDone},
    })

    // 6) delete de "d" → debe desaparecer
    writeJournalLine(t, m.journal, journalRecord{
        Type: "delete",
        ID:   "d",
    })

    // 7) tipo desconocido → se ignora
    writeRawLine(t, m.journal, `{"type":"weird","id":"zzz"}`)

    m.loadJournal()

    // q: failed + restart
    jq, ok := m.jobs["q"]
    if !ok {
        t.Fatalf("q no cargado")
    }
    if jq.Status != StatusFailed || jq.EndedAt == nil || jq.Result == nil || jq.Result.Err == nil || jq.Result.Err.Code != "restart" {
        t.Fatalf("q rehidratado mal: %#v", jq)
    }

    // r: failed + restart
    jr, ok := m.jobs["r"]
    if !ok {
        t.Fatalf("r no cargado")
    }
    if jr.Status != StatusFailed || jr.EndedAt == nil || jr.Result == nil || jr.Result.Err == nil || jr.Result.Err.Code != "restart" {
        t.Fatalf("r rehidratado mal: %#v", jr)
    }

    // d: debe haber sido borrado por delete
    if _, ok := m.jobs["d"]; ok {
        t.Fatalf("d debería haber sido eliminado por el registro delete")
    }

    // ninguno creado por upsert sin job ni por tipo desconocido
    if _, ok := m.jobs["zzz"]; ok {
        t.Fatalf("registro tipo desconocido no debe crear jobs")
    }
}

// ---------- loadJournal: archivo inexistente (no-op, sin pánico) ----------
func TestLoadJournal_NoFile_NoPanic(t *testing.T) {
    m := newMgrForTest(t)
    // eliminar journal para simular inexistente
    _ = os.Remove(m.journal)
    // no debe panic ni modificar el mapa
    m.loadJournal()
    if len(m.jobs) != 0 {
        t.Fatalf("loadJournal sin archivo no debe cargar nada")
    }
}

func TestSubmit_NoPool_ReturnsEmpty(t *testing.T) {
    m := newMgrForTest(t)
    m.sched = sched.NewManager() // sin pools registrados
    id := m.Submit("missing", nil, 200*time.Millisecond)
    if id != "" {
        t.Fatalf("Submit sin pool debe devolver \"\", got %q", id)
    }
}

func TestSubmit_Success_Done(t *testing.T) {
    m := newMgrForTest(t)

    // Task rápido exitoso
    taskName := "ok"
    sm := mkSchedWithPool(t, taskName, func(ctx context.Context, params map[string]string) resp.Result {
        return resp.PlainOK("ok")
    }, 1, 1, true)
    m.sched = sm

    id := m.Submit(taskName, nil, 2*time.Second)
    if id == "" {
        t.Fatalf("id vacío")
    }

    // Espera a done
    ok := waitUntil(t, time.Second, func() bool {
        m.mu.RLock()
        j := m.jobs[id]
        st := j != nil && j.Status == StatusDone
        m.mu.RUnlock()
        return st
    })
    if !ok {
        t.Fatalf("job no llegó a DONE a tiempo")
    }

    m.mu.RLock()
    j := m.jobs[id]
    if j.Result == nil || j.Result.Body != "ok" {
        t.Fatalf("resultado inesperado: %#v", j.Result)
    }
    if j.StartedAt == nil || j.EndedAt == nil {
        t.Fatalf("timestamps no seteados: started=%v ended=%v", j.StartedAt, j.EndedAt)
    }
    m.mu.RUnlock()
}

func TestSubmit_Timeout(t *testing.T) {
    m := newMgrForTest(t)

    taskName := "slow"
    sm := mkSchedWithPool(t, taskName, func(ctx context.Context, params map[string]string) resp.Result {
        time.Sleep(200 * time.Millisecond) // más lento que el timeout
        return resp.PlainOK("late")
    }, 1, 1, true)
    m.sched = sm

    id := m.Submit(taskName, nil, 50*time.Millisecond) // timeout corto
    if id == "" {
        t.Fatalf("id vacío")
    }

    ok := waitUntil(t, 800*time.Millisecond, func() bool {
        m.mu.RLock()
        j := m.jobs[id]
        st := j != nil && j.Status == StatusTimeout
        m.mu.RUnlock()
        return st
    })
    if !ok {
        t.Fatalf("job no llegó a TIMEOUT")
    }

    m.mu.RLock()
    j := m.jobs[id]
    if j.Result == nil || j.Result.Err == nil || j.Result.Err.Code != "timeout" {
        t.Fatalf("esperaba error timeout, got %#v", j.Result)
    }
    m.mu.RUnlock()
}

func TestSubmit_CanceledWhileRunning(t *testing.T) {
    m := newMgrForTest(t)

    taskName := "cancelable"
    sm := mkSchedWithPool(t, taskName, func(ctx context.Context, params map[string]string) resp.Result {
        // Espera cancel
        select {
        case <-ctx.Done():
            return resp.Unavail("canceled", "job canceled")
        case <-time.After(2 * time.Second):
            return resp.PlainOK("should-not-happen")
        }
    }, 1, 1, true)
    m.sched = sm

    id := m.Submit(taskName, nil, time.Second)
    if id == "" {
        t.Fatalf("id vacío")
    }

    // Espera a que cambie a running
    ok := waitUntil(t, 500*time.Millisecond, func() bool {
        m.mu.RLock()
        j := m.jobs[id]
        st := j != nil && j.Status == StatusRunning
        m.mu.RUnlock()
        return st
    })
    if !ok {
        t.Fatalf("no llegó a RUNNING")
    }

    // Cancelar mientras corre
    msg, ok2 := m.Cancel(id)
    if !ok2 || msg != "canceled" {
        t.Fatalf("Cancel running => %v %v", msg, ok2)
    }

    // Debe terminar como canceled cuando el worker devuelve
    ok = waitUntil(t, 800*time.Millisecond, func() bool {
        m.mu.RLock()
        j := m.jobs[id]
        st := j != nil && j.Status == StatusCanceled
        m.mu.RUnlock()
        return st
    })
    if !ok {
        t.Fatalf("job no quedó en CANCELED")
    }
}

func TestSubmit_TimeoutWhenWorkerBusyAndNoQueue(t *testing.T) {
    // Documenta el comportamiento real del scheduler: con worker ocupado y cola 0,
    // el segundo submit termina en TIMEOUT (enq=true) en vez de !enq.
    m := newMgrForTest(t)

    taskName := "timeout-busy"
    started := make(chan struct{}, 1)

    // Primer job ocupa al único worker hasta que lo cancelen.
    task := func(ctx context.Context, params map[string]string) resp.Result {
        select { case started <- struct{}{}: default: }
        <-ctx.Done()
        return resp.Unavail("canceled", "stopped")
    }

    // 1 worker, cola 0 (capacity=0). Arrancado.
    sm := mkSchedWithPool(t, taskName, task, 1, 0, true)
    m.sched = sm

    // Primer submit: ocupa el worker
    id1 := m.Submit(taskName, nil, 2*time.Second)
    if id1 == "" { t.Fatalf("id1 vacío") }

    // Espera a que realmente arranque
    select {
    case <-started:
    case <-time.After(500 * time.Millisecond):
        t.Fatalf("el worker no arrancó a tiempo")
    }

    // Segundo submit: con worker ocupado y sin cola, el scheduler devuelve timeout
    id2 := m.Submit(taskName, nil, 200*time.Millisecond)
    if id2 == "" { t.Fatalf("id2 vacío") }

    ok := waitUntil(t, time.Second, func() bool {
        m.mu.RLock(); j := m.jobs[id2]; st := j != nil && j.Status == StatusTimeout; m.mu.RUnlock(); return st
    })
    if !ok {
        m.mu.RLock()
        got := Status("")
        if j := m.jobs[id2]; j != nil { got = j.Status }
        m.mu.RUnlock()
        t.Fatalf("job2 no quedó en TIMEOUT (got %s)", got)
    }

    // Limpieza: cancela el primer job
    _, _ = m.Cancel(id1)
    _ = waitUntil(t, time.Second, func() bool {
        m.mu.RLock(); j := m.jobs[id1]; st := j != nil && (j.Status == StatusCanceled || j.Status == StatusDone || j.Status == StatusFailed); m.mu.RUnlock(); return st
    })
}

func TestSubmit_CancelBeforeStart(t *testing.T) {
    // Cubre la rama "cancelado antes de arrancar" (select <-ctx.Done() antes de poner RUNNING).
    m := newMgrForTest(t)

    taskName := "pre-cancel"
    // Tarea normal, pero no queremos que llegue a ejecutarse.
    sm := mkSchedWithPool(t, taskName, func(ctx context.Context, params map[string]string) resp.Result {
        // si arrancara, devolvería ok; pero el test cancela antes
        return resp.PlainOK("should-not-run")
    }, 1, 1, true)
    m.sched = sm

    id := m.Submit(taskName, nil, time.Second)
    if id == "" { t.Fatalf("id vacío") }

    // Cancela de inmediato; con algo de suerte llega antes del RUNNING.
    // Para hacerlo más sólido, iteramos hasta ver QUEUED y cancelamos ahí.
    _ = waitUntil(t, 200*time.Millisecond, func() bool {
        m.mu.RLock(); j := m.jobs[id]; st := j != nil && j.Status == StatusQueued; m.mu.RUnlock(); return st
    })
    msg, ok := m.Cancel(id)
    if !ok || msg != "canceled" {
        t.Fatalf("Cancel previo al arranque => %v %v", msg, ok)
    }

    ok2 := waitUntil(t, 600*time.Millisecond, func() bool {
        m.mu.RLock(); j := m.jobs[id]; st := j != nil && j.Status == StatusCanceled && j.StartedAt == nil; m.mu.RUnlock(); return st
    })
    if !ok2 {
        m.mu.RLock()
        j := m.jobs[id]
        m.mu.RUnlock()
        t.Fatalf("job no quedó en CANCELED antes de start; got: %#v", j)
    }
}


func TestSubmit_FailedByNon2xx(t *testing.T) {
    m := newMgrForTest(t)

    taskName := "bad"
    sm := mkSchedWithPool(t, taskName, func(ctx context.Context, params map[string]string) resp.Result {
        // 400 debe mapear a FAILED (rama default)
        return resp.BadReq("bad", "bad params")
    }, 1, 1, true)
    m.sched = sm

    id := m.Submit(taskName, nil, time.Second)
    if id == "" {
        t.Fatalf("id vacío")
    }

    ok := waitUntil(t, time.Second, func() bool {
        m.mu.RLock()
        j := m.jobs[id]
        st := j != nil && j.Status == StatusFailed
        m.mu.RUnlock()
        return st
    })
    if !ok {
        t.Fatalf("job no quedó en FAILED (status no-2xx)")
    }
}

func TestDeriveProgressETA_EarlyExits(t *testing.T) {
	// no running
	j := &Job{
		Task:   "sleep",
		Status: StatusQueued,
	}
	if p, eta := deriveProgressETA(j); p != nil || eta != nil {
		t.Fatalf("esperaba nil,nil cuando no está running, got %v,%v", p, eta)
	}

	// running pero sin StartedAt
	j.Status = StatusRunning
	j.StartedAt = nil
	if p, eta := deriveProgressETA(j); p != nil || eta != nil {
		t.Fatalf("esperaba nil,nil cuando StartedAt==nil, got %v,%v", p, eta)
	}
}

func TestDeriveProgressETA_UnknownTask(t *testing.T) {
	now := time.Now().Add(-200 * time.Millisecond)
	j := &Job{
		Task:      "other",
		Status:    StatusRunning,
		StartedAt: &now,
		Params:    map[string]string{"seconds": "1"},
	}
	if p, eta := deriveProgressETA(j); p != nil || eta != nil {
		t.Fatalf("tarea no soportada debe devolver nil,nil, got %v,%v", p, eta)
	}
}

func TestDeriveProgressETA_InvalidSeconds(t *testing.T) {
	now := time.Now().Add(-100 * time.Millisecond)

	for _, sec := range []string{"", "abc", "0", "-5"} {
		j := &Job{
			Task:      "sleep",
			Status:    StatusRunning,
			StartedAt: &now,
			Params:    map[string]string{"seconds": sec},
		}
		if p, eta := deriveProgressETA(j); p != nil || eta != nil {
			t.Fatalf("seconds=%q inválido debe devolver nil,nil, got %v,%v", sec, p, eta)
		}
	}
}

func TestDeriveProgressETA_FutureStartReturnsZeroPct(t *testing.T) {
	// StartedAt en el futuro → elapsed negativo → se clipea a 0
	start := time.Now().Add(500 * time.Millisecond) // futuro
	j := &Job{
		Task:      "sleep",
		Status:    StatusRunning,
		StartedAt: &start,
		Params:    map[string]string{"seconds": "1"}, // 1s
	}
	p, eta := deriveProgressETA(j)
	if p == nil || *p != 0 {
		t.Fatalf("pct esperado 0 cuando elapse<0, got %v", p)
	}
	if eta == nil || *eta < 900 || *eta > 1100 { // ~1000ms tolerancia
		t.Fatalf("eta esperado ~1000ms, got %v", eta)
	}
}

func TestDeriveProgressETA_Completed(t *testing.T) {
	// elapsed >= d → 100% y eta=0
	d := 1 * time.Second
	start := time.Now().Add(-d - 200*time.Millisecond) // más que suficiente
	j := &Job{
		Task:      "sleep",
		Status:    StatusRunning,
		StartedAt: &start,
		Params:    map[string]string{"seconds": "1"},
	}
	p, eta := deriveProgressETA(j)
	if p == nil || *p != 100 {
		t.Fatalf("pct esperado 100, got %v", p)
	}
	if eta == nil || *eta != 0 {
		t.Fatalf("eta esperado 0, got %v", eta)
	}
}

