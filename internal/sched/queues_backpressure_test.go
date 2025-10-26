package sched

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"so-http10-demo/internal/resp"
)

// tarea lenta para ocupar al worker y saturar la cola
func slowTask(_ context.Context, _ map[string]string) resp.Result {
	time.Sleep(200 * time.Millisecond)
	return resp.PlainOK("ok\n")
}

// go test ./internal/sched -run TestPool_Backpressure -v -count=1
func TestPool_Backpressure(t *testing.T) {
	p := NewPool("bp", slowTask, /*workers=*/1, /*capacity=*/2)
	p.Start()
	defer p.Close()

	const total = 32
	var enq, rej int64
	var wg sync.WaitGroup
	wg.Add(total)

	for i := 0; i < total; i++ {
		go func() {
			defer wg.Done()
			if _, ok := p.SubmitAndWait(map[string]string{}, 10*time.Millisecond); ok {
				atomic.AddInt64(&enq, 1)
			} else {
				atomic.AddInt64(&rej, 1)
			}
		}()
	}
	wg.Wait()

	if rej == 0 {
		t.Fatalf("esperaba rechazos por backpressure; enqueued=%d rejected=%d", enq, rej)
	}
}
