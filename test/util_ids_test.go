package test

import (
	"testing"

	"so-http10-demo/internal/util"
)

func TestNewReqID(t *testing.T) {
	id1, id2 := util.NewReqID(), util.NewReqID()
	if len(id1) != 16 || len(id2) != 16 || id1 == id2 { t.Fatalf("ids: %q %q", id1, id2) }
}
