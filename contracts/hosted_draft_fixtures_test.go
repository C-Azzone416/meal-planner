package contracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// These types and the decoder below are fixture-only specifications of proposed
// HD1, not hosted handlers, persistence, a bridge, or a shipped client.
const hostedDraftFixtures = "testdata/fixtures/hosted-drafts-v1"

type hostedDraftFixtureManifest struct {
	Accepted []struct {
		Name         string   `json:"name"`
		WarningCodes []string `json:"warning_codes"`
	} `json:"accepted"`
	Rejected []struct {
		File      string `json:"file"`
		Status    int    `json:"status"`
		Code      string `json:"code"`
		IssueCode string `json:"issue_code"`
	} `json:"rejected"`
}

type hostedDraftFixtureSubmission struct {
	ContentSchemaVersion int           `json:"content_schema_version"`
	Content              draft.Content `json:"content"`
}

type hostedDraftFixtureReview struct {
	Revision                int64           `json:"revision"`
	State                   string          `json:"state"`
	DraftRevision           int64           `json:"draft_revision"`
	PolicyVersion           *string         `json:"policy_version"`
	Candidate               json.RawMessage `json:"candidate"`
	ConfirmationsNextCursor string          `json:"confirmations_next_cursor,omitempty"`
	Confirmations           []struct {
		Path          string    `json:"path"`
		ClaimKind     string    `json:"claim_kind"`
		DraftRevision int64     `json:"draft_revision"`
		PolicyVersion string    `json:"policy_version"`
		ValueDigest   string    `json:"value_digest"`
		Confirmed     bool      `json:"confirmed"`
		Reviewer      string    `json:"reviewer"`
		ConfirmedAt   time.Time `json:"confirmed_at"`
	} `json:"confirmations"`
}

type hostedDraftFixtureEnvelope struct {
	SchemaVersion        int    `json:"schema_version"`
	ContentSchemaVersion int    `json:"content_schema_version"`
	ID                   string `json:"id"`
	Revision             int64  `json:"revision"`
	Status               string `json:"status"`
	Origin               string `json:"origin"`
	Owner                struct {
		Issuer  string `json:"issuer"`
		Subject string `json:"subject"`
	} `json:"owner"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
	Content          draft.Content            `json:"content"`
	Review           hostedDraftFixtureReview `json:"review"`
	ApprovedRecipeID *string                  `json:"approved_recipe_id"`
}

type hostedDraftFixtureReceipt struct {
	ID                   string        `json:"id"`
	DraftID              string        `json:"draft_id"`
	SubmittedAt          time.Time     `json:"submitted_at"`
	AcceptedRevision     int64         `json:"accepted_revision"`
	ContentSchemaVersion int           `json:"content_schema_version"`
	SubmissionContent    draft.Content `json:"submission_content"`
	Links                struct {
		Receipt string `json:"receipt"`
		Draft   string `json:"draft"`
	} `json:"links"`
}

type hostedDraftFixtureResponse struct {
	APIVersion string                     `json:"api_version"`
	Draft      hostedDraftFixtureEnvelope `json:"draft"`
	Receipt    hostedDraftFixtureReceipt  `json:"receipt"`
	Validation draft.ValidationResult     `json:"validation"`
}

type hostedDraftFixtureApp struct {
	ContentSchemaVersion int           `json:"content_schema_version"`
	Content              draft.Content `json:"content"`
	DraftRevision        int64         `json:"draft_revision"`
	ReviewRevision       int64         `json:"review_revision"`
}

func readHostedDraftFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(hostedDraftFixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeHostedDraftFixture[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var value T
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		t.Fatal("fixture contains trailing JSON")
	}
	return value
}

// Compare semantic JSON as well as typed values: successful typed round trips
// alone could conceal an accidentally omitted Content field in the fixture type.
func equalHostedDraftJSON(t *testing.T, expected, actual []byte) {
	t.Helper()
	decode := func(b []byte) any {
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(expected), decode(actual)) {
		t.Fatalf("JSON content changed\nexpected: %s\nactual: %s", expected, actual)
	}
}

func marshalHostedDraftFixture(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHostedDraftFixtureRoundTrips(t *testing.T) {
	manifest := decodeHostedDraftFixture[hostedDraftFixtureManifest](t, readHostedDraftFixture(t, "manifest.json"))
	if len(manifest.Accepted) != 2 {
		t.Fatal("both incomplete and full fixtures are required")
	}
	for _, accepted := range manifest.Accepted {
		t.Run(accepted.Name, func(t *testing.T) {
			localRaw := readHostedDraftFixture(t, accepted.Name+".local.json")
			local := decodeHostedDraftFixture[draft.Draft](t, localRaw)
			if !draft.ValidID(local.ID) || local.SchemaVersion != 1 || local.Status != "draft" || local.Revision != 7 {
				t.Fatal("invalid synthetic local envelope")
			}
			equalHostedDraftJSON(t, localRaw, marshalHostedDraftFixture(t, local))
			submitRaw := readHostedDraftFixture(t, accepted.Name+".submit.json")
			submit, failure := checkHostedDraftFixtureSubmission(submitRaw)
			if failure.Code != "" {
				t.Fatalf("submission rejected: %+v", failure)
			}
			equalHostedDraftJSON(t, submitRaw, marshalHostedDraftFixture(t, submit))
			if !reflect.DeepEqual(local.Content, submit.Content) {
				t.Fatal("local content was changed during submission mapping")
			}
			hostedRaw := readHostedDraftFixture(t, accepted.Name+".hosted.json")
			hosted := decodeHostedDraftFixture[hostedDraftFixtureResponse](t, hostedRaw)
			equalHostedDraftJSON(t, hostedRaw, marshalHostedDraftFixture(t, hosted))
			appRaw := readHostedDraftFixture(t, accepted.Name+".app.json")
			app := decodeHostedDraftFixture[hostedDraftFixtureApp](t, appRaw)
			equalHostedDraftJSON(t, appRaw, marshalHostedDraftFixture(t, app))
			// Independent, pinned content goldens catch coordinated omissions from
			// all five fixture copies, which hop-to-hop equality alone cannot.
			for _, hop := range []struct {
				name    string
				content draft.Content
			}{
				{"local", local.Content},
				{"submit", submit.Content},
				{"app", app.Content},
				{"hosted draft", hosted.Draft.Content},
				{"hosted receipt", hosted.Receipt.SubmissionContent},
			} {
				t.Run(hop.name, func(t *testing.T) {
					assertHostedDraftContentGolden(t, accepted.Name, hop.content)
				})
			}
			if !reflect.DeepEqual(submit.Content, hosted.Draft.Content) ||
				!reflect.DeepEqual(submit.Content, hosted.Receipt.SubmissionContent) ||
				!reflect.DeepEqual(submit.Content, app.Content) {
				t.Fatal("submission, immutable receipt, hosted content and app content differ")
			}
			// App → API uses the content wrapper, not the app state/read tokens.
			resend, failure := checkHostedDraftFixtureSubmission(marshalHostedDraftFixture(t, hostedDraftFixtureSubmission{1, app.Content}))
			if failure.Code != "" || !reflect.DeepEqual(resend.Content, local.Content) {
				t.Fatal("app-to-API round trip lost content")
			}
			if accepted.Name == "full" {
				for _, hop := range []draft.Content{local.Content, submit.Content, hosted.Draft.Content, hosted.Receipt.SubmissionContent, app.Content, resend.Content} {
					assertHostedDraftPreservedFields(t, hop)
				}
			}
			validation := draft.Validate(submit.Content)
			if !reflect.DeepEqual(validation, hosted.Validation) || !validation.StructurallyValid || validation.Scope != "structural" {
				t.Fatal("hosted structural validation differs from shared core")
			}
			var warningCodes []string
			for _, warning := range validation.Warnings {
				warningCodes = append(warningCodes, warning.Code)
			}
			if !slices.Equal(warningCodes, accepted.WarningCodes) {
				t.Fatalf("warnings = %v, expected %v", warningCodes, accepted.WarningCodes)
			}
			assertHostedDraftFixtureEnvelope(t, hosted, app)
			// Model opening the accepted snapshot for editing via the real core
			// clone. A shallow clone now aliases receipt slices/pointers and fails.
			app.Content = hosted.Receipt.SubmissionContent.Clone()
			originalSnapshot := marshalHostedDraftFixture(t, hosted.Receipt)
			app.Content.Title = "Synthetic app-only edit"
			if len(app.Content.Sources) > 0 {
				app.Content.Sources[0].Label = "Synthetic changed source"
				app.Content.Estimates.TotalMinutes.Basis.Note = "Synthetic changed basis"
				app.Content.Ingredients[0].Quantity = "Synthetic edited quantity"
				app.Content.Instructions[0].PhaseBasis.SourceRef = "agent_source"
				*app.Content.PhaseEstimates.PrepAhead.ActiveMinutes.Value = 99
			}
			if reflect.DeepEqual(app.Content, hosted.Receipt.SubmissionContent) {
				t.Fatal("app edit must change editable content")
			}
			equalHostedDraftJSON(t, originalSnapshot, marshalHostedDraftFixture(t, hosted.Receipt))
		})
	}
}

// These hashes pin the entire canonical Content, independently of fixture files.
// A deliberate content change requires review of the content and golden together.
func assertHostedDraftContentGolden(t *testing.T, name string, c draft.Content) {
	t.Helper()
	golden, ok := map[string]string{
		"incomplete": "faaa71934c2baa0da95d2b672ef6c609d7ed4211e076976ae7cf1dd048d62ca9",
		"full":       "89f4215565014aafbeb173155514a19b3e02ee0c8d6b93ab8a3df0c79a7066da",
	}[name]
	if !ok {
		t.Fatalf("missing pinned content golden for %q", name)
	}
	canonical := canonicalHostedDraftFixtureJSON(t, c)
	if got := fmt.Sprintf("%x", sha256.Sum256(canonical)); got != golden {
		t.Fatalf("%s canonical content changed: SHA-256 = %s, want pinned %s\ncanonical content: %s", name, got, golden, canonical)
	}
}

// Focused assertions give descriptive failures for selected preservation rules;
// the independent goldens above pin every canonical field in both accepted cases.
func assertHostedDraftPreservedFields(t *testing.T, c draft.Content) {
	t.Helper()
	if len(c.Ingredients) < 4 || c.Ingredients[0].Quantity != "1 1/2" ||
		c.Ingredients[1].Quantity != "to taste" || c.Ingredients[2].Quantity != "0" || c.Ingredients[3].Quantity != "" {
		t.Fatal("original quantity text was lost")
	}
	if len(c.Sources) < 2 || c.Sources[0].Locator != "Example paragraph 2" || c.Sources[1].Locator != "Synthetic page 12" {
		t.Fatal("source locator was lost")
	}
	if c.Estimates == nil || c.Estimates.TotalMinutes == nil || c.Estimates.TotalMinutes.Basis == nil ||
		c.Estimates.TotalMinutes.Basis.SourceRef != "url_source" || c.Estimates.TotalMinutes.Basis.Note != "Synthetic citation; not checked." {
		t.Fatal("estimate source_ref or basis note was lost")
	}
	if len(c.Instructions) == 0 || c.Instructions[0].PhaseBasis == nil || c.Instructions[0].PhaseBasis.SourceRef != "book_source" ||
		c.Instructions[0].PhaseBasis.Note != "Synthetic citation; not checked." {
		t.Fatal("step source_ref or basis note was lost")
	}
	if c.PhaseEstimates == nil || c.PhaseEstimates.PrepAhead == nil || c.PhaseEstimates.PrepAhead.ActiveMinutes == nil ||
		c.PhaseEstimates.PrepAhead.ActiveMinutes.Value == nil || *c.PhaseEstimates.PrepAhead.ActiveMinutes.Value != 0 {
		t.Fatal("explicit zero estimate was lost")
	}
}

func assertHostedDraftFixtureEnvelope(t *testing.T, h hostedDraftFixtureResponse, app hostedDraftFixtureApp) {
	t.Helper()
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if h.APIVersion != "draft-v1" || h.Draft.SchemaVersion != 1 || h.Draft.ContentSchemaVersion != 1 ||
		h.Receipt.ContentSchemaVersion != 1 || app.ContentSchemaVersion != 1 {
		t.Fatal("version boundary changed")
	}
	if !uuid.MatchString(h.Draft.ID) || !uuid.MatchString(h.Receipt.ID) || h.Draft.ID == h.Receipt.ID || h.Receipt.DraftID != h.Draft.ID {
		t.Fatal("invalid server ID relationship")
	}
	if h.Draft.Owner.Issuer != "https://auth.preview.example.invalid" || h.Draft.Owner.Subject != "synthetic-owner-a" {
		t.Fatal("fixture must use the synthetic Preview owner")
	}
	if h.Draft.Status != "draft" || h.Draft.Origin != "agent" || h.Draft.ApprovedRecipeID != nil ||
		h.Draft.Revision != 1 || h.Receipt.AcceptedRevision != 1 || app.DraftRevision != 1 || app.ReviewRevision != 1 {
		t.Fatal("acceptance must create an unreviewed draft at hosted revision 1")
	}
	r := h.Draft.Review
	if r.Revision != 1 || r.DraftRevision != 1 || r.State != "not_started" || r.PolicyVersion != nil ||
		string(r.Candidate) != "null" || len(r.Confirmations) != 0 || r.Confirmations == nil {
		t.Fatal("submission must not confer review or approval")
	}
	if h.Receipt.Links.Receipt != "/draft-receipts/"+h.Receipt.ID || h.Receipt.Links.Draft != "/drafts/"+h.Draft.ID {
		t.Fatal("receipt links do not identify original owner-authenticated work")
	}
	for _, stamp := range []time.Time{h.Draft.CreatedAt, h.Draft.UpdatedAt, h.Receipt.SubmittedAt} {
		if stamp.IsZero() || stamp.Nanosecond() != 0 || stamp.Format(time.RFC3339)[19:] != "Z" {
			t.Fatal("timestamps must be whole-second UTC")
		}
	}
}

func TestHostedDraftFixtureUnknownZeroAndProvenance(t *testing.T) {
	incomplete := decodeHostedDraftFixture[hostedDraftFixtureApp](t, readHostedDraftFixture(t, "incomplete.app.json")).Content
	full := decodeHostedDraftFixture[hostedDraftFixtureApp](t, readHostedDraftFixture(t, "full.app.json")).Content
	if incomplete.Estimates != nil || incomplete.PhaseEstimates != nil || incomplete.Yield != "" ||
		incomplete.Title != "  Synthetic café draft 🥣  " {
		t.Fatal("unknown estimates/yield or original text were replaced")
	}
	if full.PhaseEstimates.PrepAhead.ActiveMinutes.Value == nil || *full.PhaseEstimates.PrepAhead.ActiveMinutes.Value != 0 {
		t.Fatal("explicit estimated zero became unknown")
	}
	for _, e := range []*draft.Estimates{full.Estimates, full.PhaseEstimates.PrepAhead, full.PhaseEstimates.PrepTonight, full.PhaseEstimates.CookTonight} {
		if e.ActiveMinutes == nil || e.TotalMinutes == nil || e.Attention == nil || e.Cleanup == nil || e.Variability == nil {
			t.Fatal("full fixture must exercise every estimate at each scope")
		}
	}
	if len(full.Sources) != 4 || len(full.Guidance) != 4 || len(full.Instructions) != 4 || full.Instructions[3].Phase != "" ||
		full.Instructions[0].PhaseBasis.SourceRef != "book_source" || full.Guidance[3].Basis.Kind != draft.BasisSourced || full.Guidance[3].Basis.SourceRef != "" {
		t.Fatal("provenance, unclassified step or unreferenced sourced claim was lost")
	}
	if full.Ingredients[0].Quantity != "1 1/2" || full.Ingredients[1].Quantity != "to taste" ||
		full.Ingredients[2].Quantity != "0" || full.Ingredients[3].Quantity != "" || full.Yield != "Serves 4–6 (original text)" {
		t.Fatal("string quantities or ambiguous original yield were coerced")
	}
	// Empty optional inputs have the local core's documented equivalence, while
	// canonical JSON and explicit zero keep distinct semantics and digest inputs.
	canonical, failure := checkHostedDraftFixtureSubmission([]byte(`{"content_schema_version":1,"content":{"title":"Synthetic"}}`))
	if failure.Code != "" {
		t.Fatal(failure)
	}
	equivalent, failure := checkHostedDraftFixtureSubmission([]byte(`{"content":{"sources":null,"title":"Synthetic","instructions":[],"ingredients":null,"guidance":[],"estimates":{}},"content_schema_version":1}`))
	if failure.Code != "" || !reflect.DeepEqual(canonical, equivalent) || hostedDraftFixtureDigest(t, canonical) != hostedDraftFixtureDigest(t, equivalent) {
		t.Fatal("equivalent optional forms must produce identical canonical snapshots")
	}
	withZero, failure := checkHostedDraftFixtureSubmission([]byte(`{"content_schema_version":1,"content":{"title":"Synthetic","estimates":{"active_minutes":{"value":0,"basis":{"kind":"agent_estimate"}}}}}`))
	if failure.Code != "" || hostedDraftFixtureDigest(t, canonical) == hostedDraftFixtureDigest(t, withZero) {
		t.Fatal("explicit zero must produce different snapshot content from unknown")
	}
}

func TestHostedDraftFixturePreviewLabelsAndLinks(t *testing.T) {
	context := decodeHostedDraftFixture[struct {
		Environment       string            `json:"environment"`
		WebOrigin         string            `json:"web_origin"`
		APIOrigin         string            `json:"api_origin"`
		DestinationLabel  string            `json:"destination_label"`
		AccountLabel      string            `json:"account_label"`
		OwnerIssuer       string            `json:"owner_issuer"`
		OwnerSubject      string            `json:"owner_subject"`
		OtherOwnerSubject string            `json:"other_owner_subject"`
		Links             map[string]string `json:"links"`
	}](t, readHostedDraftFixture(t, "preview-context.json"))
	if context.Environment != "Preview" || context.WebOrigin != "https://preview.example.invalid" ||
		context.APIOrigin != "https://api.preview.example.invalid" ||
		context.DestinationLabel != "Preview · https://preview.example.invalid" ||
		context.AccountLabel != "Account · synthetic-owner-a" || context.OwnerSubject != "synthetic-owner-a" ||
		context.OwnerIssuer != "https://auth.preview.example.invalid" || context.OtherOwnerSubject != "synthetic-owner-b" {
		t.Fatal("dry run must identify the exact synthetic Preview destination and separate owners")
	}
	base, err := url.Parse(context.WebOrigin)
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Links) != 4 {
		t.Fatal("dry run must include all exact receipt and draft links")
	}
	for _, name := range []string{"incomplete", "full"} {
		response := decodeHostedDraftFixture[hostedDraftFixtureResponse](t, readHostedDraftFixture(t, name+".hosted.json"))
		if response.Draft.Owner.Issuer != context.OwnerIssuer || response.Draft.Owner.Subject != context.OwnerSubject {
			t.Fatal("response and dry-run account disagree")
		}
		for kind, relative := range map[string]string{"receipt": response.Receipt.Links.Receipt, "draft": response.Receipt.Links.Draft} {
			link, err := url.Parse(relative)
			if err != nil || link.IsAbs() || link.Host != "" || base.ResolveReference(link).String() != context.Links[name+"_"+kind] {
				t.Fatalf("%s %s link does not resolve to the exact Preview test-pack URL", name, kind)
			}
		}
	}
}

func hostedDraftFixtureDigest(t *testing.T, s hostedDraftFixtureSubmission) [32]byte {
	t.Helper()
	return sha256.Sum256(append([]byte("draft-v1\n1\n"), canonicalHostedDraftFixtureJSON(t, s.Content)...))
}

// HD1 canonical JSON is independent of Go's struct order and HTML/JS escaping.
// Decode typed JSON to scalar values first, then encode by the contract's rules.
func canonicalHostedDraftFixtureJSON(t *testing.T, value any) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(marshalHostedDraftFixture(t, value)))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	quote := func(s string) {
		out.WriteByte('"')
		for _, r := range s {
			switch r {
			case '"', '\\':
				out.WriteByte('\\')
				out.WriteRune(r)
			case '\b':
				out.WriteString(`\b`)
			case '\t':
				out.WriteString(`\t`)
			case '\n':
				out.WriteString(`\n`)
			case '\f':
				out.WriteString(`\f`)
			case '\r':
				out.WriteString(`\r`)
			default:
				if r < 0x20 {
					fmt.Fprintf(&out, `\u%04x`, r)
				} else {
					out.WriteRune(r)
				}
			}
		}
		out.WriteByte('"')
	}
	var write func(any)
	write = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for key := range x {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			out.WriteByte('{')
			for i, key := range keys {
				if i > 0 {
					out.WriteByte(',')
				}
				quote(key)
				out.WriteByte(':')
				write(x[key])
			}
			out.WriteByte('}')
		case []any:
			out.WriteByte('[')
			for i, item := range x {
				if i > 0 {
					out.WriteByte(',')
				}
				write(item)
			}
			out.WriteByte(']')
		case string:
			quote(x)
		case json.Number:
			out.WriteString(x.String()) // Core Content contains only base-10 integers.
		case bool:
			fmt.Fprint(&out, x)
		case nil:
			out.WriteString("null")
		default:
			t.Fatalf("unexpected canonical JSON type %T", v)
		}
	}
	write(tree)
	return []byte(out.String())
}

func TestHostedDraftFixtureDigestCanonicalization(t *testing.T) {
	// A fixed vector catches Go's default escaping of <>& and U+2028/U+2029,
	// including nested key sorting and the exact short/control escape spelling.
	vector := map[string]any{"z": "<>&\u2028\u2029é", "a": map[string]any{"z": "\"\\/\b\t\n\f\r\x01", "a": 1}}
	want := "{\"a\":{\"a\":1,\"z\":\"\\\"\\\\/\\b\\t\\n\\f\\r\\u0001\"},\"z\":\"<>&\u2028\u2029é\"}"
	if got := string(canonicalHostedDraftFixtureJSON(t, vector)); got != want {
		t.Fatalf("canonical JSON = %q, want %q", got, want)
	}
	original, failure := checkHostedDraftFixtureSubmission(readHostedDraftFixture(t, "full.submit.json"))
	if failure.Code != "" {
		t.Fatal(failure)
	}
	baseline := hostedDraftFixtureDigest(t, original)
	t.Run("string order", func(t *testing.T) {
		changed := original
		changed.Content = original.Content.Clone()
		changed.Content.Title = "🥣 Synthetic grain bowl"
		if baseline == hostedDraftFixtureDigest(t, changed) {
			t.Fatal("string order must affect digest")
		}
	})
	t.Run("array order", func(t *testing.T) {
		changed := original
		changed.Content = original.Content.Clone()
		changed.Content.Ingredients[0], changed.Content.Ingredients[1] = changed.Content.Ingredients[1], changed.Content.Ingredients[0]
		if baseline == hostedDraftFixtureDigest(t, changed) {
			t.Fatal("array order must affect digest")
		}
	})
}

type hostedDraftFixtureFailure struct {
	Status    int
	Code      string
	IssueCode string
}

// Test-only raw wrapper decoder. Core owns all nested content rules. Keeping this
// out of non-test files makes this a fixture executable, not an API implementation.
func checkHostedDraftFixtureSubmission(raw []byte) (hostedDraftFixtureSubmission, hostedDraftFixtureFailure) {
	var result hostedDraftFixtureSubmission
	fail := func(status int, code, issue string) (hostedDraftFixtureSubmission, hostedDraftFixtureFailure) {
		return hostedDraftFixtureSubmission{}, hostedDraftFixtureFailure{status, code, issue}
	}
	if len(raw) > draft.MaxInputBytes {
		return fail(413, "input_too_large", "")
	}
	if !utf8.Valid(raw) {
		return fail(400, "invalid_request", "")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := decoder.Token(); err != nil || tok != json.Delim('{') {
		return fail(400, "invalid_request", "")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		tok, err := decoder.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return fail(400, "invalid_request", "")
		}
		if _, exists := fields[key]; exists {
			return fail(400, "duplicate_key", "")
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return fail(400, "invalid_request", "")
		}
		fields[key] = value
	}
	if tok, err := decoder.Token(); err != nil || tok != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return fail(400, "invalid_request", "")
	}
	for key := range fields {
		if key != "content_schema_version" && key != "content" {
			return fail(400, "unknown_field", "")
		}
	}
	version, ok := fields["content_schema_version"]
	content, hasContent := fields["content"]
	if !ok || !hasContent {
		return fail(400, "invalid_request", "")
	}
	version = bytes.TrimSpace(version)
	if !regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`).Match(version) {
		return fail(400, "invalid_type", "")
	}
	if !bytes.Equal(version, []byte("1")) {
		return fail(422, "unsupported_content_schema", "")
	}
	result.ContentSchemaVersion = 1
	var issues []draft.Issue
	result.Content, issues = draft.DecodeContent(content)
	if len(issues) > 0 {
		code := issues[0].Code
		if code == draft.IssueMalformedJSON {
			code = "invalid_request"
		}
		return fail(400, code, "")
	}
	validation := draft.Validate(result.Content)
	if !validation.StructurallyValid {
		return fail(400, "invalid_content", validation.Errors[0].Code)
	}
	return result, hostedDraftFixtureFailure{}
}

func TestHostedDraftFixtureRejections(t *testing.T) {
	manifest := decodeHostedDraftFixture[hostedDraftFixtureManifest](t, readHostedDraftFixture(t, "manifest.json"))
	seen := map[string]bool{}
	for _, rejected := range manifest.Rejected {
		t.Run(rejected.File, func(t *testing.T) {
			if seen[rejected.File] {
				t.Fatal("duplicate manifest entry")
			}
			seen[rejected.File] = true
			_, failure := checkHostedDraftFixtureSubmission(readHostedDraftFixture(t, rejected.File))
			if failure.Status != rejected.Status || failure.Code != rejected.Code || failure.IssueCode != rejected.IssueCode {
				t.Fatalf("got %+v; expected status=%d code=%s issue=%s", failure, rejected.Status, rejected.Code, rejected.IssueCode)
			}
		})
	}
	files, err := filepath.Glob(filepath.Join(hostedDraftFixtures, "rejected", "*.json"))
	if err != nil || len(files) != len(seen) {
		t.Fatal("every rejected fixture must be listed and checked")
	}
	required := []string{
		"array-content",
		"bom-prefix",
		"boolean-version",
		"case-basis-kind",
		"case-content-title",
		"case-content-title-pair",
		"case-estimate-active-minutes",
		"case-root-content",
		"case-variant",
		"claim-without-basis",
		"content-approval",
		"content-approved",
		"content-created_at",
		"content-id",
		"content-origin",
		"content-owner",
		"content-owner_issuer",
		"content-owner_subject",
		"content-receipt_id",
		"content-recipe_id",
		"content-review",
		"content-revision",
		"content-schema_version",
		"content-status",
		"content-updated_at",
		"control-character",
		"dangling-source-reference",
		"decimal-version",
		"duplicate-content",
		"duplicate-estimate-value",
		"duplicate-root",
		"duplicate-source-id",
		"empty-body",
		"estimate-without-value",
		"exponent-version",
		"huge-version",
		"ingredients-null-element",
		"ingredients-number-element",
		"invalid-utf8",
		"missing-content",
		"missing-version",
		"nul-character",
		"null-content",
		"null-version",
		"number-title",
		"oversized-body",
		"raw-control-byte",
		"root-approval",
		"root-approved",
		"root-array",
		"root-boolean",
		"root-created_at",
		"root-id",
		"root-null",
		"root-number",
		"root-origin",
		"root-owner",
		"root-owner_issuer",
		"root-owner_subject",
		"root-receipt_id",
		"root-recipe_id",
		"root-review",
		"root-revision",
		"root-schema_version",
		"root-status",
		"root-string",
		"root-updated_at",
		"string-content",
		"string-ingredients",
		"string-version",
		"trailing-json",
		"truncated-json",
		"unknown-field-null",
		"unpaired-surrogate",
		"unsupported-version",
		"version-array",
		"version-negative",
		"version-negative-zero",
		"version-object",
		"version-zero",
	}
	if len(required) != 80 {
		t.Fatalf("required rejection list must name all 80 cases; got %d", len(required))
	}
	requiredSet := map[string]bool{}
	for _, name := range required {
		requiredSet[name] = true
		if !seen["rejected/"+name+".json"] {
			t.Fatalf("missing required rejection fixture: %s", name)
		}
	}
	for file := range seen {
		if name := strings.TrimSuffix(strings.TrimPrefix(file, "rejected/"), ".json"); !requiredSet[name] {
			t.Fatalf("unlisted rejection fixture: %s", name)
		}
	}
	if !bytes.Contains(readHostedDraftFixture(t, "rejected/invalid-utf8.json"), []byte{0xff}) {
		t.Fatal("invalid UTF-8 fixture must retain the literal raw byte, not an escape or replacement")
	}
}

func TestHostedDraftFixtureWrapperUTF8(t *testing.T) {
	// Keep the invalid byte outside content: core never sees this version field.
	// Without the wrapper's raw UTF-8 check, encoding/json accepts/replaces the
	// byte and the version type check returns invalid_type, not invalid_request.
	for _, tc := range []struct {
		name string
		raw  string
		code string
	}{
		{"valid UTF-8 wrong version type", "{\"content_schema_version\":\"x\",\"content\":{\"title\":\"Synthetic\"}}", "invalid_type"},
		{"invalid UTF-8 in wrapper", "{\"content_schema_version\":\"\xff\",\"content\":{\"title\":\"Synthetic\"}}", "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, failure := checkHostedDraftFixtureSubmission([]byte(tc.raw))
			if failure != (hostedDraftFixtureFailure{Status: 400, Code: tc.code}) {
				t.Fatalf("got %+v; want status=400 code=%s", failure, tc.code)
			}
		})
	}
}
