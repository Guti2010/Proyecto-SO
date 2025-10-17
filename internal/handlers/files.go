package handlers

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"so-http10-demo/internal/resp"
)

// dataDir es el directorio de trabajo para operaciones de archivo.
// Está montado desde el host en docker-compose.yml (./data:/app/data).
const dataDir = "/app/data"

// WriteRepeat es un seam (punto de inyección) para tests: envuelve f.WriteString.
// En producción usa la implementación por defecto; en tests puede reasignarse
// para forzar errores de escritura y cubrir esa rama.
var WriteRepeat = func(f *os.File, content string) error {
	_, err := f.WriteString(content)
	return err
}

// sanitize permite solo nombres simples de archivo: sin "../" ni separadores.
// Evita traversal y rutas absolutas.
func sanitize(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
		return "", false
	}
	return name, true
}

// CreateFile crea un archivo en dataDir y escribe 'content' 'repeat' veces.
// Respuestas:
//   200 -> "ok\n" (texto)
//   400 -> JSON {"error":"bad_name",...} si el nombre es inválido
//   500 -> JSON {"error":"fs_error",...} ante errores de E/S
func CreateFile(q map[string]string) resp.Result {
	name, ok := sanitize(q["name"])
	if !ok {
		return resp.BadReq("bad_name", "invalid file name")
	}
	content := q["content"]
	rep, _ := strconv.Atoi(q["repeat"])
	if rep <= 0 {
		rep = 1
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return resp.IntErr("fs_error", "cannot create data dir")
	}
	path := filepath.Join(dataDir, name)
	f, err := os.Create(path)
	if err != nil {
		return resp.IntErr("fs_error", "cannot create file")
	}
	defer f.Close()

	for i := 0; i < rep; i++ {
		if err := WriteRepeat(f, content); err != nil {
			return resp.IntErr("fs_error", "write failed")
		}
	}
	return resp.PlainOK("ok\n")
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
