package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/version"
)

func noEnv(string) string { return "" }

func TestVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"--version"}, noEnv, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if want := "mealplanner-mcp " + version.Version + "\n"; out.String() != want {
		t.Fatalf("out = %q, want %q", out.String(), want)
	}
	if errb.Len() != 0 {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestBadInvocationExits2BeforeProtocol(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown flag":  {"--nope"},
		"positional":    {"extra"},
		"relative dir":  {"--data-dir", "relative/path"},
		"empty dir":     {"--data-dir", ""},
		"tilde dir":     {"--data-dir", "~/drafts"},
		"missing value": {"--data-dir"},
	} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), args, noEnv, strings.NewReader(""), &out, &errb); code != 2 {
			t.Errorf("%s: code = %d, want 2 (stderr %q)", name, code, errb.String())
		}
		if out.Len() != 0 {
			t.Errorf("%s: stdout = %q, want empty", name, out.String())
		}
	}
}

func TestEOFWithNoRequestsExitsCleanly(t *testing.T) {
	var out, errb bytes.Buffer
	args := []string{"--data-dir", t.TempDir()}
	if code := run(context.Background(), args, noEnv, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("code = %d, stderr %q", code, errb.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}
