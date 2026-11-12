// Package memstore is an in-memory draft.Store for tests of the core and of the adapters. It
// follows the Store contract (copies in and out, never overwrites on Create, atomic revision
// check on Replace) and can be told to hold damaged entries so error paths can be exercised.
package memstore

import (
	"context"
	"sync"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// Store is an in-memory draft.Store. The zero value is not usable; call New.
type Store struct {
	mu      sync.Mutex
	drafts  map[string]draft.Draft
	damaged map[string]*draft.Error // id -> error returned by Get/Replace; listed as skipped
}

var _ draft.Store = (*Store)(nil)

// New returns an empty store.
func New() *Store {
	return &Store{drafts: map[string]draft.Draft{}, damaged: map[string]*draft.Error{}}
}

// MarkDamaged makes Get and Replace of id fail with err and makes List report id as skipped,
// like an unreadable file in a file store. err.Category should be corrupt or unsupported_schema.
// The damaged entry is never repaired or removed by the store.
func (s *Store) MarkDamaged(id string, err *draft.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.damaged[id] = err
}

func canceled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return &draft.Error{Category: draft.CategoryIO, Code: draft.CodeCanceled, Message: "the request was cancelled before it was committed", Err: err}
	}
	return nil
}

func (s *Store) Create(ctx context.Context, d draft.Draft) error {
	if err := canceled(ctx); err != nil {
		return err
	}
	if !draft.Validate(d.Content).StructurallyValid {
		return &draft.Error{Category: draft.CategoryInternal, Code: draft.CodeInternal, Message: "the draft has invalid content"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.drafts[d.ID]; ok {
		return &draft.Error{Category: draft.CategoryConflict, Code: draft.CodeAlreadyExists, Message: "a draft with this id already exists"}
	}
	if _, ok := s.damaged[d.ID]; ok {
		return &draft.Error{Category: draft.CategoryConflict, Code: draft.CodeAlreadyExists, Message: "a draft with this id already exists"}
	}
	if len(s.drafts)+len(s.damaged) >= draft.MaxStoredDrafts { // every draft-named entry counts, readable or not
		return &draft.Error{Category: draft.CategoryLimitExceeded, Code: draft.CodeStoreFull, Message: "the store already holds the maximum number of drafts"}
	}
	s.drafts[d.ID] = d.Clone()
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (draft.Draft, error) {
	if err := canceled(ctx); err != nil {
		return draft.Draft{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.damaged[id]; ok {
		c := *e
		return draft.Draft{}, &c
	}
	d, ok := s.drafts[id]
	if !ok {
		return draft.Draft{}, notFound()
	}
	return d.Clone(), nil
}

func (s *Store) Replace(ctx context.Context, id string, expectedRevision int64, next draft.Draft) error {
	if err := canceled(ctx); err != nil {
		return err
	}
	if !draft.Validate(next.Content).StructurallyValid {
		return &draft.Error{Category: draft.CategoryInternal, Code: draft.CodeInternal, Message: "the draft has invalid content"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.damaged[id]; ok {
		c := *e
		return &c
	}
	cur, ok := s.drafts[id]
	if !ok {
		return notFound()
	}
	if cur.Revision != expectedRevision {
		return &draft.Error{
			Category:        draft.CategoryConflict,
			Code:            draft.CodeRevisionMismatch,
			Message:         "the stored revision differs from the expected revision",
			CurrentRevision: cur.Revision,
		}
	}
	if expectedRevision >= draft.MaxRevision {
		return &draft.Error{Category: draft.CategoryLimitExceeded, Code: draft.CodeRevisionLimit, Message: "the draft is at the maximum revision and cannot be updated"}
	}
	if next.ID != id || next.Revision != expectedRevision+1 || !next.CreatedAt.Equal(cur.CreatedAt) {
		return &draft.Error{Category: draft.CategoryInternal, Code: draft.CodeInternal, Message: "the replacement draft is inconsistent with the stored draft"}
	}
	s.drafts[id] = next.Clone()
	return nil
}

func (s *Store) List(ctx context.Context) (draft.ListResult, error) {
	if err := canceled(ctx); err != nil {
		return draft.ListResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res := draft.ListResult{Drafts: make([]draft.Draft, 0, len(s.drafts)), Skipped: []draft.SkippedEntry{}}
	for _, d := range s.drafts { // unspecified order, as the interface allows
		res.Drafts = append(res.Drafts, d.Clone())
	}
	for id, e := range s.damaged {
		res.Skipped = append(res.Skipped, draft.SkippedEntry{ID: id, Category: e.Category, Message: e.Message})
	}
	return res, nil
}

func (s *Store) Info(ctx context.Context) (draft.StoreInfo, error) {
	if err := canceled(ctx); err != nil {
		return draft.StoreInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return draft.StoreInfo{DraftCount: len(s.drafts), SkippedCount: len(s.damaged)}, nil
}

func notFound() *draft.Error {
	return &draft.Error{Category: draft.CategoryNotFound, Code: draft.CodeDraftNotFound, Message: "no draft with this id"}
}
