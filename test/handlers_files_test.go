package test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"so-http10-demo/internal/handlers"
)

func TestCreateDeleteFilesVariants(t *testing.T) {
	if r := handlers.CreateFile(map[string]string{"name": "../x"}); r.Status != 400 {
		t.Fatalf("want 400, got %+v", r)
	}
	if r := handlers.DeleteFile(map[string]string{"name": "does-not-exist.txt"}); r.Status != 404 {
		t.Fatalf("want 404, got %+v", r)
	}

	name := "repeat1.txt"
	r := handlers.CreateFile(map[string]string{"name": name, "content": "Z"})
	if r.Status != 200 {
		t.Fatalf("create: %+v", r)
	}
	path := filepath.Join("/app/data", name)
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "Z" {
		t.Fatalf("rb: %v %q", err, string(b))
	}
	if r := handlers.DeleteFile(map[string]string{"name": name}); r.Status != 200 {
		t.Fatalf("del: %+v", r)
	}
}

func TestCreateFile_RepeatAndSanitize(t *testing.T) {
	// nombre con separador → 400
	if r := handlers.CreateFile(map[string]string{"name": "a/b.txt"}); r.Status != 400 {
		t.Fatalf("expect 400 for name with slash, got %+v", r)
	}
	// repeat explícito
	name := "repeat3.txt"
	r := handlers.CreateFile(map[string]string{"name": name, "content": "X", "repeat": "3"})
	if r.Status != 200 {
		t.Fatalf("create: %+v", r)
	}
	path := filepath.Join("/app/data", name)
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "XXX" {
		t.Fatalf("repeat=3 readback: %v %q", err, string(b))
	}
	if r := handlers.DeleteFile(map[string]string{"name": name}); r.Status != 200 {
		t.Fatalf("delete repeat3: %+v", r)
	}
}

func TestSanitize_EmptyAndBackslashThroughAPI(t *testing.T) {
	// nombre vacío -> 400 (sanitize false)
	if r := handlers.CreateFile(map[string]string{"name": "", "content": "x"}); r.Status != 400 {
		t.Fatalf("empty name should be 400, got %+v", r)
	}
	// nombre con backslash -> 400 (sanitize false)
	if r := handlers.CreateFile(map[string]string{"name": `a\b.txt`, "content": "x"}); r.Status != 400 {
		t.Fatalf(`backslash name should be 400, got %+v`, r)
	}
}

func TestCreateFile_FailsWhenNameCollidesWithDir(t *testing.T) {
	// prepara un directorio /app/data/dirfile
	dirName := "dirfile"
	if err := os.MkdirAll(filepath.Join("/app/data", dirName), 0o755); err != nil {
		t.Fatalf("prep dir: %v", err)
	}
	// intentar crear archivo con el mismo "name" debe fallar con 500
	if r := handlers.CreateFile(map[string]string{"name": dirName, "content": "x"}); r.Status != 500 {
		t.Fatalf("expect 500 when name collides with directory, got %+v", r)
	}
	// limpieza
	_ = os.RemoveAll(filepath.Join("/app/data", dirName))
}

func TestDeleteFile_ErrorWhenDirNotEmpty(t *testing.T) {
	// crea un directorio con contenido: os.Remove devolverá error != IsNotExist
	base := "nonemptydir"
	full := filepath.Join("/app/data", base)
	if err := os.MkdirAll(filepath.Join(full, "child"), 0o755); err != nil {
		t.Fatalf("prep nonempty dir: %v", err)
	}
	// esto debe devolver 500 (fs_error)
	if r := handlers.DeleteFile(map[string]string{"name": base}); r.Status != 500 {
		t.Fatalf("expect 500 for non-empty dir removal, got %+v", r)
	}
	// cleanup
	_ = os.RemoveAll(full)
}

func TestCreateFile_RepeatNegativeOrInvalid_ToDefaultOne(t *testing.T) {
	// repeat negativo -> fuerza rama rep<=0
	name := "negrepeat.txt"
	r := handlers.CreateFile(map[string]string{"name": name, "content": "K", "repeat": "-5"})
	if r.Status != 200 {
		t.Fatalf("create neg repeat: %+v", r)
	}
	b, err := os.ReadFile(filepath.Join("/app/data", name))
	if err != nil || string(b) != "K" {
		t.Fatalf("expect single write: %v %q", err, string(b))
	}
	_ = handlers.DeleteFile(map[string]string{"name": name})

	// repeat inválido (no numérico) -> Atoi=0 -> rep<=0 -> default 1
	name = "badrepeat.txt"
	r = handlers.CreateFile(map[string]string{"name": name, "content": "Q", "repeat": "NaN"})
	if r.Status != 200 {
		t.Fatalf("create bad repeat: %+v", r)
	}
	b, err = os.ReadFile(filepath.Join("/app/data", name))
	if err != nil || string(b) != "Q" {
		t.Fatalf("expect single write NaN: %v %q", err, string(b))
	}
	_ = handlers.DeleteFile(map[string]string{"name": name})
}

func TestDeleteFile_InvalidNames_Return400(t *testing.T) {
	if r := handlers.DeleteFile(map[string]string{"name": "../x"}); r.Status != 400 {
		t.Fatalf("delete ../x -> 400, got %+v", r)
	}
	if r := handlers.DeleteFile(map[string]string{"name": `a\b.txt`}); r.Status != 400 {
		t.Fatalf(`delete a\b.txt -> 400, got %+v`, r)
	}
}

func TestCreateFile_ForcedWriteError(t *testing.T) {
	// guarda y restaura el writer real
	real := handlers.WriteRepeat
	defer func() { handlers.WriteRepeat = real }()

	// forzamos error en la primera escritura
	handlers.WriteRepeat = func(_ *os.File, _ string) error { return errors.New("boom") }

	r := handlers.CreateFile(map[string]string{"name": "failwrite.txt", "content": "X", "repeat": "2"})
	if r.Status != 500 {
		t.Fatalf("expected 500 on forced write error, got %+v", r)
	}
}
