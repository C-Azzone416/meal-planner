package draft_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// decodeFixture decodes a testdata file and fails the test if it does not decode.
func decodeFixture(t *testing.T, name string) draft.Content {
	t.Helper()
	c, issues := draft.DecodeContent(readFixture(t, name))
	if len(issues) != 0 {
		t.Fatalf("%s did not decode: %+v", name, issues)
	}
	return c
}

// codes renders issues as "code path" strings for comparison.
func codes(issues []draft.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Code+" "+i.Path)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func intp(v int) *int { return &v }

func agent() *draft.Basis { return &draft.Basis{Kind: draft.BasisAgentEstimate} }

func runes(n int, r string) string { return strings.Repeat(r, n) }
