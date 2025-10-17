package handlers

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"so-http10-demo/internal/resp"
)

// SleepTask espera "seconds" segundos simulando IO y devuelve 200.
func SleepTask(params map[string]string) resp.Result {
	sec, _ := strconv.Atoi(params["seconds"])
	if sec < 0 {
		sec = 0
	}
	time.Sleep(time.Duration(sec) * time.Second)
	return resp.PlainOK(fmt.Sprintf("slept %d s\n", sec))
}

// SpinTask simula CPU-bound: itera durante "seconds" segundos.
func SpinTask(params map[string]string) resp.Result {
	sec, _ := strconv.Atoi(params["seconds"])
	if sec < 0 {
		sec = 0
	}
	end := time.Now().Add(time.Duration(sec) * time.Second)
	x := 0.0
	for time.Now().Before(end) {
		x += math.Sqrt(99991.0) // operación pequeña para gastar CPU
		if x > 1e9 {
			x = 0
		}
	}
	return resp.PlainOK(fmt.Sprintf("spun %d s\n", sec))
}
