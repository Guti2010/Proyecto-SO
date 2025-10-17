package test

import (
	"testing"

	"so-http10-demo/internal/http10"
)

func TestSplitTarget_Basic(t *testing.T) {
	p, q := http10.SplitTarget("/p?a=1&b=2")
	if p != "/p" || q != "a=1&b=2" {
		t.Fatalf("split: %q %q", p, q)
	}
}

func TestParseQuery_Basic(t *testing.T) {
	m := http10.ParseQuery("a=1&b=2")
	if m["a"] != "1" || m["b"] != "2" {
		t.Fatalf("basic: %#v", m)
	}
}

func TestParseQuery_EmptyString(t *testing.T) {
	m := http10.ParseQuery("")
	if len(m) != 0 {
		t.Fatalf("expected empty, got %#v", m)
	}
}

func TestParseQuery_EmptyPairs(t *testing.T) {
	m := http10.ParseQuery("&&a=1&&b=2&")
	if len(m) != 2 || m["a"] != "1" || m["b"] != "2" {
		t.Fatalf("empty pairs: %#v", m)
	}
}

func TestParseQuery_KeyWithoutEquals_AndValueWithEquals(t *testing.T) {
	m := http10.ParseQuery("flag&x=1&y=")
	if m["flag"] != "" || m["x"] != "1" || m["y"] != "" {
		t.Fatalf("flag & empty value: %#v", m)
	}
	m = http10.ParseQuery("tok=a=b=c&flag")
	if m["tok"] != "a=b=c" || m["flag"] != "" {
		t.Fatalf("value with '=': %#v", m)
	}
}
