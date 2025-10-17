package test

import (
	"encoding/json"
	"strings"
	"testing"

	"so-http10-demo/internal/handlers"
)

func TestHandlersBasic(t *testing.T) {
	if handlers.HelpText() == "" { t.Fatal("help empty") }
	if got := strings.TrimSpace(handlers.Reverse("áé")); got != "éá" { t.Fatalf("reverse: %q", got) }
	if handlers.FibonacciText(10) != "55\n" { t.Fatal("fib") }

	if err := json.Unmarshal([]byte(handlers.StatusJSON()), &map[string]any{}); err != nil { t.Fatalf("status: %v", err) }
	if err := json.Unmarshal([]byte(handlers.TimestampJSON()), &map[string]any{}); err != nil { t.Fatalf("ts: %v", err) }

	var hm map[string]string
	if err := json.Unmarshal([]byte(handlers.HashJSON("hola")), &hm); err != nil || len(hm["hex"]) != 64 {
		t.Fatalf("hash: %v %v", err, hm)
	}

	js := handlers.RandomJSON(5, 10, 12)
	var rm map[string][]int
	if err := json.Unmarshal([]byte(js), &rm); err != nil || len(rm["values"]) != 5 { t.Fatalf("random: %v %v", err, rm) }
	for _, v := range rm["values"] { if v < 10 || v > 12 { t.Fatalf("oob: %v", v) } }
}

func TestFibonacci_EdgeCases(t *testing.T) {
	if got := handlers.FibonacciText(-1); !strings.HasPrefix(got, "error:") {
		t.Fatalf("want error on negative, got %q", got)
	}
	if got := handlers.FibonacciText(0); got != "0\n" {
		t.Fatalf("n=0: %q", got)
	}
	if got := handlers.FibonacciText(1); got != "1\n" {
		t.Fatalf("n=1: %q", got)
	}
}

func TestRandomJSON_Edges(t *testing.T) {
	// n <= 0 → n=1
	js := handlers.RandomJSON(0, 3, 3)
	var m map[string][]int
	if err := json.Unmarshal([]byte(js), &m); err != nil || len(m["values"]) != 1 || m["values"][0] != 3 {
		t.Fatalf("n<=0 case: %v %#v", err, m)
	}
	// max < min → swap
	js = handlers.RandomJSON(2, 10, 5)
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatalf("swap parse: %v", err)
	}
	for _, v := range m["values"] {
		if v < 5 || v > 10 { t.Fatalf("swap out of range: %v", v) }
	}
	// n > 1000 → clamp
	js = handlers.RandomJSON(1500, 0, 0)
	if err := json.Unmarshal([]byte(js), &m); err != nil || len(m["values"]) != 1000 {
		t.Fatalf("clamp n>1000: %v len=%d", err, len(m["values"]))
	}
}
