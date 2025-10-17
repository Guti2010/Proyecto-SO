package handlers

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strconv"
	"time"

	"so-http10-demo/internal/resp"
)

// Reusamos sanitize(name) del package handlers (definida en files.go).
// Reusamos dataDir="/app/data" (definida en files.go).

// ---------- /wordcount ----------
// Cuenta líneas, palabras (tokens separados por espacios en blanco) y bytes.
func WordCountJSON(params map[string]string) resp.Result {
	name := params["name"]
	if name == "" {
		return resp.BadReq("name", "file name required")
	}
	path, ok := sanitize(name)
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}

	f, err := os.Open(dataDir + "/" + path)
	if err != nil {
		if os.IsNotExist(err) {
			return resp.NotFound("not_found", "file does not exist")
		}
		return resp.IntErr("fs_error", "open failed")
	}
	defer f.Close()

	start := time.Now()
	var lines, words, bytes int64

	sc := bufio.NewScanner(f)
	// default token size ~64K por línea; si esperas líneas enormes, ajusta el buffer:
	// buf := make([]byte, 0, 1024*1024); sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		lines++
		b := sc.Bytes()
		bytes += int64(len(b) + 1) // +1 por '\n' que Scanner no incluye
		inWord := false
		for _, c := range b {
			if c > ' ' {
				if !inWord {
					words++
					inWord = true
				}
			} else {
				inWord = false
			}
		}
	}
	if err := sc.Err(); err != nil {
		return resp.IntErr("fs_error", "scan error")
	}

	out := map[string]any{
		"file":       path,
		"lines":      lines,
		"words":      words,
		"bytes":      bytes,
		"elapsed_ms": time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// ---------- /grep ----------
// Cuenta coincidencias y devuelve las primeras 10 líneas que matchean.
func GrepJSON(params map[string]string) resp.Result {
	name := params["name"]
	pat := params["pattern"]
	if name == "" || pat == "" {
		return resp.BadReq("params", "name and pattern required")
	}
	path, ok := sanitize(name)
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return resp.BadReq("pattern", "invalid regex")
	}

	f, err := os.Open(dataDir + "/" + path)
	if err != nil {
		if os.IsNotExist(err) {
			return resp.NotFound("not_found", "file does not exist")
		}
		return resp.IntErr("fs_error", "open failed")
	}
	defer f.Close()

	start := time.Now()
	sc := bufio.NewScanner(f)
	matches := 0
	first := make([]string, 0, 10)
	for sc.Scan() {
		line := sc.Text()
		if re.MatchString(line) {
			matches++
			if len(first) < 10 {
				first = append(first, line)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return resp.IntErr("fs_error", "scan error")
	}
	out := map[string]any{
		"file":       path,
		"pattern":    pat,
		"matches":    matches,
		"first":      first,
		"elapsed_ms": time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// ---------- /hashfile ----------
// SHA-256 en streaming del archivo.
func HashFileJSON(params map[string]string) resp.Result {
	name := params["name"]
	algo := params["algo"]
	if algo == "" {
		algo = "sha256"
	}
	if algo != "sha256" {
		return resp.BadReq("algo", "only sha256 is supported for now")
	}
	if name == "" {
		return resp.BadReq("name", "file name required")
	}
	path, ok := sanitize(name)
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}

	f, err := os.Open(dataDir + "/" + path)
	if err != nil {
		if os.IsNotExist(err) {
			return resp.NotFound("not_found", "file does not exist")
		}
		return resp.IntErr("fs_error", "open failed")
	}
	defer f.Close()

	start := time.Now()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return resp.IntErr("fs_error", "read error")
	}
	out := map[string]any{
		"file":       path,
		"algo":       "sha256",
		"hex":        hex.EncodeToString(h.Sum(nil)),
		"elapsed_ms": time.Since(start).Milliseconds(),
	}
	b, _ := json.Marshal(out)
	return resp.JSONOK(string(b))
}

// atoi utilidad local (tolerante a error).
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
