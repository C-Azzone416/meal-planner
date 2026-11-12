package localstore

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

const (
	tsLine = `"2026-11-04T09:30:00Z"`
	maxRev = "9007199254740991" // 2^53-1
)

// envelopeFile is a valid stored file for id with the given revision, as the store writes it.
func envelopeFile(t *testing.T, id string, rev int64) string {
	t.Helper()
	raw, err := encode(mkDraft(id, rev, minimal("T")))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// mutate replaces the first occurrence of old, which must exist.
func mutate(t *testing.T, raw, old, replacement string) string {
	t.Helper()
	if !strings.Contains(raw, old) {
		t.Fatalf("fixture has no %q", old)
	}
	return strings.Replace(raw, old, replacement, 1)
}

func stamp(tm time.Time) string { return `"` + tm.UTC().Format(storedTimeLayout) + `"` }

type envelopeCase struct {
	name  string
	valid bool
	edit  func(t *testing.T, raw string) string
}

func field(name, old, replacement string) func(*testing.T, string) string {
	return func(t *testing.T, raw string) string {
		return mutate(t, raw, `"`+name+`": `+old, `"`+name+`": `+replacement)
	}
}

func dropLine(name string) func(*testing.T, string) string {
	return func(t *testing.T, raw string) string {
		start := strings.Index(raw, `  "`+name+`": `)
		if start < 0 {
			t.Fatalf("no %s line", name)
		}
		end := start + strings.Index(raw[start:], "\n") + 1
		return raw[:start] + raw[end:]
	}
}

func envelopeCases(now time.Time) []envelopeCase {
	created := func(v string) func(*testing.T, string) string { return field("created_at", tsLine, v) }
	updated := func(v string) func(*testing.T, string) string { return field("updated_at", tsLine, v) }
	// both moves created_at and updated_at together so only the clock rule is under test.
	both := func(v string) func(*testing.T, string) string {
		return func(t *testing.T, raw string) string {
			return field("updated_at", tsLine, v)(t, field("created_at", tsLine, v)(t, raw))
		}
	}
	rev := func(v string) func(*testing.T, string) string { return field("revision", "1", v) }
	return []envelopeCase{
		{"baseline", true, func(t *testing.T, raw string) string { return raw }},
		{"updated after created", true, updated(`"2026-11-04T09:30:01Z"`)},
		{"updated equals created", true, updated(tsLine)},
		{"created one hour ago", true, both(stamp(now.Add(-time.Hour)))},
		{"created at the epoch", true, both(`"1970-01-01T00:00:00Z"`)},
		{"created inside the skew allowance", true, both(stamp(now.Add(draft.MaxFutureSkew - time.Hour)))},
		{"revision 2", true, rev("2")},
		{"revision 2^53-2", true, rev("9007199254740990")},
		{"revision 2^53-1", true, rev(maxRev)},

		{"created_at missing", false, dropLine("created_at")},
		{"created_at null", false, created("null")},
		{"created_at zero time", false, created(`"0001-01-01T00:00:00Z"`)},
		{"created_at empty", false, created(`""`)},
		{"created_at number", false, created("1762248600")},
		{"created_at offset", false, created(`"2026-11-04T11:30:00+02:00"`)},
		{"created_at +00:00", false, created(`"2026-11-04T09:30:00+00:00"`)},
		{"created_at -00:00", false, created(`"2026-11-04T09:30:00-00:00"`)},
		{"created_at nanoseconds", false, created(`"2026-11-04T09:30:00.123456789Z"`)},
		{"created_at zero fraction", false, created(`"2026-11-04T09:30:00.000Z"`)},
		{"created_at lowercase", false, created(`"2026-11-04t09:30:00z"`)},
		{"created_at date only", false, created(`"2026-11-04"`)},
		{"created_at space separator", false, created(`"2026-11-04 09:30:00Z"`)},
		{"created_at impossible date", false, created(`"2026-02-30T09:30:00Z"`)},
		{"created_at far future", false, both(stamp(now.Add(2 * draft.MaxFutureSkew)))},
		{"created_at year 9999", false, both(`"9999-12-31T23:59:59Z"`)},

		{"updated_at missing", false, dropLine("updated_at")},
		{"updated_at null", false, updated("null")},
		{"updated_at zero time", false, updated(`"0001-01-01T00:00:00Z"`)},
		{"updated_at offset", false, updated(`"2026-11-04T11:30:00+02:00"`)},
		{"updated_at nanoseconds", false, updated(`"2026-11-04T09:30:00.5Z"`)},
		{"updated_at before created_at", false, updated(`"2026-11-04T09:29:59Z"`)},
		{"updated_at far future", false, updated(stamp(now.Add(2 * draft.MaxFutureSkew)))},

		{"revision 0", false, rev("0")},
		{"revision negative", false, rev("-1")},
		{"revision 2^53", false, rev("9007199254740992")},
		{"revision int64 max", false, rev("9223372036854775807")},
		{"revision int64 min", false, rev("-9223372036854775808")},
		{"revision overflows int64", false, rev("9223372036854775808")},
		{"revision fraction", false, rev("1.5")},
		{"revision exponent", false, rev("1e2")},
		{"revision string", false, rev(`"1"`)},
		{"revision null", false, rev("null")},
		{"revision missing", false, dropLine("revision")},
	}
}

// TestStoredEnvelopeValidation covers every envelope rule on real stored files: valid ones read and
// list; invalid ones are corrupt/invalid_content, are skipped by list, refuse Replace, and leave the
// bytes untouched.
func TestStoredEnvelopeValidation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range envelopeCases(now) {
		t.Run(tc.name, func(t *testing.T) {
			s, root := newStore(t)
			id := testID(1)
			mustCreate(t, s, mkDraft(id, 1, minimal("original")))
			raw := []byte(tc.edit(t, envelopeFile(t, id, 1)))
			path := filepath.Join(root, "drafts", id+".json")
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := s.Get(ctxb(), id)
			list, lerr := s.List(ctxb())
			if lerr != nil {
				t.Fatal(lerr)
			}
			if tc.valid {
				if err != nil || got.ID != id {
					t.Fatalf("Get = %+v, %v", got, err)
				}
				if len(list.Drafts) != 1 || len(list.Skipped) != 0 {
					t.Fatalf("List = %+v", list)
				}
				return
			}
			wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, root)
			if len(list.Drafts) != 0 || len(list.Skipped) != 1 || list.Skipped[0].ID != id || list.Skipped[0].Category != draft.CategoryCorrupt {
				t.Fatalf("List = %+v", list)
			}
			err = s.Replace(ctxb(), id, 1, mkDraft(id, 2, minimal("replacement")))
			wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, root)
			svc := draft.NewService(s)
			_, err = svc.Update(ctxb(), id, 1, minimal("replacement"))
			wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, root)
			if !bytes.Equal(readFile(t, path), raw) {
				t.Fatal("read/list/failed replacement changed file bytes")
			}
		})
	}
}

func writeRevision(t *testing.T, s *Store, root, id string, rev int64, createdAt, updatedAt time.Time) (string, []byte) {
	t.Helper()
	mustCreate(t, s, mkDraft(id, 1, minimal("original")))
	d := mkDraft(id, rev, minimal("T"))
	d.CreatedAt, d.UpdatedAt = createdAt, updatedAt
	raw, err := encode(d)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "drafts", id+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, raw
}

// TestRevisionCap: an update from 2^53-2 succeeds, from 2^53-1 is refused with no write, and one
// request above the cap is an invalid revision. Every path that increments revision is covered.
func TestRevisionCap(t *testing.T) {
	past := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	t.Run("update at 2^53-2 succeeds", func(t *testing.T) {
		s, root := newStore(t)
		id := testID(1)
		path, _ := writeRevision(t, s, root, id, draft.MaxRevision-1, past, past)
		res, err := draft.NewService(s).Update(ctxb(), id, draft.MaxRevision-1, minimal("next"))
		if err != nil || res.Draft.Revision != draft.MaxRevision {
			t.Fatalf("Update = %+v, %v", res, err)
		}
		got, err := s.Get(ctxb(), id)
		if err != nil || got.Revision != draft.MaxRevision || got.Title != "next" {
			t.Fatalf("Get = %+v, %v (file %s)", got, err, readFile(t, path))
		}
	})
	t.Run("update at 2^53-1 is refused with no write", func(t *testing.T) {
		s, root := newStore(t)
		id := testID(1)
		path, raw := writeRevision(t, s, root, id, draft.MaxRevision, past, past)
		before := snapshot(t, root)
		_, err := draft.NewService(s).Update(ctxb(), id, draft.MaxRevision, minimal("next"))
		de := wantErr(t, err, draft.CategoryLimitExceeded, draft.CodeRevisionLimit, root)
		if !contains(de.Message, maxRev) {
			t.Errorf("message %q should name the cap", de.Message)
		}
		if !bytes.Equal(readFile(t, path), raw) {
			t.Fatal("refused update changed the file")
		}
		equalSnapshots(t, before, snapshot(t, root))
		if got, err := s.Get(ctxb(), id); err != nil || got.Revision != draft.MaxRevision {
			t.Fatalf("Get = %+v, %v", got, err)
		}
	})
	t.Run("stale revision at the cap is still a mismatch", func(t *testing.T) {
		s, root := newStore(t)
		id := testID(1)
		writeRevision(t, s, root, id, draft.MaxRevision, past, past)
		_, err := draft.NewService(s).Update(ctxb(), id, 1, minimal("next"))
		de := wantErr(t, err, draft.CategoryConflict, draft.CodeRevisionMismatch, root)
		if de.CurrentRevision != draft.MaxRevision {
			t.Fatalf("current_revision = %d", de.CurrentRevision)
		}
	})
	t.Run("expected revision above the cap is invalid", func(t *testing.T) {
		s, root := newStore(t)
		id := testID(1)
		_, raw := writeRevision(t, s, root, id, draft.MaxRevision, past, past)
		for _, rev := range []int64{draft.MaxRevision + 1, 1<<63 - 1} {
			_, err := draft.NewService(s).Update(ctxb(), id, rev, minimal("next"))
			wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeInvalidRevision, root)
		}
		if !bytes.Equal(readFile(t, filepath.Join(root, "drafts", id+".json")), raw) {
			t.Fatal("file changed")
		}
	})
	t.Run("store Replace refuses to exceed the cap", func(t *testing.T) {
		s, root := newStore(t)
		id := testID(1)
		path, raw := writeRevision(t, s, root, id, draft.MaxRevision, past, past)
		next := mkDraft(id, draft.MaxRevision, minimal("next"))
		next.Revision = draft.MaxRevision + 1
		next.CreatedAt, next.UpdatedAt = past, past
		err := s.Replace(ctxb(), id, draft.MaxRevision, next)
		wantErr(t, err, draft.CategoryLimitExceeded, draft.CodeRevisionLimit, root)
		if !bytes.Equal(readFile(t, path), raw) {
			t.Fatal("file changed")
		}
	})
	t.Run("int64 max revision on disk is corrupt, never incremented", func(t *testing.T) {
		s, root := newStore(t)
		id := testID(1)
		mustCreate(t, s, mkDraft(id, 1, minimal("original")))
		raw := []byte(mutate(t, envelopeFile(t, id, 1), `"revision": 1`, `"revision": 9223372036854775807`))
		path := filepath.Join(root, "drafts", id+".json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := draft.NewService(s).Update(ctxb(), id, 9223372036854775807, minimal("next"))
		wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeInvalidRevision, root)
		_, err = draft.NewService(s).Update(ctxb(), id, 1, minimal("next"))
		wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, root)
		if !bytes.Equal(readFile(t, path), raw) {
			t.Fatal("file changed")
		}
	})
}

// TestWriteRefusesWhatReadWouldReject: the store never writes an envelope it would not read back.
func TestWriteRefusesWhatReadWouldReject(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	zone := time.FixedZone("plus2", 2*3600)
	for name, mod := range map[string]func(*draft.Draft){
		"zero created_at":       func(d *draft.Draft) { d.CreatedAt = time.Time{} },
		"zero updated_at":       func(d *draft.Draft) { d.UpdatedAt = time.Time{} },
		"offset created_at":     func(d *draft.Draft) { d.CreatedAt = d.CreatedAt.In(zone); d.UpdatedAt = d.CreatedAt },
		"nanosecond updated_at": func(d *draft.Draft) { d.UpdatedAt = d.UpdatedAt.Add(time.Nanosecond) },
		"updated before":        func(d *draft.Draft) { d.UpdatedAt = d.CreatedAt.Add(-time.Second) },
		"future created_at": func(d *draft.Draft) {
			d.CreatedAt = now.Add(2 * draft.MaxFutureSkew)
			d.UpdatedAt = d.CreatedAt
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, root := newStore(t)
			d := mkDraft(testID(1), 1, minimal("T"))
			d.CreatedAt, d.UpdatedAt = now, now
			mod(&d)
			wantErr(t, s.Create(ctxb(), d), draft.CategoryInternal, draft.CodeInternal, root)
			if _, err := os.Stat(filepath.Join(root, "drafts", d.ID+".json")); !os.IsNotExist(err) {
				t.Fatalf("a refused create left a file: %v", err)
			}
		})
	}
	t.Run("replace", func(t *testing.T) {
		s, root := newStore(t)
		id := testID(1)
		base := mkDraft(id, 1, minimal("T"))
		base.CreatedAt, base.UpdatedAt = now, now
		mustCreate(t, s, base)
		before := snapshot(t, root)
		next := base
		next.Revision, next.UpdatedAt = 2, now.Add(-time.Second)
		wantErr(t, s.Replace(ctxb(), id, 1, next), draft.CategoryInternal, draft.CodeInternal, root)
		equalSnapshots(t, before, snapshot(t, root))
	})
}

// TestUpdatedAtNeverMovesBackwards: a service clock behind the stored updated_at must not leave a
// draft the store reads as corrupt.
func TestUpdatedAtNeverMovesBackwards(t *testing.T) {
	s, root := newStore(t)
	clock := time.Now().UTC().Truncate(time.Second)
	svc := draft.NewService(s, draft.WithClock(func() time.Time { return clock }))
	created, err := svc.Create(ctxb(), minimal("T"))
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(-time.Hour) // the clock steps back
	res, err := svc.Update(ctxb(), created.Draft.ID, 1, minimal("T2"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Draft.UpdatedAt.Before(created.Draft.CreatedAt) || !res.Draft.UpdatedAt.Equal(created.Draft.UpdatedAt) {
		t.Fatalf("updated_at = %v, want %v", res.Draft.UpdatedAt, created.Draft.UpdatedAt)
	}
	if got, err := s.Get(ctxb(), created.Draft.ID); err != nil || got.Revision != 2 {
		t.Fatalf("Get = %+v, %v (%s)", got, err, root)
	}
}
