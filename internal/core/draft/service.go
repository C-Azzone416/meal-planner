package draft

import (
	"context"
	"time"
)

// Service owns validation, ID and timestamp assignment, revision logic, ordering and paging over
// a Store. Phase 1: the methods are stubs that return internal errors until the behavior lands.
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
	s := &Service{store: store, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

func errNotImplemented() error {
	return &Error{Category: CategoryInternal, Code: CodeInternal, Message: "not implemented"}
}

// Create validates in and stores it as a new draft with a new ID.
func (s *Service) Create(ctx context.Context, in Content) (WriteResult, error) {
	return WriteResult{}, errNotImplemented()
}

// Get returns the stored draft with the given ID.
func (s *Service) Get(ctx context.Context, id string) (Draft, error) {
	return Draft{}, errNotImplemented()
}

// List returns one page of draft summaries, newest update first.
func (s *Service) List(ctx context.Context, opts ListOptions) (ListPage, error) {
	return ListPage{}, errNotImplemented()
}

// Update replaces the whole content of draft id if its revision is expectedRevision.
func (s *Service) Update(ctx context.Context, id string, expectedRevision int64, in Content) (WriteResult, error) {
	return WriteResult{}, errNotImplemented()
}

// Validate checks candidate content only. It is pure and touches no store.
func (s *Service) Validate(in Content) ValidationResult { return Validate(in) }

// ValidateStored loads a stored draft and validates its content. It never modifies the store.
func (s *Service) ValidateStored(ctx context.Context, id string) (ValidationResult, error) {
	return ValidationResult{}, errNotImplemented()
}

// Info returns the store status.
func (s *Service) Info(ctx context.Context) (StoreInfo, error) {
	return StoreInfo{}, errNotImplemented()
}

// DecodeContent is the single strict decoder for content supplied as JSON.
func DecodeContent(raw []byte) (Content, []Issue) {
	return Content{}, []Issue{{Code: "internal", Path: "/", Message: "not implemented"}}
}

// Validate reports every structural finding for in, in schema order.
func Validate(in Content) ValidationResult {
	return ValidationResult{
		Scope:  ValidationScope,
		Errors: []Issue{{Code: "internal", Path: "/", Message: "not implemented"}}, Warnings: []Issue{},
		Notice: ValidationNotice,
	}
}
