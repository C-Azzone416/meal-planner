package localstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// testID returns a well-formed draft id derived from n.
func testID(n int) string {
	const hexd = "0123456789abcdef"
	b := []byte("drf_00000000000000000000")
	for i := 0; i < 4; i++ {
		b[len(b)-1-i] = hexd[(n>>(4*i))&0xf]
	}
	return string(b)
}

var ts = time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)

func intp(v int) *int { return &v }

// fullContent has every field populated, including zero-valued estimates, to prove nothing is lost.
func fullContent() draft.Content {
	est := &draft.Estimates{
		ActiveMinutes: &draft.MinutesEstimate{Value: intp(0), Basis: &draft.Basis{Kind: draft.BasisAgentEstimate}},
		TotalMinutes:  &draft.MinutesEstimate{Value: intp(90), Basis: &draft.Basis{Kind: draft.BasisSourced, SourceRef: "s1", Note: "stated by the page"}},
		Attention:     &draft.LevelEstimate{Value: draft.LevelLow, Basis: &draft.Basis{Kind: draft.BasisAgentEstimate}},
	}
	return draft.Content{
		Title:       "Synthetic Bean Stew <&> é世\U0001F372",
		Yield:       "Serves 4-6",
		Ingredients: []draft.Ingredient{{Text: "2 cups example beans", Quantity: "2", Unit: "cups", Name: "example beans", Note: "soaked"}, {Text: "salt, to taste"}},
		Instructions: []draft.Step{
			{Text: "Soak the beans.\nOvernight.", Phase: draft.PhasePrepAhead, PhaseBasis: &draft.Basis{Kind: draft.BasisAgentEstimate, Note: "unchecked"}},
			{Text: "Simmer."},
		},
		Notes:     "Synthetic example.",
		Sources:   []draft.Source{{ID: "s1", Kind: draft.SourceURL, Label: "Example page", URL: "https://example.com/r/1", Locator: "top"}},
		Estimates: est,
		PhaseEstimates: &draft.PhaseEstimates{
			CookTonight: &draft.Estimates{TotalMinutes: &draft.MinutesEstimate{Value: intp(60), Basis: &draft.Basis{Kind: draft.BasisAgentEstimate}}},
		},
		Guidance: []draft.Guidance{{Topic: draft.TopicStorage, Text: "Keeps a few days.", Basis: &draft.Basis{Kind: draft.BasisAgentEstimate}}},
	}
}

func mkDraft(id string, rev int64, c draft.Content) draft.Draft {
	return draft.Draft{
		SchemaVersion: draft.SchemaVersion, ID: id, Revision: rev, Status: draft.StatusDraft,
		CreatedAt: ts, UpdatedAt: ts.Add(time.Duration(rev-1) * time.Minute), Content: c,
	}
}

func minimal(title string) draft.Content {
	return draft.Content{Title: title, Ingredients: []draft.Ingredient{}, Instructions: []draft.Step{}, Sources: []draft.Source{}, Guidance: []draft.Guidance{}}
}

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	s, err := Open(root, Options{LockTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	s.logf = func(string, ...any) {}
	return s, root
}

func ctxb() context.Context { return context.Background() }

// wantErr asserts err is a *draft.Error with the given category and code, and that its message
// leaks neither the data root nor any of the given secrets.
func wantErr(t *testing.T, err error, cat draft.Category, code string, root string, secrets ...string) *draft.Error {
	t.Helper()
	var de *draft.Error
	if !errors.As(err, &de) {
		t.Fatalf("err = %v (%T), want *draft.Error %s/%s", err, err, cat, code)
	}
	if de.Category != cat || de.Code != code {
		t.Fatalf("err = %s/%s (%s), want %s/%s", de.Category, de.Code, de.Message, cat, code)
	}
	for _, s := range append([]string{root}, secrets...) {
		if s != "" && strings.Contains(de.Message, s) {
			t.Errorf("message %q leaks %q", de.Message, s)
		}
	}
	return de
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// snapshot maps every path under dir to its contents (or a marker for directories).
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if fi.IsDir() {
			out[rel] = "<dir>"
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func equalSnapshots(t *testing.T, a, b map[string]string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("snapshots differ in size: %d vs %d", len(a), len(b))
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			t.Fatalf("snapshots differ at %q", k)
		}
	}
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission checks do not apply")
	}
}
