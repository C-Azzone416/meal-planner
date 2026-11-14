package localstore

import (
	"encoding/json"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// storedTimeLayout is the one spelling of a timestamp the store writes and accepts: UTC, whole
// seconds, a literal "Z". Offsets (even +00:00) and fractional seconds are rejected on read.
const storedTimeLayout = "2006-01-02T15:04:05Z"

// parseStoredTime parses the raw JSON value of a timestamp field. It accepts only a JSON string in
// exactly the storedTimeLayout spelling: null, a missing field, numbers, offsets and fractions are
// all rejected, and so is the zero time.
func parseStoredTime(raw json.RawMessage) (time.Time, bool) {
	var s string
	if raw == nil || json.Unmarshal(raw, &s) != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(storedTimeLayout, s)
	if err != nil || t.Format(storedTimeLayout) != s || t.IsZero() {
		return time.Time{}, false
	}
	return t, true
}

// validEnvelope checks the server-assigned fields of a draft: revision in 1..MaxRevision,
// created_at and updated_at non-zero, whole-second UTC, updated_at >= created_at, and neither more
// than MaxFutureSkew ahead of now. The read path checks the raw spelling of the timestamps first
// (parseStoredTime); the write path runs this on values so the store never writes a file it would
// refuse to read.
func validEnvelope(d draft.Draft, now time.Time) bool {
	if d.Revision < 1 || d.Revision > draft.MaxRevision {
		return false
	}
	limit := now.Add(draft.MaxFutureSkew)
	for _, t := range []time.Time{d.CreatedAt, d.UpdatedAt} {
		if t.IsZero() || t.Nanosecond() != 0 || t.After(limit) {
			return false
		}
		if _, off := t.Zone(); off != 0 {
			return false
		}
	}
	return !d.UpdatedAt.Before(d.CreatedAt)
}

// futureEnvelope distinguishes clock skew from malformed envelope data without
// exposing any stored timestamps or filesystem details.
func futureEnvelope(d draft.Draft, now time.Time) bool {
	limit := now.Add(draft.MaxFutureSkew)
	return d.CreatedAt.After(limit) || d.UpdatedAt.After(limit)
}
