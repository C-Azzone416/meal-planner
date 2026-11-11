package draft

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var sourceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Validate reports every structural finding for in, in schema order (field order, then list
// index), capped at MaxIssuesReported errors and warnings each. It is pure: no store, no clock,
// no I/O. It never modifies in, treats nil and empty lists alike, and never echoes a value in a
// message.
func Validate(in Content) ValidationResult {
	v := &validator{sourceIDs: map[string]bool{}}
	for _, s := range in.Sources {
		if s.ID != "" {
			v.sourceIDs[s.ID] = true
		}
	}
	v.content(in)
	if encodedSize(in) > MaxDraftBytes {
		v.err(IssueDraftTooLarge, "/", "the encoded draft exceeds the size limit")
	}
	return ValidationResult{
		Scope:             ValidationScope,
		StructurallyValid: len(v.errors) == 0,
		Errors:            nonNil(v.errors),
		Warnings:          nonNil(v.warnings),
		Truncated:         v.truncated,
		Notice:            ValidationNotice,
	}
}

func nonNil(s []Issue) []Issue {
	if s == nil {
		return []Issue{}
	}
	return s
}

// encodedSize is the byte length of json.Marshal (default, compact) of c in canonical form
// (nil lists as []), which is what MaxDraftBytes bounds.
func encodedSize(c Content) int {
	b, err := json.Marshal(normalize(c))
	if err != nil {
		return 0
	}
	return len(b)
}

type validator struct {
	errors    []Issue
	warnings  []Issue
	truncated bool
	sourceIDs map[string]bool
}

func (v *validator) err(code, path, msg string) {
	if len(v.errors) >= MaxIssuesReported {
		v.truncated = true
		return
	}
	v.errors = append(v.errors, Issue{Code: code, Path: path, Message: msg})
}

func (v *validator) warn(code, path, msg string) {
	if len(v.warnings) >= MaxIssuesReported {
		v.truncated = true
		return
	}
	v.warnings = append(v.warnings, Issue{Code: code, Path: path, Message: msg})
}

// textRule describes a string field.
type textRule struct {
	required bool
	maxRunes int
}

// multilineText is shared by typed input validation and stored JSON checks.
// These paths have already been matched to fields of the content schema.
func multilineText(path string) bool {
	if path == "/notes" {
		return true
	}
	list, item, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	_, field, _ := strings.Cut(item, "/")
	return (list == "instructions" || list == "guidance") && field == "text"
}

// text checks one string field. A blank required field is `required` and nothing else; otherwise
// invalid characters and length are both reported.
func (v *validator) text(path, s string, r textRule) {
	if r.required && strings.TrimFunc(s, unicode.IsSpace) == "" {
		v.err(IssueRequired, path, "a non-blank value is required")
		return
	}
	multiline := multilineText(path)
	if !validChars(s, multiline) {
		if multiline {
			v.err(IssueInvalidChars, path, "contains characters that are not allowed (bidirectional controls, control characters other than newline, carriage return and tab, or invalid UTF-8)")
		} else {
			v.err(IssueInvalidChars, path, "contains characters that are not allowed (control characters, bidirectional controls, or invalid UTF-8)")
		}
	}
	if utf8.RuneCountInString(s) > r.maxRunes {
		v.err(IssueTooLong, path, fmt.Sprintf("exceeds the limit of %d characters", r.maxRunes))
	}
}

func validChars(s string, multiline bool) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if multiline && (r == '\n' || r == '\r' || r == '\t') {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return false
		}
	}
	return true
}

func (v *validator) list(path string, n, max int) {
	if n > max {
		v.err(IssueTooManyItems, path, fmt.Sprintf("has more than the limit of %d items", max))
	}
}

func idx(path string, i int, field string) string {
	return path + "/" + strconv.Itoa(i) + "/" + field
}

func (v *validator) content(c Content) {
	v.text("/title", c.Title, textRule{required: true, maxRunes: MaxTitleRunes})
	v.text("/yield", c.Yield, textRule{maxRunes: MaxYieldRunes})

	v.list("/ingredients", len(c.Ingredients), MaxIngredients)
	if len(c.Ingredients) == 0 {
		v.warn(WarnNoIngredients, "/ingredients", "the draft has no ingredients")
	}
	for i, ing := range c.Ingredients {
		v.text(idx("/ingredients", i, "text"), ing.Text, textRule{required: true, maxRunes: MaxIngredientText})
		v.text(idx("/ingredients", i, "quantity"), ing.Quantity, textRule{maxRunes: MaxIngredientShort})
		v.text(idx("/ingredients", i, "unit"), ing.Unit, textRule{maxRunes: MaxIngredientShort})
		v.text(idx("/ingredients", i, "name"), ing.Name, textRule{maxRunes: MaxIngredientField})
		v.text(idx("/ingredients", i, "note"), ing.Note, textRule{maxRunes: MaxIngredientText})
	}

	v.list("/instructions", len(c.Instructions), MaxInstructions)
	if len(c.Instructions) == 0 {
		v.warn(WarnNoInstructions, "/instructions", "the draft has no instructions")
	}
	withPhase := 0
	for i, st := range c.Instructions {
		v.step(i, st)
		if st.Phase != "" {
			withPhase++
		}
	}
	if withPhase > 0 && withPhase < len(c.Instructions) {
		v.warn(WarnPartialPhaseAssignment, "/instructions", "some steps have a phase and others do not")
	}

	v.text("/notes", c.Notes, textRule{maxRunes: MaxNotesRunes})

	v.list("/sources", len(c.Sources), MaxSources)
	if len(c.Sources) == 0 {
		v.warn(WarnNoSources, "/sources", "the draft has no sources")
	}
	seen := map[string]bool{}
	for i, s := range c.Sources {
		v.source(i, s, seen)
	}

	if c.Estimates != nil {
		v.estimates("/estimates", c.Estimates)
	}
	if pe := c.PhaseEstimates; pe != nil {
		if pe.PrepAhead != nil {
			v.estimates("/phase_estimates/prep_ahead", pe.PrepAhead)
		}
		if pe.PrepTonight != nil {
			v.estimates("/phase_estimates/prep_tonight", pe.PrepTonight)
		}
		if pe.CookTonight != nil {
			v.estimates("/phase_estimates/cook_tonight", pe.CookTonight)
		}
	}

	v.list("/guidance", len(c.Guidance), MaxGuidance)
	for i, g := range c.Guidance {
		v.guidance(i, g)
	}
}

func (v *validator) step(i int, st Step) {
	v.text(idx("/instructions", i, "text"), st.Text, textRule{required: true, maxRunes: MaxStepRunes})
	phasePath := idx("/instructions", i, "phase")
	basisPath := idx("/instructions", i, "phase_basis")
	switch st.Phase {
	case "":
		if st.PhaseBasis != nil {
			v.err(IssueInvalidValue, basisPath, "phase_basis is not allowed without a phase")
		}
	case PhasePrepAhead:
		if st.PhaseBasis == nil {
			v.err(IssueBasisRequired, basisPath, "a prep_ahead step must carry a basis")
		}
	case PhasePrepTonight, PhaseCookTonight:
	default:
		v.err(IssueInvalidValue, phasePath, "phase must be one of prep_ahead, prep_tonight, cook_tonight")
	}
	if st.PhaseBasis != nil {
		v.basis(basisPath, st.PhaseBasis)
	}
}

func (v *validator) source(i int, s Source, seen map[string]bool) {
	idPath := idx("/sources", i, "id")
	switch {
	case strings.TrimFunc(s.ID, unicode.IsSpace) == "":
		v.err(IssueRequired, idPath, "a non-blank id is required")
	case !sourceIDPattern.MatchString(s.ID):
		v.err(IssueInvalidValue, idPath, "id must match ^[a-z][a-z0-9_-]{0,31}$")
	case seen[s.ID]:
		v.err(IssueDuplicateID, idPath, "source ids must be unique within the draft")
	}
	if s.ID != "" {
		seen[s.ID] = true
	}

	kindPath := idx("/sources", i, "kind")
	kindOK := true
	switch s.Kind {
	case SourceURL, SourcePublication, SourceUserSupplied, SourceAgentGenerated:
	case "":
		v.err(IssueRequired, kindPath, "a kind is required")
		kindOK = false
	default:
		v.err(IssueInvalidValue, kindPath, "kind must be one of url, publication, user_supplied, agent_generated")
		kindOK = false
	}

	v.text(idx("/sources", i, "label"), s.Label, textRule{maxRunes: MaxLabelRunes})
	v.text(idx("/sources", i, "locator"), s.Locator, textRule{maxRunes: MaxLabelRunes})

	urlPath := idx("/sources", i, "url")
	if !kindOK {
		if s.URL != "" {
			v.sourceURL(urlPath, s.URL)
		}
		return
	}
	if s.Kind == SourceURL {
		if strings.TrimFunc(s.URL, unicode.IsSpace) == "" {
			v.err(IssueRequired, urlPath, "a url is required for a url source")
		} else {
			v.sourceURL(urlPath, s.URL)
		}
		return
	}
	if s.URL != "" {
		v.err(IssueInvalidValue, urlPath, "url is only allowed for a url source")
	}
	blank := func(x string) bool { return strings.TrimFunc(x, unicode.IsSpace) == "" }
	if blank(s.Label) && blank(s.Locator) {
		v.err(IssueRequired, idx("/sources", i, "label"), "a label or a locator is required for a non-url source")
	}
}

// sourceURL checks a URL as data only: it is never fetched.
func (v *validator) sourceURL(path, raw string) {
	if len(raw) > MaxURLBytes {
		v.err(IssueTooLong, path, fmt.Sprintf("exceeds the limit of %d bytes", MaxURLBytes))
		return
	}
	if !validChars(raw, false) {
		v.err(IssueInvalidChars, path, "contains characters that are not allowed")
		return
	}
	u, err := url.Parse(raw)
	switch {
	case strings.ContainsFunc(raw, unicode.IsSpace):
		v.err(IssueInvalidValue, path, "url must not contain whitespace")
	case err != nil || !u.IsAbs() || u.Hostname() == "":
		v.err(IssueInvalidValue, path, "url must be an absolute http or https URL with a host")
	case u.Scheme != "http" && u.Scheme != "https":
		v.err(IssueInvalidValue, path, "url scheme must be http or https")
	case u.User != nil:
		v.err(IssueInvalidValue, path, "url must not contain credentials")
	}
}

func (v *validator) basis(path string, b *Basis) {
	switch b.Kind {
	case BasisAgentEstimate:
	case BasisSourced:
		if b.SourceRef == "" {
			v.warn(WarnSourcedWithoutReference, path, "a sourced basis should name a source_ref")
		}
	case "":
		v.err(IssueRequired, path+"/kind", "a basis kind is required")
	default:
		v.err(IssueInvalidValue, path+"/kind", "basis kind must be one of agent_estimate, sourced")
	}
	if b.SourceRef != "" && !v.sourceIDs[b.SourceRef] {
		v.err(IssueUnknownSourceRef, path+"/source_ref", "source_ref does not match any source id in this draft")
	}
	v.text(path+"/note", b.Note, textRule{maxRunes: MaxNoteRunes})
}

func (v *validator) estimates(path string, e *Estimates) {
	v.minutes(path+"/active_minutes", e.ActiveMinutes)
	v.minutes(path+"/total_minutes", e.TotalMinutes)
	v.level(path+"/attention", e.Attention)
	v.level(path+"/cleanup", e.Cleanup)
	v.level(path+"/variability", e.Variability)
	a, t := e.ActiveMinutes, e.TotalMinutes
	if a != nil && t != nil && a.Value != nil && t.Value != nil && *a.Value > *t.Value {
		v.warn(WarnActiveExceedsTotal, path+"/active_minutes/value", "active_minutes is greater than total_minutes")
	}
}

func (v *validator) minutes(path string, m *MinutesEstimate) {
	if m == nil {
		return
	}
	switch {
	case m.Value == nil:
		v.err(IssueRequired, path+"/value", "a value is required")
	case *m.Value < 0 || *m.Value > MaxMinutes:
		v.err(IssueInvalidValue, path+"/value", fmt.Sprintf("value must be an integer from 0 to %d", MaxMinutes))
	}
	v.estimateBasis(path, m.Basis)
}

func (v *validator) level(path string, l *LevelEstimate) {
	if l == nil {
		return
	}
	switch l.Value {
	case LevelLow, LevelMedium, LevelHigh:
	case "":
		v.err(IssueRequired, path+"/value", "a value is required")
	default:
		v.err(IssueInvalidValue, path+"/value", "value must be one of low, medium, high")
	}
	v.estimateBasis(path, l.Basis)
}

func (v *validator) estimateBasis(path string, b *Basis) {
	if b == nil {
		v.err(IssueBasisRequired, path+"/basis", "an estimate must carry a basis")
		return
	}
	v.basis(path+"/basis", b)
}

func (v *validator) guidance(i int, g Guidance) {
	switch g.Topic {
	case TopicPrepAhead, TopicStorage, TopicFoodSafety, TopicOther:
	case "":
		v.err(IssueRequired, idx("/guidance", i, "topic"), "a topic is required")
	default:
		v.err(IssueInvalidValue, idx("/guidance", i, "topic"), "topic must be one of prep_ahead, storage, food_safety, other")
	}
	v.text(idx("/guidance", i, "text"), g.Text, textRule{required: true, maxRunes: MaxGuidanceRunes})
	if g.Basis == nil {
		v.err(IssueBasisRequired, idx("/guidance", i, "basis"), "a guidance statement must carry a basis")
		return
	}
	v.basis(idx("/guidance", i, "basis"), g.Basis)
}
