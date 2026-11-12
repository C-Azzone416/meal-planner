package memstore_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/core/draft/memstore"
	"github.com/C-Azzone416/meal-planner/internal/core/draft/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) draft.Store { return memstore.New() })
}

var bg = context.Background()

func damaged(cat draft.Category, code string) *draft.Error {
	return &draft.Error{Category: cat, Code: code, Message: "damaged entry"}
}

func TestMarkDamagedGetReplaceAndList(t *testing.T) {
	s := memstore.New()
	good := storetest.Minimal(storetest.ID(1))
	if err := s.Create(bg, good); err != nil {
		t.Fatal(err)
	}
	s.MarkDamaged(storetest.ID(2), damaged(draft.CategoryCorrupt, draft.CodeInvalidJSON))
	s.MarkDamaged(storetest.ID(3), damaged(draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported))

	for id, want := range map[string]*draft.Error{
		storetest.ID(2): damaged(draft.CategoryCorrupt, draft.CodeInvalidJSON),
		storetest.ID(3): damaged(draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported),
	} {
		_, err := s.Get(bg, id)
		var de *draft.Error
		if !errors.As(err, &de) || de.Category != want.Category || de.Code != want.Code {
			t.Errorf("Get(%s) = %v, want %s/%s", id, err, want.Category, want.Code)
		}
		next := storetest.Minimal(id)
		next.Revision = 2
		err = s.Replace(bg, id, 1, next)
		if !errors.As(err, &de) || de.Category != want.Category || de.Code != want.Code {
			t.Errorf("Replace(%s) = %v, want %s/%s", id, err, want.Category, want.Code)
		}
	}
	// The returned error is a copy: changing it does not change what the store returns next time.
	_, err := s.Get(bg, storetest.ID(2))
	err.(*draft.Error).Code = "tampered"
	if _, err := s.Get(bg, storetest.ID(2)); err.(*draft.Error).Code != draft.CodeInvalidJSON {
		t.Error("a returned damaged-entry error aliases the store's")
	}

	res, err := s.List(bg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Drafts) != 1 || res.Drafts[0].ID != good.ID {
		t.Fatalf("drafts = %+v", res.Drafts)
	}
	skipped := map[string]draft.Category{}
	for _, sk := range res.Skipped {
		skipped[sk.ID] = sk.Category
	}
	if len(res.Skipped) != 2 || skipped[storetest.ID(2)] != draft.CategoryCorrupt || skipped[storetest.ID(3)] != draft.CategoryUnsupportedSchema {
		t.Fatalf("skipped = %+v", res.Skipped)
	}
	info, _ := s.Info(bg)
	if info.DraftCount != 1 || info.SkippedCount != 2 {
		t.Fatalf("info = %+v", info)
	}
	// The healthy draft is untouched by all of that.
	if got, err := s.Get(bg, good.ID); err != nil || !reflect.DeepEqual(got, good) {
		t.Fatalf("good draft = %+v, %v", got, err)
	}
}

func TestCreateCollidingWithDamagedEntry(t *testing.T) {
	s := memstore.New()
	s.MarkDamaged(storetest.ID(7), damaged(draft.CategoryCorrupt, draft.CodeInvalidContent))
	err := s.Create(bg, storetest.Minimal(storetest.ID(7)))
	var de *draft.Error
	if !errors.As(err, &de) || de.Category != draft.CategoryConflict || de.Code != draft.CodeAlreadyExists {
		t.Fatalf("Create over a damaged entry = %v, want conflict/already_exists", err)
	}
	// It was not replaced: the entry is still damaged and nothing readable appeared.
	if _, err := s.Get(bg, storetest.ID(7)); !errors.As(err, &de) || de.Category != draft.CategoryCorrupt {
		t.Fatalf("Get = %v", err)
	}
	res, _ := s.List(bg)
	if len(res.Drafts) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("list = %+v", res)
	}
}

// The cap counts every draft-named entry, readable or not.
func TestStoreCapCountsDamagedEntries(t *testing.T) {
	s := memstore.New()
	for i := 1; i <= draft.MaxStoredDrafts-5; i++ {
		if err := s.Create(bg, storetest.Minimal(storetest.ID(i))); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		s.MarkDamaged(storetest.ID(5000+i), damaged(draft.CategoryCorrupt, draft.CodeInvalidJSON))
	}
	// 995 readable + 5 damaged = 1000: full.
	err := s.Create(bg, storetest.Minimal(storetest.ID(9000)))
	var de *draft.Error
	if !errors.As(err, &de) || de.Category != draft.CategoryLimitExceeded || de.Code != draft.CodeStoreFull {
		t.Fatalf("Create at the cap with damaged entries = %v, want limit_exceeded/store_full", err)
	}
	if _, err := s.Get(bg, storetest.ID(9000)); err == nil {
		t.Fatal("a rejected create left a draft")
	}
	info, _ := s.Info(bg)
	if info.DraftCount != draft.MaxStoredDrafts-5 || info.SkippedCount != 5 {
		t.Fatalf("info = %+v", info)
	}
	// One below the cap still works.
	s2 := memstore.New()
	for i := 1; i <= draft.MaxStoredDrafts-2; i++ {
		_ = s2.Create(bg, storetest.Minimal(storetest.ID(i)))
	}
	s2.MarkDamaged(storetest.ID(5000), damaged(draft.CategoryCorrupt, draft.CodeInvalidJSON))
	if err := s2.Create(bg, storetest.Minimal(storetest.ID(9000))); err != nil {
		t.Fatalf("Create one below the cap: %v", err)
	}
	if err := s2.Create(bg, storetest.Minimal(storetest.ID(9001))); err == nil {
		t.Fatal("Create at the cap succeeded")
	}
}

func TestCancelledContextOnEveryOperation(t *testing.T) {
	s := memstore.New()
	ctx, cancel := context.WithCancel(bg)
	cancel()
	d := storetest.Minimal(storetest.ID(1))
	checks := map[string]error{
		"Create":  s.Create(ctx, d),
		"Replace": s.Replace(ctx, d.ID, 1, d),
	}
	_, checks["Get"] = s.Get(ctx, d.ID)
	_, checks["List"] = s.List(ctx)
	_, checks["Info"] = s.Info(ctx)
	for name, err := range checks {
		var de *draft.Error
		if !errors.As(err, &de) || de.Category != draft.CategoryIO || de.Code != draft.CodeCanceled || !errors.Is(err, context.Canceled) {
			t.Errorf("%s = %v, want io/canceled wrapping context.Canceled", name, err)
		}
	}
}

// The store is safe for concurrent use (run with -race).
func TestConcurrentUse(t *testing.T) {
	s := memstore.New()
	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := storetest.Minimal(storetest.ID(i))
			if err := s.Create(bg, d); err != nil {
				t.Error(err)
			}
			_, _ = s.Get(bg, d.ID)
			_, _ = s.List(bg)
			_, _ = s.Info(bg)
		}()
	}
	wg.Wait()
	if info, _ := s.Info(bg); info.DraftCount != 20 {
		t.Fatalf("info = %+v", info)
	}
}

func TestReplaceRefusesToExceedMaxRevision(t *testing.T) {
	s := memstore.New()
	d := storetest.Minimal(storetest.ID(1))
	d.Revision = draft.MaxRevision
	if err := s.Create(bg, d); err != nil {
		t.Fatal(err)
	}
	next := d
	next.Revision = draft.MaxRevision + 1
	err := s.Replace(bg, d.ID, draft.MaxRevision, next)
	var de *draft.Error
	if !errors.As(err, &de) || de.Category != draft.CategoryLimitExceeded || de.Code != draft.CodeRevisionLimit {
		t.Fatalf("Replace = %v, want limit_exceeded/revision_limit", err)
	}
	if got, err := s.Get(bg, d.ID); err != nil || got.Revision != draft.MaxRevision {
		t.Fatalf("Get = %+v, %v", got, err)
	}
}
