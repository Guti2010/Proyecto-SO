package test

import (
	"testing"

	"so-http10-demo/internal/resp"
)

func TestRespConstructors(t *testing.T) {
	if r := resp.PlainOK("x"); r.Status != 200 || r.JSON || r.Err != nil { t.Fatalf("PlainOK: %+v", r) }
	if r := resp.JSONOK(`{}`); r.Status != 200 || !r.JSON || r.Err != nil { t.Fatalf("JSONOK: %+v", r) }
	for _, f := range []func(string, string) resp.Result{
		resp.BadReq, resp.NotFound, resp.Conflict, resp.TooMany, resp.IntErr, resp.Unavail,
	} {
		r := f("c", "d")
		if !r.JSON || r.Err == nil || r.Err.Code != "c" || r.Err.Detail != "d" { t.Fatalf("ctor: %+v", r) }
	}
}
