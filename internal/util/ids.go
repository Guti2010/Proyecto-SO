package util

import (
	"crypto/rand"
	"encoding/hex"
)

// NewReqID genera un identificador corto (16 caracteres hex) para
// correlacionar peticiones en logs y respuestas.
func NewReqID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
