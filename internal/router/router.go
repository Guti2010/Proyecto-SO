package router

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"time"

	"so-http10-demo/internal/handlers"
	"so-http10-demo/internal/http10"
	"so-http10-demo/internal/jobs"
	"so-http10-demo/internal/resp"
	"so-http10-demo/internal/sched"
)

// -----------------------------------------------------------------------------
// Config de timeouts por tipo (CPU/IO) desde variables de entorno.
//   TIMEOUT_CPU: ej. "60s" (default 60s)
//   TIMEOUT_IO : ej. "120s" (default 120s)
// -----------------------------------------------------------------------------
var (
	cpuTimeout = getDurEnv("TIMEOUT_CPU", 60*time.Second)
	ioTimeout  = getDurEnv("TIMEOUT_IO", 120*time.Second)
)

func getDurEnv(key string, def time.Duration) time.Duration {
	if s := os.Getenv(key); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// Manager global para pools.
var manager = sched.NewManager()

var jobman = jobs.NewManager(manager, 10*time.Minute)

// InitPools registra pools con configuración.
func InitPools(cfg map[string]int) {
	wSleep := cfg["workers.sleep"]
	qSleep := cfg["queue.sleep"]
	wSpin := cfg["workers.spin"]
	qSpin := cfg["queue.spin"]

	// Pools básicos (sleep/spin) que llaman a handlers.* con TaskFunc
	_ = manager.Register("sleep", sched.NewPool("sleep",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.SleepTask(p) },
		wSleep, qSleep))

	_ = manager.Register("spin", sched.NewPool("spin",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.SpinTask(p) },
		wSpin, qSpin))

	// CPU
	_ = manager.Register("isprime", sched.NewPool("isprime",
		func(ctx context.Context, p map[string]string) resp.Result { return handlers.IsPrimeJSONCtx(ctx, p) },
		cfg["workers.isprime"], cfg["queue.isprime"]))

	_ = manager.Register("factor", sched.NewPool("factor",
		func(ctx context.Context, p map[string]string) resp.Result { return handlers.FactorJSONCtx(ctx, p) },
		cfg["workers.factor"], cfg["queue.factor"]))

	_ = manager.Register("pi", sched.NewPool("pi",
		func(ctx context.Context, p map[string]string) resp.Result { return handlers.PiJSONCtx(ctx, p) },
		cfg["workers.pi"], cfg["queue.pi"]))

	_ = manager.Register("mandelbrot", sched.NewPool("mandelbrot",
		func(ctx context.Context, p map[string]string) resp.Result { return handlers.MandelbrotJSONCtx(ctx, p) },
		cfg["workers.mandelbrot"], cfg["queue.mandelbrot"]))

	_ = manager.Register("matrixmul", sched.NewPool("matrixmul",
		func(ctx context.Context, p map[string]string) resp.Result { return handlers.MatrixMulHashCtx(ctx, p) },
		cfg["workers.matrixmul"], cfg["queue.matrixmul"]))

	// IO
	_ = manager.Register("wordcount", sched.NewPool("wordcount",
		func(ctx context.Context, p map[string]string) resp.Result { return handlers.WordCountJSONCtx(ctx, p) },
		cfg["workers.wordcount"], cfg["queue.wordcount"]))

	_ = manager.Register("grep", sched.NewPool("grep",
		func(ctx context.Context, p map[string]string) resp.Result { return handlers.GrepJSONCtx(ctx, p) },
		cfg["workers.grep"], cfg["queue.grep"]))

	_ = manager.Register("hashfile", sched.NewPool("hashfile",
		func(ctx context.Context, p map[string]string) resp.Result { return handlers.HashFileJSONCtx(ctx, p) },
		cfg["workers.hashfile"], cfg["queue.hashfile"]))

	_ = manager.Register("sortfile", sched.NewPool("sortfile",
		func(ctx context.Context, p map[string]string) resp.Result { return handlers.SortFileJSONCtx(ctx, p) },
		cfg["workers.sortfile"], cfg["queue.sortfile"]))

	_ = manager.Register("compress", sched.NewPool("compress",
		func(ctx context.Context, p map[string]string) resp.Result { return handlers.CompressJSONCtx(ctx, p) },
		cfg["workers.compress"], cfg["queue.compress"]))
}

// Dispatch resuelve rutas sobre HTTP/1.0 (GET).
func Dispatch(method, target string) resp.Result {
	if method != "GET" {
		return resp.BadReq("method", "only GET")
	}

	path, q := http10.SplitTarget(target)
	args := http10.ParseQuery(q)

	switch path {
	// Básicas
	case "/":
		return resp.PlainOK("hola mundo\n")
	case "/help":
		return handlers.Help()
	case "/timestamp":
		return handlers.Timestamp(nil)
	case "/reverse":
		return handlers.Reverse(args)
	case "/toupper":
		return handlers.ToUpper(args)
	case "/hash":
		return handlers.Hash(args)
	case "/random":
		return handlers.Random(args)
	case "/fibonacci":
		return handlers.Fibonacci(args)

	// Archivos
	case "/createfile":
		return handlers.CreateFile(args)
	case "/deletefile":
		return handlers.DeleteFile(args)

	// Pools / simulación
	case "/sleep":
		r, _ := submitSync("sleep", args, ioTimeout)
		return r
	case "/simulate":
		task := args["task"]
		if task != "sleep" && task != "spin" {
			return resp.BadReq("task", "use task=sleep|spin")
		}
		// sleep → IO timeout, spin → CPU timeout
		tout := cpuTimeout
		if task == "sleep" {
			tout = ioTimeout
		}
		r, _ := submitSync(task, args, tout)
		return r
	case "/loadtest":
		n, errN := strconv.Atoi(args["tasks"])
		s, errS := strconv.Atoi(args["sleep"])
		if errN != nil || n <= 0 {
			return resp.BadReq("tasks", "must be integer > 0")
		}
		if errS != nil || s < 0 {
			return resp.BadReq("sleep", "must be integer >= 0")
		}
		ok := 0
		for i := 0; i < n; i++ {
			if r, enq := submitSync("sleep",
				map[string]string{"seconds": strconv.Itoa(s)},
				ioTimeout); enq && r.Status == 200 {
				ok++
			}
		}
		return resp.PlainOK("ok " + strconv.Itoa(ok) + "/" + strconv.Itoa(n) + "\n")

	// Métricas
	case "/metrics":
		return resp.JSONOK(manager.MetricsJSON())

	// CPU-bound (todos usan cpuTimeout)
	case "/isprime":
		r, _ := submitSync("isprime", args, cpuTimeout); return r
	case "/factor":
		r, _ := submitSync("factor", args, cpuTimeout); return r
	case "/pi":
		r, _ := submitSync("pi", args, cpuTimeout); return r
	case "/mandelbrot":
		r, _ := submitSync("mandelbrot", args, cpuTimeout); return r
	case "/matrixmul":
		r, _ := submitSync("matrixmul", args, cpuTimeout); return r

	// IO-bound (todos usan ioTimeout)
	case "/wordcount":
		r, _ := submitSync("wordcount", args, ioTimeout); return r
	case "/grep":
		r, _ := submitSync("grep", args, ioTimeout); return r
	case "/hashfile":
		r, _ := submitSync("hashfile", args, ioTimeout); return r
	case "/sortfile":
		r, _ := submitSync("sortfile", args, ioTimeout); return r
	case "/compress":
		r, _ := submitSync("compress", args, ioTimeout); return r

	// Jobs
	case "/jobs/submit":
		task := args["task"]
		if task == "" {
			return resp.BadReq("task", "task=<pool_name> required")
		}
		// el timeout lo maneja el Job Manager internamente; aquí sólo encolamos
		params := make(map[string]string, len(args))
		for k, v := range args {
			if k == "task" {
				continue
			}
			params[k] = v
		}
		id := jobman.Submit(task, params, cpuTimeout) // puedes separar por tipo si quieres
		if id == "" {
			return resp.NotFound("no_pool", "pool not found")
		}
		out := map[string]any{"job_id": id, "status": "queued"}
		b, _ := json.Marshal(out)
		return resp.JSONOK(string(b))

	case "/jobs/status":
		id := args["id"]
		if id == "" {
			return resp.BadReq("id", "id required")
		}
		if js, ok := jobman.SnapshotJSON(id); ok {
			return resp.JSONOK(js)
		}
		return resp.NotFound("not_found", "job not found")

	case "/jobs/result":
		id := args["id"]
		if id == "" {
			return resp.BadReq("id", "id required")
		}
		body, ok, err := jobman.ResultJSON(id)
		if !ok {
			return resp.NotFound("not_found", "job not found")
		}
		if err != nil {
			return resp.BadReq("not_ready", "job not finished yet")
		}
		return resp.JSONOK(body)

	case "/jobs/cancel":
		id := args["id"]
		if id == "" {
			return resp.BadReq("id", "id required")
		}
		st, ok := jobman.Cancel(id)
		if !ok {
			return resp.NotFound("not_found", "job not found")
		}
		out := map[string]any{"status": st}
		b, _ := json.Marshal(out)
		return resp.JSONOK(string(b))

	case "/jobs/list":
		return resp.JSONOK(jobman.ListJSON())
	}

	return resp.NotFound("not_found", "route")
}

// submitSync encola con timeout y espera resultado/timeout de ejecución.
// Devuelve (resultado, encolado?). Si encolado=false → backpressure (503).
func submitSync(name string, args map[string]string, timeout time.Duration) (resp.Result, bool) {
	p, ok := manager.Pool(name)
	if !ok {
		return resp.IntErr("no_pool", "pool not found"), true
	}
	return p.SubmitAndWait(args, timeout)
}

// Close cierra recursos del router (Job Manager).
func Close() {
	if jobman != nil {
		jobman.Close()
	}
}

// PoolsSummary devuelve un mapa resumido por pool para /status (sin ciclo).
func PoolsSummary() map[string]any {
	var raw map[string]any
	_ = json.Unmarshal([]byte(manager.MetricsJSON()), &raw)

	pools := make(map[string]any, len(raw))
	for name, v := range raw {
		m := v.(map[string]any)
		w := m["workers"].(map[string]any)
		pools[name] = map[string]any{
			"workers": map[string]any{
				"total": w["total"],
				"busy":  w["busy"],
				"idle":  w["idle"],
			},
			"queue_len": m["queue_len"],
			"queue_cap": m["queue_cap"],
		}
	}
	return pools
}
