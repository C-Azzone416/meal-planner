package memstore_test

import (
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/core/draft/memstore"
	"github.com/C-Azzone416/meal-planner/internal/core/draft/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) draft.Store { return memstore.New() })
}
