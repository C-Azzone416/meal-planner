package draft_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/core/draft/memstore"
	"github.com/C-Azzone416/meal-planner/internal/core/draft/storetest"
)

var (
	t0  = time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)
	ctx = context.Background()
)

// clock is a settable clock.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// seqIDs yields drf_...01, drf_...02, ...
func seqIDs() func() (string, error) {
	n := 0
	return func() (string, error) { n++; return storetest.ID(n), nil }
}

// spyStore wraps a Store, counts calls and lets a test inject behavior.
type spyStore struct {
	draft.Store
	calls                                  map[string]int
	onCreate                               func(draft.Draft) error
	beforeReplace                          func()
	createErr, getErr, listErr, replaceErr error
}

func newSpy() *spyStore { return &spyStore{Store: memstore.New(), calls: map[string]int{}} }

func (s *spyStore) Create(c context.Context, d draft.Draft) error {
	s.calls["Create"]++
	if s.onCreate != nil {
		if err := s.onCreate(d); err != nil {
			return err
		}
	}
	if s.createErr != nil {
		return s.createErr
	}
	return s.Store.Create(c, d)
}
func (s *spyStore) Get(c context.Context, id string) (draft.Draft, error) {
	s.calls["Get"]++
	if s.getErr != nil {
		return draft.Draft{}, s.getErr
	}
	return s.Store.Get(c, id)
}
func (s *spyStore) Replace(c context.Context, id string, rev int64, n draft.Draft) error {
	s.calls["Replace"]++
	if s.beforeReplace != nil {
		s.beforeReplace()
	}
	if s.replaceErr != nil {
		return s.replaceErr
	}
	return s.Store.Replace(c, id, rev, n)
}
func (s *spyStore) List(c context.Context) (draft.ListResult, error) {
	s.calls["List"]++
	if s.listErr != nil {
		return draft.ListResult{}, s.listErr
	}
	return s.Store.List(c)
}
func (s *spyStore) total() int {
	n := 0
	for _, v := range s.calls {
		n += v
	}
	return n
}

type env struct {
	svc *draft.Service
	st  *spyStore
	clk *clock
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{st: newSpy(), clk: &clock{now: t0}}
	e.svc = draft.NewService(e.st, draft.WithClock(e.clk.Now), draft.WithIDGenerator(seqIDs()))
	return e
}

func wantErr(t *testing.T, err error, cat draft.Category, code string) *draft.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("want %s/%s, got no error", cat, code)
	}
	var de *draft.Error
	if !errors.As(err, &de) {
		t.Fatalf("error %v is not a *draft.Error", err)
	}
	if de.Category != cat || de.Code != code {
		t.Fatalf("got %s/%s (%s), want %s/%s", de.Category, de.Code, de.Message, cat, code)
	}
	return de
}

func (e *env) create(t *testing.T, c draft.Content) draft.Draft {
	t.Helper()
	res, err := e.svc.Create(ctx, c)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return res.Draft
}

func (e *env) snapshot(t *testing.T) map[string]draft.Draft {
	t.Helper()
	res, err := e.st.Store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]draft.Draft{}
	for _, d := range res.Drafts {
		m[d.ID] = d
	}
	return m
}

func sameSnapshot(t *testing.T, what string, before, after map[string]draft.Draft) {
	t.Helper()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("%s changed stored state:\nbefore %+v\nafter  %+v", what, before, after)
	}
}

func TestCreate(t *testing.T) {
	e := newEnv(t)
	res, err := e.svc.Create(ctx, decodeFixture(t, "valid_minimal.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := res.Draft
	if d.ID != storetest.ID(1) || d.SchemaVersion != draft.SchemaVersion || d.Revision != 1 || d.Status != draft.StatusDraft ||
		!d.CreatedAt.Equal(t0) || !d.UpdatedAt.Equal(t0) || d.Title != "Synthetic Toast" {
		t.Fatalf("unexpected draft: %+v", d)
	}
	if !res.Validation.StructurallyValid || len(res.Validation.Errors) != 0 ||
		!eq(codes(res.Validation.Warnings), []string{"no_ingredients /ingredients", "no_instructions /instructions", "no_sources /sources"}) {
		t.Fatalf("validation = %+v", res.Validation)
	}
	got, err := e.svc.Get(ctx, d.ID)
	if err != nil || !reflect.DeepEqual(got, d) {
		t.Fatalf("Get after Create = %+v, %v", got, err)
	}
}

// The stored form of contract section 16.5 is exactly what Create produces for the minimal example.
func TestCreateMatchesStoredFileFixture(t *testing.T) {
	svc := draft.NewService(memstore.New(),
		draft.WithClock(func() time.Time { return t0 }),
		draft.WithIDGenerator(func() (string, error) { return "drf_0123456789abcdef0123", nil }))
	res, err := svc.Create(ctx, decodeFixture(t, "valid_minimal.json"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(res.Draft, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := readFixture(t, "stored_minimal.json")
	if string(b)+"\n" != string(want) {
		t.Fatalf("encoded draft differs from the fixture:\n%s\nwant:\n%s", b, want)
	}
	// And the fixture reads back to an identical Draft.
	var back draft.Draft
	if err := json.Unmarshal(want, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, res.Draft) {
		t.Fatalf("fixture decodes to %+v", back)
	}
}

func TestCreateFullKeepsEverything(t *testing.T) {
	e := newEnv(t)
	in := decodeFixture(t, "valid_full.json")
	res, err := e.svc.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Validation.Warnings) != 0 {
		t.Fatalf("warnings = %v", codes(res.Validation.Warnings))
	}
	if !reflect.DeepEqual(res.Draft.Content, in) {
		t.Fatalf("content changed through Create:\n got %+v\nwant %+v", res.Draft.Content, in)
	}
	got, _ := e.svc.Get(ctx, res.Draft.ID)
	if !reflect.DeepEqual(got.Content, in) {
		t.Fatal("content changed through Get")
	}
	// Source/provenance, the phases and the estimates survive without silent loss.
	if got.Sources[0].URL != "https://example.com/recipes/synthetic-bean-stew" || got.Instructions[0].PhaseBasis.Note == "" ||
		got.Estimates.TotalMinutes.Basis.SourceRef != "s1" || *got.PhaseEstimates.CookTonight.TotalMinutes.Value != 60 {
		t.Fatalf("provenance or breakdown lost: %+v", got.Content)
	}
}

func TestCreateRejectsWithoutMutation(t *testing.T) {
	e := newEnv(t)
	e.create(t, draft.Content{Title: "Existing"})
	before := e.snapshot(t)
	e.st.calls = map[string]int{}

	_, err := e.svc.Create(ctx, decodeFixture(t, "invalid_content.json"))
	de := wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeValidationFailed)
	if !eq(codes(de.Issues), []string{
		"required /title", "required /ingredients/0/text", "basis_required /instructions/0/phase_basis", "required /sources/0/url",
		"duplicate_id /sources/1/id", "invalid_value /estimates/active_minutes/value", "unknown_source_ref /estimates/active_minutes/basis/source_ref"}) {
		t.Fatalf("issues = %v", codes(de.Issues))
	}
	if e.st.calls["Create"] != 0 {
		t.Fatal("a rejected Create reached the store")
	}
	sameSnapshot(t, "a rejected Create", before, e.snapshot(t))

	// Empty content: a missing title is the only error.
	_, err = e.svc.Create(ctx, draft.Content{})
	de = wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeValidationFailed)
	if !eq(codes(de.Issues), []string{"required /title"}) || len(de.Warnings) != 3 {
		t.Fatalf("empty content: issues %v warnings %v", codes(de.Issues), codes(de.Warnings))
	}
	sameSnapshot(t, "an empty Create", before, e.snapshot(t))
}

func TestCreateIsNotIdempotent(t *testing.T) {
	e := newEnv(t)
	c := decodeFixture(t, "valid_full.json")
	a, b := e.create(t, c), e.create(t, c)
	if a.ID == b.ID {
		t.Fatal("identical content produced the same draft")
	}
	if len(e.snapshot(t)) != 2 {
		t.Fatal("want two drafts")
	}
}

func TestCreateTimestamps(t *testing.T) {
	zone := time.FixedZone("EST", -5*3600)
	clk := time.Date(2026, 11, 4, 4, 30, 15, 987654321, zone) // 09:30:15.987 UTC
	svc := draft.NewService(memstore.New(), draft.WithClock(func() time.Time { return clk }), draft.WithIDGenerator(seqIDs()))
	res, err := svc.Create(ctx, draft.Content{Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 4, 9, 30, 15, 0, time.UTC)
	if res.Draft.CreatedAt != want || res.Draft.UpdatedAt != want {
		t.Fatalf("timestamps = %v / %v, want %v", res.Draft.CreatedAt, res.Draft.UpdatedAt, want)
	}
	if res.Draft.CreatedAt.Location() != time.UTC {
		t.Fatal("timestamps must be in UTC")
	}
	b, _ := json.Marshal(res.Draft)
	if !strings.Contains(string(b), `"created_at":"2026-11-04T09:30:15Z"`) {
		t.Fatalf("encoded timestamp: %s", b)
	}
}

func TestCreateIDGeneration(t *testing.T) {
	t.Run("default generator makes well-formed unique ids", func(t *testing.T) {
		svc := draft.NewService(memstore.New())
		seen := map[string]bool{}
		for i := 0; i < 50; i++ {
			res, err := svc.Create(ctx, draft.Content{Title: "T"})
			if err != nil {
				t.Fatal(err)
			}
			if !draft.ValidID(res.Draft.ID) || seen[res.Draft.ID] {
				t.Fatalf("bad or repeated id %q", res.Draft.ID)
			}
			seen[res.Draft.ID] = true
		}
	})
	t.Run("a colliding id is regenerated", func(t *testing.T) {
		e := newEnv(t)
		ids := []string{storetest.ID(1), storetest.ID(1), storetest.ID(1), storetest.ID(2)}
		e.svc = draft.NewService(e.st, draft.WithClock(e.clk.Now), draft.WithIDGenerator(func() (string, error) {
			id := ids[0]
			ids = ids[1:]
			return id, nil
		}))
		first := e.create(t, draft.Content{Title: "first"})
		second := e.create(t, draft.Content{Title: "second"})
		if first.ID != storetest.ID(1) || second.ID != storetest.ID(2) || second.Title != "second" {
			t.Fatalf("ids %s %s", first.ID, second.ID)
		}
		got, _ := e.svc.Get(ctx, storetest.ID(1))
		if got.Title != "first" {
			t.Fatal("a collision overwrote the existing draft")
		}
	})
	t.Run("five collisions give up", func(t *testing.T) {
		e := newEnv(t)
		e.create(t, draft.Content{Title: "first"})
		n := 0
		e.svc = draft.NewService(e.st, draft.WithClock(e.clk.Now), draft.WithIDGenerator(func() (string, error) { n++; return storetest.ID(1), nil }))
		before := e.snapshot(t)
		_, err := e.svc.Create(ctx, draft.Content{Title: "again"})
		wantErr(t, err, draft.CategoryInternal, draft.CodeIDGenerationFailed)
		if n != 5 {
			t.Fatalf("generator called %d times, want 5", n)
		}
		sameSnapshot(t, "an exhausted Create", before, e.snapshot(t))
	})
	t.Run("a failing generator", func(t *testing.T) {
		e := newEnv(t)
		boom := errors.New("entropy exhausted")
		e.svc = draft.NewService(e.st, draft.WithIDGenerator(func() (string, error) { return "", boom }))
		_, err := e.svc.Create(ctx, draft.Content{Title: "T"})
		wantErr(t, err, draft.CategoryInternal, draft.CodeIDGenerationFailed)
		if !errors.Is(err, boom) {
			t.Fatal("the cause must be available through errors.Is")
		}
		if e.st.total() != 0 {
			t.Fatal("the store was called")
		}
	})
	t.Run("a malformed generated id is refused", func(t *testing.T) {
		e := newEnv(t)
		e.svc = draft.NewService(e.st, draft.WithIDGenerator(func() (string, error) { return "../etc/passwd", nil }))
		_, err := e.svc.Create(ctx, draft.Content{Title: "T"})
		wantErr(t, err, draft.CategoryInternal, draft.CodeIDGenerationFailed)
		if e.st.total() != 0 {
			t.Fatal("the store was called")
		}
	})
}

func TestStoreErrorsPassThroughUnchanged(t *testing.T) {
	full := &draft.Error{Category: draft.CategoryLimitExceeded, Code: draft.CodeStoreFull, Message: "full"}
	e := newEnv(t)
	e.st.createErr = full
	_, err := e.svc.Create(ctx, draft.Content{Title: "T"})
	if err != error(full) {
		t.Fatalf("Create error = %v", err)
	}
	e.st.createErr = fmt.Errorf("wrapped: %w", &draft.Error{Category: draft.CategoryIO, Code: draft.CodeIOError, Message: "disk"})
	_, err = e.svc.Create(ctx, draft.Content{Title: "T"})
	wantErr(t, err, draft.CategoryIO, draft.CodeIOError)
}

// Anything that is not a *draft.Error is mapped to internal, and its text is not exposed.
func TestForeignErrorsBecomeInternal(t *testing.T) {
	boom := errors.New("open /home/someone/.config/mealplanner/drafts: permission denied")
	e := newEnv(t)
	d := e.create(t, draft.Content{Title: "Secret Title"})
	e.st.createErr, e.st.getErr, e.st.listErr, e.st.replaceErr = boom, boom, boom, boom
	check := func(name string, err error) {
		t.Helper()
		de := wantErr(t, err, draft.CategoryInternal, draft.CodeInternal)
		if strings.Contains(de.Message, "/home") || strings.Contains(de.Error(), "permission") {
			t.Errorf("%s leaks the cause: %q", name, de.Error())
		}
		if !errors.Is(err, boom) {
			t.Errorf("%s hides the cause from errors.Is", name)
		}
	}
	_, err := e.svc.Create(ctx, draft.Content{Title: "T"})
	check("Create", err)
	_, err = e.svc.Get(ctx, d.ID)
	check("Get", err)
	_, err = e.svc.List(ctx, draft.ListOptions{})
	check("List", err)
	_, err = e.svc.Update(ctx, d.ID, 1, draft.Content{Title: "T"})
	check("Update(Get)", err)
	e.st.getErr = nil
	_, err = e.svc.Update(ctx, d.ID, 1, draft.Content{Title: "T"})
	check("Update(Replace)", err)
}

func TestGet(t *testing.T) {
	e := newEnv(t)
	d := e.create(t, decodeFixture(t, "valid_full.json"))
	got, err := e.svc.Get(ctx, d.ID)
	if err != nil || !reflect.DeepEqual(got, d) {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	_, err = e.svc.Get(ctx, storetest.ID(99))
	wantErr(t, err, draft.CategoryNotFound, draft.CodeDraftNotFound)

	// Malformed IDs never reach the store.
	e.st.calls = map[string]int{}
	for _, id := range []string{
		"", "drf_", "drf_0123456789abcdef012", "drf_0123456789abcdef01234", "drf_0123456789ABCDEF0123", "DRF_0123456789abcdef0123",
		"drf_0123456789abcdef012g", "0123456789abcdef0123", "../drf_0123456789abcdef0123", "drf_0123456789abcdef0123/", "drf_0123456789abcdef0123.json",
		"drf_0123456789abcdef0123\n", " drf_0123456789abcdef0123", "drf_../../../../etc/passwd", "drf_0123456789abcdef01é3",
	} {
		_, err := e.svc.Get(ctx, id)
		wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeInvalidID)
		if strings.Contains(err.Error(), "etc/passwd") {
			t.Errorf("the id is echoed: %v", err)
		}
	}
	if e.st.total() != 0 {
		t.Fatalf("an invalid id reached the store: %v", e.st.calls)
	}
}

func TestGetCorruptAndUnsupported(t *testing.T) {
	mem := memstore.New()
	svc := draft.NewService(mem)
	mem.MarkDamaged(storetest.ID(1), &draft.Error{Category: draft.CategoryCorrupt, Code: draft.CodeInvalidJSON, Message: "x"})
	mem.MarkDamaged(storetest.ID(2), &draft.Error{Category: draft.CategoryUnsupportedSchema, Code: draft.CodeSchemaUnsupported, Message: "y"})
	_, err := svc.Get(ctx, storetest.ID(1))
	wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidJSON)
	_, err = svc.Get(ctx, storetest.ID(2))
	wantErr(t, err, draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported)
}

func TestListOrderAndPaging(t *testing.T) {
	e := newEnv(t)
	// Created at t0, t0+1s, ..., with ties: updated_at descending, then id ascending.
	var ids []string
	for i := 0; i < 7; i++ {
		e.clk.now = t0.Add(time.Duration(i/2) * time.Second) // pairs share a timestamp
		ids = append(ids, e.create(t, draft.Content{Title: fmt.Sprintf("d%d", i)}).ID)
	}
	// ids are sequential, so within a shared timestamp the lower id comes first.
	want := []string{ids[6], ids[4], ids[5], ids[2], ids[3], ids[0], ids[1]}
	page, err := e.svc.List(ctx, draft.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range page.Drafts {
		got = append(got, s.ID)
	}
	if !eq(got, want) {
		t.Fatalf("order\n got %v\nwant %v", got, want)
	}
	if page.Total != 7 || page.Offset != 0 || page.Limit != draft.DefaultListLimit || page.HasMore || page.SkippedTotal != 0 {
		t.Fatalf("page = %+v", page)
	}

	tests := []struct {
		name         string
		opts         draft.ListOptions
		wantIDs      []string
		wantMore     bool
		wantLimit    int
		wantOffset   int
		wantTotalAll int
	}{
		{"limit 3", draft.ListOptions{Limit: 3}, want[:3], true, 3, 0, 7},
		{"limit 3 offset 3", draft.ListOptions{Limit: 3, Offset: 3}, want[3:6], true, 3, 3, 7},
		{"last partial page", draft.ListOptions{Limit: 3, Offset: 6}, want[6:], false, 3, 6, 7},
		{"exact end", draft.ListOptions{Limit: 4, Offset: 3}, want[3:], false, 4, 3, 7},
		{"offset at total", draft.ListOptions{Limit: 3, Offset: 7}, nil, false, 3, 7, 7},
		{"offset beyond total", draft.ListOptions{Limit: 3, Offset: 700}, nil, false, 3, 700, 7},
		{"limit 1", draft.ListOptions{Limit: 1, Offset: 1}, want[1:2], true, 1, 1, 7},
		{"max limit", draft.ListOptions{Limit: draft.MaxListLimit}, want, false, draft.MaxListLimit, 0, 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page, err := e.svc.List(ctx, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, s := range page.Drafts {
				got = append(got, s.ID)
			}
			if !eq(got, tc.wantIDs) {
				t.Errorf("ids %v, want %v", got, tc.wantIDs)
			}
			if page.HasMore != tc.wantMore || page.Limit != tc.wantLimit || page.Offset != tc.wantOffset || page.Total != tc.wantTotalAll {
				t.Errorf("page = %+v", page)
			}
			if page.Drafts == nil || page.Skipped == nil {
				t.Error("Drafts and Skipped must not be nil")
			}
		})
	}
}

func TestListDefaultLimitIs25(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 30; i++ {
		e.create(t, draft.Content{Title: "t"})
	}
	page, _ := e.svc.List(ctx, draft.ListOptions{})
	if len(page.Drafts) != 25 || !page.HasMore || page.Limit != 25 || page.Total != 30 {
		t.Fatalf("default page: %d drafts, %+v", len(page.Drafts), page)
	}
	page, _ = e.svc.List(ctx, draft.ListOptions{Offset: 25})
	if len(page.Drafts) != 5 || page.HasMore {
		t.Fatalf("second page: %d drafts, has_more %v", len(page.Drafts), page.HasMore)
	}
}

func TestListInvalidOptions(t *testing.T) {
	e := newEnv(t)
	e.create(t, draft.Content{Title: "T"})
	e.st.calls = map[string]int{}
	for _, l := range []int{-1, -100, draft.MaxListLimit + 1, 1000000} {
		_, err := e.svc.List(ctx, draft.ListOptions{Limit: l})
		wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeInvalidLimit)
	}
	for _, o := range []int{-1, -1000} {
		_, err := e.svc.List(ctx, draft.ListOptions{Offset: o})
		wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeInvalidOffset)
	}
	if e.st.total() != 0 {
		t.Fatal("an invalid List reached the store")
	}
}

func TestListEmptyStoreIsEmptyNotNull(t *testing.T) {
	e := newEnv(t)
	page, err := e.svc.List(ctx, draft.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(page)
	if !strings.Contains(string(b), `"drafts":[]`) || !strings.Contains(string(b), `"skipped":[]`) || strings.Contains(string(b), "null") {
		t.Fatalf("empty page encodes as %s", b)
	}
	if page.Total != 0 || page.HasMore {
		t.Fatalf("page = %+v", page)
	}
}

func TestListSummaries(t *testing.T) {
	e := newEnv(t)
	full := e.create(t, decodeFixture(t, "valid_full.json"))
	min := e.create(t, draft.Content{Title: "Bare"})
	page, _ := e.svc.List(ctx, draft.ListOptions{})
	by := map[string]draft.Summary{}
	for _, s := range page.Drafts {
		by[s.ID] = s
	}
	f := by[full.ID]
	// valid_full: agent_estimate bases: step phase_basis, active_minutes, attention, cook_tonight active and total, storage guidance = 6.
	if f.Title != "Synthetic Bean Stew" || f.Status != "draft" || f.Revision != 1 || f.IngredientCount != 3 || f.InstructionCount != 3 ||
		f.SourceCount != 2 || f.AgentEstimateCount != 6 || !f.CreatedAt.Equal(t0) {
		t.Fatalf("full summary = %+v", f)
	}
	m := by[min.ID]
	if m.IngredientCount != 0 || m.InstructionCount != 0 || m.SourceCount != 0 || m.AgentEstimateCount != 0 {
		t.Fatalf("minimal summary = %+v", m)
	}
}

func TestListReportsSkippedEntries(t *testing.T) {
	mem := memstore.New()
	svc := draft.NewService(mem, draft.WithIDGenerator(seqIDs()))
	if _, err := svc.Create(ctx, draft.Content{Title: "ok"}); err != nil {
		t.Fatal(err)
	}
	for i := 100; i < 130; i++ { // 30 damaged entries
		mem.MarkDamaged(storetest.ID(i), &draft.Error{Category: draft.CategoryCorrupt, Code: draft.CodeInvalidJSON, Message: "damaged"})
	}
	page, err := svc.List(ctx, draft.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Drafts) != 1 {
		t.Fatalf("readable drafts: total %d, %d listed", page.Total, len(page.Drafts))
	}
	if len(page.Skipped) != draft.MaxSkippedReported || page.SkippedTotal != 30 {
		t.Fatalf("skipped: %d reported, total %d", len(page.Skipped), page.SkippedTotal)
	}
	// A bad entry never fails the list, and the damaged entries are untouched.
	if _, err := svc.Get(ctx, storetest.ID(100)); err == nil {
		t.Fatal("damaged entry became readable")
	}
}

func TestUpdate(t *testing.T) {
	e := newEnv(t)
	d := e.create(t, draft.Content{Title: "Synthetic Toast"})
	e.clk.now = t0.Add(11 * time.Minute)
	in := draft.Content{Title: "Synthetic Toast", Ingredients: []draft.Ingredient{{Text: "2 slices bread"}}}
	res, err := e.svc.Update(ctx, d.ID, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	u := res.Draft
	if u.ID != d.ID || u.Revision != 2 || u.Status != draft.StatusDraft || u.SchemaVersion != draft.SchemaVersion ||
		!u.CreatedAt.Equal(t0) || !u.UpdatedAt.Equal(t0.Add(11*time.Minute)) || len(u.Ingredients) != 1 {
		t.Fatalf("updated draft = %+v", u)
	}
	// The no_ingredients warning is gone (contract 16.6) and the others remain.
	if !eq(codes(res.Validation.Warnings), []string{"no_instructions /instructions", "no_sources /sources"}) {
		t.Fatalf("warnings = %v", codes(res.Validation.Warnings))
	}
	got, _ := e.svc.Get(ctx, d.ID)
	if !reflect.DeepEqual(got, u) {
		t.Fatalf("stored draft differs from the returned one")
	}
}

func TestUpdateReplacesWholeContent(t *testing.T) {
	e := newEnv(t)
	d := e.create(t, decodeFixture(t, "valid_full.json"))
	res, err := e.svc.Update(ctx, d.ID, 1, draft.Content{Title: "Only a title"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Draft.Ingredients) != 0 || res.Draft.Estimates != nil || res.Draft.PhaseEstimates != nil || len(res.Draft.Sources) != 0 || res.Draft.Yield != "" {
		t.Fatalf("an update must replace, not merge: %+v", res.Draft)
	}
	if res.Draft.Ingredients == nil || res.Draft.Guidance == nil {
		t.Fatal("lists must be empty, not nil")
	}
}

func TestUpdateIdenticalContentStillAdvances(t *testing.T) {
	e := newEnv(t)
	c := decodeFixture(t, "valid_full.json")
	d := e.create(t, c)
	e.clk.now = t0.Add(time.Minute)
	r1, err := e.svc.Update(ctx, d.ID, 1, c)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := e.svc.Update(ctx, d.ID, 2, c)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Draft.Revision != 2 || r2.Draft.Revision != 3 || !r1.Draft.UpdatedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("revisions %d, %d; updated_at %v", r1.Draft.Revision, r2.Draft.Revision, r1.Draft.UpdatedAt)
	}
}

func TestUpdateStaleIsConflict(t *testing.T) {
	e := newEnv(t)
	d := e.create(t, draft.Content{Title: "v1"})
	if _, err := e.svc.Update(ctx, d.ID, 1, draft.Content{Title: "v2"}); err != nil {
		t.Fatal(err)
	}
	before := e.snapshot(t)
	e.st.calls = map[string]int{}
	_, err := e.svc.Update(ctx, d.ID, 1, draft.Content{Title: "stale"})
	de := wantErr(t, err, draft.CategoryConflict, draft.CodeRevisionMismatch)
	if de.CurrentRevision != 2 {
		t.Fatalf("CurrentRevision = %d", de.CurrentRevision)
	}
	if want := "expected revision 1 but the draft is at revision 2; read it again and retry"; de.Message != want {
		t.Fatalf("message = %q", de.Message)
	}
	if e.st.calls["Replace"] != 0 {
		t.Fatal("a stale update early-failed in the service and must not reach Replace")
	}
	sameSnapshot(t, "a stale update", before, e.snapshot(t))
	// A revision from the future is stale too.
	_, err = e.svc.Update(ctx, d.ID, 9, draft.Content{Title: "future"})
	if de := wantErr(t, err, draft.CategoryConflict, draft.CodeRevisionMismatch); de.CurrentRevision != 2 {
		t.Fatalf("CurrentRevision = %d", de.CurrentRevision)
	}
	sameSnapshot(t, "a future-revision update", before, e.snapshot(t))
}

// Another writer slips in between the service's read and the store's atomic check.
func TestUpdateLosesRaceAtTheStore(t *testing.T) {
	e := newEnv(t)
	d := e.create(t, draft.Content{Title: "v1"})
	e.st.beforeReplace = func() {
		e.st.beforeReplace = nil
		other := draft.NewService(e.st.Store, draft.WithClock(e.clk.Now))
		if _, err := other.Update(ctx, d.ID, 1, draft.Content{Title: "winner"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := e.svc.Update(ctx, d.ID, 1, draft.Content{Title: "loser"})
	de := wantErr(t, err, draft.CategoryConflict, draft.CodeRevisionMismatch)
	if de.CurrentRevision != 2 {
		t.Fatalf("CurrentRevision = %d", de.CurrentRevision)
	}
	got, _ := e.svc.Get(ctx, d.ID)
	if got.Title != "winner" || got.Revision != 2 {
		t.Fatalf("stored = %q rev %d", got.Title, got.Revision)
	}
}

func TestUpdateRejections(t *testing.T) {
	e := newEnv(t)
	d := e.create(t, decodeFixture(t, "valid_full.json"))
	before := e.snapshot(t)
	e.st.calls = map[string]int{}

	for _, rev := range []int64{0, -1, -9000} {
		_, err := e.svc.Update(ctx, d.ID, rev, draft.Content{Title: "x"})
		wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeInvalidRevision)
	}
	for _, id := range []string{"", "nope", strings.ToUpper(d.ID), d.ID + "x", "../" + d.ID} {
		_, err := e.svc.Update(ctx, id, 1, draft.Content{Title: "x"})
		wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeInvalidID)
	}
	// The id is checked before the revision.
	_, err := e.svc.Update(ctx, "bad", 0, draft.Content{Title: "x"})
	wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeInvalidID)
	// Invalid content: all findings, nothing stored.
	_, err = e.svc.Update(ctx, d.ID, 1, decodeFixture(t, "invalid_content.json"))
	de := wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeValidationFailed)
	if len(de.Issues) != 7 {
		t.Fatalf("issues = %v", codes(de.Issues))
	}
	// Validation comes before the store: even a stale revision with bad content is a validation error.
	_, err = e.svc.Update(ctx, d.ID, 5, draft.Content{})
	wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeValidationFailed)
	if e.st.total() != 0 {
		t.Fatalf("a rejected update reached the store: %v", e.st.calls)
	}
	sameSnapshot(t, "rejected updates", before, e.snapshot(t))

	// Missing draft.
	_, err = e.svc.Update(ctx, storetest.ID(99), 1, draft.Content{Title: "x"})
	wantErr(t, err, draft.CategoryNotFound, draft.CodeDraftNotFound)
	if len(e.snapshot(t)) != 1 {
		t.Fatal("an update of a missing draft created it")
	}
}

func TestUpdateOfDamagedDraftFailsAndLeavesItAlone(t *testing.T) {
	mem := memstore.New()
	svc := draft.NewService(mem)
	mem.MarkDamaged(storetest.ID(1), &draft.Error{Category: draft.CategoryCorrupt, Code: draft.CodeInvalidContent, Message: "bad"})
	mem.MarkDamaged(storetest.ID(2), &draft.Error{Category: draft.CategoryUnsupportedSchema, Code: draft.CodeSchemaUnsupported, Message: "newer"})
	_, err := svc.Update(ctx, storetest.ID(1), 1, draft.Content{Title: "x"})
	wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent)
	_, err = svc.Update(ctx, storetest.ID(2), 1, draft.Content{Title: "x"})
	wantErr(t, err, draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported)
	info, _ := mem.Info(ctx)
	if info.DraftCount != 0 || info.SkippedCount != 2 {
		t.Fatalf("info = %+v", info)
	}
}

// Reads never change what is stored.
func TestReadsDoNotMutate(t *testing.T) {
	e := newEnv(t)
	e.create(t, decodeFixture(t, "valid_full.json"))
	e.create(t, draft.Content{Title: "other"})
	before := e.snapshot(t)
	for i := 0; i < 3; i++ {
		if _, err := e.svc.List(ctx, draft.ListOptions{Limit: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Get(ctx, storetest.ID(1)); err != nil {
			t.Fatal(err)
		}
		_ = e.svc.Validate(decodeFixture(t, "invalid_content.json"))
		if _, err := e.svc.Info(ctx); err != nil {
			t.Fatal(err)
		}
		_, _ = e.svc.Get(ctx, storetest.ID(99))
	}
	sameSnapshot(t, "reads", before, e.snapshot(t))
	e.st.calls = map[string]int{}
	e.svc.Validate(draft.Content{Title: "x"})
	if e.st.total() != 0 {
		t.Fatal("Validate touched the store")
	}
}

// Values passed in and returned are copies: neither side can change stored state through them.
func TestIsolation(t *testing.T) {
	mutate := func(d *draft.Draft) {
		d.Title = "MUTATED"
		d.Ingredients[0].Text = "MUTATED"
		d.Instructions[0].PhaseBasis.Note = "MUTATED"
		d.Sources[0].Label = "MUTATED"
		*d.Estimates.ActiveMinutes.Value = 9999
		d.Estimates.ActiveMinutes.Basis.Kind = draft.BasisSourced
		*d.PhaseEstimates.CookTonight.TotalMinutes.Value = 9999
		d.Guidance[0].Basis.Note = "MUTATED"
	}
	t.Run("caller input to Create", func(t *testing.T) {
		e := newEnv(t)
		in := decodeFixture(t, "valid_full.json")
		d := e.create(t, in)
		before := e.snapshot(t)
		d2 := draft.Draft{Content: in}
		mutate(&d2)
		sameSnapshot(t, "mutating the Create input", before, e.snapshot(t))
		got, _ := e.svc.Get(ctx, d.ID)
		if !reflect.DeepEqual(got.Content, decodeFixture(t, "valid_full.json")) {
			t.Fatal("stored content changed")
		}
	})
	t.Run("value returned by Create", func(t *testing.T) {
		e := newEnv(t)
		d := e.create(t, decodeFixture(t, "valid_full.json"))
		before := e.snapshot(t)
		mutate(&d)
		sameSnapshot(t, "mutating the Create result", before, e.snapshot(t))
	})
	t.Run("value returned by Get", func(t *testing.T) {
		e := newEnv(t)
		d := e.create(t, decodeFixture(t, "valid_full.json"))
		before := e.snapshot(t)
		got, _ := e.svc.Get(ctx, d.ID)
		mutate(&got)
		sameSnapshot(t, "mutating the Get result", before, e.snapshot(t))
		again, _ := e.svc.Get(ctx, d.ID)
		if again.Title == "MUTATED" {
			t.Fatal("a later Get sees the caller's mutation")
		}
	})
	t.Run("input to and value returned by Update", func(t *testing.T) {
		e := newEnv(t)
		d := e.create(t, decodeFixture(t, "valid_full.json"))
		in := decodeFixture(t, "valid_full.json")
		res, err := e.svc.Update(ctx, d.ID, 1, in)
		if err != nil {
			t.Fatal(err)
		}
		before := e.snapshot(t)
		d2 := draft.Draft{Content: in}
		mutate(&d2)
		mutate(&res.Draft)
		sameSnapshot(t, "mutating Update input and result", before, e.snapshot(t))
	})
	t.Run("Create result shares nothing with the Get result", func(t *testing.T) {
		e := newEnv(t)
		d := e.create(t, decodeFixture(t, "valid_full.json"))
		got, _ := e.svc.Get(ctx, d.ID)
		mutate(&d)
		if got.Title == "MUTATED" || *got.Estimates.ActiveMinutes.Value != 20 {
			t.Fatal("values returned by different calls alias each other")
		}
	})
}

func TestContextCancelled(t *testing.T) {
	e := newEnv(t)
	d := e.create(t, draft.Content{Title: "T"})
	before := e.snapshot(t)
	e.st.calls = map[string]int{}
	c, cancel := context.WithCancel(ctx)
	cancel()
	checks := map[string]func() error{
		"Create": func() error { _, err := e.svc.Create(c, draft.Content{Title: "x"}); return err },
		"Get":    func() error { _, err := e.svc.Get(c, d.ID); return err },
		"List":   func() error { _, err := e.svc.List(c, draft.ListOptions{}); return err },
		"Update": func() error { _, err := e.svc.Update(c, d.ID, 1, draft.Content{Title: "x"}); return err },
		"Info":   func() error { _, err := e.svc.Info(c); return err },
	}
	for name, f := range checks {
		err := f()
		wantErr(t, err, draft.CategoryIO, draft.CodeCanceled)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: errors.Is(context.Canceled) is false", name)
		}
	}
	if e.st.total() != 0 {
		t.Fatalf("a cancelled call reached the store: %v", e.st.calls)
	}
	sameSnapshot(t, "cancelled calls", before, e.snapshot(t))
}

func TestInfoPassesThrough(t *testing.T) {
	e := newEnv(t)
	e.create(t, draft.Content{Title: "T"})
	e.svc = draft.NewService(e.st)
	info, err := e.svc.Info(ctx)
	if err != nil || info.DraftCount != 1 {
		t.Fatalf("Info = %+v, %v", info, err)
	}
}

// Error text for the caller never carries recipe content.
func TestErrorMessagesDoNotLeakContent(t *testing.T) {
	e := newEnv(t)
	const secret = "My Secret Grandmother Recipe"
	d := e.create(t, draft.Content{Title: secret})
	var errs []error
	_, err := e.svc.Update(ctx, d.ID, 7, draft.Content{Title: secret})
	errs = append(errs, err)
	_, err = e.svc.Create(ctx, draft.Content{Title: secret, Ingredients: []draft.Ingredient{{Text: ""}}, Notes: secret + "\x01"})
	errs = append(errs, err)
	_, err = e.svc.Get(ctx, secret)
	errs = append(errs, err)
	for _, err := range errs {
		b, _ := json.Marshal(err)
		if strings.Contains(err.Error(), secret) || strings.Contains(string(b), secret) {
			t.Errorf("error leaks content: %v / %s", err, b)
		}
	}
}

func TestErrorType(t *testing.T) {
	var _ error = (*draft.Error)(nil)
	inner := errors.New("cause")
	e := &draft.Error{Category: draft.CategoryIO, Code: draft.CodeIOError, Message: "disk unavailable", Err: inner}
	if e.Error() != "io: disk unavailable" {
		t.Errorf("Error() = %q", e.Error())
	}
	if !errors.Is(e, inner) || errors.Unwrap(e) != inner {
		t.Error("Unwrap does not return the cause")
	}
	b, _ := json.Marshal(e)
	if strings.Contains(string(b), "cause") || string(b) != `{"category":"io","code":"io_error","message":"disk unavailable"}` {
		t.Errorf("JSON = %s", b)
	}
	b, _ = json.Marshal(&draft.Error{Category: draft.CategoryConflict, Code: draft.CodeRevisionMismatch, Message: "m", CurrentRevision: 2})
	if string(b) != `{"category":"conflict","code":"revision_mismatch","message":"m","current_revision":2}` {
		t.Errorf("JSON = %s", b)
	}
}

// Results always encode lists as [], never null, and optional objects only when present.
func TestResultsEncodeEmptyListsNotNull(t *testing.T) {
	e := newEnv(t)
	res, err := e.svc.Create(ctx, draft.Content{Title: "T", Estimates: &draft.Estimates{}, PhaseEstimates: &draft.PhaseEstimates{PrepAhead: &draft.Estimates{}}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	s := string(b)
	for _, want := range []string{`"ingredients":[]`, `"instructions":[]`, `"sources":[]`, `"guidance":[]`, `"errors":[]`} {
		if !strings.Contains(s, want) {
			t.Errorf("%s lacks %s", s, want)
		}
	}
	for _, bad := range []string{"null", `"estimates"`, `"phase_estimates"`, "{}"} {
		if strings.Contains(s, bad) {
			t.Errorf("%s contains %s", s, bad)
		}
	}
	got, _ := e.svc.Get(ctx, res.Draft.ID)
	b, _ = json.Marshal(got)
	if strings.Contains(string(b), "null") {
		t.Errorf("Get result contains null: %s", b)
	}
}

func TestValidateIsPureOnService(t *testing.T) {
	svc := draft.NewService(nil) // no store at all
	res := svc.Validate(decodeFixture(t, "valid_full.json"))
	if !res.StructurallyValid {
		t.Fatal(res)
	}
}

// A bare context error from a store (not wrapped in a *draft.Error) is a cancellation, not an
// internal error.
func TestBareContextErrorsMapToCanceled(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, fmt.Errorf("read: %w", context.DeadlineExceeded)} {
		e := newEnv(t)
		d := e.create(t, draft.Content{Title: "T"})
		e.st.createErr, e.st.getErr, e.st.listErr, e.st.replaceErr = cause, cause, cause, cause
		checks := map[string]func() error{
			"Create": func() error { _, err := e.svc.Create(ctx, draft.Content{Title: "x"}); return err },
			"Get":    func() error { _, err := e.svc.Get(ctx, d.ID); return err },
			"List":   func() error { _, err := e.svc.List(ctx, draft.ListOptions{}); return err },
			"Update": func() error { _, err := e.svc.Update(ctx, d.ID, 1, draft.Content{Title: "x"}); return err },
		}
		for name, f := range checks {
			err := f()
			wantErr(t, err, draft.CategoryIO, draft.CodeCanceled)
			if !errors.Is(err, cause) {
				t.Errorf("%s: the cause is lost for errors.Is", name)
			}
		}
		e.st.getErr = nil
		_, err := e.svc.Update(ctx, d.ID, 1, draft.Content{Title: "x"})
		wantErr(t, err, draft.CategoryIO, draft.CodeCanceled)
	}
}
