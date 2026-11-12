package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// TestShowAndListStoredEnvelope: a stored file with a bad revision or timestamp is exit 3 on show,
// skipped by list, and left byte for byte; the matching valid files read normally.
func TestShowAndListStoredEnvelope(t *testing.T) {
	const ok = `"2026-11-04T09:30:00Z"`
	future := `"` + time.Now().UTC().Add(2*draft.MaxFutureSkew).Format("2006-01-02T15:04:05Z") + `"`
	for _, tc := range []struct {
		name                  string
		rev, created, updated string // an empty created or updated omits the member
		valid                 bool
	}{
		{"baseline", "1", ok, ok, true},
		{"revision 2^53-1", "9007199254740991", ok, ok, true},
		{"updated after created", "3", ok, `"2026-11-05T00:00:00Z"`, true},

		{"created_at missing", "1", "", ok, false},
		{"created_at null", "1", "null", ok, false},
		{"created_at zero", "1", `"0001-01-01T00:00:00Z"`, ok, false},
		{"created_at offset", "1", `"2026-11-04T11:30:00+02:00"`, ok, false},
		{"created_at nanoseconds", "1", `"2026-11-04T09:30:00.5Z"`, ok, false},
		{"created_at far future", "1", future, future, false},
		{"updated_at missing", "1", ok, "", false},
		{"updated_at null", "1", ok, "null", false},
		{"updated_at before created_at", "1", ok, `"2026-11-04T09:29:59Z"`, false},
		{"revision 0", "0", ok, ok, false},
		{"revision 2^53", "9007199254740992", ok, ok, false},
		{"revision int64 max", "9223372036854775807", ok, ok, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "data")
			seed(t, root, `{"title":"Good"}`)
			const id = "drf_aaaaaaaaaaaaaaaaaaaa"
			var b strings.Builder
			fmt.Fprintf(&b, `{"schema_version":1,"id":%q,"revision":%s,"status":"draft",`, id, tc.rev)
			if tc.created != "" {
				fmt.Fprintf(&b, `"created_at":%s,`, tc.created)
			}
			if tc.updated != "" {
				fmt.Fprintf(&b, `"updated_at":%s,`, tc.updated)
			}
			b.WriteString(`"title":"T"}`)
			if err := os.WriteFile(filepath.Join(root, "drafts", id+".json"), []byte(b.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, root)
			for _, asJSON := range []bool{false, true} {
				args := []string{"draft", "show", id, "--data-dir", root}
				if asJSON {
					args = append(args, "--json")
				}
				show := invoke(t, "", nil, args...)
				if tc.valid {
					if show.code != 0 || show.stdout == "" {
						t.Fatalf("show = %+v", show)
					}
				} else if show.code != 3 || show.stdout != "" || !strings.Contains(show.err, "the draft file has an invalid revision or timestamp") {
					t.Fatalf("show = %+v, want exit 3", show)
				}
			}
			list := invoke(t, "", nil, "draft", "list", "--data-dir", root, "--json")
			var page draft.ListPage
			onlyJSON(t, list.stdout, &page)
			wantTotal, wantSkipped := 2, 0
			if !tc.valid {
				wantTotal, wantSkipped = 1, 1
			}
			if list.code != 0 || page.Total != wantTotal || page.SkippedTotal != wantSkipped {
				t.Fatalf("list = %+v, page = %+v", list, page)
			}
			if !tc.valid && (page.Skipped[0].ID != id || page.Skipped[0].Category != draft.CategoryCorrupt) {
				t.Fatalf("skipped = %+v", page.Skipped)
			}
			sameSnapshot(t, before, snapshot(t, root))
		})
	}
}
