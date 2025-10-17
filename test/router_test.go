package test

import (
	"encoding/json"
	"strings"
	"testing"

	"so-http10-demo/internal/router"
)

func TestRouterAllBasics(t *testing.T) {
	cases := []struct{ target string; json bool }{
		{"/", false}, {"/help", false}, {"/status", true}, {"/timestamp", true},
		{"/reverse?text=ab", false}, {"/toupper?text=ab", false},
		{"/hash?text=x", true}, {"/random?count=2&min=5&max=5", true},
		{"/fibonacci?num=7", false},
	}
	for _, tc := range cases {
		r := router.Dispatch("GET", tc.target)
		if r.Status != 200 || r.JSON != tc.json { t.Fatalf("%s -> %+v", tc.target, r) }
		if strings.HasPrefix(tc.target, "/toupper") && r.Body != "AB\n" { t.Fatalf("toupper: %q", r.Body) }
		if strings.HasPrefix(tc.target, "/reverse") && strings.TrimSpace(r.Body) != "ba" { t.Fatalf("reverse: %q", r.Body) }
	}
	if r := router.Dispatch("GET", "/nope"); r.Status != 404 || r.Err == nil { t.Fatalf("404: %+v", r) }

	// valida que los JSON sean parseables donde aplica
	for _, tcase := range []string{"/status", "/timestamp", "/hash?text=x", "/random?count=1&min=0&max=0"} {
		r := router.Dispatch("GET", tcase)
		if err := json.Unmarshal([]byte(r.Body), &map[string]any{}); err != nil { t.Fatalf("%s json: %v", tcase, err) }
	}
}

func TestRouter_FileRoutes(t *testing.T) {
	// create
	r := router.Dispatch("GET", "/createfile?name=rtr.txt&content=Z&repeat=2")
	if r.Status != 200 || r.JSON { t.Fatalf("router create: %+v", r) }
	// delete
	r = router.Dispatch("GET", "/deletefile?name=rtr.txt")
	if r.Status != 200 || r.JSON { t.Fatalf("router delete: %+v", r) }
}
