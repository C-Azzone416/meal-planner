//go:build teststall

package main

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// Test-tagged builds only (go build -tags teststall): MEALPLANNER_TEST_STALL_MS delays every
// Create, and MEALPLANNER_TEST_GRACE_MS shortens the shutdown grace period. Never in a release build.
func init() {
	if v, err := strconv.Atoi(os.Getenv("MEALPLANNER_TEST_GRACE_MS")); err == nil {
		shutdownGrace = time.Duration(v) * time.Millisecond
	}
	if v, err := strconv.Atoi(os.Getenv("MEALPLANNER_TEST_STALL_MS")); err == nil {
		wrapStore = func(s draft.Store) draft.Store { return stallStore{s, time.Duration(v) * time.Millisecond} }
	}
}

type stallStore struct {
	draft.Store
	d time.Duration
}

func (s stallStore) Create(ctx context.Context, d draft.Draft) error {
	time.Sleep(s.d) // deliberately ignores ctx: simulates a call that cannot be interrupted
	return s.Store.Create(ctx, d)
}
