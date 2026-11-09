package main

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// clean makes stored or decoded text safe to print: control characters (newline and tab survive only in multi-line text),
// Unicode format characters (such as bidi overrides) and line/paragraph separators are shown as
// escapes, so a draft cannot move the cursor, recolor the terminal or reorder the text around it.
// multiline=false also escapes newlines and tabs. Every string that did not come from this program must go
// through clean (or block) before it is printed.
func clean(s string, multiline bool) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case multiline && (r == '\t' || r == '\n'):
			b.WriteRune(r)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029':
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// block indents continuation lines of multi-line text.
func block(s, indent string) string {
	return strings.ReplaceAll(clean(s, true), "\n", "\n"+indent)
}

func renderValidation(w io.Writer, res draft.ValidationResult) {
	for _, i := range res.Errors {
		fmt.Fprintf(w, "error    %-22s %s  %s\n", clean(i.Code, false), clean(i.Path, false), clean(i.Message, false))
	}
	for _, i := range res.Warnings {
		fmt.Fprintf(w, "warning  %-22s %s  %s\n", clean(i.Code, false), clean(i.Path, false), clean(i.Message, false))
	}
	if res.Truncated {
		fmt.Fprintln(w, "(more findings were omitted)")
	}
	fmt.Fprintf(w, "structurally valid: %s (%d errors, %d warnings)\n", yesNo(res.StructurallyValid), len(res.Errors), len(res.Warnings))
	fmt.Fprintln(w, clean(res.Notice, false))
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func renderList(w io.Writer, page draft.ListPage) {
	if len(page.Drafts) == 0 && page.Total == 0 {
		fmt.Fprintln(w, "no drafts")
	}
	for _, s := range page.Drafts {
		fmt.Fprintf(w, "%s  r%d  %s  %s\n", clean(s.ID, false), s.Revision, s.UpdatedAt.UTC().Format(time.RFC3339), clean(s.Title, false))
	}
	if len(page.Drafts) == 0 && page.Total > 0 {
		fmt.Fprintf(w, "no drafts at offset %d (total %d)\n", page.Offset, page.Total)
	}
	if page.HasMore {
		fmt.Fprintf(w, "showing %d-%d of %d; use --offset %d for more\n", page.Offset+1, page.Offset+len(page.Drafts), page.Total, page.Offset+len(page.Drafts))
	}
	for _, s := range page.Skipped {
		fmt.Fprintf(w, "skipped %s: %s: %s\n", clean(s.ID, false), clean(string(s.Category), false), clean(s.Message, false))
	}
	if page.SkippedTotal > 0 {
		fmt.Fprintf(w, "%d unreadable %s skipped and left untouched\n", page.SkippedTotal, plural(page.SkippedTotal, "entry", "entries"))
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// basisLabel renders a basis so an unverified claim never looks authoritative.
func basisLabel(b *draft.Basis) string {
	if b == nil {
		return "[no basis]"
	}
	var s string
	switch b.Kind {
	case draft.BasisAgentEstimate:
		s = "[agent estimate, unverified]"
	case draft.BasisSourced:
		ref := b.SourceRef
		if ref == "" {
			ref = "no reference"
		}
		s = "[sourced: " + clean(ref, false) + "]"
	default:
		s = "[" + clean(string(b.Kind), false) + "]"
	}
	if b.Note != "" {
		s += " (" + clean(b.Note, false) + ")"
	}
	return s
}

func phaseLabel(p draft.Phase) string {
	return clean(strings.ReplaceAll(string(p), "_", " "), false)
}

func renderDraft(w io.Writer, d draft.Draft) {
	fmt.Fprintf(w, "%s  revision %d\n", clean(d.ID, false), d.Revision)
	fmt.Fprintln(w, "status: draft (not reviewed or verified)")
	fmt.Fprintf(w, "title: %s\n", clean(d.Title, false))
	if d.Yield != "" {
		fmt.Fprintf(w, "yield: %s\n", clean(d.Yield, false))
	}
	fmt.Fprintf(w, "created: %s  updated: %s\n", d.CreatedAt.UTC().Format(time.RFC3339), d.UpdatedAt.UTC().Format(time.RFC3339))

	fmt.Fprintf(w, "\ningredients (%d):\n", len(d.Ingredients))
	for _, in := range d.Ingredients {
		fmt.Fprintf(w, "  - %s\n", clean(in.Text, false))
	}
	fmt.Fprintf(w, "\ninstructions (%d):\n", len(d.Instructions))
	for i, st := range d.Instructions {
		line := fmt.Sprintf("  %d. ", i+1)
		if st.Phase != "" {
			line += "[" + phaseLabel(st.Phase) + "] "
		}
		line += block(st.Text, "     ")
		if st.Phase == draft.PhasePrepAhead || st.PhaseBasis != nil {
			line += " " + basisLabel(st.PhaseBasis)
		}
		fmt.Fprintln(w, line)
	}
	if d.Notes != "" {
		fmt.Fprintf(w, "\nnotes:\n  %s\n", block(d.Notes, "  "))
	}
	fmt.Fprintf(w, "\nsources (%d):\n", len(d.Sources))
	for _, s := range d.Sources {
		line := fmt.Sprintf("  %s  %s", clean(s.ID, false), clean(string(s.Kind), false))
		for _, part := range []string{s.Label, s.URL, s.Locator} {
			if part != "" {
				line += "  " + clean(part, false)
			}
		}
		fmt.Fprintln(w, line)
	}

	fmt.Fprintln(w, "\nestimates (unknown means not provided, not zero):")
	renderEstimates(w, d.Estimates, "  ")
	fmt.Fprintln(w, "\nphase estimates:")
	for _, ph := range []struct {
		p draft.Phase
		e *draft.Estimates
	}{
		{draft.PhasePrepAhead, phaseEst(d.PhaseEstimates, draft.PhasePrepAhead)},
		{draft.PhasePrepTonight, phaseEst(d.PhaseEstimates, draft.PhasePrepTonight)},
		{draft.PhaseCookTonight, phaseEst(d.PhaseEstimates, draft.PhaseCookTonight)},
	} {
		if ph.e == nil {
			fmt.Fprintf(w, "  %s: unknown\n", phaseLabel(ph.p))
			continue
		}
		fmt.Fprintf(w, "  %s:\n", phaseLabel(ph.p))
		renderEstimates(w, ph.e, "    ")
	}

	fmt.Fprintf(w, "\nguidance (%d):\n", len(d.Guidance))
	for _, g := range d.Guidance {
		fmt.Fprintf(w, "  - %s: %s %s\n", clean(string(g.Topic), false), block(g.Text, "    "), basisLabel(g.Basis))
	}
}

func phaseEst(pe *draft.PhaseEstimates, p draft.Phase) *draft.Estimates {
	if pe == nil {
		return nil
	}
	switch p {
	case draft.PhasePrepAhead:
		return pe.PrepAhead
	case draft.PhasePrepTonight:
		return pe.PrepTonight
	}
	return pe.CookTonight
}

func renderEstimates(w io.Writer, e *draft.Estimates, indent string) {
	if e == nil {
		e = &draft.Estimates{}
	}
	minutes := func(label string, m *draft.MinutesEstimate) {
		if m == nil || m.Value == nil {
			fmt.Fprintf(w, "%s%s: unknown\n", indent, label)
			return
		}
		fmt.Fprintf(w, "%s%s: %d %s\n", indent, label, *m.Value, basisLabel(m.Basis))
	}
	level := func(label string, l *draft.LevelEstimate) {
		if l == nil {
			fmt.Fprintf(w, "%s%s: unknown\n", indent, label)
			return
		}
		fmt.Fprintf(w, "%s%s: %s %s\n", indent, label, clean(string(l.Value), false), basisLabel(l.Basis))
	}
	minutes("active minutes", e.ActiveMinutes)
	minutes("total minutes", e.TotalMinutes)
	level("attention", e.Attention)
	level("cleanup", e.Cleanup)
	level("variability", e.Variability)
}
