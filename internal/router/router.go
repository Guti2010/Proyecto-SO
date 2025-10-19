package router

import (
	"context"
	"strconv"
	"time"

	"so-http10-demo/internal/handlers"
	"so-http10-demo/internal/http10"
	"so-http10-demo/internal/resp"
	"so-http10-demo/internal/sched"
)

// Manager global para pools.
var manager = sched.NewManager()

// InitPools registra pools con configuración.
func InitPools(cfg map[string]int) {
	wSleep := cfg["workers.sleep"]
	qSleep := cfg["queue.sleep"]
	wSpin := cfg["workers.spin"]
	qSpin := cfg["queue.spin"]

	// Adaptadores: elevan handlers.* (sin ctx) a TaskFunc (con ctx).
	sleepTF := func(_ context.Context, p map[string]string) resp.Result { return handlers.SleepTask(p) }
	spinTF := func(_ context.Context, p map[string]string) resp.Result { return handlers.SpinTask(p) }

	_ = manager.Register("sleep", sched.NewPool("sleep", sleepTF, wSleep, qSleep))
	_ = manager.Register("spin", sched.NewPool("spin", spinTF, wSpin, qSpin))
	// CPU
	_ = manager.Register("isprime", sched.NewPool("isprime",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.IsPrimeJSON(p) },
		cfg["workers.isprime"], cfg["queue.isprime"]))
	_ = manager.Register("factor", sched.NewPool("factor",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.FactorJSON(p) },
		cfg["workers.factor"], cfg["queue.factor"]))
	_ = manager.Register("pi", sched.NewPool("pi",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.PiJSON(p) },
		cfg["workers.pi"], cfg["queue.pi"]))
	_ = manager.Register("mandelbrot", sched.NewPool("mandelbrot",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.MandelbrotJSON(p) },
		cfg["workers.mandelbrot"], cfg["queue.mandelbrot"]))
	_ = manager.Register("matrixmul", sched.NewPool("matrixmul",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.MatrixMulHash(p) },
		cfg["workers.matrixmul"], cfg["queue.matrixmul"]))

	// IO
	_ = manager.Register("wordcount", sched.NewPool("wordcount",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.WordCountJSON(p) },
		cfg["workers.wordcount"], cfg["queue.wordcount"]))
	_ = manager.Register("grep", sched.NewPool("grep",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.GrepJSON(p) },
		cfg["workers.grep"], cfg["queue.grep"]))
	_ = manager.Register("hashfile", sched.NewPool("hashfile",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.HashFileJSON(p) },
		cfg["workers.hashfile"], cfg["queue.hashfile"]))

	// IO (agrega estas dos líneas)
	_ = manager.Register("sortfile", sched.NewPool("sortfile",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.SortFileJSON(p) },
		cfg["workers.sortfile"], cfg["queue.sortfile"]))

	_ = manager.Register("compress", sched.NewPool("compress",
		func(_ context.Context, p map[string]string) resp.Result { return handlers.CompressJSON(p) },
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
	case "/":
		return resp.PlainOK("hola mundo\n")
	case "/help":
		return resp.PlainOK(handlers.HelpText())
	case "/status":
		return resp.JSONOK(handlers.StatusJSON())
	case "/timestamp":
		return resp.JSONOK(handlers.TimestampJSON())
	case "/reverse":
		return resp.PlainOK(handlers.Reverse(args["text"]))
	case "/toupper":
    	return resp.PlainOK(handlers.ToUpper(args["text"])) 
	case "/hash":
		return resp.JSONOK(handlers.HashJSON(args["text"]))
	case "/random":
		return resp.JSONOK(handlers.RandomJSON(atoi(args["count"]), atoi(args["min"]), atoi(args["max"])))
	case "/fibonacci":
		return resp.PlainOK(handlers.FibonacciText(atoi(args["num"])))
	case "/createfile":
		return handlers.CreateFile(args)
	case "/deletefile":
		return handlers.DeleteFile(args)

	// --- M5: pools/colas ---
	case "/sleep":
		r, _ := submitSync("sleep", args, 15*time.Second)
		return r
	case "/simulate":
		task := args["task"]
		if task != "sleep" && task != "spin" {
			return resp.BadReq("task", "use task=sleep|spin")
		}
		r, _ := submitSync(task, args, 30*time.Second)
		return r
	case "/loadtest":
		// lanza N tareas sleep de X segundos; cuenta cuántas se ejecutaron (no backpressure)
		n := atoi(args["tasks"])
		if n <= 0 {
			n = 1
		}
		sec := args["sleep"]
		ok := 0
		for i := 0; i < n; i++ {
			if r, enq := submitSync("sleep", map[string]string{"seconds": sec}, 10*time.Second); enq && r.Status == 200 {
				ok++
			}
		}
		return resp.PlainOK("ok " + strconv.Itoa(ok) + "/" + strconv.Itoa(n) + "\n")
	case "/metrics":
		return resp.JSONOK(manager.MetricsJSON())

	case "/isprime":
		r, _ := submitSync("isprime", args, 30*time.Second); return r
	case "/factor":
		r, _ := submitSync("factor", args, 60*time.Second); return r
	case "/pi":
		// si te pasaron timeout_ms en la URL, respétalo hasta un máximo razonable
		t := 120 * time.Second
		if ms, ok := args["timeout_ms"]; ok {
			if v, err := strconv.Atoi(ms); err == nil && v > 0 {
				if d := time.Duration(v) * time.Millisecond; d > t {
					t = d
				} else {
					t = d
				}
			}
		}
		r, _ := submitSync("pi", args, t)
		return r

	case "/mandelbrot":
		r, _ := submitSync("mandelbrot", args, 60*time.Second); return r
	case "/matrixmul":
		r, _ := submitSync("matrixmul", args, 60*time.Second); return r

	case "/wordcount":
		r, _ := submitSync("wordcount", args, 60*time.Second); return r
	case "/grep":
		r, _ := submitSync("grep", args, 60*time.Second); return r
	case "/hashfile":
		r, _ := submitSync("hashfile", args, 60*time.Second); return r	
	case "/sortfile":
		r, _ := submitSync("sortfile", args, 10*time.Minute); return r
	case "/compress":
		r, _ := submitSync("compress", args, 10*time.Minute); return r
	}

	return resp.NotFound("not_found", "route")
}

// submitSync encola con timeout y espera resultado/timeout de ejecución.
// Devuelve (resultado, encolado?). Si encolado=false → backpressure.
func submitSync(name string, args map[string]string, timeout time.Duration) (resp.Result, bool) {
	p, ok := manager.Pool(name)
	if !ok {
		return resp.IntErr("no_pool", "pool not found"), true
	}
	return p.SubmitAndWait(args, timeout)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
