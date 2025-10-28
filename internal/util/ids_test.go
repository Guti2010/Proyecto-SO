package util

import (
	"encoding/hex"
	"testing"
)

// helper: valida que solo use [0-9a-f]
func isHexLower(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func TestNewReqID_BasicProps(t *testing.T) {
	t.Parallel()

	id := NewReqID()
	if len(id) != 16 {
		t.Fatalf("len=%d, want 16 (8 bytes hex)", len(id))
	}
	if !isHexLower(id) {
		t.Fatalf("id must be lowercase hex [0-9a-f], got %q", id)
	}
	if id == "0000000000000000" {
		t.Fatalf("id should not be all zeros")
	}
}

func TestNewReqID_HexRoundtrip(t *testing.T) {
	t.Parallel()

	id := NewReqID()
	raw, err := hex.DecodeString(id)
	if err != nil {
		t.Fatalf("hex.DecodeString failed for %q: %v", id, err)
	}
	if len(raw) != 8 {
		t.Fatalf("decoded len=%d, want 8", len(raw))
	}
	// Encode canonical debe coincidir exactamente (hex.EncodeToString usa minúsculas)
	enc := hex.EncodeToString(raw)
	if enc != id {
		t.Fatalf("roundtrip mismatch: %q -> %x -> %q", id, raw, enc)
	}
}

func TestNewReqID_Uniqueness_Sample(t *testing.T) {
	t.Parallel()

	const n = 256 // tamaño razonable para test; colisión extremadamente improbable
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := NewReqID()
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate id generated: %q", id)
		}
		seen[id] = struct{}{}
	}
}

// Extra: dos llamadas consecutivas deben diferir casi siempre.
// Si alguna vez colisionara (ultra improbable), este test fallaría junto con el de unicidad.
func TestNewReqID_TwoCallsDiffer(t *testing.T) {
	t.Parallel()

	a := NewReqID()
	b := NewReqID()
	if a == b {
		t.Fatalf("two consecutive ids are equal: %q", a)
	}
}
