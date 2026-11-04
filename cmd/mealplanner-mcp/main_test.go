package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/version"
)

func TestVersion(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"--version"}, strings.NewReader(""), &out); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if want := "mealplanner-mcp " + version.Version + "\n"; out.String() != want {
		t.Fatalf("out = %q, want %q", out.String(), want)
	}
}

func TestStdioEOFIsProtocolClean(t *testing.T) {
	var out bytes.Buffer
	if code := run(nil, strings.NewReader("ignored\n"), &out); code != 0 || out.Len() != 0 {
		t.Fatalf("code=%d out=%q", code, out.String())
	}
}
