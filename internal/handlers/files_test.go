package handlers

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"strconv"
)

// ---------- helpers ----------

func mustUnmarshal[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("json.Unmarshal: %v\ninput=%q", err, s)
	}
	return v
}

func uniqueName(prefix string) string {
	// nombre simple (pasa sanitize)
	return prefix + "_" + time.Now().UTC().Format("150405.000000000") + ".txt"
}

// Limpia si existe (para no dejar basura entre corridas)
func cleanup(path string) { _ = os.Remove(path) }

// ---------- unit tests ----------

func TestSanitize(t *testing.T) {
	t.Parallel()
	if _, ok := sanitize(""); ok {
		t.Fatalf("empty name should be invalid")
	}
	if _, ok := sanitize("../x"); ok {
		t.Fatalf(".. should be invalid")
	}
	if _, ok := sanitize("a/b"); ok {
		t.Fatalf("slash should be invalid")
	}
	if _, ok := sanitize(`a\b`); ok {
		t.Fatalf("backslash should be invalid")
	}
	if s, ok := sanitize("ok_name.txt"); !ok || s != "ok_name.txt" {
		t.Fatalf("valid name rejected: %q %v", s, ok)
	}
}

func TestJsonNoEscape(t *testing.T) {
	t.Parallel()
	out := map[string]any{"url": "/x?a=1&b=2"}
	s := jsonNoEscape(out)
	if strings.Contains(s, `\u0026`) {
		t.Fatalf("jsonNoEscape should not escape '&': %q", s)
	}
	if !strings.Contains(s, "&b=2") {
		t.Fatalf("missing & in output: %q", s)
	}
}

func TestCreateFile_FailIfExists_ShowsHints(t *testing.T) {
	// Prepara archivo existente
	name := uniqueName("demo")
	full := filepath.Join(dataDir, name)
	defer cleanup(full)

	r0 := CreateFile(map[string]string{
		"name":    name,
		"content": "hello",
		"repeat":  "1",
		"conflict": "overwrite", // crear sí o sí
	})
	if r0.Status != 200 {
		t.Fatalf("setup create: %+v", r0)
	}

	// Sin conflict => fail (default). Debe 409 + hints sin \u0026
	r := CreateFile(map[string]string{
		"name":    name,
		"content": "ignored",
		"repeat":  "2",
	})
	if r.Status != 409 || !r.JSON {
		t.Fatalf("expected 409 JSON, got: %+v", r)
	}
	if strings.Contains(r.Body, `\u0026`) {
		t.Fatalf("hints must not be escaped: %q", r.Body)
	}
	if !strings.Contains(r.Body, "suggested_name") {
		t.Fatalf("missing suggested_name in body: %q", r.Body)
	}
}

func TestCreateFile_Autorename_WhenExists(t *testing.T) {
	// Deja existente y luego autorename
	name := uniqueName("demo")
	full0 := filepath.Join(dataDir, name)
	defer cleanup(full0)

	// Crea el original
	r0 := CreateFile(map[string]string{
		"name":     name,
		"content":  "x",
		"repeat":   "1",
		"conflict": "overwrite",
	})
	if r0.Status != 200 {
		t.Fatalf("setup create: %+v", r0)
	}

	// Pide autorename
	r := CreateFile(map[string]string{
		"name":     name,
		"content":  "x",
		"repeat":   "3",
		"conflict": "autorename",
	})
	if r.Status != 200 || !r.JSON {
		t.Fatalf("autorename create: %+v", r)
	}

	type out struct {
		File        string `json:"file"`
		Action      string `json:"action"`
		Policy      string `json:"policy"`
		RenamedFrom string `json:"renamed_from"`
		Bytes       int64  `json:"bytes"`
	}
	o := mustUnmarshal[out](t, r.Body)
	if o.Action != "autorename" || o.Policy != "autorename" || o.RenamedFrom != name {
		t.Fatalf("payload mismatch: %+v", o)
	}
	if o.File == name || !strings.Contains(o.File, "(") {
		t.Fatalf("autorename didn't change name: %q", o.File)
	}
	// bytes = repeat * (len(content)+1) → 3*(1+1)=6
	if o.Bytes != 6 {
		t.Fatalf("bytes mismatch: %d", o.Bytes)
	}
	// limpia el renombrado también
	cleanup(filepath.Join(dataDir, o.File))
}

func TestCreateFile_Overwrite_ComputesBytes(t *testing.T) {
	name := uniqueName("overwrite")
	full := filepath.Join(dataDir, name)
	defer cleanup(full)

	// crea primero
	_ = CreateFile(map[string]string{
		"name":     name,
		"content":  "abc",
		"repeat":   "2",
		"conflict": "overwrite",
	})
	// sobrescribe con contenido diferente y repeat=4
	r := CreateFile(map[string]string{
		"name":     name,
		"content":  "ZZ",
		"repeat":   "4",
		"conflict": "overwrite",
	})
	type out struct {
		Action string `json:"action"`
		Policy string `json:"policy"`
		Bytes  int64  `json:"bytes"`
	}
	o := mustUnmarshal[out](t, r.Body)
	if o.Action != "overwritten" || o.Policy != "overwrite" {
		t.Fatalf("bad action/policy: %+v", o)
	}
	// bytes = 4 * (len("ZZ")+1) = 4*3=12
	if o.Bytes != 12 {
		t.Fatalf("bytes=%d want=12", o.Bytes)
	}
}

func TestCreateFile_Validations_And_WriteError(t *testing.T) {
	// repeat inválido
	if r := CreateFile(map[string]string{"name": "x.txt", "repeat": "0"}); r.Status != 400 {
		t.Fatalf("repeat=0 should 400: %+v", r)
	}
	if r := CreateFile(map[string]string{"name": "x.txt", "repeat": "NaN"}); r.Status != 400 {
		t.Fatalf("repeat NaN should 400: %+v", r)
	}
	// bad name
	if r := CreateFile(map[string]string{"name": "../x"}); r.Status != 400 {
		t.Fatalf("bad name should 400: %+v", r)
	}

	// Fuerza error de escritura con el seam WriteRepeat
	name := uniqueName("failwrite")
	full := filepath.Join(dataDir, name)
	defer cleanup(full)

	prev := WriteRepeat
	defer func() { WriteRepeat = prev }()
	failOnce := true
	WriteRepeat = func(f *os.File, content string) error {
		// fallar en el primer Write (el del contenido, no el "\n")
		if failOnce && content != "\n" {
			failOnce = false
			return errors.New("boom")
		}
		_, err := f.WriteString(content)
		return err
	}

	r := CreateFile(map[string]string{
		"name":     name,
		"content":  "payload",
		"repeat":   "1",
		"conflict": "overwrite",
	})
	if r.Status != 500 || r.Err == nil || r.Err.Code != "fs_error" {
		t.Fatalf("expected write-failed 500, got: %+v", r)
	}
}

func TestDeleteFile_OK_And_NotFound(t *testing.T) {
	// crea
	name := uniqueName("todel")
	full := filepath.Join(dataDir, name)
	defer cleanup(full)

	cr := CreateFile(map[string]string{
		"name":     name,
		"content":  "bye",
		"repeat":   "1",
		"conflict": "overwrite",
	})
	if cr.Status != 200 {
		t.Fatalf("setup create: %+v", cr)
	}

	// delete ok
	dr := DeleteFile(map[string]string{"name": name})
	if dr.Status != 200 || dr.Body != "deleted\n" {
		t.Fatalf("delete ok: %+v", dr)
	}
	// delete not found
	dr2 := DeleteFile(map[string]string{"name": name})
	if dr2.Status != 404 || dr2.Err == nil || dr2.Err.Code != "not_found" {
		t.Fatalf("delete not found: %+v", dr2)
	}
}

func TestNameHelpers_FirstAvailable_And_Fallback(t *testing.T) {
	// crea base y (1) para forzar que sugiera (2)
	base := uniqueName("namehelper")
	ext := filepath.Ext(base)
	noext := strings.TrimSuffix(base, ext)

	full0 := filepath.Join(dataDir, base)
	full1 := filepath.Join(dataDir, noext+"(1)"+ext)
	defer cleanup(full0)
	defer cleanup(full1)

	_ = CreateFile(map[string]string{"name": base, "conflict": "overwrite"})
	_ = CreateFile(map[string]string{"name": noext + "(1)" + ext, "conflict": "overwrite"})

	sug := firstAvailableByRules(base)
	if !strings.HasSuffix(sug, "(2)"+ext) {
		t.Fatalf("expected (2) suggestion, got %q", sug)
	}

	// fallbackName se usa solo como última red; probamos su formato
	if fb := fallbackName("demo.txt"); fb != "demo_copy.txt" {
		t.Fatalf("fallbackName: %q", fb)
	}
}

func TestDeleteFile_BadName(t *testing.T) {
	t.Parallel()
	r := DeleteFile(map[string]string{"name": "../evil"})
	if r.Status != 400 || r.Err == nil || r.Err.Code != "bad_name" {
		t.Fatalf("bad_name expected, got: %+v", r)
	}
}

func TestDeleteFile_FsError_NonEmptyDir(t *testing.T) {
	// Creamos un directorio no vacío con el mismo nombre que intentaremos borrar.
	dir := uniqueName("nonemptydir")
	dirPath := filepath.Join(dataDir, dir)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer func() {
		// Limpieza: borra el archivo interno y luego el directorio
		_ = os.Remove(filepath.Join(dirPath, "inside.txt"))
		_ = os.Remove(dirPath)
	}()

	f, err := os.Create(filepath.Join(dirPath, "inside.txt"))
	if err != nil {
		t.Fatalf("create inner: %v", err)
	}
	_ = f.Close()

	// Al intentar borrar el "archivo" con ese nombre, en realidad es un dir no vacío:
	// os.Remove() falla y el handler debe responder fs_error 500.
	r := DeleteFile(map[string]string{"name": dir})
	if r.Status != 500 || r.Err == nil || r.Err.Code != "fs_error" {
		t.Fatalf("expected fs_error 500, got: %+v", r)
	}
}

func TestFirstAvailableAppendCounter_NoExt_IncrementsChain(t *testing.T) {
	_ = os.MkdirAll(dataDir, 0o755)

	// Nombre sin NINGÚN punto (no extensión)
	baseNoExt := "plain_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_noext"

	// Ocupamos base y (1), (2) para que sugiera (3)
	names := []string{
		baseNoExt,           // base sin extensión
		baseNoExt + "(1)",   // ya ocupado
		baseNoExt + "(2)",   // ya ocupado
	}
	for _, n := range names {
		_ = CreateFile(map[string]string{"name": n, "conflict": "overwrite"})
		defer os.Remove(filepath.Join(dataDir, n))
	}

	got := firstAvailableAppendCounter(baseNoExt)
	want := baseNoExt + "(3)"
	if got != want {
		t.Fatalf("want %q got %q", want, got)
	}
}

func TestFirstAvailableAppendCounter_WithExt_SkipsToNextFree(t *testing.T) {
	_ = os.MkdirAll(dataDir, 0o755)

	// Base con extensión
	base := uniqueName("extdemo") // termina en ".txt"
	ext := filepath.Ext(base)
	noext := strings.TrimSuffix(base, ext)

	// Ocupa base y (1)..(5) para que sugiera (6)
	for k := -1; k <= 5; k++ {
		var name string
		if k < 0 {
			name = base
		} else {
			name = noext + "(" + strconv.Itoa(k) + ")" + ext
		}
		_ = CreateFile(map[string]string{"name": name, "conflict": "overwrite"})
		defer os.Remove(filepath.Join(dataDir, name))
	}

	got := firstAvailableAppendCounter(base)
	want := noext + "(6)" + ext
	if got != want {
		t.Fatalf("want %q got %q", want, got)
	}
}

func TestFirstAvailableAppendCounter_BaseAlreadyHasParens(t *testing.T) {
	_ = os.MkdirAll(dataDir, 0o755)

	// La regla es "append (k) siempre", sin tocar paréntesis existentes.
	// base: foo(4).txt -> espera foo(4)(2).txt si foo(4).txt y foo(4)(1).txt existen
	base := "foo(4).txt"
	full0 := filepath.Join(dataDir, base)
	full1 := filepath.Join(dataDir, "foo(4)(1).txt")
	_ = CreateFile(map[string]string{"name": base, "conflict": "overwrite"})
	_ = CreateFile(map[string]string{"name": "foo(4)(1).txt", "conflict": "overwrite"})
	defer os.Remove(full0)
	defer os.Remove(full1)

	got := firstAvailableAppendCounter(base)
	if got != "foo(4)(2).txt" {
		t.Fatalf("want %q got %q", "foo(4)(2).txt", got)
	}
}
