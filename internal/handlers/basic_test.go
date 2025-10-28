package handlers

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"so-http10-demo/internal/resp"
)

// ---------- helpers ----------

func mustParseJSON[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\ninput: %q", err, s)
	}
	return v
}

// Serializa tests que modifican la variable global Submit.
var submitGuard sync.Mutex

func withSubmit(
	t *testing.T,
	mock func(task string, params map[string]string, timeout time.Duration) (resp.Result, bool),
	body func(),
) {
	t.Helper()
	submitGuard.Lock()
	defer submitGuard.Unlock()
	prev := Submit
	Submit = mock
	defer func() { Submit = prev }()
	body()
}

// ---------- tests for core (unexported) ----------

func TestReverseCore(t *testing.T) {
	t.Parallel()
	got := reverseCore("¡Hola, 世界!")
	want := "!界世 ,aloH¡\n"
	if got != want {
		t.Fatalf("reverseCore: got %q want %q", got, want)
	}
}

func TestToUpperCore(t *testing.T) {
	t.Parallel()
	got := toUpperCore("aBc123ñ")
	want := "ABC123Ñ\n"
	if got != want {
		t.Fatalf("toUpperCore: got %q want %q", got, want)
	}
}

func TestHashCore(t *testing.T) {
	t.Parallel()
	type out struct {
		Algo string `json:"algo"`
		Hex  string `json:"hex"`
	}
	o := mustParseJSON[out](t, hashCore("abc"))
	if o.Algo != "sha256" {
		t.Fatalf("algo = %q", o.Algo)
	}
	// SHA-256("abc")
	const exp = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if o.Hex != exp {
		t.Fatalf("hex = %q want %q", o.Hex, exp)
	}
}

func TestTimestampCore(t *testing.T) {
	t.Parallel()
	type out struct {
		Unix int64  `json:"unix"`
		UTC  string `json:"utc"`
	}
	o := mustParseJSON[out](t, timestampCore())
	tt, err := time.Parse(time.RFC3339, o.UTC)
	if err != nil {
		t.Fatalf("utc not RFC3339: %v (val=%q)", err, o.UTC)
	}
	if o.Unix != tt.Unix() {
		t.Fatalf("unix mismatch: json=%d parse(utc)=%d", o.Unix, tt.Unix())
	}
}

func TestFibonacciCore(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int
		want string
	}{
		{0, "0\n"},
		{1, "1\n"},
		{10, "55\n"},
		{-5, "error: num debe ser >=0\n"},
	}
	for _, tc := range cases {
		got := fibonacciCore(tc.n)
		if got != tc.want {
			t.Fatalf("fib(%d)=%q want %q", tc.n, got, tc.want)
		}
	}
}

func TestRandomCore(t *testing.T) {
	t.Parallel()
	type out struct {
		Values []int `json:"values"`
	}
	o := mustParseJSON[out](t, randomCore(20, -1, 1))
	if len(o.Values) != 20 {
		t.Fatalf("len=%d want 20", len(o.Values))
	}
	for i, v := range o.Values {
		if v < -1 || v > 1 {
			t.Fatalf("value[%d]=%d out of range [-1,1]", i, v)
		}
	}
}

// ---------- tests for exported handlers ----------

func TestHelpContainsRoutes(t *testing.T) {
	t.Parallel()
	b := Help()
	if b.Status != 200 || b.JSON {
		t.Fatalf("status/json = %d/%v", b.Status, b.JSON)
	}
	wantSnippets := []string{
		"/reverse?text=abc",
		"/toupper?text=abc",
		"/random?count=n&min=a&max=b",
		"/timestamp",
		"/hash?text=abc",
		"/createfile?name=FILE",
		"/jobs/submit?task=TASK",
	}
	for _, s := range wantSnippets {
		if !strings.Contains(b.Body, s) {
			t.Fatalf("Help() body missing %q", s)
		}
	}
}

func TestTimestampHandler(t *testing.T) {
	t.Parallel()
	r := Timestamp(nil)
	if r.Status != 200 || !r.JSON {
		t.Fatalf("Timestamp: %+v", r)
	}
	type out struct {
		Unix int64  `json:"unix"`
		UTC  string `json:"utc"`
	}
	o := mustParseJSON[out](t, r.Body)
	if _, err := time.Parse(time.RFC3339, o.UTC); err != nil {
		t.Fatalf("UTC not RFC3339: %v", err)
	}
}

func TestReverseAndToUpperHandlers(t *testing.T) {
	t.Parallel()
	// Reverse ok
	r := Reverse(map[string]string{"text": "Hola"})
	if r.Status != 200 || r.JSON {
		t.Fatalf("Reverse ok: status/json = %d/%v", r.Status, r.JSON)
	}
	if r.Body != "aloH\n" {
		t.Fatalf("Reverse body=%q", r.Body)
	}
	// Reverse missing
	miss := Reverse(map[string]string{})
	if miss.Status != 400 || !miss.JSON || miss.Err == nil || miss.Err.Code != "missing_param" {
		t.Fatalf("Reverse missing: %+v", miss)
	}

	// ToUpper ok
	u := ToUpper(map[string]string{"text": "aBc"})
	if u.Status != 200 || u.JSON || u.Body != "ABC\n" {
		t.Fatalf("ToUpper ok: %+v", u)
	}
	// ToUpper missing
	um := ToUpper(map[string]string{})
	if um.Status != 400 || um.Err == nil || um.Err.Code != "missing_param" {
		t.Fatalf("ToUpper missing: %+v", um)
	}
}

func TestHashHandler(t *testing.T) {
	t.Parallel()
	// OK
	h := Hash(map[string]string{"text": "abc"})
	if h.Status != 200 || !h.JSON {
		t.Fatalf("Hash ok: %+v", h)
	}
	type out struct {
		Algo string `json:"algo"`
		Hex  string `json:"hex"`
	}
	o := mustParseJSON[out](t, h.Body)
	if o.Algo != "sha256" || len(o.Hex) != 64 {
		t.Fatalf("Hash payload: %+v", o)
	}
	// Missing param
	m := Hash(map[string]string{})
	if m.Status != 400 || m.Err == nil || m.Err.Code != "missing_param" {
		t.Fatalf("Hash missing: %+v", m)
	}
}

func TestRandomHandler(t *testing.T) {
	t.Parallel()
	// Missing params
	if r := Random(map[string]string{}); r.Status != 400 {
		t.Fatalf("want 400 for missing count")
	}
	if r := Random(map[string]string{"count": "1"}); r.Status != 400 {
		t.Fatalf("want 400 for missing min")
	}
	if r := Random(map[string]string{"count": "1", "min": "0"}); r.Status != 400 {
		t.Fatalf("want 400 for missing max")
	}
	// Invalid ints
	if r := Random(map[string]string{"count": "0", "min": "0", "max": "1"}); r.Status != 400 {
		t.Fatalf("count must be >=1")
	}
	if r := Random(map[string]string{"count": "1", "min": "a", "max": "1"}); r.Status != 400 {
		t.Fatalf("min must be int")
	}
	if r := Random(map[string]string{"count": "1", "min": "0", "max": "x"}); r.Status != 400 {
		t.Fatalf("max must be int")
	}
	if r := Random(map[string]string{"count": "1", "min": "5", "max": "2"}); r.Status != 400 {
		t.Fatalf("min<=max validation")
	}

	// OK path & range check
	ok := Random(map[string]string{"count": "5", "min": "-1", "max": "1"})
	if ok.Status != 200 || !ok.JSON {
		t.Fatalf("Random ok: %+v", ok)
	}
	type out struct{ Values []int `json:"values"` }
	o := mustParseJSON[out](t, ok.Body)
	if len(o.Values) != 5 {
		t.Fatalf("len=%d want 5", len(o.Values))
	}
	for i, v := range o.Values {
		if v < -1 || v > 1 {
			t.Fatalf("value[%d]=%d out of range [-1,1]", i, v)
		}
	}
}

func TestFibonacciHandler(t *testing.T) {
	t.Parallel()
	if r := Fibonacci(map[string]string{}); r.Status != 400 || r.Err == nil || r.Err.Code != "missing_param" {
		t.Fatalf("Fibonacci missing: %+v", r)
	}
	if r := Fibonacci(map[string]string{"num": "-3"}); r.Status != 400 || r.Err == nil {
		t.Fatalf("Fibonacci negative must 400: %+v", r)
	}
	if r := Fibonacci(map[string]string{"num": "x"}); r.Status != 400 || r.Err == nil {
		t.Fatalf("Fibonacci bad int: %+v", r)
	}
	if r := Fibonacci(map[string]string{"num": "10"}); r.Status != 200 || r.Body != "55\n" {
		t.Fatalf("Fibonacci ok: %+v", r)
	}
}

// Tests que modifican Submit: usar withSubmit (sin t.Parallel)

func TestSleepSimulateLoadTestWithSubmit(t *testing.T) {
	withSubmit(t, func(task string, params map[string]string, timeout time.Duration) (resp.Result, bool) {
		return resp.PlainOK("ok\n"), true
	}, func() {
		// Sleep ok
		s := Sleep(map[string]string{"seconds": "0"})
		if s.Status != 200 {
			t.Fatalf("Sleep ok: %+v", s)
		}
		// Sleep validation
		if r := Sleep(map[string]string{}); r.Status != 400 {
			t.Fatalf("Sleep missing: %+v", r)
		}
		if r := Sleep(map[string]string{"seconds": "-1"}); r.Status != 400 {
			t.Fatalf("Sleep negative: %+v", r)
		}
		// Simulate ok
		sm := Simulate(map[string]string{"task": "sleep", "seconds": "1"})
		if sm.Status != 200 {
			t.Fatalf("Simulate ok: %+v", sm)
		}
		// Simulate validation
		if r := Simulate(map[string]string{}); r.Status != 400 {
			t.Fatalf("Simulate missing all: %+v", r)
		}
		if r := Simulate(map[string]string{"task": "x", "seconds": "1"}); r.Status != 400 {
			t.Fatalf("Simulate bad task: %+v", r)
		}
		if r := Simulate(map[string]string{"task": "sleep", "seconds": "-2"}); r.Status != 400 {
			t.Fatalf("Simulate negative seconds: %+v", r)
		}
		// LoadTest ok
		lt := LoadTest(map[string]string{"tasks": "3", "sleep": "0"})
		if lt.Status != 200 || !strings.HasPrefix(lt.Body, "ok 3/3") {
			t.Fatalf("LoadTest ok: %+v", lt)
		}
	})
}

func TestSubmitNilErrors(t *testing.T) {
	withSubmit(t, nil, func() {
		if r := Sleep(map[string]string{"seconds": "0"}); r.Status != 500 || r.Err == nil {
			t.Fatalf("Sleep with Submit=nil must 500: %+v", r)
		}
		if r := Simulate(map[string]string{"task": "sleep", "seconds": "0"}); r.Status != 500 || r.Err == nil {
			t.Fatalf("Simulate with Submit=nil must 500: %+v", r)
		}
	})
}

func TestSimulate_SpinAndBadSeconds(t *testing.T) {
	withSubmit(t, func(task string, params map[string]string, timeout time.Duration) (resp.Result, bool) {
		// verifica que se propaga "spin" y "seconds"
		if task != "spin" || params["seconds"] != "0" {
			return resp.BadReq("mismatch", "bad params"), true
		}
		return resp.PlainOK("ok\n"), true
	}, func() {
		// OK con task=spin
		ok := Simulate(map[string]string{"task": "spin", "seconds": "0"})
		if ok.Status != 200 || ok.JSON {
			t.Fatalf("Simulate spin ok: %+v", ok)
		}
		// seconds no entero → 400
		bad := Simulate(map[string]string{"task": "sleep", "seconds": "NaN"})
		if bad.Status != 400 || bad.Err == nil {
			t.Fatalf("Simulate bad seconds: %+v", bad)
		}
	})
}

// --- LoadTest: validaciones de parámetros ---
func TestLoadTest_Validation(t *testing.T) {
	// faltan params
	if r := LoadTest(map[string]string{}); r.Status != 400 {
		t.Fatalf("missing tasks+sleep: %+v", r)
	}
	// falta sleep
	if r := LoadTest(map[string]string{"tasks": "2"}); r.Status != 400 {
		t.Fatalf("missing sleep: %+v", r)
	}
	// tasks no-int
	if r := LoadTest(map[string]string{"tasks": "x", "sleep": "0"}); r.Status != 400 {
		t.Fatalf("tasks must int: %+v", r)
	}
	// tasks <= 0
	if r := LoadTest(map[string]string{"tasks": "0", "sleep": "0"}); r.Status != 400 {
		t.Fatalf("tasks<=0: %+v", r)
	}
	// sleep no-int
	if r := LoadTest(map[string]string{"tasks": "2", "sleep": "x"}); r.Status != 400 {
		t.Fatalf("sleep must int: %+v", r)
	}
	// sleep < 0
	if r := LoadTest(map[string]string{"tasks": "2", "sleep": "-1"}); r.Status != 400 {
		t.Fatalf("sleep<0: %+v", r)
	}
}

// --- LoadTest: mezcla (200 cuenta, 400 no, enq=false no) ---
func TestLoadTest_PartialSuccess_And_NotEnqueued(t *testing.T) {
	withSubmit(t, func(task string, params map[string]string, timeout time.Duration) (resp.Result, bool) {
		// 4 llamadas: 200/true, 400/true, 200/false, 200/true  -> ok 2/4
		call++
		switch call {
		case 1:
			return resp.PlainOK("ok\n"), true          // cuenta
		case 2:
			return resp.BadReq("fail", "nope"), true   // no cuenta (status!=200)
		case 3:
			return resp.PlainOK("ok\n"), false         // no cuenta (enq=false)
		default:
			return resp.PlainOK("ok\n"), true          // cuenta
		}
	}, func() {
		call = 0
		r := LoadTest(map[string]string{"tasks": "4", "sleep": "0"})
		if r.Status != 200 || !strings.HasPrefix(r.Body, "ok 2/4") {
			t.Fatalf("want ok 2/4, got: %+v", r)
		}
		if call != 4 {
			t.Fatalf("Submit calls=%d want=4", call)
		}
	})
}
var call int // fuera para que el closure pueda actualizarlo

// --- LoadTest: Submit=nil (500 internal) ---
func TestLoadTest_SubmitNil(t *testing.T) {
	withSubmit(t, nil, func() {
		r := LoadTest(map[string]string{"tasks": "1", "sleep": "0"})
		if r.Status != 500 || r.Err == nil {
			t.Fatalf("expected 500 when Submit=nil: %+v", r)
		}
	})
}
