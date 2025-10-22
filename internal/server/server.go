package server

import (
	"bufio"
	"encoding/json"
	"net"
	"strconv"
	"sync/atomic"
	"time"
	"os"

	"so-http10-demo/internal/http10"
	"so-http10-demo/internal/router"
	"so-http10-demo/internal/util"
)

var (
	startedAt = time.Now()
	connCount uint64
)

func pid() int              { return os.Getpid() }           // importa "os"
func uptime() time.Duration { return time.Since(startedAt) }
func conns() uint64         { return atomic.LoadUint64(&connCount) }

func HandleConn(c net.Conn) {
	defer c.Close()

	trace := map[string]string{
		"X-Request-Id": util.NewReqID(),
		"X-Worker-Pid": strconv.Itoa(pid()),
		"Connection":   "close",
	}

	// Parseo HTTP/1.0
	r := bufio.NewReader(c)
	req, err := http10.ParseRequest(r)
	if err != nil {
		http10.WriteErrorJSON(c, 400, "bad_request", err.Error(), trace)
		return
	}

	// Intercepta /status aquí (evita importar server en router)
	if req.Method == "GET" {
		path, _ := http10.SplitTarget(req.Target)
		if path == "/status" {
			out := map[string]any{
				"pid":         pid(),
				"uptime_ms":   uptime().Milliseconds(),
				"started_at":  startedAt.UTC().Format(time.RFC3339Nano),
				"connections": conns(),
				"pools":       router.PoolsSummary(), // <- viene del router
			}
			b, _ := json.Marshal(out)
			http10.WriteJSONH(c, 200, string(b), trace)
			return
		}
	}

	// Resto de rutas
	res := router.Dispatch(req.Method, req.Target)

	// Mezcla headers de trazabilidad con los del Result (si tienes ese campo)
	hdrs := map[string]string{}
	for k, v := range trace {
		hdrs[k] = v
	}
	if res.Headers != nil {
		for k, v := range res.Headers {
			hdrs[k] = v
		}
	}

	if res.JSON {
		if res.Err != nil {
			http10.WriteErrorJSON(c, res.Status, res.Err.Code, res.Err.Detail, hdrs)
		} else {
			http10.WriteJSONH(c, res.Status, res.Body, hdrs)
		}
	} else {
		http10.WritePlainH(c, res.Status, res.Body, hdrs)
	}
}

func ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		atomic.AddUint64(&connCount, 1) // cuenta conexiones aceptadas
		go HandleConn(conn)
	}
}
