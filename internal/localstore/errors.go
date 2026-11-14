package localstore

import (
	"context"
	"errors"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// Messages are safe for an agent: no paths, titles or recipe text. The cause (which may hold
// paths) travels in Err for stderr logs only.
func newError(cat draft.Category, code, msg string, cause error) *draft.Error {
	return &draft.Error{Category: cat, Code: code, Message: msg, Err: cause}
}

func ioError(code, msg string, cause error) *draft.Error {
	return newError(draft.CategoryIO, code, msg, cause)
}

func corrupt(code, msg string, cause error) *draft.Error {
	return newError(draft.CategoryCorrupt, code, msg, cause)
}

func internal(msg string, cause error) *draft.Error {
	return newError(draft.CategoryInternal, draft.CodeInternal, msg, cause)
}

func notFound() *draft.Error {
	return newError(draft.CategoryNotFound, draft.CodeDraftNotFound, "no draft with that id", nil)
}

func invalidID() *draft.Error {
	return newError(draft.CategoryInvalidArgument, draft.CodeInvalidID, "draft id must look like drf_ followed by 20 lowercase hex characters", nil)
}

func lockTimeout(cause error) *draft.Error {
	return ioError(draft.CodeLockTimeout, "another process holds the draft lock; retry", cause)
}

// ctxError maps a cancelled or expired context to an io error.
func ctxError(err error) *draft.Error {
	return ioError(draft.CodeCanceled, "the operation was cancelled before anything was written", err)
}

// asDraftError returns err as a *draft.Error, wrapping anything else as an io error.
func asDraftError(err error) *draft.Error {
	var de *draft.Error
	if errors.As(err, &de) {
		return de
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ctxError(err)
	}
	return ioError(draft.CodeIOError, "the draft store could not complete the operation", err)
}
