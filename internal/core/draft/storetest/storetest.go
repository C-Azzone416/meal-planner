// Package storetest is the shared conformance suite for draft.Store implementations
// (contract sections 9.2 and 9.3). The in-memory store runs it in this repository's core tests.
// A future local file store can run the same suite against its own implementation:
//
//	storetest.Run(t, func(t *testing.T) draft.Store { return openStoreInTempDir(t) })
//
// newStore must return a fresh, empty store each time it is called.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// Run runs every conformance test against stores made by newStore.
func Run(t *testing.T, newStore func(t *testing.T) draft.Store) {
	t.Helper()
	tests := []struct {
		name string
		fn   func(t *testing.T, s draft.Store)
	}{
		{"CreateGetRoundTrip", testRoundTrip},
		{"GetMissing", testGetMissing},
		{"CreateNeverOverwrites", testCreateDuplicate},
		{"NoAliasingOnCreate", testNoAliasCreate},
		{"NoAliasingOnGetAndList", testNoAliasRead},
		{"ReplaceUpdates", testReplace},
		{"ReplaceStaleRevision", testReplaceStale},
		{"ReplaceMissing", testReplaceMissing},
		{"RevisionCapPrecedesLookup", testRevisionCapPrecedesLookup},
		{"ReplaceInconsistentNext", testReplaceInconsistent},
		{"ReplaceAfterRevisionMovedOn", testReplaceMovedOn},
		{"ConcurrentReplaceOneWinner", testConcurrentReplace},
		{"ConcurrentCreateOneWinner", testConcurrentCreate},
		{"ListEmpty", testListEmpty},
		{"ListReturnsEverything", testListAll},
		{"ReadsDoNotModify", testReadsDoNotModify},
		{"InfoCounts", testInfo},
		{"CancelledWritesChangeNothing", testCancelled},
		{"StoreFull", testStoreFull},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.fn(t, newStore(t)) })
	}
}

var (
	epoch = time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)
	bg    = context.Background()
)

// ID returns the n-th well-formed draft ID, for example ID(1) = "drf_00000000000000000001".
func ID(n int) string { return fmt.Sprintf("drf_%020x", n) }

// Minimal returns a fully formed minimal draft (revision 1) with the given ID.
func Minimal(id string) draft.Draft {
	return draft.Draft{
		SchemaVersion: draft.SchemaVersion, ID: id, Revision: 1, Status: draft.StatusDraft,
		CreatedAt: epoch, UpdatedAt: epoch,
		Content: draft.Content{
			Title:        "Synthetic Toast",
			Ingredients:  []draft.Ingredient{},
			Instructions: []draft.Step{},
			Sources:      []draft.Source{},
			Guidance:     []draft.Guidance{},
		},
	}
}

// Full returns a fully formed draft that exercises every field (contract section 16.2).
func Full(id string) draft.Draft {
	d := Minimal(id)
	five, twenty, ninety, zero := 5, 20, 90, 0
	est := func(v *int, kind draft.BasisKind, ref string) *draft.MinutesEstimate {
		return &draft.MinutesEstimate{Value: v, Basis: &draft.Basis{Kind: kind, SourceRef: ref}}
	}
	d.Content = draft.Content{
		Title: "Synthetic Bean Stew",
		Yield: "Serves 4-6",
		Ingredients: []draft.Ingredient{
			{Text: "2 cups dried example beans, soaked", Quantity: "2", Unit: "cups", Name: "dried example beans", Note: "soaked"},
			{Text: "1 onion, chopped"},
			{Text: "salt, to taste"},
		},
		Instructions: []draft.Step{
			{Text: "Soak the beans overnight.\nThen drain.", Phase: draft.PhasePrepAhead,
				PhaseBasis: &draft.Basis{Kind: draft.BasisAgentEstimate, Note: "Typical for dried beans; not checked against a source."}},
			{Text: "Chop the onion.", Phase: draft.PhasePrepTonight},
			{Text: "Simmer everything until tender.", Phase: draft.PhaseCookTonight},
		},
		Notes: "Synthetic example for documentation only.\tTab kept.",
		Sources: []draft.Source{
			{ID: "s1", Kind: draft.SourceURL, Label: "Example page", URL: "https://example.com/recipes/synthetic-bean-stew"},
			{ID: "s2", Kind: draft.SourceAgentGenerated, Label: "Drafted by the agent from pasted text", Locator: "p. 4"},
		},
		Estimates: &draft.Estimates{
			ActiveMinutes: est(&twenty, draft.BasisAgentEstimate, ""),
			TotalMinutes:  est(&ninety, draft.BasisSourced, "s1"),
			Attention:     &draft.LevelEstimate{Value: draft.LevelLow, Basis: &draft.Basis{Kind: draft.BasisAgentEstimate}},
		},
		PhaseEstimates: &draft.PhaseEstimates{
			PrepTonight: &draft.Estimates{ActiveMinutes: est(&zero, draft.BasisAgentEstimate, "")}, // a known zero
			CookTonight: &draft.Estimates{ActiveMinutes: est(&five, draft.BasisAgentEstimate, "")},
		},
		Guidance: []draft.Guidance{
			{Topic: draft.TopicStorage, Text: "Leftovers may keep for several days in a fridge.",
				Basis: &draft.Basis{Kind: draft.BasisAgentEstimate, Note: "General suggestion; unverified."}},
		},
	}
	return d
}

// Unicode returns a draft whose strings are not ASCII and must round-trip byte for byte (no
// trimming, case folding or normalization).
func Unicode(id string) draft.Draft {
	d := Minimal(id)
	d.Title = "Crème brûlée 🍮 قهوة 日本語́ é vs é"
	d.Ingredients = []draft.Ingredient{{Text: "  œuf \"quoted\" <b>&</b> \\ / ~  "}}
	d.Notes = "ligne 1\r\nligne 2\n"
	return d
}

func expectError(t *testing.T, err error, cat draft.Category, code string) *draft.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s/%s, got nil error", cat, code)
	}
	var de *draft.Error
	if !errors.As(err, &de) {
		t.Fatalf("error %v is not a *draft.Error", err)
	}
	if de.Category != cat || de.Code != code {
		t.Fatalf("got %s/%s, want %s/%s", de.Category, de.Code, cat, code)
	}
	return de
}

func mustCreate(t *testing.T, s draft.Store, d draft.Draft) {
	t.Helper()
	if err := s.Create(bg, d); err != nil {
		t.Fatalf("Create(%s): %v", d.ID, err)
	}
}

func mustGet(t *testing.T, s draft.Store, id string) draft.Draft {
	t.Helper()
	d, err := s.Get(bg, id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	return d
}

func equal(t *testing.T, what string, got, want draft.Draft) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s differs from what was written:\n got: %#v\nwant: %#v", what, got, want)
	}
}

func testRoundTrip(t *testing.T, s draft.Store) {
	for i, d := range []draft.Draft{Minimal(ID(1)), Full(ID(2)), Unicode(ID(3))} {
		mustCreate(t, s, d)
		equal(t, fmt.Sprintf("case %d", i), mustGet(t, s, d.ID), d)
	}
}

func testGetMissing(t *testing.T, s draft.Store) {
	_, err := s.Get(bg, ID(1))
	expectError(t, err, draft.CategoryNotFound, draft.CodeDraftNotFound)
}

func testCreateDuplicate(t *testing.T, s draft.Store) {
	first := Full(ID(1))
	mustCreate(t, s, first)
	second := Minimal(ID(1))
	second.Title = "Different"
	expectError(t, s.Create(bg, second), draft.CategoryConflict, draft.CodeAlreadyExists)
	equal(t, "first draft after a rejected duplicate", mustGet(t, s, ID(1)), first)
}

// mutate changes every reachable string, slice element and pointer of d in place.
func mutate(d *draft.Draft) {
	d.Title = "MUTATED"
	d.Ingredients[0].Text = "MUTATED"
	d.Instructions[0].Text = "MUTATED"
	d.Instructions[0].PhaseBasis.Note = "MUTATED"
	d.Sources[0].Label = "MUTATED"
	*d.Estimates.ActiveMinutes.Value = 9999
	d.Estimates.ActiveMinutes.Basis.Kind = draft.BasisSourced
	d.Estimates.Attention.Value = draft.LevelHigh
	*d.PhaseEstimates.CookTonight.ActiveMinutes.Value = 9999
	d.Guidance[0].Basis.Note = "MUTATED"
	d.Guidance[0].Text = "MUTATED"
}

func testNoAliasCreate(t *testing.T, s draft.Store) {
	want := Full(ID(1))
	in := Full(ID(1))
	mustCreate(t, s, in)
	mutate(&in) // the caller keeps using its value
	equal(t, "draft after the caller mutated its input", mustGet(t, s, ID(1)), want)
}

func testNoAliasRead(t *testing.T, s draft.Store) {
	want := Full(ID(1))
	mustCreate(t, s, want.Clone())
	got := mustGet(t, s, ID(1))
	mutate(&got)
	equal(t, "draft after the caller mutated a Get result", mustGet(t, s, ID(1)), want)
	res, err := s.List(bg)
	if err != nil || len(res.Drafts) != 1 {
		t.Fatalf("List: %v, %d drafts", err, len(res.Drafts))
	}
	mutate(&res.Drafts[0])
	equal(t, "draft after the caller mutated a List result", mustGet(t, s, ID(1)), want)
}

// next builds the successor of d with new content.
func next(d draft.Draft, title string) draft.Draft {
	n := d.Clone()
	n.Revision++
	n.UpdatedAt = d.UpdatedAt.Add(time.Minute)
	n.Title = title
	return n
}

func testReplace(t *testing.T, s draft.Store) {
	d := Full(ID(1))
	mustCreate(t, s, d)
	n := next(d, "Replaced")
	n.Ingredients = []draft.Ingredient{}
	n.Estimates = nil
	if err := s.Replace(bg, ID(1), 1, n); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	equal(t, "replaced draft", mustGet(t, s, ID(1)), n)
	n2 := next(n, "Replaced again")
	if err := s.Replace(bg, ID(1), 2, n2); err != nil {
		t.Fatalf("second Replace: %v", err)
	}
	equal(t, "twice replaced draft", mustGet(t, s, ID(1)), n2)
}

func testReplaceStale(t *testing.T, s draft.Store) {
	d := Full(ID(1))
	mustCreate(t, s, d)
	n := next(d, "Replaced")
	if err := s.Replace(bg, ID(1), 1, n); err != nil {
		t.Fatal(err)
	}
	stale := next(d, "Stale")
	de := expectError(t, s.Replace(bg, ID(1), 1, stale), draft.CategoryConflict, draft.CodeRevisionMismatch)
	if de.CurrentRevision != 2 {
		t.Fatalf("CurrentRevision = %d, want 2", de.CurrentRevision)
	}
	equal(t, "draft after a stale replace", mustGet(t, s, ID(1)), n)
}

func testReplaceMissing(t *testing.T, s draft.Store) {
	d := Minimal(ID(1))
	expectError(t, s.Replace(bg, ID(1), 1, next(d, "x")), draft.CategoryNotFound, draft.CodeDraftNotFound)
	res, err := s.List(bg)
	if err != nil || len(res.Drafts) != 0 {
		t.Fatalf("a replace of a missing draft must not create it: %v, %d drafts", err, len(res.Drafts))
	}
}

func testReplaceInconsistent(t *testing.T, s draft.Store) {
	d := Full(ID(1))
	mustCreate(t, s, d)
	cases := map[string]func(n *draft.Draft){
		"other id":          func(n *draft.Draft) { n.ID = ID(2) },
		"revision skipped":  func(n *draft.Draft) { n.Revision = 3 },
		"revision repeated": func(n *draft.Draft) { n.Revision = 1 },
		"created_at moved":  func(n *draft.Draft) { n.CreatedAt = n.CreatedAt.Add(time.Hour) },
	}
	for name, mod := range cases {
		n := next(d, "Bad")
		mod(&n)
		err := s.Replace(bg, ID(1), 1, n)
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		expectError(t, err, draft.CategoryInternal, draft.CodeInternal)
		equal(t, name+": draft after the rejected replace", mustGet(t, s, ID(1)), d)
	}
	if _, err := s.Get(bg, ID(2)); err == nil {
		t.Fatal("a replace with a different next.ID must not create that draft")
	}
}

func testListEmpty(t *testing.T, s draft.Store) {
	res, err := s.List(bg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Drafts) != 0 || len(res.Skipped) != 0 {
		t.Fatalf("empty store listed %d drafts, %d skipped", len(res.Drafts), len(res.Skipped))
	}
	if res.Drafts == nil {
		t.Fatal("ListResult.Drafts must not be nil")
	}
}

func testListAll(t *testing.T, s draft.Store) {
	want := map[string]draft.Draft{}
	for i := 1; i <= 5; i++ {
		d := Full(ID(i))
		d.Title = fmt.Sprintf("Draft %d", i)
		mustCreate(t, s, d)
		want[d.ID] = d
	}
	res, err := s.List(bg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Drafts) != len(want) {
		t.Fatalf("listed %d drafts, want %d", len(res.Drafts), len(want))
	}
	for _, got := range res.Drafts {
		w, ok := want[got.ID]
		if !ok {
			t.Fatalf("unexpected draft %s", got.ID)
		}
		equal(t, "listed draft "+got.ID, got, w)
		delete(want, got.ID)
	}
}

func testReadsDoNotModify(t *testing.T, s draft.Store) {
	mustCreate(t, s, Full(ID(1)))
	mustCreate(t, s, Unicode(ID(2)))
	snapshot := func() (draft.Draft, draft.Draft, int) {
		res, err := s.List(bg)
		if err != nil {
			t.Fatal(err)
		}
		return mustGet(t, s, ID(1)), mustGet(t, s, ID(2)), len(res.Drafts)
	}
	a1, a2, an := snapshot()
	for i := 0; i < 3; i++ {
		if _, err := s.Info(bg); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get(bg, ID(9)); err == nil { // a failed read changes nothing either
			t.Fatal("expected not found")
		}
		snapshot()
	}
	b1, b2, bn := snapshot()
	equal(t, "draft 1 after reads", b1, a1)
	equal(t, "draft 2 after reads", b2, a2)
	if an != bn {
		t.Fatalf("draft count changed from %d to %d", an, bn)
	}
}

func testInfo(t *testing.T, s draft.Store) {
	info, err := s.Info(bg)
	if err != nil || info.DraftCount != 0 || info.SkippedCount != 0 {
		t.Fatalf("empty store Info = %+v, %v", info, err)
	}
	mustCreate(t, s, Minimal(ID(1)))
	mustCreate(t, s, Minimal(ID(2)))
	info, err = s.Info(bg)
	if err != nil || info.DraftCount != 2 || info.SkippedCount != 0 {
		t.Fatalf("Info after two creates = %+v, %v", info, err)
	}
}

// A context that is already done must abort a write before its commit point.
func testCancelled(t *testing.T, s draft.Store) {
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if err := s.Create(ctx, Minimal(ID(1))); err == nil {
		t.Fatal("Create with a cancelled context succeeded")
	} else if !isDraftError(err) {
		t.Fatalf("Create error %v is not a *draft.Error", err)
	}
	if _, err := s.Get(bg, ID(1)); err == nil {
		t.Fatal("a cancelled Create left a draft behind")
	}
	d := Minimal(ID(2))
	mustCreate(t, s, d)
	if err := s.Replace(ctx, ID(2), 1, next(d, "x")); err == nil {
		t.Fatal("Replace with a cancelled context succeeded")
	} else if !isDraftError(err) {
		t.Fatalf("Replace error %v is not a *draft.Error", err)
	}
	equal(t, "draft after a cancelled replace", mustGet(t, s, ID(2)), d)
}

func isDraftError(err error) bool {
	var de *draft.Error
	return errors.As(err, &de)
}

// testStoreFull fills a store to MaxStoredDrafts. It creates a thousand small drafts, so it is
// skipped with -short.
func testStoreFull(t *testing.T, s draft.Store) {
	if testing.Short() {
		t.Skip("creates MaxStoredDrafts drafts")
	}
	for i := 1; i <= draft.MaxStoredDrafts; i++ {
		mustCreate(t, s, Minimal(ID(i)))
	}
	expectError(t, s.Create(bg, Minimal(ID(draft.MaxStoredDrafts+1))), draft.CategoryLimitExceeded, draft.CodeStoreFull)
	if _, err := s.Get(bg, ID(draft.MaxStoredDrafts+1)); err == nil {
		t.Fatal("a rejected create left a draft behind")
	}
	info, err := s.Info(bg)
	if err != nil || info.DraftCount != draft.MaxStoredDrafts {
		t.Fatalf("Info = %+v, %v; want %d drafts", info, err, draft.MaxStoredDrafts)
	}
	// An update of an existing draft still works when the store is full.
	d := mustGet(t, s, ID(1))
	if err := s.Replace(bg, ID(1), 1, next(d, "still updatable")); err != nil {
		t.Fatalf("Replace on a full store: %v", err)
	}
}

// A replace that was based on an old revision loses once the draft has moved on more than once,
// and reports the revision the draft is really at.
func testReplaceMovedOn(t *testing.T, s draft.Store) {
	d := Full(ID(1))
	mustCreate(t, s, d)
	n2 := next(d, "two")
	n3 := next(n2, "three")
	n4 := next(n3, "four")
	for i, n := range []draft.Draft{n2, n3, n4} {
		if err := s.Replace(bg, ID(1), int64(i+1), n); err != nil {
			t.Fatalf("Replace to revision %d: %v", i+2, err)
		}
	}
	for _, based := range []int64{1, 2, 3} {
		stale := next(d, "stale")
		stale.Revision = based + 1
		de := expectError(t, s.Replace(bg, ID(1), based, stale), draft.CategoryConflict, draft.CodeRevisionMismatch)
		if de.CurrentRevision != 4 {
			t.Fatalf("based on revision %d: CurrentRevision = %d, want 4", based, de.CurrentRevision)
		}
	}
	equal(t, "draft after the stale replaces", mustGet(t, s, ID(1)), n4)
	// A revision from the future is a conflict too.
	future := next(d, "future")
	future.Revision = 10
	expectError(t, s.Replace(bg, ID(1), 9, future), draft.CategoryConflict, draft.CodeRevisionMismatch)
	equal(t, "draft after a future-revision replace", mustGet(t, s, ID(1)), n4)
}

// Many writers based on the same revision: exactly one wins, the rest get revision_mismatch with
// the current revision, and the stored draft is exactly the winner's.
func testConcurrentReplace(t *testing.T, s draft.Store) {
	const writers = 8
	base := Full(ID(1))
	mustCreate(t, s, base)
	candidates := make([]draft.Draft, writers)
	errs := make([]error, writers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range candidates {
		candidates[i] = next(base, fmt.Sprintf("writer %d", i))
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = s.Replace(bg, ID(1), 1, candidates[i].Clone())
		}(i)
	}
	close(start)
	wg.Wait()
	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("writers %d and %d both won", winner, i)
			}
			winner = i
			continue
		}
		de := expectError(t, err, draft.CategoryConflict, draft.CodeRevisionMismatch)
		if de.CurrentRevision != 2 {
			t.Fatalf("writer %d: CurrentRevision = %d, want 2", i, de.CurrentRevision)
		}
	}
	if winner < 0 {
		t.Fatal("no writer won")
	}
	equal(t, "draft after concurrent replaces", mustGet(t, s, ID(1)), candidates[winner])
}

// Many creators of one ID: exactly one succeeds, the rest get already_exists, and the stored draft
// is exactly the winner's.
func testConcurrentCreate(t *testing.T, s draft.Store) {
	const creators = 8
	candidates := make([]draft.Draft, creators)
	errs := make([]error, creators)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range candidates {
		candidates[i] = Minimal(ID(1))
		candidates[i].Title = fmt.Sprintf("creator %d", i)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = s.Create(bg, candidates[i].Clone())
		}(i)
	}
	close(start)
	wg.Wait()
	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("creators %d and %d both succeeded", winner, i)
			}
			winner = i
			continue
		}
		expectError(t, err, draft.CategoryConflict, draft.CodeAlreadyExists)
	}
	if winner < 0 {
		t.Fatal("no creator succeeded")
	}
	equal(t, "draft after concurrent creates", mustGet(t, s, ID(1)), candidates[winner])
	res, err := s.List(bg)
	if err != nil || len(res.Drafts) != 1 {
		t.Fatalf("List: %v, %d drafts; want exactly one", err, len(res.Drafts))
	}
}

func testRevisionCapPrecedesLookup(t *testing.T, s draft.Store) {
	d := Minimal(ID(1))
	if err := s.Create(bg, d); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{d.ID, ID(2)} {
		for _, rev := range []int64{draft.MaxRevision, draft.MaxRevision + 1} {
			next := Minimal(id)
			next.Revision = rev + 1
			err := s.Replace(bg, id, rev, next)
			var e *draft.Error
			if !errors.As(err, &e) || e.Code != draft.CodeRevisionLimit {
				t.Fatalf("cap must precede stale/missing lookup: %v", err)
			}
		}
	}
	got, err := s.Get(bg, d.ID)
	if err != nil || got.Revision != 1 {
		t.Fatal("cap failure changed draft")
	}
}
