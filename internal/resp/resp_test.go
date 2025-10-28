package resp

import (
	"testing"
)

// ---------- Constructores: éxito ----------

func TestPlainOK_And_JSONOK(t *testing.T) {
	r1 := PlainOK("hola\n")
	if r1.Status != 200 || r1.JSON || r1.Body != "hola\n" || r1.Err != nil {
		t.Fatalf("PlainOK mismatch: %+v", r1)
	}
	if r1.Headers != nil {
		t.Fatalf("PlainOK must have nil Headers initially")
	}

	r2 := JSONOK(`{"ok":true}`)
	if r2.Status != 200 || !r2.JSON || r2.Body != `{"ok":true}` || r2.Err != nil {
		t.Fatalf("JSONOK mismatch: %+v", r2)
	}
	if r2.Headers != nil {
		t.Fatalf("JSONOK should start with nil Headers")
	}
}

// ---------- Constructores: errores ----------

func TestErrorConstructors_Status_JSON_Err(t *testing.T) {
	type tc struct {
		name   string
		got    Result
		status int
		code   string
		detail string
	}

	tests := []tc{
		{"BadReq", BadReq("bad", "x"), 400, "bad", "x"},
		{"NotFound", NotFound("nf", "missing"), 404, "nf", "missing"},
		{"Conflict", Conflict("conf", "dup"), 409, "conf", "dup"},
		{"TooMany", TooMany("rate", "slow down"), 429, "rate", "slow down"},
		{"IntErr", IntErr("panic", "boom"), 500, "panic", "boom"},
		{"Unavail", Unavail("canceled", "ctx done"), 503, "canceled", "ctx done"},
	}

	for _, tt := range tests {
		if tt.got.Status != tt.status {
			t.Fatalf("%s status=%d want %d", tt.name, tt.got.Status, tt.status)
		}
		if !tt.got.JSON {
			t.Fatalf("%s JSON must be true", tt.name)
		}
		if tt.got.Err == nil || tt.got.Err.Code != tt.code || tt.got.Err.Detail != tt.detail {
			t.Fatalf("%s Err mismatch: %+v", tt.name, tt.got.Err)
		}
		if tt.got.Body != "" {
			t.Fatalf("%s Body should be empty when Err!=nil", tt.name)
		}
		if tt.got.Headers != nil {
			t.Fatalf("%s Headers must start nil", tt.name)
		}
	}
}

// ---------- WithHeader: crea mapa si nil, conserva campos ----------

func TestWithHeader_CreatesMap_WhenNil_AndKeepsFields(t *testing.T) {
	base := PlainOK("hi")
	if base.Headers != nil {
		t.Fatalf("precondition: Headers should be nil")
	}
	with := base.WithHeader("X-Trace", "t-1")

	// No muta el original (porque era nil y el mapa se creó en la copia).
	if base.Headers != nil {
		t.Fatalf("original Headers must remain nil")
	}
	if with.Headers == nil || with.Headers["X-Trace"] != "t-1" {
		t.Fatalf("missing header in copy: %+v", with.Headers)
	}

	// Conserva el resto de campos
	if with.Status != base.Status || with.Body != base.Body || with.JSON != base.JSON || with.Err != base.Err {
		t.Fatalf("fields changed unexpectedly: base=%+v with=%+v", base, with)
	}
}

// ---------- WithHeader: sobreescritura y encadenamiento ----------

func TestWithHeader_Chaining_And_Overwrite(t *testing.T) {
	r := JSONOK(`{}`)

	// primer header (crea mapa en la copia)
	r1 := r.WithHeader("A", "1")
	if r1.Headers["A"] != "1" {
		t.Fatalf("A missing: %+v", r1.Headers)
	}

	// segundo header encadenado; además sobrescribimos A
	r2 := r1.WithHeader("B", "2").WithHeader("A", "9")
	if r2.Headers["A"] != "9" || r2.Headers["B"] != "2" {
		t.Fatalf("chain overwrite failed: %+v", r2.Headers)
	}

	// status/body/json permanecen
	if r2.Status != 200 || !r2.JSON || r2.Body != `{}` {
		t.Fatalf("fields changed: %+v", r2)
	}
}

// ---------- WithHeader: mapa compartido si ya existía (documenta comportamiento actual) ----------

func TestWithHeader_SharesMap_WhenAlreadyNonNil(t *testing.T) {
	// r1 crea el mapa; r2 añade sobre el mismo mapa interno (comportamiento actual).
	r1 := JSONOK(`{}`).WithHeader("A", "1")
	if r1.Headers == nil {
		t.Fatalf("precondition: r1.Headers not nil")
	}
	r2 := r1.WithHeader("B", "2")

	// Debido a que el método no clona el mapa cuando ya existe,
	// r1 también ve la nueva clave.
	if r1.Headers["B"] != "2" {
		t.Fatalf("expected shared map behavior; r1 missing B: %+v", r1.Headers)
	}
	// Y r2 por supuesto también la tiene.
	if r2.Headers["B"] != "2" {
		t.Fatalf("r2 missing B: %+v", r2.Headers)
	}
}
