package draft

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"
)

var idPattern = regexp.MustCompile(`^drf_[0-9a-f]{20}$`)

// ValidID reports whether id has the draft ID format: "drf_" plus 20 lowercase hex characters.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// maxCreateAttempts is how many times Create regenerates an ID that already exists.
const maxCreateAttempts = 5

// Service owns validation, ID and timestamp assignment, revision logic, ordering and paging over
// a Store. It is safe for concurrent use if the Store is. Values passed in and returned are deep
// copies, so mutating them never changes stored state.
type Service struct {
	store Store
	now   func() time.Time
	newID func() (string, error)
}

// Option configures a Service.
type Option func(*Service)

// WithClock sets the clock used for created_at and updated_at. Default: time.Now.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithIDGenerator sets the draft ID generator. Default: "drf_" + 20 lowercase hex characters from
// crypto/rand.
func WithIDGenerator(gen func() (string, error)) Option { return func(s *Service) { s.newID = gen } }

// NewService returns a Service over store.
func NewService(store Store, opts ...Option) *Service {
	s := &Service{store: store, now: time.Now, newID: randomID}
	for _, o := range opts {
		o(s)
	}
	return s
}

func randomID() (string, error) {
	var b [10]byte // 80 bits
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "drf_" + hex.EncodeToString(b[:]), nil
}

// timestamp is the service clock in UTC, truncated to whole seconds.
func (s *Service) timestamp() time.Time { return s.now().UTC().Truncate(time.Second) }

// Create validates in and stores it as a new draft with a new ID, revision 1 and status "draft".
// Content errors are invalid_argument/validation_failed and nothing is stored. A repeat call
// makes another draft: create is not idempotent.
func (s *Service) Create(ctx context.Context, in Content) (WriteResult, error) {
	if err := checkContext(ctx); err != nil {
		return WriteResult{}, err
	}
	content := normalize(in)
	res := Validate(content)
	if !res.StructurallyValid {
		return WriteResult{}, validationError(res)
	}
	now := s.timestamp()
	for attempt := 0; attempt < maxCreateAttempts; attempt++ {
		id, err := s.newID()
		if err != nil || !ValidID(id) {
			return WriteResult{}, &Error{Category: CategoryInternal, Code: CodeIDGenerationFailed, Message: "could not generate a draft id", Err: err}
		}
		d := Draft{
			SchemaVersion: SchemaVersion,
			ID:            id,
			Revision:      1,
			Status:        StatusDraft,
			CreatedAt:     now,
			UpdatedAt:     now,
			Content:       content.Clone(),
		}
		err = s.store.Create(ctx, d.Clone())
		if err == nil {
			return WriteResult{Draft: d, Validation: res}, nil
		}
		err = asError(err)
		if isCode(err, CategoryConflict, CodeAlreadyExists) {
			continue
		}
		return WriteResult{}, err
	}
	return WriteResult{}, &Error{Category: CategoryInternal, Code: CodeIDGenerationFailed, Message: "could not generate an unused draft id"}
}

// Get returns the stored draft with the given ID. It modifies nothing.
func (s *Service) Get(ctx context.Context, id string) (Draft, error) {
	if err := checkContext(ctx); err != nil {
		return Draft{}, err
	}
	if !ValidID(id) {
		return Draft{}, invalidID()
	}
	d, err := s.store.Get(ctx, id)
	if err != nil {
		return Draft{}, asError(err)
	}
	return d.Clone(), nil
}

// List returns one page of draft summaries ordered by updated_at descending, then id ascending.
// Limit 0 means DefaultListLimit; a negative or too-large Limit is invalid_argument/invalid_limit
// and a negative Offset is invalid_argument/invalid_offset. An Offset at or beyond the total
// returns an empty page. It modifies nothing.
func (s *Service) List(ctx context.Context, opts ListOptions) (ListPage, error) {
	if err := checkContext(ctx); err != nil {
		return ListPage{}, err
	}
	limit := opts.Limit
	switch {
	case limit < 0 || limit > MaxListLimit:
		return ListPage{}, &Error{Category: CategoryInvalidArgument, Code: CodeInvalidLimit,
			Message: fmt.Sprintf("limit must be between 1 and %d (0 means %d)", MaxListLimit, DefaultListLimit)}
	case limit == 0:
		limit = DefaultListLimit
	}
	if opts.Offset < 0 {
		return ListPage{}, &Error{Category: CategoryInvalidArgument, Code: CodeInvalidOffset, Message: "offset must be 0 or greater"}
	}
	res, err := s.store.List(ctx)
	if err != nil {
		return ListPage{}, asError(err)
	}
	drafts := append([]Draft(nil), res.Drafts...)
	sort.SliceStable(drafts, func(i, j int) bool {
		if !drafts[i].UpdatedAt.Equal(drafts[j].UpdatedAt) {
			return drafts[i].UpdatedAt.After(drafts[j].UpdatedAt)
		}
		return drafts[i].ID < drafts[j].ID
	})
	page := ListPage{
		Drafts:       []Summary{},
		Total:        len(drafts),
		Offset:       opts.Offset,
		Limit:        limit,
		Skipped:      []SkippedEntry{},
		SkippedTotal: len(res.Skipped),
	}
	if opts.Offset < len(drafts) {
		end := opts.Offset + limit
		if end > len(drafts) {
			end = len(drafts)
		}
		for _, d := range drafts[opts.Offset:end] {
			page.Drafts = append(page.Drafts, summarize(d))
		}
	}
	page.HasMore = opts.Offset+len(page.Drafts) < page.Total
	for i, sk := range res.Skipped {
		if i == MaxSkippedReported {
			break
		}
		page.Skipped = append(page.Skipped, sk)
	}
	return page, nil
}

// Update replaces the whole content of draft id if its stored revision is expectedRevision, and
// sets revision to expectedRevision+1. A draft already at MaxRevision is limit_exceeded/revision_limit and
// nothing is written. A stale expectedRevision is conflict/revision_mismatch with
// CurrentRevision set; a failed or conflicting update never changes the stored draft.
func (s *Service) Update(ctx context.Context, id string, expectedRevision int64, in Content) (WriteResult, error) {
	if err := checkContext(ctx); err != nil {
		return WriteResult{}, err
	}
	if !ValidID(id) {
		return WriteResult{}, invalidID()
	}
	if expectedRevision < 1 || expectedRevision > MaxRevision {
		return WriteResult{}, &Error{Category: CategoryInvalidArgument, Code: CodeInvalidRevision,
			Message: fmt.Sprintf("expected_revision must be between 1 and %d", MaxRevision)}
	}
	content := normalize(in)
	res := Validate(content)
	if !res.StructurallyValid {
		return WriteResult{}, validationError(res)
	}
	cur, err := s.store.Get(ctx, id)
	if err != nil {
		return WriteResult{}, asError(err)
	}
	if cur.Revision != expectedRevision {
		return WriteResult{}, revisionMismatch(expectedRevision, cur.Revision)
	}
	if cur.Revision >= MaxRevision {
		return WriteResult{}, revisionLimit()
	}
	// updated_at never moves backwards: a clock that stepped back must not leave a stored draft
	// whose updated_at is earlier than its created_at, which the store would read as corrupt.
	updatedAt := s.timestamp()
	if updatedAt.Before(cur.UpdatedAt) {
		updatedAt = cur.UpdatedAt
	}
	next := Draft{
		SchemaVersion: SchemaVersion,
		ID:            id,
		Revision:      expectedRevision + 1,
		Status:        StatusDraft,
		CreatedAt:     cur.CreatedAt,
		UpdatedAt:     updatedAt,
		Content:       content,
	}
	if err := s.store.Replace(ctx, id, expectedRevision, next.Clone()); err != nil {
		return WriteResult{}, asError(err)
	}
	return WriteResult{Draft: next.Clone(), Validation: res}, nil
}

// Validate checks candidate content only. It is pure and touches no store.
func (s *Service) Validate(in Content) ValidationResult { return Validate(in) }

// Info returns the store status. It modifies nothing.
func (s *Service) Info(ctx context.Context) (StoreInfo, error) {
	if err := checkContext(ctx); err != nil {
		return StoreInfo{}, err
	}
	info, err := s.store.Info(ctx)
	if err != nil {
		return StoreInfo{}, asError(err)
	}
	return info, nil
}

func summarize(d Draft) Summary {
	return Summary{
		ID:                 d.ID,
		Title:              d.Title,
		Status:             d.Status,
		Revision:           d.Revision,
		CreatedAt:          d.CreatedAt,
		UpdatedAt:          d.UpdatedAt,
		IngredientCount:    len(d.Ingredients),
		InstructionCount:   len(d.Instructions),
		SourceCount:        len(d.Sources),
		AgentEstimateCount: countAgentEstimates(d.Content),
	}
}

// countAgentEstimates counts bases of kind agent_estimate anywhere in the draft.
func countAgentEstimates(c Content) int {
	n := 0
	count := func(b *Basis) {
		if b != nil && b.Kind == BasisAgentEstimate {
			n++
		}
	}
	est := func(e *Estimates) {
		if e == nil {
			return
		}
		if e.ActiveMinutes != nil {
			count(e.ActiveMinutes.Basis)
		}
		if e.TotalMinutes != nil {
			count(e.TotalMinutes.Basis)
		}
		for _, l := range []*LevelEstimate{e.Attention, e.Cleanup, e.Variability} {
			if l != nil {
				count(l.Basis)
			}
		}
	}
	for _, st := range c.Instructions {
		count(st.PhaseBasis)
	}
	est(c.Estimates)
	if pe := c.PhaseEstimates; pe != nil {
		est(pe.PrepAhead)
		est(pe.PrepTonight)
		est(pe.CookTonight)
	}
	for _, g := range c.Guidance {
		count(g.Basis)
	}
	return n
}

func invalidID() *Error {
	return &Error{Category: CategoryInvalidArgument, Code: CodeInvalidID, Message: "id must be drf_ followed by 20 lowercase hexadecimal characters"}
}

func revisionMismatch(expected, current int64) *Error {
	return &Error{
		Category:        CategoryConflict,
		Code:            CodeRevisionMismatch,
		Message:         fmt.Sprintf("expected revision %d but the draft is at revision %d; read it again and retry", expected, current),
		CurrentRevision: current,
	}
}

func revisionLimit() *Error {
	return &Error{
		Category: CategoryLimitExceeded,
		Code:     CodeRevisionLimit,
		Message:  fmt.Sprintf("the draft is at the maximum revision (%d) and cannot be updated; save its content as a new draft with draft_create", MaxRevision),
	}
}

func validationError(res ValidationResult) *Error {
	return &Error{
		Category: CategoryInvalidArgument,
		Code:     CodeValidationFailed,
		Message:  "the draft failed structural validation; see issues",
		Issues:   res.Errors,
		Warnings: res.Warnings,
	}
}

// checkContext returns an io/canceled error if ctx is already done.
func checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return &Error{Category: CategoryIO, Code: CodeCanceled, Message: "the request was cancelled before it was committed", Err: err}
	}
	return nil
}

// asError returns err unchanged if it is or wraps a *Error. A bare context cancellation or
// deadline becomes io/canceled, and anything else internal/internal, so every error the Service
// returns is a *Error.
func asError(err error) error {
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Category: CategoryIO, Code: CodeCanceled, Message: "the request was cancelled before it was committed", Err: err}
	}
	return &Error{Category: CategoryInternal, Code: CodeInternal, Message: "unexpected internal error", Err: err}
}

func isCode(err error, c Category, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Category == c && e.Code == code
}
