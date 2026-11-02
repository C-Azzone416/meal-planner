package main

import (
	"bytes"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/version"
)

func TestRun(t *testing.T) {
	want := "mealplanner " + version.Version + "\n"
	for _, arg := range []string{"version", "--version"} {
		var out, errb bytes.Buffer
		if code := run([]string{arg}, &out, &errb); code != 0 || out.String() != want {
			t.Errorf("%s: code=%d out=%q", arg, code, out.String())
		}
	}
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != 2 || out.Len() != 0 || errb.Len() == 0 {
		t.Errorf("no args: code=%d out=%q err=%q", code, out.String(), errb.String())
	}
}
