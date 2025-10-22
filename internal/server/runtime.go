package server

import (
	"os"
	"sync/atomic"
	"time"
)

var (
	started  = time.Now()
	connSeen uint64
)

func markConnAccepted() { atomic.AddUint64(&connSeen, 1) }

// Llama a markConnAccepted() justo cuando aceptes/atiendas un socket.
// Si en tu loop ya cuentas conexiones, sustituye por esa llamada.

func Uptime() time.Duration               { return time.Since(started) }
func ConnCount() uint64                   { return atomic.LoadUint64(&connSeen) }
func PID() int                            { return os.Getpid() }
func StartedAt() time.Time                { return started }
