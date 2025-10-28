package handlers

import (
	"strings"
	"testing"
	"time"
)

// -------- SleepTask --------

func TestSleepTask_ZeroAndNegative(t *testing.T) {
	t.Parallel()

	// seconds ausente -> Atoi("") => 0
	start := time.Now()
	r0 := SleepTask(map[string]string{})
	elapsed0 := time.Since(start)
	if r0.Status != 200 || r0.JSON || r0.Body != "slept 0 s\n" {
		t.Fatalf("missing seconds -> %v", r0)
	}
	if elapsed0 > 100*time.Millisecond {
		t.Fatalf("slept too long for 0s: %v", elapsed0)
	}

	// seconds negativo -> clamp a 0
	start = time.Now()
	rNeg := SleepTask(map[string]string{"seconds": "-3"})
	elapsedNeg := time.Since(start)
	if rNeg.Body != "slept 0 s\n" {
		t.Fatalf("negative seconds -> %v", rNeg)
	}
	if elapsedNeg > 100*time.Millisecond {
		t.Fatalf("negative slept too long: %v", elapsedNeg)
	}

	// seconds=0 explícito
	start = time.Now()
	rZero := SleepTask(map[string]string{"seconds": "0"})
	elapsedZero := time.Since(start)
	if rZero.Body != "slept 0 s\n" {
		t.Fatalf("seconds=0 -> %v", rZero)
	}
	if elapsedZero > 100*time.Millisecond {
		t.Fatalf("explicit 0s slept too long: %v", elapsedZero)
	}
}

// -------- SpinTask --------

func TestSpinTask_ZeroAndOneSecond(t *testing.T) {
	// 0s: debe salir inmediato
	start := time.Now()
	r0 := SpinTask(map[string]string{"seconds": "0"})
	elapsed0 := time.Since(start)
	if r0.Status != 200 || r0.JSON || r0.Body != "spun 0 s\n" {
		t.Fatalf("spin 0s -> %v", r0)
	}
	if elapsed0 > 100*time.Millisecond {
		t.Fatalf("spin 0s took too long: %v", elapsed0)
	}

	// 1s: debe ejecutar el bucle ~1 segundo (tolerancias amplias)
	start = time.Now()
	r1 := SpinTask(map[string]string{"seconds": "1"})
	elapsed1 := time.Since(start)
	if r1.Status != 200 || r1.JSON || !strings.HasPrefix(r1.Body, "spun 1 s") {
		t.Fatalf("spin 1s -> %v", r1)
	}
	if elapsed1 < 800*time.Millisecond || elapsed1 > 3*time.Second {
		t.Fatalf("spin 1s elapsed out of range: %v", elapsed1)
	}

	// negativo -> clamp a 0 e inmediato
	start = time.Now()
	rNeg := SpinTask(map[string]string{"seconds": "-2"})
	elapsedNeg := time.Since(start)
	if rNeg.Body != "spun 0 s\n" {
		t.Fatalf("spin negative -> %v", rNeg)
	}
	if elapsedNeg > 100*time.Millisecond {
		t.Fatalf("spin negative took too long: %v", elapsedNeg)
	}
}
