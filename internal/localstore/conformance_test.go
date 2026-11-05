package localstore

import (
	"path/filepath"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/core/draft/storetest"
)

// TestConformance runs the shared Store conformance suite against the file store. Every subtest
// gets a fresh, empty data root.
func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) draft.Store {
		s, err := Open(filepath.Join(t.TempDir(), "data"), Options{})
		if err != nil {
			t.Fatal(err)
		}
		s.logf = func(string, ...any) {}
		return s
	})
}
