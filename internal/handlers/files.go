package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"so-http10-demo/internal/resp"
)

// dataDir es el directorio de trabajo para operaciones de archivo.
// Montado desde docker-compose.yml (./data:/app/data).
const dataDir = "/app/data"

// WriteRepeat es un seam para tests: envuelve f.WriteString.
// En producción usa esta implementación; en tests puedes reasignarlo.
var WriteRepeat = func(f *os.File, content string) error {
	_, err := f.WriteString(content)
	return err
}

// sanitize permite solo nombres simples de archivo (sin "../", "/" o "\").
func sanitize(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
		return "", false
	}
	return name, true
}

// jsonNoEscape serializa sin escapar &, <, >
func jsonNoEscape(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	// json.Encoder agrega '\n' al final
	return strings.TrimRight(buf.String(), "\n")
}

/*
CreateFile crea un archivo en dataDir con control de conflictos.

Parámetros:
  - name=FILE           (obligatorio; pasa por sanitize)
  - content=TEXT        (opcional; default "")
  - repeat=N            (opcional; default 1; N>=1)
  - conflict=fail|overwrite|autorename  (opcional; default fail)

Comportamiento:
  - fail (default): si existe → 409 con suggested_name y hints.
  - overwrite: trunca/crea con ese nombre.
  - autorename:
      * regla única: siempre probar base + "(k)" con k=1..∞ (sin anidar más "(1)" sobre lo ya existente).
        Ejemplos:
          demo.txt      -> demo(1).txt, demo(2).txt, ...
          demo(4).txt   -> demo(4)(1).txt, demo(4)(2).txt, ...
          demo(1).txt   -> demo(1)(1).txt, demo(1)(2).txt, ...
*/
func CreateFile(q map[string]string) resp.Result {
	rawName := q["name"]
	name, ok := sanitize(rawName)
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}
	content := q["content"]
	rep := 1
	if v := q["repeat"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return resp.BadReq("repeat", "repeat must be integer >= 1")
		}
		rep = n
	}
	mode := q["conflict"]
	if mode == "" {
		mode = "fail"
	}
	if mode != "fail" && mode != "overwrite" && mode != "autorename" {
		return resp.BadReq("conflict", "use conflict=fail|overwrite|autorename")
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return resp.IntErr("fs_error", "cannot create data dir")
	}

	dst := filepath.Join(dataDir, name)
	start := time.Now()
	action := "created"
	renamedFrom := ""

	// ¿Existe?
	if _, err := os.Stat(dst); err == nil {
		switch mode {
		case "fail":
			sug := suggestNameWithRules(name)
			out := map[string]any{
				"error":              "exists",
				"detail":             "file already exists",
				"file":               name,
				"suggested_name":     sug,
				"how_to_overwrite":   fmt.Sprintf("/createfile?name=%s&content=...&repeat=%d&conflict=overwrite", url.QueryEscape(name), rep),
				"how_to_autorename":  fmt.Sprintf("/createfile?name=%s&content=...&repeat=%d&conflict=autorename", url.QueryEscape(name), rep),
				"how_to_use_other_name": "/createfile?name=<otro_nombre>&content=...&repeat=N",
			}
			// usar jsonNoEscape para que no aparezca \u0026 en los hints
			body := jsonNoEscape(out)
			return resp.Result{Status: 409, Body: body, JSON: true}

		case "autorename":
			renamedFrom = name
			name = firstAvailableByRules(name)
			dst = filepath.Join(dataDir, name)
			action = "autorename"

		case "overwrite":
			action = "overwritten"
		}
	}

	// Crear/truncar y escribir
	f, err := os.Create(dst)
	if err != nil {
		return resp.IntErr("fs_error", "cannot create file")
	}
	defer f.Close()

	var written int64
	for i := 0; i < rep; i++ {
		if err := WriteRepeat(f, content); err != nil {
			return resp.IntErr("fs_error", "write failed")
		}
		written += int64(len(content))
		if err := WriteRepeat(f, "\n"); err != nil {
			return resp.IntErr("fs_error", "write failed")
		}
		written += 1
	}

	out := map[string]any{
        "file":       name,
        "action":     action,  // created | overwritten | autorename
        "bytes":      written,
        "elapsed_ms": time.Since(start).Milliseconds(),
    }
    // Sólo muestra policy si NO es el default "fail"
    if mode != "fail" {
        out["policy"] = mode // overwrite | autorename
    }
    if action == "autorename" && renamedFrom != "" {
        out["renamed_from"] = renamedFrom
    }

    b, _ := json.Marshal(out)
    return resp.JSONOK(string(b))
}

// DeleteFile elimina un archivo en dataDir.
func DeleteFile(q map[string]string) resp.Result {
	name, ok := sanitize(q["name"])
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}
	path := filepath.Join(dataDir, name)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return resp.NotFound("not_found", "file does not exist")
		}
		return resp.IntErr("fs_error", "cannot delete file")
	}
	return resp.PlainOK("deleted\n")
}

// ---------- Helpers de nombres ----------

// Regla única para autorename (siempre "(k)" creciente sin anidar más niveles en la última parte):
//   demo.txt      -> demo(1).txt, demo(2).txt, ...
//   demo(4).txt   -> demo(4)(1).txt, demo(4)(2).txt, ...
//   demo(1).txt   -> demo(1)(1).txt, demo(1)(2).txt, ...
func firstAvailableByRules(base string) string {
	return firstAvailableAppendCounter(base)
}

func suggestNameWithRules(base string) string {
	return firstAvailableAppendCounter(base)
}

func firstAvailableAppendCounter(base string) string {
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	for k := 1; k < 1_000_000; k++ {
		cand := fmt.Sprintf("%s(%d)%s", name, k, ext)
		if _, err := os.Stat(filepath.Join(dataDir, cand)); os.IsNotExist(err) {
			return cand
		}
	}
	return fallbackName(base)
}

func fallbackName(base string) string {
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return name + "_copy" + ext
}

