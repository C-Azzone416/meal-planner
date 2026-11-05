package draft_test

import (
	"reflect"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func fullEstimates() *draft.Estimates {
	b := func() *draft.Basis { return &draft.Basis{Kind: draft.BasisSourced, SourceRef: "s1", Note: "n"} }
	return &draft.Estimates{
		ActiveMinutes: &draft.MinutesEstimate{Value: intp(1), Basis: b()},
		TotalMinutes:  &draft.MinutesEstimate{Value: intp(2), Basis: b()},
		Attention:     &draft.LevelEstimate{Value: draft.LevelLow, Basis: b()},
		Cleanup:       &draft.LevelEstimate{Value: draft.LevelMedium, Basis: b()},
		Variability:   &draft.LevelEstimate{Value: draft.LevelHigh, Basis: b()},
	}
}

// everything has every pointer and list in Content populated, so a Clone that forgets one is caught.
func everything() draft.Content {
	b := func() *draft.Basis { return &draft.Basis{Kind: draft.BasisAgentEstimate, Note: "n"} }
	return draft.Content{
		Title:        "T",
		Ingredients:  []draft.Ingredient{{Text: "a"}, {Text: "b"}},
		Instructions: []draft.Step{{Text: "s", Phase: draft.PhasePrepAhead, PhaseBasis: b()}, {Text: "t", Phase: draft.PhasePrepTonight, PhaseBasis: b()}},
		Sources:      []draft.Source{{ID: "s1", Kind: draft.SourceUserSupplied, Label: "l"}},
		Estimates:    fullEstimates(),
		PhaseEstimates: &draft.PhaseEstimates{
			PrepAhead: fullEstimates(), PrepTonight: fullEstimates(), CookTonight: fullEstimates(),
		},
		Guidance: []draft.Guidance{{Topic: draft.TopicOther, Text: "g", Basis: b()}},
	}
}

// addresses collects the address of every non-nil pointer and every non-empty slice's backing
// array reachable from v, and reports any nil pointer or nil/empty slice it meets.
func addresses(v reflect.Value, path string, into map[uintptr]string, holes *[]string) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			*holes = append(*holes, path)
			return
		}
		into[v.Pointer()] = path
		addresses(v.Elem(), path, into, holes)
	case reflect.Slice:
		if v.Len() == 0 {
			*holes = append(*holes, path)
			return
		}
		into[v.Pointer()] = path
		for i := 0; i < v.Len(); i++ {
			addresses(v.Index(i), path+"[]", into, holes)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			addresses(v.Field(i), path+"."+v.Type().Field(i).Name, into, holes)
		}
	}
}

// Clone shares no pointer and no slice backing array with the original, at any depth.
func TestCloneSharesNothing(t *testing.T) {
	orig := everything()
	origAddrs := map[uintptr]string{}
	var holes []string
	addresses(reflect.ValueOf(orig), "Content", origAddrs, &holes)
	// Phase estimates' unused pointer fields are all populated in the fixture, so the only
	// legitimately empty slices/pointers would be a test gap.
	if len(holes) != 0 {
		t.Fatalf("test fixture leaves fields empty, so Clone is not fully exercised: %v", holes)
	}
	if len(origAddrs) < 30 {
		t.Fatalf("only %d addresses in the fixture", len(origAddrs))
	}
	clone := orig.Clone()
	if !reflect.DeepEqual(orig, clone) {
		t.Fatal("clone differs from the original")
	}
	cloneAddrs := map[uintptr]string{}
	addresses(reflect.ValueOf(clone), "Content", cloneAddrs, &holes)
	for addr, path := range cloneAddrs {
		if other, shared := origAddrs[addr]; shared {
			t.Errorf("clone%s shares memory with the original at %s", path[len("Content"):], other)
		}
	}
	// The same holds for a whole Draft, and for the clone of a clone.
	d := draft.Draft{ID: "x", Content: orig}
	dc := d.Clone()
	dAddrs, cAddrs := map[uintptr]string{}, map[uintptr]string{}
	addresses(reflect.ValueOf(d), "Draft", dAddrs, &holes)
	addresses(reflect.ValueOf(dc), "Draft", cAddrs, &holes)
	for addr, path := range cAddrs {
		if _, shared := dAddrs[addr]; shared {
			t.Errorf("draft clone shares memory at %s", path)
		}
	}
	if !reflect.DeepEqual(d, dc) {
		t.Fatal("draft clone differs")
	}
}

// Clone keeps nil and empty distinct, so a clone compares equal to its original.
func TestCloneKeepsNilAndEmpty(t *testing.T) {
	for _, c := range []draft.Content{
		{},
		{Ingredients: []draft.Ingredient{}, Instructions: []draft.Step{}, Sources: []draft.Source{}, Guidance: []draft.Guidance{}},
		{Estimates: &draft.Estimates{}, PhaseEstimates: &draft.PhaseEstimates{}},
		{Estimates: &draft.Estimates{ActiveMinutes: &draft.MinutesEstimate{}}},
	} {
		if cl := c.Clone(); !reflect.DeepEqual(c, cl) {
			t.Errorf("clone of %+v is %+v", c, cl)
		}
	}
}
