package server

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"so-http10-demo/internal/http10"
	"so-http10-demo/internal/router"
	"so-http10-demo/internal/util"
)

// ---- runtime (para /status) ----
var (
	started  = time.Now()
	connSeen uint64
)

func Uptime() time.Duration        { return time.Since(started) }
func ConnCount() uint64            { return atomic.LoadUint64(&connSeen) }
func PID() int                     { return os.Getpid() }
func StartedAt() time.Time         { return started }
func markConnAccepted()            { atomic.AddUint64(&connSeen, 1) }

// HandleConn procesa exactamente una petición HTTP/1.0 y cierra la conexión.
//  - Parseo estricto HTTP/1.0
//  - Trazabilidad: X-Request-Id (+ headers de pools como X-Worker-Id)
//  - Delegación al router
//  - Respuestas con Content-Length y Connection: close
func HandleConn(c net.Conn) {
	defer c.Close()

	// Identificador de trazabilidad por respuesta.
	trace := map[string]string{
		"X-Request-Id": util.NewReqID(),
		"Connection":   "close",
		// Nota: X-Worker-Id vendrá desde res.Headers cuando el handler provenga de un pool.
	}

	// Parseo de la request (request-line + headers).
	r := bufio.NewReader(c)
	req, err := http10.ParseRequest(r)
	if err != nil {
		http10.WriteErrorJSON(c, 400, "bad_request", err.Error(), trace)
		return
	}

	// Enrutamiento
	res := router.Dispatch(req.Method, req.Target)

	// Mezcla de headers extra del resultado (p. ej. X-Worker-Id desde el pool).
	if res.Headers != nil {
		for k, v := range res.Headers {
			if k == "" || v == "" {
				continue
			}
			trace[k] = v
		}
	}

	// Serialización
	if res.JSON {
		if res.Err != nil {
			http10.WriteErrorJSON(c, res.Status, res.Err.Code, res.Err.Detail, trace)
		} else {
			http10.WriteJSONH(c, res.Status, res.Body, trace)
		}
	} else {
		http10.WritePlainH(c, res.Status, res.Body, trace)
	}
}

// ListenAndServe abre un listener TCP en addr y atiende conexiones
// lanzando una goroutine por cada cliente. Bloquea hasta error fatal.
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
		markConnAccepted()
		go HandleConn(conn)
	}
}
