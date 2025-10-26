package server

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
)

// Abre N conexiones paralelas contra HandleConn usando net.Pipe.
// Ejecuta con: go test ./internal/server -run TestConcurrentConnections_NoRace -race -v -count=1
func TestConcurrentConnections_NoRace(t *testing.T) {
	const N = 200
	var wg sync.WaitGroup
	wg.Add(N)

	for i := 0; i < N; i++ {
		srv, cli := net.Pipe()

		go func() {
			defer wg.Done()
			defer cli.Close()

			// atiende la conexión como si fuera un socket real
			go HandleConn(srv)

			// request mínimo HTTP/1.0
			_, _ = cli.Write([]byte("GET /help HTTP/1.0\r\n\r\n"))

			br := bufio.NewReader(cli)
			status, _ := br.ReadString('\n')
			if !strings.HasPrefix(status, "HTTP/1.0 200") {
				t.Fatalf("status=%q", status)
			}
		}()
	}

	wg.Wait()
}
