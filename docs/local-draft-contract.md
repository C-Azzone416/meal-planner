# Local Draft Contract

Status as of 2026-11-09 is contract v1 (`schema_version` 1) with a separate #23 local-tool restore candidate. Resolves the decisions in issue #20 for epic #19.

> **Release boundary.** The November web release retains the shared draft core and command scaffolds. The #23 candidate restores the executable local tools and store for review and #25 host acceptance. [Local draft workflow](local-workflow.md)

This document defines the shared contract for Go core (#21), local store (#22), MCP adapter (#23), CLI (#24) and host verification (#25). All examples are synthetic.

Local drafts are designed as a **staging workflow** independent of hosted API and database setup. The long-term direction is unchanged: structured recipes live in hosted Postgres behind the authenticated Go API (see [architecture](architecture.md)). Nothing here replaces that, and nothing here is a hosted save.

## 1. Scope and non-goals

The local slice supports creating, reading, listing, updating and validating a recipe draft, plus reading server status. A stdio MCP server and a small CLI use the same Go core.

Not in this slice, and not implied by anything below:

- delete, archive, publish, approve, hosted save, sync, import or export operations;
- fetching source URLs or any network access; OneNote/Microsoft Graph; calls to an LLM;
- weekly planning, random selection, serving scaling, ingredient normalization or consolidation, shopping or prep lists, scheduling, cooking UI, feedback;
- a reviewed/approved state (see decision 2);
- HTTP/hosted MCP, OAuth, MCP prompts or resources.

## 2. Decision register

Status values:

- **Decided** — settled. "Origin" says who settled it: *PM/user* (instruction recorded here), or *Contract* (engineering choice within the agreed scope, made in this document).
- **Open — blocking X** — not settled; X must not be built or claimed until it is. Open items never silently become requirements.

### 2.1 The five questions from #20

| # | Question | Decision (summary) | Status | Origin |
| --- | --- | --- | --- | --- |
| 1 | Draft content and validity | `title` is the only required field. Everything else may be empty or absent. Errors block a save; warnings are saved and flagged. | Decided | PM/user (incomplete drafts allowed); Contract (exact minimum) |
| 2 | Review boundary | Every saved object is a draft. No reviewed/approved state exists. Validation is structural only. | Decided | PM/user |
| 3 | Recipe breakdown and estimates | Optional. Unknown (absent) is distinct from zero. Every estimate and every prep-ahead/safety claim carries a basis marker. | Decided | PM/user (rules); Contract (representation) |
| 4 | Local persistence contract | Versioned JSON, one file per draft, under a data root. Atomic write plus optimistic revision plus cross-process lock. | Decided | PM/user (format, locking, permissions); Contract (details) |
| 5 | Update contract | Whole-draft replacement with an expected revision. Create is not idempotent. | Decided | PM/user (shape); Contract (idempotency argument) |

#### Decision 1 — Draft content and validity

**Decision.** The saved representation is the [`Draft`](#41-go-declarations) object. The minimum required to save is a non-blank `title` that satisfies the field rules; the rules for every field are in [section 4](#4-data-model-schema-version-1) and [section 5](#5-validation). `ingredients`, `instructions`, `sources`, `notes` and all estimates may be empty or absent and still save. Missing recipe substance is reported as **warnings** (`no_ingredients`, `no_instructions`, `no_sources`). Anything that makes the object malformed or ambiguous (wrong type, unknown field, out-of-range value, dangling reference, claim without a basis, over a size bound) is an **error** and blocks the save with no write.

**Rationale.**

- A draft is a work in progress that an agent builds across several calls. Forcing complete content up front pushes agents to invent filler to get past validation, which is worse than an honest empty list with a warning.
- `title` is the one field every list and inspection view needs to identify a draft, so it is the one thing worth blocking on.
- Blocking only on malformed or ambiguous data keeps stored files unambiguous for the future hosted mapping, while warnings keep the gaps visible to the agent and the user.
- Unknown fields are errors rather than ignored so that nothing the caller sent is silently dropped.

**Status.** Decided.

#### Decision 2 — Review boundary

**Decision.** Every saved object is a draft. The object carries a server-assigned, read-only `status` whose only value is `"draft"`. There is no reviewed or approved state, no transition operation, and no field a client can set to claim one. Validation is **structural only**: every validation result carries `scope: "structural"` and a fixed notice that it is not food-safety verification, recipe review or approval. `structurally_valid: true` means "the object is well-formed", never "the recipe is correct or safe".

**Rationale.** The product plan leaves open which fields need explicit approval and what the food-safety source policy is. Building a review state now would invent those rules. Keeping `status` as an explicit constant makes the draft nature visible in every output and gives a future reviewed state an obvious place to be introduced behind a `schema_version` bump.

**Status.** Decided. The review transition itself is **Open — blocking** any reviewed recipe, hosted draft save or planning from drafts (see [section 15](#15-open-items)).

#### Decision 3 — Recipe breakdown and estimates

**Decision.**

- The three-part breakdown is represented by an optional `phase` on each instruction step (`prep_ahead`, `prep_tonight`, `cook_tonight`), not by three separate step lists. A step with no `phase` is unclassified. This avoids duplicating steps and keeps one authoritative order.
- Time, attention, cleanup and variability estimates are optional, at recipe level (`estimates`) and optionally per phase (`phase_estimates`, same shape).
- **Unknown is not zero.** An estimate that is not provided is absent and means "unknown". `{"value": 0, ...}` means "estimated to be zero". There is no sentinel value.
- Each estimate carries its own `basis`; each `prep_ahead` step carries a `phase_basis`; each `guidance` entry (storage, prep-ahead, food-safety statements) carries a `basis`. A basis has a `kind`:
  - `agent_estimate` — the agent's own suggestion, not checked against anything;
  - `sourced` — the submitter says it comes from a source, optionally naming a `source_ref` that must be the `id` of an entry in the draft's `sources`.
- `sourced` is a claim by the submitter. The application does not fetch or verify the source, so `sourced` never means verified.
- A claim without a basis is an **error** (`basis_required`). A `sourced` basis without `source_ref` is a **warning** (`sourced_without_reference`). The estimates and claims themselves are never required.

**Rationale.**

- The plan requires separate active and total time, attention, cleanup and variability, and requires prep-ahead to be a visible, unverified suggestion until a review policy exists. A per-claim basis is the smallest mechanism that keeps those distinctions visible in every read.
- Per-estimate bases (rather than one per draft) matter because source recipes often state total time but not active time, so mixed bases within one recipe are normal.
- Requiring a basis only when a claim is present satisfies "do not require unsupported assertions to create a draft" while preventing an unlabeled claim from looking authoritative.
- A phase tag on steps plus per-phase estimates is enough to compute the weekend and weeknight effort budgets later without this slice deciding any planning rule.

**Status.** Decided.

#### Decision 4 — Local persistence contract

**Decision.** Versioned JSON files, one per draft, under a data root; see [section 10](#10-local-store-internallocalstore) for the complete contract. In brief:

- Default data root is `os.UserConfigDir()/mealplanner`, overridden by `--data-dir`, then by `MEALPLANNER_DATA_DIR`. Precedence: flag, then environment, then default. Relative paths are **rejected**.
- IDs are `drf_` plus 20 lowercase hex characters, validated by regex. A create always makes a new draft and never overwrites.
- Directories are `0700`, files `0600`. Writes use temp file in the same directory, `fsync`, atomic rename, directory `fsync`.
- An unreadable, corrupt or unsupported-schema file is reported and skipped by list, never deleted or rewritten.
- Bounds and ordering are in [section 6](#6-limits) and [section 7](#7-identifiers-revisions-ordering-and-paging).
- Pure Go standard library, no cgo: `CGO_ENABLED=0` cross-compilation for windows/amd64, linux/arm64 and darwin/arm64 must keep passing.

**Rationale.** JSON files need no new dependency, build cleanly without cgo, can be inspected by a person, and map directly onto the shared `Draft` type that a future hosted draft API can reuse as a request body. An embedded database would add a dependency and cgo/portability risk for a few hundred small records. One file per draft keeps corruption local to one draft and makes atomic replacement a single rename.

**Status.** Decided.

#### Decision 5 — Update contract

**Decision.**

- **Whole-draft replacement** with a required `expected_revision`. The caller sends the complete content; the stored content is replaced; `revision` increments by one. There is no patch format.
- A mismatched revision is a `conflict` error with `code: "revision_mismatch"` and the `current_revision`; nothing is written.
- The store's atomic rename is the commit point. Once it has happened the operation has succeeded: a later failure (directory `fsync`, lock release, context cancellation) is logged to stderr without payload and does not turn the call into an error ([section 10.3](#103-writes)).
- **Create is not idempotent.** Every successful create makes a new draft with a new ID, even for identical content. There is no client-supplied idempotency key.

**Rationale.**

- *Replacement vs patch.* Patches (JSON Patch/merge) need rules for list item identity, deletes, ordering and null-versus-absent, and null-versus-absent is exactly what this contract uses to mean "unknown". Replacement is unambiguous, needs no merge semantics, and the draft is small (bounded at 128 KiB). The cost is that an agent must resend the whole draft, which `draft_get` makes easy.
- *Why not an idempotency key.* A correct key requires persisting keys, deciding their lifetime and scope, and defining what happens when the same key arrives with different content. That is machinery for a single-user local tool, and a half-implemented key would be a false promise of idempotence. The mitigation is documentation: after an ambiguous create failure (timeout, I/O error), call `draft_list` before retrying.
- *Consequence to surface.* Because there is no delete in this slice, an accidental duplicate draft stays until the user removes the file by hand from the drafts directory. This is a deliberate trade-off, recorded in [section 15](#15-open-items).

**Status.** Decided.

### 2.2 Additional decisions

| Decision | Status | Origin |
| --- | --- | --- |
| Target platform is macOS (darwin/arm64) now. Code stays portable; cross-compile for windows/amd64, linux/arm64, darwin/arm64 with `CGO_ENABLED=0` must pass. | Decided | PM/user |
| Data root: flag, then env var `MEALPLANNER_DATA_DIR`, then default. Relative paths rejected. | Decided | PM/user (precedence); Contract (rejection, env var name) |
| Optimistic revision plus cross-process advisory lock (`flock`). | Decided | PM/user |
| Package layout in [section 12](#12-package-layout-and-parallel-work). | Decided | PM/user (core, store, cmd); Contract (`internal/mcpadapter`) |
| MCP via the official Go SDK `github.com/modelcontextprotocol/go-sdk`; version pinned in #23. | Decided | PM/user |
| Tool names, CLI spelling, exit codes, error categories, limits. | Decided | Contract |
| Reviewed/approved state and its transition. | Open — blocking reviewed recipes, hosted draft save, planning from drafts | PM |
| Food-safety source and review policy for prep-ahead/storage guidance. | Open — blocking any display of guidance as reviewed or verified | PM |
| Mapping of local drafts to the hosted draft API and Postgres model. | Open — blocking hosted save | PM |

## 3. Conventions

- JSON is UTF-8. Field names are `snake_case`. All timestamps are RFC 3339 in UTC with a `Z` suffix and whole-second precision, for example `2026-11-04T09:30:00Z`.
- **Strings.** Stored exactly as supplied: no trimming, case folding or Unicode normalization. Length limits count Unicode code points (runes). A required text field must contain at least one non-whitespace character. Control characters (including C0, DEL and C1) and the bidirectional embedding, override and isolate controls LRE, RLE, PDF, LRO, RLO, LRI, RLI, FSI and PDI are rejected in content. The implicit directional marks LRM (U+200E), RLM (U+200F) and ALM (U+061C) are allowed in text fields. Only `notes`, instruction `text`, and `guidance.text` may contain `\n`, `\r` and `\t`; single-line fields also reject U+2028, U+2029 and U+FEFF (BOM). These three characters remain allowed in multiline text. Ordinary right-to-left text, ZWSP, ZWNJ, ZWJ and SHY are allowed in free-text fields. The same rules apply at write time and on stored reads; enums, identifiers and URLs also retain their own constraints.
- **Lists.** On input, an absent field, `null` and `[]` are equivalent. The service normalizes nil lists to `[]` on input and again after decoding a stored file, so output (API results and stored files) always emits list fields as `[]` and never `null` or omitted. `ListPage.Drafts` and `ListResult.Drafts` are never nil.
- **Optional scalars and objects.** Absent and `null` are equivalent on input. Optional strings are omitted from output when empty. Optional objects (`estimates`, `phase_estimates`, `phase_basis`) are omitted from output when absent. An `Estimates` or `PhaseEstimates` object with every field absent normalizes to nil, so it is omitted too.
- **Unknown fields** anywhere in input are an error (`unknown_field`), as are duplicate source IDs. Field names are matched **exact-case**: `Title` is an unknown field, not `title`. A **duplicate key** at any object level is an error (category `invalid_argument`, issue code `duplicate_key`); it is found by a token-level pre-pass over the raw JSON, because plain `encoding/json` silently keeps the last value. Server-assigned fields (`schema_version`, `id`, `revision`, `status`, `created_at`, `updated_at`) are not accepted in input content.
- **Issue paths** are [JSON Pointers](https://www.rfc-editor.org/rfc/rfc6901) into the *content* object, for example `/ingredients/2/text`. Whole-object issues use `/`. Decode-stage issues (`malformed_json`, `duplicate_key`, `unknown_field`, `invalid_type`) carry a path when the decoder can determine one, otherwise `/`; tests must assert their `code`, not their `path`.

## 4. Data model (schema version 1)

`schema_version` is the version of this data contract (an integer, currently `1`). It is unrelated to the application/binary version (`internal/core/version.Version`, a build string such as `dev` or `v0.3.0`). Both are reported separately by `server_status`.

A **draft** is the server-assigned envelope plus the client-supplied **content**. Create and update accept content only.

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `schema_version` | integer | server | Always `1` in this contract. |
| `id` | string | server | `drf_` + 20 lowercase hex. |
| `revision` | integer | server | Starts at `1`, increments by 1 per successful update. |
| `status` | string | server | Always `"draft"`. |
| `created_at` | timestamp | server | Set at create, never changes. |
| `updated_at` | timestamp | server | Set at create and at every update. |
| `title` | string | **yes** | 1–200 runes, single line. |
| `yield` | string | no | Free text such as `Serves 4-6`, up to 100 runes, single line. Not parsed, not scaled. |
| `ingredients` | list of Ingredient | no | Up to 100. Empty → warning `no_ingredients`. |
| `instructions` | list of Step | no | Up to 100, in order. Empty → warning `no_instructions`. |
| `notes` | string | no | Up to 4000 runes. Free text. |
| `sources` | list of Source | no | Up to 20 provenance entries. Empty → warning `no_sources`. |
| `estimates` | Estimates | no | Whole-recipe estimates. |
| `phase_estimates` | PhaseEstimates | no | Estimates per phase. |
| `guidance` | list of Guidance | no | Up to 20 storage/prep-ahead/safety statements, each with a basis. |

**Ingredient** — `text` required (1–300 runes, single line): the ingredient as written. Optional single-line strings `quantity` (≤50, kept as text so `1 1/2` and `to taste` survive), `unit` (≤50), `name` (≤200), `note` (≤300). None of these are interpreted, normalized or validated against a unit list.

**Step** — `text` required (1–2000 runes). Optional `phase` (`prep_ahead`, `prep_tonight`, `cook_tonight`). Optional `phase_basis` (a Basis): **required when `phase` is `prep_ahead`**, because that tag is a claim that the step can be done ahead; allowed but not required for the other phases; rejected (`invalid_value`) when `phase` is absent.

**Source** — provenance. `id` required, unique within the draft, matching `^[a-z][a-z0-9_-]{0,31}$`. `kind` required: `url`, `publication`, `user_supplied`, `agent_generated`. Optional single-line `label` (≤200) and `locator` (≤200, a page, section or location within the source). `url` (≤2048 bytes) is **required** when `kind` is `url` and **not allowed** otherwise. For kinds other than `url`, at least one of `label` or `locator` is required. A URL must parse as absolute with scheme `http` or `https`, must have a host, and must not contain userinfo (credentials). A URL is data: nothing in this system ever fetches it.

**Basis** — `kind` required (`agent_estimate` or `sourced`). Optional `source_ref`: if present it must equal the `id` of a Source in the same draft (`unknown_source_ref` otherwise). Optional single-line `note` (≤300).

**Estimates** — all fields optional; each present field is an object `{"value": ..., "basis": {...}}` where both members are required inside the object:

| Field | `value` | Meaning |
| --- | --- | --- |
| `active_minutes` | integer 0–10080 | Hands-on time. |
| `total_minutes` | integer 0–10080 | Time until ready, including unattended time. |
| `attention` | `low`, `medium`, `high` | How much attention the cooking needs. |
| `cleanup` | `low`, `medium`, `high` | Dishes and mess. |
| `variability` | `low`, `medium`, `high` | How much the timing may vary. |

The levels are ordinal labels, not measurements. An absent field is "unknown"; `{"value": 0, ...}` is "estimated zero". An empty `estimates` object is the same as none.

**PhaseEstimates** — optional `prep_ahead`, `prep_tonight`, `cook_tonight`, each an Estimates.

**Guidance** — `topic` required (`prep_ahead`, `storage`, `food_safety`, `other`); `text` required (1–1000 runes); `basis` required. A statement the submitter wants to make about storage duration, safe prep-ahead or food safety belongs here, labeled with its basis; it is never inferred or validated by the application.

### 4.1 Go declarations

Normative for `internal/core/draft` (#21 owns the files; #22 may code against these now). Field names, JSON tags and types are part of the contract. Constants for limits are listed in [section 6](#6-limits).

```go
package draft

import "time"

const (
	SchemaVersion    = 1
	StatusDraft      = "draft"
	ValidationScope  = "structural"
	ValidationNotice = "Structural check only. This is not food-safety verification, recipe review, or approval."
)

// SupportedSchemaVersions lists the versions this build can read.
var SupportedSchemaVersions = []int{SchemaVersion}

// Draft is the stored and returned object: server-assigned envelope plus Content.
// Content is embedded so the JSON form is flat.
type Draft struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	Revision      int64     `json:"revision"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Content
}

// Content is the client-supplied part of a draft (create/update/validate input).
type Content struct {
	Title          string          `json:"title"`
	Yield          string          `json:"yield,omitempty"`
	Ingredients    []Ingredient    `json:"ingredients"`
	Instructions   []Step          `json:"instructions"`
	Notes          string          `json:"notes,omitempty"`
	Sources        []Source        `json:"sources"`
	Estimates      *Estimates      `json:"estimates,omitempty"`
	PhaseEstimates *PhaseEstimates `json:"phase_estimates,omitempty"`
	Guidance       []Guidance      `json:"guidance"`
}

type Ingredient struct {
	Text     string `json:"text"`
	Quantity string `json:"quantity,omitempty"`
	Unit     string `json:"unit,omitempty"`
	Name     string `json:"name,omitempty"`
	Note     string `json:"note,omitempty"`
}

type Phase string

const (
	PhasePrepAhead   Phase = "prep_ahead"
	PhasePrepTonight Phase = "prep_tonight"
	PhaseCookTonight Phase = "cook_tonight"
)

type Step struct {
	Text       string `json:"text"`
	Phase      Phase  `json:"phase,omitempty"`
	PhaseBasis *Basis `json:"phase_basis,omitempty"`
}

type BasisKind string

const (
	BasisAgentEstimate BasisKind = "agent_estimate"
	BasisSourced       BasisKind = "sourced"
)

type Basis struct {
	Kind      BasisKind `json:"kind"`
	SourceRef string    `json:"source_ref,omitempty"`
	Note      string    `json:"note,omitempty"`
}

type SourceKind string

const (
	SourceURL            SourceKind = "url"
	SourcePublication    SourceKind = "publication"
	SourceUserSupplied   SourceKind = "user_supplied"
	SourceAgentGenerated SourceKind = "agent_generated"
)

type Source struct {
	ID      string     `json:"id"`
	Kind    SourceKind `json:"kind"`
	Label   string     `json:"label,omitempty"`
	URL     string     `json:"url,omitempty"`
	Locator string     `json:"locator,omitempty"`
}

type Level string

const (
	LevelLow    Level = "low"
	LevelMedium Level = "medium"
	LevelHigh   Level = "high"
)

// Value is a pointer so a missing value is detectable (and reported as
// "required") rather than silently decoding to zero.
type MinutesEstimate struct {
	Value *int   `json:"value"`
	Basis *Basis `json:"basis"`
}

type LevelEstimate struct {
	Value Level  `json:"value"`
	Basis *Basis `json:"basis"`
}

// A nil pointer means "unknown". It is never the same as a present zero.
type Estimates struct {
	ActiveMinutes *MinutesEstimate `json:"active_minutes,omitempty"`
	TotalMinutes  *MinutesEstimate `json:"total_minutes,omitempty"`
	Attention     *LevelEstimate   `json:"attention,omitempty"`
	Cleanup       *LevelEstimate   `json:"cleanup,omitempty"`
	Variability   *LevelEstimate   `json:"variability,omitempty"`
}

type PhaseEstimates struct {
	PrepAhead   *Estimates `json:"prep_ahead,omitempty"`
	PrepTonight *Estimates `json:"prep_tonight,omitempty"`
	CookTonight *Estimates `json:"cook_tonight,omitempty"`
}

type GuidanceTopic string

const (
	TopicPrepAhead  GuidanceTopic = "prep_ahead"
	TopicStorage    GuidanceTopic = "storage"
	TopicFoodSafety GuidanceTopic = "food_safety"
	TopicOther      GuidanceTopic = "other"
)

type Guidance struct {
	Topic GuidanceTopic `json:"topic"`
	Text  string        `json:"text"`
	Basis *Basis        `json:"basis"`
}

// Issue is one validation finding. Used for both errors and warnings.
type Issue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidationResult is shared by core, CLI and MCP. Scope is always
// ValidationScope and Notice is always ValidationNotice.
type ValidationResult struct {
	Scope             string  `json:"scope"`
	StructurallyValid bool    `json:"structurally_valid"`  // len(Errors) == 0
	Errors            []Issue `json:"errors"`              // never nil
	Warnings          []Issue `json:"warnings"`            // never nil
	Truncated         bool    `json:"truncated,omitempty"` // true if either list was capped
	Notice            string  `json:"notice"`
}

// Summary is the list view of a draft.
type Summary struct {
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	Status             string    `json:"status"`
	Revision           int64     `json:"revision"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	IngredientCount    int       `json:"ingredient_count"`
	InstructionCount   int       `json:"instruction_count"`
	SourceCount        int       `json:"source_count"`
	AgentEstimateCount int       `json:"agent_estimate_count"` // bases of kind agent_estimate anywhere in the draft
}

type SkippedEntry struct {
	ID       string   `json:"id,omitempty"` // present when the file name was a well-formed draft file name
	Category Category `json:"category"`     // corrupt or unsupported_schema or io
	Message  string   `json:"message"`      // never contains paths or recipe text
}

type ListOptions struct {
	Limit  int // 0 means DefaultListLimit; negative or > MaxListLimit is invalid_limit
	Offset int // >= 0; negative is invalid_offset
}

type ListPage struct {
	Drafts       []Summary      `json:"drafts"` // never nil
	Total        int            `json:"total"`  // readable drafts in the store (may be below the MaxStoredDrafts count)
	Offset       int            `json:"offset"`
	Limit        int            `json:"limit"`
	HasMore      bool           `json:"has_more"`
	Skipped      []SkippedEntry `json:"skipped"`       // at most MaxSkippedReported entries
	SkippedTotal int            `json:"skipped_total"` // all unreadable entries
}

type WriteResult struct {
	Draft      Draft            `json:"draft"`
	Validation ValidationResult `json:"validation"` // Errors is always empty here; warnings only
}
```

## 5. Validation

`Validate(Content) ValidationResult` is a pure function: no store, no clock, no I/O. `DecodeContent([]byte) (Content, []Issue)` is the single strict decoder (field names exact-case, unknown fields rejected, duplicate keys at any level rejected as `duplicate_key` by a token-level pre-pass before `encoding/json` decodes, exactly one JSON value, no trailing data, empty input is `malformed_json`) used by CLI file/stdin input and by the MCP adapter, so that both produce identical results for identical input. The MCP adapter receives the `draft` argument as raw JSON and passes it unmodified to `DecodeContent`; it never decodes it into a typed struct first. After decoding, nil lists are normalized to `[]` and all-absent `Estimates`/`PhaseEstimates` to nil. If decoding fails, the decode issue is the whole error list and semantic validation is skipped (the decoder reports at most one issue). Otherwise `Validate` reports **all** findings in schema order (field order, then list index), capped at 50 errors and 50 warnings (`truncated: true` if capped).

`DecodeContent` as built: it also rejects raw input over `MaxInputBytes` as `input_too_large`, as defense in depth for callers that did not check; a root that is not a JSON object, or is `null`, is `invalid_type` at `/`; invalid UTF-8, and nesting more than 64 levels below the root, are `malformed_json`; an unpaired surrogate escape (a `\uD800`–`\uDFFF` escape that does not form a valid pair) is `invalid_characters` at the field's path, because `encoding/json` would otherwise silently replace it with U+FFFD. Syntax errors, duplicate keys and nesting depth are found while the document is parsed, so they are reported before unknown fields, wrong types and surrogate escapes, which are then reported first-in-document-order. `draft.ValidateRaw(raw []byte) ValidationResult` runs `DecodeContent` and then `Validate`; on a decode failure it returns a result whose only error is the decode issue, with empty warnings and `scope` and `notice` set. The CLI `draft validate` and the MCP `draft_validate` both use it, so they report identical results for identical input.

### 5.1 Errors (block the save; nothing is written)

| Code | Meaning |
| --- | --- |
| `malformed_json` | Not parseable as a single JSON value (including empty input, trailing data), not valid UTF-8, or nested more than 64 levels below the root. |
| `input_too_large` | Raw input exceeds 256 KiB (checked by CLI input readers before decoding, and again by `DecodeContent`). |
| `duplicate_key` | The same key appears twice in one JSON object, at any level. The issue category is `invalid_argument`. The core uses it only as an issue code inside `validation_failed`, for a duplicate key inside the draft content; the MCP adapter also uses it as a top-level code (section 8). |
| `unknown_field` | A field not in this schema (names are exact-case), including server-assigned fields. |
| `invalid_type` | Wrong JSON type for a field, or a root that is not an object (including `null`; reported at `/`). |
| `required` | A required field is missing or blank (`title`, `ingredients[].text`, `instructions[].text`, `sources[].id`/`kind`, `sources[].url` for kind `url`, a label or locator for other source kinds, estimate `value`, `basis.kind`, `guidance[].text`/`topic`). |
| `invalid_value` | Value outside its enum, range or pattern; bad URL; `phase_basis` without `phase`; `url` on a non-`url` source. |
| `invalid_characters` | Disallowed control characters, bidirectional embedding/override/isolate controls, U+2028/U+2029/BOM in a single-line field, invalid UTF-8 in a string passed to `Validate` directly, or an unpaired surrogate escape in raw input. Invalid UTF-8 in raw input is `malformed_json`. |
| `too_long` | A string exceeds its limit. |
| `too_many_items` | A list exceeds its limit. |
| `draft_too_large` | `json.Marshal` (Go default, compact) of the decoded, normalized `Content` exceeds 128 KiB. |
| `duplicate_id` | Two sources share an `id` (reported at the second). |
| `unknown_source_ref` | A `basis.source_ref` does not match any source `id`. |
| `basis_required` | An estimate, a `prep_ahead` step or a guidance entry has no basis. |

### 5.2 Warnings (the draft is saved, and the warnings are returned)

| Code | Condition |
| --- | --- |
| `no_ingredients` | `ingredients` is empty. |
| `no_instructions` | `instructions` is empty. |
| `no_sources` | `sources` is empty. |
| `sourced_without_reference` | A basis has `kind: sourced` and no `source_ref`. |
| `active_exceeds_total` | An Estimates object has both minute values and `active_minutes` > `total_minutes`. |
| `partial_phase_assignment` | Some instruction steps have a `phase` and others do not. |

Warnings never block a save and never change any stored value. They are recomputed on demand and are not stored in the draft file. Adding a new error code or tightening a rule is a contract change (see section 11.3 for versioning).

## 6. Limits

All limits are enforced by core validation and exported as constants from `internal/core/draft` so adapters and tests share them. "Runes" are Unicode code points.

| Constant | Value | Applies to |
| --- | --- | --- |
| `MaxDraftBytes` | 131072 (128 KiB) | Length of `json.Marshal` (Go default, compact) of the decoded, normalized `Content`. |
| `MaxInputBytes` | 262144 (256 KiB) | Raw bytes the CLI reads from a file or stdin. Reading stops at one byte over; over-limit is `input_too_large`. |
| `MaxStoredDrafts` | 1000 | Every file in `drafts/` whose name matches the draft-file pattern, readable or not, counted under the lock. A create at or beyond this fails `limit_exceeded`. `total` in a list counts readable drafts only, so it may be below the count this cap uses. |
| `MaxTitleRunes` | 200 | `title` |
| `MaxYieldRunes` | 100 | `yield` |
| `MaxNotesRunes` | 4000 | `notes` |
| `MaxIngredients` | 100 | `ingredients` items |
| `MaxInstructions` | 100 | `instructions` items |
| `MaxSources` | 20 | `sources` items |
| `MaxGuidance` | 20 | `guidance` items |
| `MaxIngredientText` | 300 | `ingredients[].text`, `.note` |
| `MaxIngredientField` | 200 | `ingredients[].name` |
| `MaxIngredientShort` | 50 | `ingredients[].quantity` and `.unit` |
| `MaxStepRunes` | 2000 | `instructions[].text` |
| `MaxGuidanceRunes` | 1000 | `guidance[].text` |
| `MaxLabelRunes` | 200 | `sources[].label`, `.locator` |
| `MaxURLBytes` | 2048 | `sources[].url` |
| `MaxNoteRunes` | 300 | `basis.note` |
| `MaxMinutes` | 10080 | minute estimate values (7 days) |
| `DefaultListLimit` / `MaxListLimit` | 25 / 100 | `draft_list` and `draft list` page size |
| `MaxSkippedReported` | 20 | skipped entries returned per list call |
| `MaxIssuesReported` | 50 | errors and warnings each, per validation |
| `MaxStoredFileBytes` | 524288 (512 KiB) | Store read cap: a file larger than this is treated as `corrupt` without being fully read. A write whose encoded file would exceed it fails `internal`/`encoded_too_large`. |

## 7. Identifiers, revisions, ordering and paging

- **ID format.** `drf_` followed by 20 lowercase hexadecimal characters (80 random bits from `crypto/rand`), validated everywhere an ID enters the system by `^drf_[0-9a-f]{20}$`. Lowercase only, so IDs are not aliased on case-insensitive filesystems such as default macOS volumes. The ID contains no path separators, so a validated ID can only name a file directly inside the drafts directory. The service generates IDs; clients never supply them on create.
- **Collisions.** The store's `Create` fails with `conflict`/`already_exists` if the file exists; the service regenerates the ID and retries up to 5 times, then returns `internal`/`id_generation_failed`. Create never overwrites.
- **Revision.** `revision` is an integer starting at 1. A successful update sets it to `expected_revision + 1`. Revisions are never reused or decremented. An update with identical content still increments the revision and `updated_at` (simple and predictable; no no-op detection). The rename in the store is the commit point: an error after it (directory `fsync`, lock release, context cancellation) does not fail the operation; it is logged to stderr without payload and the call returns success with the new revision.
- **List order.** `updated_at` descending, then `id` ascending. The service sorts, not the store, so every `Store` implementation yields the same order.
- **Paging.** In the core, `Limit` 0 means the default (25); a negative `Limit` or one above 100 is `invalid_argument`/`invalid_limit` (never clamped); a negative `Offset` is `invalid_argument`/`invalid_offset`. Adapters do not add their own rules: the CLI additionally rejects `--limit 0` as a usage error (exit 2), and `draft_list` passes its integers through. Offset paging is accepted: this is a single-user local store, and a concurrent write between pages can shift entries. `total` is the number of readable drafts (it can be lower than the number of draft-named files the `MaxStoredDrafts` cap counts); `has_more` is `offset + len(drafts) < total`. An offset at or beyond `total` returns an empty page, not an error.
- **Timestamps** come from the service's clock (injectable for tests), converted to UTC and truncated to whole seconds. Clients cannot set them.

## 8. Errors

One error type and one set of categories are shared by core, CLI and MCP.

| Category | Meaning | Codes |
| --- | --- | --- |
| `invalid_argument` | The request is not acceptable: bad input, bad ID format, bad paging, bad data directory. Nothing was changed. | `validation_failed`, `invalid_id`, `invalid_revision`, `invalid_limit`, `invalid_offset`, `invalid_data_dir`; `invalid_arguments` and `duplicate_key` (adapter-only as top-level codes, below) |
| `not_found` | No draft with that ID. | `draft_not_found` |
| `conflict` | The request lost a race or is stale. Nothing was changed. | `revision_mismatch`, `already_exists` |
| `corrupt` | A stored file is damaged: unparseable, wrong shape or invalid content under create-time rules, ID not matching its file name, not a regular file, over the read cap. | `invalid_json`, `invalid_content`, `id_mismatch`, `not_regular_file`, `file_too_large` |
| `unsupported_schema` | A stored file has a `schema_version` this build cannot read. | `schema_unsupported` |
| `limit_exceeded` | A store bound would be exceeded. | `store_full` |
| `io` | The environment failed: permissions, missing or read-only directory, disk, lock not acquired in time, or the context cancelled while waiting for it. | `io_error`, `lock_timeout`, `canceled`, `data_dir_unavailable` |
| `internal` | A bug or impossible state. | `internal`, `id_generation_failed`, `encoded_too_large` |

`validation_failed` (category `invalid_argument`) carries the validation `issues` (errors) and `warnings`. Issue codes (section 5) are plain strings, not `Code…` constants; `duplicate_key` is one of them. `revision_mismatch` carries `current_revision`. Malformed JSON, unknown fields and other decode-stage failures on create/update are reported as `invalid_argument`/`validation_failed` whose `issues` hold the decode issue; the specific cause is the issue `code`.

Go representation, in `internal/core/draft` (the JSON tags are the wire form used by CLI `--json` and MCP error results):

```go
type Category string

const (
	CategoryInvalidArgument   Category = "invalid_argument"
	CategoryNotFound          Category = "not_found"
	CategoryConflict          Category = "conflict"
	CategoryCorrupt           Category = "corrupt"
	CategoryUnsupportedSchema Category = "unsupported_schema"
	CategoryLimitExceeded     Category = "limit_exceeded"
	CategoryIO                Category = "io"
	CategoryInternal          Category = "internal"
)

// Error codes, grouped by category. Names and values match internal/core/draft.
const (
	CodeValidationFailed = "validation_failed"
	CodeInvalidID        = "invalid_id"
	CodeInvalidRevision  = "invalid_revision"
	CodeInvalidLimit     = "invalid_limit"
	CodeInvalidOffset    = "invalid_offset"
	CodeInvalidDataDir   = "invalid_data_dir"

	CodeDraftNotFound = "draft_not_found"

	CodeRevisionMismatch = "revision_mismatch"
	CodeAlreadyExists    = "already_exists"

	CodeInvalidJSON    = "invalid_json"
	CodeInvalidContent = "invalid_content"
	CodeIDMismatch     = "id_mismatch"
	CodeNotRegularFile = "not_regular_file"
	CodeFileTooLarge   = "file_too_large"

	CodeSchemaUnsupported = "schema_unsupported"

	CodeStoreFull = "store_full"

	CodeIOError            = "io_error"
	CodeLockTimeout        = "lock_timeout"
	CodeCanceled           = "canceled"
	CodeDataDirUnavailable = "data_dir_unavailable"

	CodeInternal           = "internal"
	CodeIDGenerationFailed = "id_generation_failed"
	CodeEncodedTooLarge    = "encoded_too_large"
)

type Error struct {
	Category        Category `json:"category"`
	Code            string   `json:"code"`
	Message         string   `json:"message"`
	Issues          []Issue  `json:"issues,omitempty"`
	Warnings        []Issue  `json:"warnings,omitempty"`
	CurrentRevision int64    `json:"current_revision,omitempty"`
	Err             error    `json:"-"` // underlying cause, for logs and errors.Is/As; never serialized
}

func (e *Error) Error() string // "<category>: <message>"
func (e *Error) Unwrap() error // returns Err
```

Rules:

- Every error returned by the `Service` and by any `Store` is (or wraps, so `errors.As` works) a `*draft.Error`. Anything else reaching an adapter is mapped to `internal`, except that a bare `context.Canceled` or `context.DeadlineExceeded` from a store is mapped to `io`/`canceled`.
- `Message` is safe to show to an agent: it MUST NOT contain filesystem paths, recipe text, titles or source URLs. Issue messages describe the rule and may name the field path, never echo the value. `Err` may contain paths and is for stderr logs only.
- Adapters pass `category`, `code`, `message`, `issues`, `warnings`, `current_revision` through unchanged. They do not invent categories.
- The MCP adapter has two codes of its own, both with category `invalid_argument` and no `issues`: `invalid_arguments` (arguments that do not match the tool's input schema: a missing required property, a type mismatch including a `draft` that is not a JSON object or is `null`, or an unknown root property) and `duplicate_key` (the same key twice in the `arguments` root). The core uses `duplicate_key` only as an issue code inside `validation_failed`, for a duplicate key inside the draft content. The core never returns `invalid_arguments`.

## 9. Core API (`internal/core/draft`)

### 9.1 Service

The service owns validation, ID and timestamp assignment, revision logic, ordering and paging. It imports no MCP, CLI, HTTP, database or provider code.

```go
// Option configures a Service.
type Option func(*Service)

func NewService(store Store, opts ...Option) *Service
func WithClock(now func() time.Time) Option               // default: time.Now
func WithIDGenerator(gen func() (string, error)) Option  // default: "drf_" + 20 hex from crypto/rand

func (s *Service) Create(ctx context.Context, in Content) (WriteResult, error)
func (s *Service) Get(ctx context.Context, id string) (Draft, error)
func (s *Service) List(ctx context.Context, opts ListOptions) (ListPage, error)
func (s *Service) Update(ctx context.Context, id string, expectedRevision int64, in Content) (WriteResult, error)
func (s *Service) Validate(in Content) ValidationResult        // pure, touches no store
func (s *Service) Info(ctx context.Context) (StoreInfo, error) // store status for server_status

func DecodeContent(raw []byte) (Content, []Issue)  // strict decoder, see section 5
func ValidateRaw(raw []byte) ValidationResult      // DecodeContent then Validate; used by the CLI and MCP adapters
func Validate(in Content) ValidationResult         // what Service.Validate calls
```

Semantics:

- **Create.** Normalize (nil lists to `[]`, all-absent estimate objects to nil) and validate (errors → `invalid_argument`/`validation_failed`, no store call). Assign ID, `schema_version`, `status: "draft"`, `revision: 1`, `created_at = updated_at = now`. Call `store.Create`. Return the stored draft and the validation result (warnings only). A repeat call makes another draft.
- **Get.** Validate the ID format (`invalid_id`), then `store.Get`. Does not modify anything.
- **List.** Validate options (section 7: `Limit` 0 is the default, negative or too large is `invalid_limit`, negative `Offset` is `invalid_offset`). Call `store.List` once; sort; page; build summaries (`Drafts` is `[]`, never nil); report up to `MaxSkippedReported` skipped entries and the full `skipped_total`. Does not modify anything.
- **Update.** In order: ID format; `expectedRevision >= 1` (`invalid_revision`); validate content (errors → `validation_failed`); `store.Get` (not found / corrupt / unsupported propagate); if the stored revision differs from `expectedRevision` return `conflict`/`revision_mismatch` with `current_revision`; build the next draft (same ID and `created_at`, `revision = expected + 1`, new `updated_at`, new content); call `store.Replace`, which repeats the revision check atomically. A failed or conflicting update never changes the stored draft. If the store reports success, the update succeeded: post-commit problems are the store's to log, not to return (section 10.3).
- **Validate.** `Service.Validate` checks candidate content only. There is no operation that validates a stored draft in v1.
- **Isolation.** Values passed in and returned are deep-copied at the service boundary: mutating a returned `Draft` or a previously passed `Content` never changes stored state.
- **Context.** All methods honor `ctx` cancellation before the commit point (the store's rename, section 10.3); cancellation after it does not turn a committed write into an error.

### 9.2 Persistence interface

The interface lives in `internal/core/draft` so the domain depends on no store implementation. #22 implements it; #21 supplies an in-memory implementation.

```go
// StoreInfo is the store's status, without recipe content or paths.
type StoreInfo struct {
	DataDirExists      bool
	PrivatePermissions bool // data root and drafts directory have no group/other access bits; true when the root does not exist yet (it will be created 0700); always false where not supported
	CrossProcessLock   bool // true when writes are protected by the cross-process lock
	DraftCount         int  // readable drafts
	SkippedCount       int  // unreadable entries
}

// ListResult is every readable draft plus a description of each entry that could not be read.
type ListResult struct {
	Drafts  []Draft // never nil
	Skipped []SkippedEntry
}

type Store interface {
	// Create stores d as a new draft. d is fully formed (ID, revision 1, timestamps) and
	// already validated by the service. It NEVER overwrites: if a draft with d.ID exists it
	// returns conflict/already_exists. If the store already holds MaxStoredDrafts files matching the
	// draft-file name pattern, readable or not (counted under the lock), it returns
	// limit_exceeded/store_full. On any error nothing is left behind.
	Create(ctx context.Context, d Draft) error

	// Get returns the stored draft. Errors: not_found/draft_not_found, corrupt/*,
	// unsupported_schema/schema_unsupported, io/*. It never modifies anything.
	Get(ctx context.Context, id string) (Draft, error)

	// Replace atomically replaces the stored draft id with next, if and only if the stored
	// revision equals expectedRevision. The check and the replacement are one atomic step
	// with respect to every other Store user of the same data root, including other
	// processes. Errors: conflict/revision_mismatch (with CurrentRevision; nothing written),
	// not_found, corrupt/* and unsupported_schema (the damaged file is NOT replaced), io/*.
	// The store also requires next.ID == id, next.Revision == expectedRevision+1 and
	// next.CreatedAt equal to the stored value; a violation is internal/internal and writes
	// nothing. On any error the previous valid file is intact.
	Replace(ctx context.Context, id string, expectedRevision int64, next Draft) error

	// List returns all readable drafts in unspecified order (the service sorts) and one
	// SkippedEntry per entry that could not be used. A bad entry never fails the whole call.
	// An empty or non-existent data root returns an empty result and no error.
	// It never modifies, repairs or deletes anything.
	List(ctx context.Context) (ListResult, error)

	// Info reports store status for server_status. It never modifies anything.
	Info(ctx context.Context) (StoreInfo, error)
}
```

Implementations must also satisfy, and the shared conformance suite checks:

- A `Draft` read back equals the `Draft` written, field for field (round trip loses nothing, including empty lists as empty).
- A `Draft` passed in or returned is not aliased with internal state (in-memory implementation copies).
- `Create` after `Create` with the same ID: second is `already_exists`, first is unchanged.
- `Replace` with a stale revision: `revision_mismatch`, stored draft unchanged.
- Read-only operations (`Get`, `List`, `Info`) leave the store byte-for-byte unchanged.

### 9.3 Parallel-work seam

Types, constants, the `Error` type, the `Store` interface and the `Service` signatures above are the seam between #21 and #22. See [section 12](#12-package-layout-and-parallel-work) for sequencing and the shared conformance suite.

## 10. Local store (`internal/localstore`)

### 10.1 Data directory resolution

Resolution order (first match wins):

1. `--data-dir <path>` flag on `mealplanner-mcp` and on the `mealplanner` commands that open the store (`draft list`, `draft show`). `mealplanner draft validate` never touches the store and does not accept `--data-dir` (section 14); `version` and `--version` resolve nothing.
2. Environment variable `MEALPLANNER_DATA_DIR`. An empty value counts as unset.
3. Default: `filepath.Join(os.UserConfigDir(), "mealplanner")`.

| Platform | Default data root |
| --- | --- |
| macOS | `~/Library/Application Support/mealplanner` |
| Linux | `$XDG_CONFIG_HOME/mealplanner`, else `~/.config/mealplanner` |
| Windows | `%AppData%\mealplanner` |

**Relative paths are rejected.** Whatever the source (flag or environment), the value must be an absolute path (`filepath.IsAbs`); it is then `filepath.Clean`ed. A relative path, including one beginning with `~` (which only a shell expands), fails with `invalid_argument`/`invalid_data_dir`. An explicitly set but empty `--data-dir ""` also fails. Rationale: MCP hosts launch servers with a working directory the user does not control or see, so resolving against the cwd would put data somewhere unpredictable; rejecting beats guessing, and the error message tells the user to supply an absolute path. A bad value from a higher-precedence source is an error; it does not fall through to a lower source. If no override is given and `os.UserConfigDir()` fails (for example `HOME` is unset in a sanitized host environment), resolution fails with `invalid_data_dir` and a message asking for `--data-dir`.

```go
package localstore

const EnvDataDir = "MEALPLANNER_DATA_DIR"

type DirSource string // "flag", "env", "default"

// ResolveDataDir applies the precedence above. flagSet reports whether --data-dir was
// provided at all (so an empty explicit value is distinguishable from absent).
// getenv and userConfigDir are injected so tests do not touch the process environment.
func ResolveDataDir(flagValue string, flagSet bool, getenv func(string) string,
	userConfigDir func() (string, error)) (dir string, source DirSource, err error)

type Options struct {
	LockTimeout time.Duration // 0 means 5 * time.Second
}

// Open returns a store rooted at dataDir, which must be absolute. Open creates nothing
// on disk; directories and the lock file are created lazily by the first Create.
func Open(dataDir string, opts Options) (*Store, error)

var _ draft.Store = (*Store)(nil)
```

`mealplanner-mcp`, `mealplanner draft list` and `mealplanner draft show` call `ResolveDataDir` then `Open`; neither re-implements the rules. The MCP server resolves and opens at startup; if that fails it writes the error to stderr and exits with status 2 without speaking the protocol. `--version` and `version` never resolve a data directory.

### 10.2 Layout and permissions

```text
<data-root>/                  0700, created by the first Create
  .lock                       0600, empty, never deleted
  drafts/                     0700
    drf_0123456789abcdef0123.json   0600, one draft
    .tmp-<random>                   transient, same directory as the target
```

- `Open` never creates or modifies anything. Read operations on a missing data root behave as an empty store.
- Directories are created with `0700` and files with `0600`. An existing data root is **not** `chmod`ed; if it has group/other bits, `Info.PrivatePermissions` is `false` and `server_status` reports it, and operation continues. `Info.PrivatePermissions` is `true` when the data root does not exist yet, because the first `Create` will make it `0700`. Draft files are always written `0600`.
- File name is exactly `<id>.json`. `List` considers only names matching `^drf_[0-9a-f]{20}\.json$`; other names (including `.tmp-*` and `.DS_Store`) are ignored and not reported. The file's `id` must equal its name, else `corrupt`/`id_mismatch`.
- A draft file is the `Draft` JSON object, indented with two spaces, with a trailing newline, fields in the declared struct order, so files are readable and diff-friendly.
- **Containment.** IDs are validated before any path is built and cannot contain separators or dots. Each operation opens descriptor-backed `os.Root` handles for the chosen data root and verified `drafts/` directory; subsequent directory listing, file reads, locking, temporary-file operations, cleanup, rename and directory sync stay relative to those handles. An inode check rejects a directory swapped between `Lstat` and opening. Renaming or replacing the root or `drafts/` pathname after opening cannot redirect that operation into a different directory. `.lock` and draft files must be regular files; their opened identity must match `Lstat`, with `os.Root` escape prevention plus the identity check enforcing the boundary on every platform. Darwin/Linux also request `O_NOFOLLOW | O_NONBLOCK`; because Go may resolve contained symlinks internally, `O_NOFOLLOW` alone is not the containment guarantee. Nonblocking opens prevent a swapped FIFO from hanging the operation. Identity checks retry at most three times to tolerate legitimate atomic replacement, and unverified file handles are never read. The selected data root itself may be a symlink. Invalid draft entries fail as `corrupt`/`not_regular_file`; invalid directories fail as `io`/`data_dir_unavailable`.
- **Filesystem threat boundary.** Descriptor anchoring prevents pathname/symlink substitution from escaping the opened directory. It is not a sandbox against an actor who can change mounts, create hard links to other accessible files, or already read/write the user's files. Such an actor can still damage data or cause denial of service. Use a private local data directory; the implementation does not claim availability against same-user filesystem mutation.
- **Temp files.** While holding the lock, a write also removes `.tmp-*` files in `drafts/` that are older than 24 hours. It removes nothing else, ever.
- Default location is outside any checkout. If a user points `--data-dir` inside a checkout, keeping it out of Git is their responsibility; tests must use `t.TempDir()`.

### 10.3 Writes

`Create` and `Replace` follow the same steps; the lock is held only for the check and the rename, not while writing the temp file.

1. Marshal the draft; refuse (`internal`/`encoded_too_large`) if the encoded file would exceed `MaxStoredFileBytes` (512 KiB).
2. Create `drafts/` (`MkdirAll`, `0700`) and `.lock` if missing (`Create` only; `Replace` of an absent draft is `not_found` without creating anything).
3. Write the content to `drafts/.tmp-<random>` in the **same directory** (so rename cannot cross filesystems), mode `0600`, `Sync()` the file, close it. If the temp file cannot be created, the error is `io`/`io_error` with the path-free message "cannot write to the drafts directory; check that it is writable".
4. Acquire the cross-process lock (section 10.5).
5. Under the lock:
   - `Create`: count every file whose name matches the draft-file pattern, readable or not (must be `< MaxStoredDrafts`), `Lstat` the target (must not exist), `rename` temp → target.
   - `Replace`: re-check that the target still exists (if it is absent, remove the temp file and return `not_found`), then read and decode the current file (`corrupt`/`unsupported_schema` abort and leave it alone), compare its revision to `expectedRevision` (mismatch → `conflict`), check `next` per the interface rules, `rename` temp → target.
6. `Sync()` the `drafts/` directory (best effort; Windows skips it, so a successful write does not log a failure every time).
7. Release the lock.
8. On any failure before the rename, remove the temp file (including the `not_found` path above). A crash can leave a `.tmp-*` file; it is ignored by reads and listing, never promoted, and may be deleted by hand.

The rename is the commit point. Readers see either the complete previous file or the complete new file, never a partial file. After the rename succeeds, a cancelled context cannot undo it; before it, cancellation aborts with no change. **Errors after the rename do not fail the operation:** a directory `fsync` error, a lock release error or context cancellation is logged to stderr (category and code only, no payload or path) and the call returns success. Reporting failure for a write that is already visible would make a retry create a duplicate or conflict with itself.

Go's `os.File.Sync` is used for durability (it requests a full flush on macOS).

### 10.4 Reads and corruption

- `Get` and `List` take no lock; atomic rename makes that safe.
- Every file read is limited to the 512 KiB cap; a larger file is `corrupt`/`file_too_large` without being read in full.
- The file must contain one valid UTF-8 JSON object. First check the root for duplicate decoded keys and case-folded collisions and read an exact-case integer `schema_version`. An invalid header is `corrupt`/`invalid_content`. An unambiguous version outside `SupportedSchemaVersions` is `unsupported_schema`/`schema_unsupported`. Its payload stays opaque to the current schema validator. For supported versions, check exact-case field names, duplicate keys, shape and string Unicode throughout the document before typed decoding. Then check the stored envelope (`status == "draft"`, `id` matches the file name and `revision >= 1`) and run the same `draft.Validate(Content)` used by create and update. Every create-time content error makes the stored file `corrupt`/`invalid_content`: required/blank text, lengths and list/encoded-size limits, enums, source IDs and URLs (including required URL, scheme, host, whitespace and credentials rules), minutes bounds, bases and source references, and character rules. Validation warnings alone do not make a file corrupt. CLI show/list, MCP get/list/update preconditions and the under-lock `Replace` re-read all use this path.
- `Get` of a damaged file returns the error. `List` omits it from `drafts`, adds a `SkippedEntry` (with `id` when the name is well-formed, category, and a message without paths or content), counts it in `skipped_total`, and continues. A file that vanishes between the directory read and the file read is skipped silently and is not reported as skipped. **Nothing is ever deleted, repaired, rewritten or moved automatically.** `Replace` of a damaged draft fails and leaves the file as it is; the user decides what to do with it by hand.
- **Compatibility and recovery.** Previously readable files that fail create-time validation now become unreadable and are skipped by list; a normal update cannot repair them. Stop local writers, back up the original file outside `drafts/`, and prepare corrected content without the six envelope fields (`schema_version`, `id`, `revision`, `status`, `created_at`, `updated_at`). Run `mealplanner draft validate` on that content. After it passes, manually repair only the original file's content while retaining its envelope, or use `draft_create` to save the corrected content under a new ID and move the original out of `drafts/` yourself. Re-read the result with `draft show`. The server never edits or migrates an invalid file automatically.
- A missing data root, an empty `drafts/` directory and a directory containing only ignored names are all an empty store.

### 10.5 Concurrency

Two layers, both required:

1. **Optimistic revision.** Every update states the revision it was based on. A stale update is `conflict`/`revision_mismatch` and writes nothing. The service checks early (to fail fast) and the store re-checks authoritatively under the lock.
2. **Cross-process advisory lock.** `flock(2)` with `LOCK_EX` on `<data-root>/.lock`, so the "read current revision, compare, rename" sequence is atomic across two MCP server processes (and the CLI). `flock` is used rather than `fcntl` byte-range locks because it is associated with the open file description: a fresh `open` of the lock file per operation gives mutual exclusion between goroutines in the same process as well as between processes. The lock is released by `LOCK_UN` and by the kernel if the process dies, so there is no stale-lock cleanup.

Lock scope and timeout:

- **Scope:** one lock for the whole data root, held by `Create` and `Replace` only, for the duration of steps 5–6 above (a few file operations, not the temp-file write). Reads never take it.
- **Acquisition:** non-blocking attempts (`LOCK_EX|LOCK_NB`) polled every 25 ms until acquired, `ctx` is done, or `Options.LockTimeout` (default 5 s) elapses. Timeout returns `io`/`lock_timeout` ("another process holds the draft lock; retry"), which is retryable and distinct from `conflict`. Context cancellation while waiting for the lock returns `io`/`canceled`, distinct from `lock_timeout`; nothing has been written.
- **Limits:** the lock file must be on a local filesystem. Network filesystems are not supported (advisory locks are unreliable there).

### 10.6 Portability

- The code is pure Go (stdlib only), builds with `CGO_ENABLED=0`, and the existing cross-compile checks (windows/amd64, linux/arm64, darwin/arm64) must keep passing.
- Locking is behind build tags: `lock_flock.go` with `//go:build darwin || linux` uses `syscall.Flock`; `lock_other.go` with `//go:build !darwin && !linux` uses only an in-process mutex. `O_NOFOLLOW | O_NONBLOCK` is likewise behind build tags. Windows uses `os.Root` containment and opened-file identity checks instead. JavaScript and Plan 9 targets fail closed at `Open` because descriptor anchoring is unavailable there. On those platforms `Info.CrossProcessLock` is `false` and `server_status` reports it: the optimistic-revision check still catches most stale writers, but two processes can race between the check and the rename, so concurrent multi-process writes are **not supported** there. This limitation is documented, not hidden. Directory `Sync` errors and `chmod` bits are best effort on Windows, where `Info.PrivatePermissions` reports `false`.
- On Windows the directory `Sync()` after the rename is skipped.
- macOS is the supported and verified platform for this slice.

## 11. Schema versioning and compatibility

### 11.1 Versions

- `schema_version` is `1`. It appears in every stored file and every returned `Draft`.
- A build reads exactly the versions in `SupportedSchemaVersions` (`[1]`) and writes only `SchemaVersion`.
- An unsupported version is reported (never guessed at, downgraded, or rewritten): `unsupported_schema` on `Get`/`Replace`, a skipped entry on `List`.

### 11.2 Distinct from the application version

The application version (`version.Version`) changes with every release and says nothing about the data format. `schema_version` changes only when the stored or exchanged shape changes in a way an older reader would misread. `server_status` returns both.

### 11.3 Changing the contract

Because unknown fields are rejected, **any** new field, new required rule, enum value or changed meaning requires a new `schema_version`, a new `SupportedSchemaVersions` entry (if the old one is still read), and an explicit migration decision recorded here. Adding or tightening a validation error code is likewise a contract change. Adding a warning code is not, provided it never blocks a save.

## 12. Package layout and parallel work

| Package | Contents | May import | Must not import |
| --- | --- | --- | --- |
| `internal/core/draft` | Types, validation, strict decoder, `Service`, `Store` interface, `Error`, limits. | Standard library only. | MCP SDK, CLI/flag code, `net/http`, `database/sql` or any DB driver, `os/exec`, provider SDKs, `internal/localstore`. |
| `internal/core/draft/memstore` | In-memory `Store` for tests of core and adapters. | `internal/core/draft`, stdlib. | As above. |
| `internal/core/draft/storetest` | Shared `Store` conformance suite (`Run(t, newStore func(t *testing.T) draft.Store)`). | `internal/core/draft`, `testing`. | As above. |
| `internal/localstore` | File `Store`, `ResolveDataDir`, `Open`, locking (build-tagged). | `internal/core/draft`, stdlib. | MCP SDK, CLI code, DB drivers, network packages. |
| `internal/mcpadapter` | MCP server construction, tool schemas, handlers, error and result mapping. | `internal/core/draft`, `internal/core/version`, the MCP SDK, stdlib. | `internal/localstore` (the store is injected via `*draft.Service`; the data-directory source label is injected as an adapter option, section 13.2). |
| `cmd/mealplanner-mcp` | `main`: flags, `ResolveDataDir`, `Open`, build service, run server on stdio. | `internal/localstore`, `internal/mcpadapter`, `internal/core/...` | Recipe logic. |
| `cmd/mealplanner` | `main`: commands, flags, human and JSON output, exit codes. | `internal/localstore`, `internal/core/...` | Recipe logic, the MCP SDK. |

`internal/mcpadapter` is introduced (rather than putting tool handlers in `cmd/mealplanner-mcp`) so that tool schemas, handlers and error mapping can be unit-tested in-process through the SDK's in-memory transport against `memstore`, without building and spawning a binary for every case. `cmd/mealplanner-mcp/main.go` stays a thin entry point, as the existing `run(args, stdin, stdout)` pattern already is. Subprocess tests of the built binary remain #23's and #25's job.

`internal/core/draft` must stay free of the forbidden imports; #21 should add a test that fails if `go list -deps` of the package shows any (the check is part of #21's acceptance, not a CI change).

### 12.1 Sequencing for parallel work

- **Step 0 (first PR of #21, small, no logic).** Land `internal/core/draft` with exactly the type, constant, error and `Store` declarations from sections 4.1, 8 and 9.2, and `Service` method stubs that return `internal`. This compiles and unblocks everyone. (#22 may start before it merges by copying those declarations verbatim into a throwaway branch; they are normative, so the merge is trivial.)
- **#21** continues with validation, decoder, service, `memstore`, and `storetest` (the conformance suite).
- **#22** implements `localstore` against the interface and runs `storetest` against it, in addition to its own tests (two-process concurrency, interruption, containment, permissions, restart persistence, cross-compile).
- **#23 and #24** start once `Service` is real (#21) and can use `memstore`; they integrate `localstore` when #22 lands.

### 12.2 CI classification note

`.github/scripts/classify.sh` maps `internal/core/*` to the *core* component and `cmd/*`, `internal/localstore/*` and `internal/mcpadapter/*` to the *cli* component (local Go, contracts compatibility and integration). The local Go job tests the internal and command packages, including the restored paths, with `go test ./internal/... ./cmd/...`. Unknown paths continue to select every check.

## 13. MCP tools (`cmd/mealplanner-mcp`, via `internal/mcpadapter`)

Server: stdio transport, official Go SDK `github.com/modelcontextprotocol/go-sdk` (pinned at `v1.8.0`, which supports protocol versions `2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26` and `2024-11-05`; through `initialize` it negotiates up to `2025-11-25`, so a client that requests `2026-07-28` is answered with `2025-11-25`; the stateless `2026-07-28` flow is `server/discover` plus per-request `_meta` carrying `io.modelcontextprotocol/protocolVersion` and `clientCapabilities`). Server name `mealplanner-mcp`, server version `version.Version`. Only tools are advertised: no resources, prompts, sampling or elicitation.

Tool names are fixed: `draft_create`, `draft_get`, `draft_list`, `draft_update`, `draft_validate`, `server_status`.

| Tool | Mutates | Annotations (`readOnly` / `destructive` / `idempotent` / `openWorld`) |
| --- | --- | --- |
| `draft_create` | yes (adds a draft) | false / false / false / false |
| `draft_get` | no | true / – / – / false |
| `draft_list` | no | true / – / – / false |
| `draft_update` | yes (replaces a draft's content) | false / true / false / false |
| `draft_validate` | no | true / – / – / false |
| `server_status` | no | true / – / – / false |

Annotations describe behavior for the host's benefit. They are hints, not authorization, and must be accurate (`draft_update` is marked destructive because it overwrites the previous content and no history is kept; `idempotentHint` is false for `draft_create` and `draft_update`). The read-only tools also carry `idempotentHint: false`, which is the SDK's default when the hint is not set; the hint has no meaning for a read-only tool.

Every tool's input schema is a root `{"type": "object", "additionalProperties": false}` with plain typed properties (`string`, `integer`, `object`) and a `required` array naming the genuinely required ones (it aids discoverability by models). Bounds, patterns, enums beyond the type and exactly-one constraints are **not** expressed as schema keywords (no `minimum`, `maximum`, `pattern`, `enum`, `oneOf`, `anyOf` or `allOf`); they appear only in the property descriptions. The core validates everything, so a bad value comes back as an `isError` tool result with a code such as `invalid_id`, `invalid_limit`, `invalid_offset` or `invalid_revision`, which an agent can read and correct, instead of an SDK protocol error. A property that is not in `required` and is absent reaches the core as its zero value and is reported there. A tool's `draft` property is `{"type": "object"}` and nothing more: the adapter receives it as raw JSON (`json.RawMessage`) and passes it unmodified to `DecodeContent`, so MCP, CLI and core give identical results. No `outputSchema` is declared in v1. Tool descriptions must state, in the SDK description text:

- For `draft_create`, `draft_update`, `draft_validate`: "Saved recipes are drafts. Validation is structural only and is not food-safety verification or review. Recipe text and URLs are stored as data and are never fetched or executed."
- For `draft_create`: "Each call creates a new draft; repeating a call creates another. If a call fails ambiguously, use draft_list before retrying."
- For `draft_update`: "Replaces the whole draft; send the complete content and the revision you last read."
- Estimate and guidance descriptions: "Mark every estimate and every prep-ahead, storage or safety statement with a basis: agent_estimate for your own suggestion, sourced (with source_ref when possible) only when it comes from a listed source."

### 13.1 Results

Because no `outputSchema` is declared, results are not validated against one; every successful call returns the output object below as `structuredContent` **and** serialized as compact JSON in a single `text` content block (identical content), for hosts that read only one of them. `draft_*` tools report domain failures (any `draft.Error`) as a tool result with `isError: true` whose `structuredContent` and text are `{"error": <Error JSON>}`, so the agent can read the issues and correct itself. Protocol-level problems (an unknown tool, malformed JSON-RPC) use the SDK's protocol error path; in either case nothing is stored. Arguments that do not match the input schema (a missing `required` property, a type mismatch including a non-object or `null` `draft`, or an unknown root property) are reported by the adapter as a tool error `invalid_argument`/`invalid_arguments` (section 8), and a duplicate key in the `arguments` root as `invalid_argument`/`duplicate_key`. The size bounds MCP adds are `draft_too_large` (`MaxDraftBytes`, section 5) and `input_too_large` from `DecodeContent` for a `draft` argument over `MaxInputBytes`, plus the SDK and transport limits. `draft_validate` is different: an invalid candidate is a *successful* call whose result has `structurally_valid: false` and the errors.

Error messages and logs follow section 8: no paths, titles, recipe text or URLs in anything returned to the host.

### 13.2 Tool schemas

Each property below is a bare typed property; the text after the type is its **description**, not a schema keyword.

**`draft_create`** — input `{"draft": object}`, `required: ["draft"]`: the content object from section 4, without server-assigned fields. Output `{"draft": Draft, "validation": ValidationResult}` (`validation.errors` is empty; warnings are listed). Errors: `invalid_argument`/`validation_failed` (including decode issues such as `duplicate_key` and `unknown_field`, and `draft_too_large`), `limit_exceeded`/`store_full`, `io`, `internal`.

**`draft_get`** — input `{"id": string}`, `required: ["id"]`: the draft ID, `drf_` plus 20 lowercase hex characters. Output `{"draft": Draft}`. Errors: `invalid_argument`/`invalid_id`, `not_found`, `corrupt`, `unsupported_schema`, `io`.

**`draft_list`** — input `{"limit": integer, "offset": integer}`, both optional (no `required`). `limit` description: "1 to 100, default 25". `offset` description: "0 or more, default 0". Output is `ListPage` (section 4.1). Errors: `invalid_argument`/`invalid_limit` or `invalid_offset`, `io`. Unreadable files are not errors; they appear in `skipped`. An explicit `limit` of 0 is the default (the core rule, section 7).

**`draft_update`** — input `{"id": string, "expected_revision": integer, "draft": object}`, `required: ["id", "expected_revision", "draft"]`. Descriptions: `id` as for `draft_get`; `expected_revision` "the revision you last read, 1 or more"; `draft` as for `draft_create`. Output `{"draft": Draft, "validation": ValidationResult}`. Errors: `invalid_argument` (`invalid_id`, `invalid_revision`, `validation_failed`), `not_found`, `conflict`/`revision_mismatch` (with `current_revision`), `corrupt`, `unsupported_schema`, `io` (including `lock_timeout` and `canceled`), `internal`.

**`draft_validate`** — input `{"draft": object}` only, `required: ["draft"]`: a candidate, not saved. There is no `id` form and no way to validate a stored draft in v1. Output `{"validation": ValidationResult}`. Never writes and never touches the store. Decode-stage problems in the candidate (unknown field, wrong type, `duplicate_key`) and `draft_too_large` are returned as validation errors, not as tool errors, so this tool has no domain errors of its own.

Example input schema (`draft_update`); the other tools differ only in their property lists:

```json
{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "id": { "type": "string", "description": "Draft ID: drf_ followed by 20 lowercase hex characters." },
    "expected_revision": { "type": "integer", "description": "The revision you last read; 1 or more." },
    "draft": { "type": "object", "description": "Complete recipe content (not the server-assigned fields)." }
  },
  "required": ["id", "expected_revision", "draft"]
}
```

**`server_status`** — input `{}`. Output:

```json
{
  "binary": "mealplanner-mcp",
  "binary_version": "dev",
  "schema_version": 1,
  "supported_schema_versions": [1],
  "mode": "local",
  "local_only": true,
  "operations": ["draft_create", "draft_get", "draft_list", "draft_update", "draft_validate", "server_status"],
  "data_dir": { "source": "flag", "exists": true, "private_permissions": true },
  "store": { "available": true, "draft_count": 2, "skipped_count": 0, "cross_process_lock": true },
  "limits": { "max_draft_bytes": 131072, "default_list_limit": 25, "max_list_limit": 100, "max_stored_drafts": 1000 }
}
```

`data_dir.source` is `flag`, `env` or `default`. `cmd/mealplanner-mcp` supplies it to `internal/mcpadapter` through an adapter option (for example `WithDataDirSource(string)`), set from the `DirSource` that `localstore.ResolveDataDir` returned; `StoreInfo` is unchanged and carries no source, and `internal/mcpadapter` still must not import `internal/localstore`. The data directory **path is deliberately not returned**: tool output is placed in the agent conversation and may be sent to a model provider, and the path can contain a user name. The user knows the path they configured. `data_dir.private_permissions` follows `StoreInfo.PrivatePermissions`, so it is `true` when the data root does not exist yet. `server_status` does not fail because the store has a problem; it sets `store.available: false` and adds `store.error: {"category": ..., "message": ...}` instead. It makes no network calls and contains no recipe content, titles, URLs or secrets. Update checks and remote release information are out of scope for this slice.

### 13.3 Process behavior

- **stdout is protocol only.** No banner, notice, log line or stray `fmt.Print` may reach stdout. Diagnostics go to stderr.
- **Logging.** One structured line per tool call to stderr: tool name, outcome (`ok` or error category and code), duration, and the draft `id` when there is one (IDs are random and carry no content). Never titles, text, URLs, issue values, or the data directory path.
- **Shutdown.** On stdin EOF, `SIGINT` or `SIGTERM` the server stops accepting calls, lets in-flight calls finish for up to 5 seconds, and exits 0 on EOF or signal with no call outstanding, 1 if the deadline passed. Context cancellation of an in-flight call aborts it before its commit point; a persisted draft is never partial (section 10.3).
- **Framing and ordering.** Each line must be one valid UTF-8 JSON object. The 4 MiB frame limit excludes the LF or CRLF terminator and includes all other padding. A complete final object can also end at EOF or shutdown, where any trailing CR counts as padding. After validating the whole bounded input line, the gate removes only outer JSON whitespace and appends exactly one LF for the SDK. Object contents and string/number spellings are not rewritten. Blank lines, non-JSON whitespace, malformed/concatenated JSON, non-object values and batches are rejected before SDK delivery, as are duplicate decoded keys, case-folded collisions and unpaired surrogate escapes anywhere in the frame, including client response `result`/`error` and unknown or extra envelope members at any depth. Reserved protocol members (including cancellation `requestId`) require canonical casing. The offending frame and later frames are not executed; earlier accepted requests receive the normal shutdown grace period before exit 1. Tool calls may complete out of order, so clients must match responses by `id`. Before dispatch the gate also preflights the pinned SDK wire parser (including version/type/depth checks), and validates cancellation parameter types before releasing a target. The SDK still validates other method-specific message rules. `resources/list` and `prompts/list` return empty lists through the SDK although those capabilities are not advertised.
- **Request-ID compatibility.** The stdio transport accepts string IDs or canonical signed decimal integers from `-9007199254740991` to `9007199254740991`. Use a string for a larger identifier. String request IDs and cancellation `params.requestId` must contain valid UTF-8 and paired UTF-16 surrogate escapes: invalid bytes or lone surrogates are rejected before decoding rather than replaced with U+FFFD. Valid strings preserve their decoded Unicode value (JSON escape spelling may differ); empty strings, NUL escapes, literal U+FFFD, valid surrogate pairs, and strings up to the overall frame limit remain supported. Numeric `-0`, decimal/exponent spellings, out-of-range integers, null and non-string/non-integer IDs are rejected before the pinned SDK can normalize or round them. The rejected frame and later frames are not executed; already accepted requests receive the normal shutdown grace period, then the process exits 1 with a fixed diagnostic. This is an explicit client compatibility boundary, not a claim that every JSON-RPC numeric spelling is supported.
- **Shutdown gate.** Requests are tracked before their bytes reach the SDK. Each accepted ID is reserved for the session, including after reply or cancellation. An ID can be used again in a new session. A reused ID stops input before dispatch, creates no response obligation and receives no reply of its own. The server withholds EOF from the SDK while accepted requests still await responses, up to the five-second session deadline. Cancellation removes the target response obligation without releasing its ID or removing the deadline. Unknown or retired cancellation targets are ignored. Rejection or expiry exits 1. Frames above 4 MiB are rejected before SDK delivery.
- **Session ID budgets.** Each session retains at most 32,768 distinct request IDs and 8 MiB of decoded string ID bytes. The count bounds the registry overhead for small or numeric IDs, and the byte limit accommodates large IDs within the existing frame limit. Both apply after replies and cancellation. A request that would exceed either budget is rejected before dispatch. Earlier work has its normal shutdown grace, and the process exits 1 with a reconnect diagnostic.
- **Whole-frame preflight.** The stdio gate checks requests, notifications and client response frames before dispatch, request registration or cancellation release. It rejects duplicate decoded or case-folded keys in any object, including within arrays, and unpaired UTF-16 surrogate escapes in any key or value. This includes `params`, `result`, `error` and unknown or extra envelope members. Each envelope member value may nest up to 64 levels below its root, preserving the params depth limit. The root fields `name`, `arguments` and `_meta` in `tools/call`, and `requestId`, `reason` and `_meta` in `notifications/cancelled`, require canonical casing. The rejected frame and subsequent input are discarded, accepted requests retain their grace period, and a fixed diagnostic goes to stderr. Direct adapter calls still use the adapter's argument and content validators.
- **Network.** The binary links `net/http` through the SDK but never makes a network call.
- **Flags.** `--data-dir <abs path>` and the existing `--version` (prints `mealplanner-mcp <version>` and exits 0 without resolving a data directory). `--help` prints usage to stdout and exits 0. Unknown flags exit 2 with usage on stderr.
- **No capability beyond the tools above**: no tool takes a path, command, or URL to act on, and none fetches, publishes or calls out.

## 14. CLI (`cmd/mealplanner`)

Existing behavior is unchanged: `mealplanner version`, `mealplanner --version`, no arguments prints usage to stderr and exits 2, an unknown command exits 2. Help (`mealplanner --help`, `help` or `-h`, `draft --help`, and `draft <command> --help`) prints usage to stdout and exits 0.

```text
mealplanner draft validate <file|-> [--json]
mealplanner draft list   [--limit N] [--offset N] [--data-dir DIR] [--json]
mealplanner draft show   <id>       [--data-dir DIR] [--json]
```

- `draft validate` reads one JSON object of **content** (section 4) from the named file, or from stdin when the argument is `-`. A missing argument is a usage error (so an interactive shell never hangs on stdin). It reads at most `MaxInputBytes`, uses `draft.ValidateRaw` (`DecodeContent`, then `Validate`), never touches the data directory or any store, and never writes. It does not accept `--data-dir`.
- `draft list` and `draft show` read the same store, with the same data-directory rules, as the MCP server. They never write. `--limit`/`--offset` follow section 7, except that the CLI accepts `--limit` 1 to 100 only: `--limit 0` and negative values are rejected as usage errors (exit 2); omit the flag for the default. A negative `--offset` is exit 2. `--data-dir` is accepted by `list` and `show` only (section 10.1).
- There are no create, update or delete commands. Creating and editing drafts is done through MCP.
- `--json` selects machine-readable output (below). It is accepted after the subcommand.
- `draft show --json` prints `{"draft": ...}`. Its content block is not directly valid input to `draft validate`, because it includes server-assigned fields; extract the content fields (for example with `jq`) first. Strict input is deliberate (section 3).

### 14.1 Output

**JSON mode.** For exit status 0 or 1, stdout contains exactly one JSON document and nothing else, then a newline:

| Command | stdout |
| --- | --- |
| `draft validate` | `{"validation": ValidationResult}` (exit 0 if `structurally_valid`, exit 1 if not) |
| `draft list` | `ListPage` |
| `draft show` | `{"draft": Draft}`; on not found (exit 1): `{"error": Error}` |

For exit status 2 or 3, stdout is empty and a human-readable message is written to stderr. Diagnostics, progress and update notices never go to stdout in any mode.

**Human mode.** Layout is not part of the contract except for these requirements:

- `draft validate` prints each error and warning with its code and path, then always ends with the fixed line from `ValidationNotice`.
- Human output escapes control characters, Unicode format characters (category Cf), U+2028, U+2029 and, in single-line fields, tabs, so stored text cannot move the cursor, forge output or hide characters. Multi-line fields (`notes`, instruction `text`, `guidance[].text`) keep their newlines and tabs and have everything else escaped. The same escaping applies to validation codes, paths and messages and to every message written to stderr. Human timestamps are UTC RFC 3339. `--json` preserves valid content for machines, including format characters such as joiners, so do not print it raw to a terminal.
- `draft show` starts with the ID, revision and the line `status: draft (not reviewed or verified)`. Every item that has a basis is printed with it: agent estimates as `[agent estimate, unverified]`, sourced items as `[sourced: <source_ref or "no reference">]`. Unknown estimates are shown as `unknown`, never as `0`. Provenance sources are listed with their kind and URL (printed as text, never fetched). Human `draft show` output is a summary: it omits each ingredient's `quantity`, `unit` and `note`. `draft show --json` is complete.
- `draft list` prints one line per draft with ID, revision, `updated_at` and title (and a final line noting any skipped unreadable entries and their count).
- Data written to stdout never includes the data directory path. Error text on stderr may.

### 14.2 Exit codes

| Code | Meaning | Examples |
| --- | --- | --- |
| 0 | Success. **Warnings do not change the exit code.** | `draft validate` of a draft with only warnings; `draft list` of an empty store. |
| 1 | The operation ran and the subject was rejected on its merits. | `draft validate` found errors (including malformed JSON, empty input, input over 256 KiB, unknown fields); `draft show <id>` for an ID that does not exist. |
| 2 | Bad invocation: anything detectable from the arguments and environment alone. | Unknown command or flag; missing argument; `show` with an ID that fails the format check; `--limit` out of range; relative or empty data directory; no default data directory available. |
| 3 | Operational failure: the environment or the stored data failed. | Input file missing or unreadable; data directory unreadable; corrupt or unsupported-schema draft (`show`); lock timeout; internal error. |

Exit code by error category and code, for the commands that call the core (`draft list`, `draft show`) and for `draft validate`:

| Category / code | Exit |
| --- | --- |
| `draft validate` result with errors (`validation_failed` content, including decode issues and `input_too_large`) | 1 |
| `not_found` / `draft_not_found` | 1 |
| Other `invalid_argument`: `invalid_id`, `invalid_limit`, `invalid_offset`, `invalid_data_dir`, bad flags or arguments | 2 |
| `corrupt`, `unsupported_schema`, `io` (including `lock_timeout`, `canceled`, `data_dir_unavailable`), `internal`, `limit_exceeded`, `conflict` | 3 |

This table restates the rules above and changes none of them: for example, `show` with a malformed ID is `invalid_id` and exits 2, and an unreadable input file is `io` and exits 3. `limit_exceeded` and `conflict` cannot arise from the read-only commands in this slice; they are listed so a future command has a defined mapping.

A strict mode that turns warnings into a failure is deferred (not in this slice).

## 15. Open items

None of these block the local draft slice (#21–#25). Each blocks the behavior named.

| Item | Blocks | Recommendation |
| --- | --- | --- |
| **Reviewed/approved state.** Which fields need approval, who approves, what invalidates an approval (an edit, a source change). | Any "reviewed recipe", the hosted draft resource's approve/reject step, planning from drafts. | Add it behind a new `schema_version` after the product plan's review questions are answered. Likely a `review` object recording who/when/which revision, cleared by any update. Not defined here on purpose. |
| **Food-safety source and review policy** (product plan, "Food safety and timing"). | Presenting prep-ahead, storage or safety guidance as sourced-and-checked rather than "submitter says so". | Keep `sourced` meaning "submitter-asserted". Decide the policy and then add a distinct verified marker; do not reuse `sourced`. |
| **Hosted mapping.** How local `Content` and ids map to the hosted draft API and Postgres model; whether local drafts are uploaded, re-created or abandoned. | Hosted draft save. | Treat `Content` as the candidate request body; define ID mapping and migration when Checkpoint E is scoped. |
| **No delete (consequence, not a question).** A duplicate or mistaken draft remains until the file is removed by hand from `<data-root>/drafts/`. | Nothing; surfaced because it is user-visible. | Accept for this slice; revisit with archive or delete only if it proves annoying. |
| **`sourced` is unverified.** The application never checks a source, so `sourced` could be mistaken for verified in a UI. | Clear UI wording in future surfaces. | CLI human output already labels it `sourced: <ref>` rather than "verified"; keep that wording in any future surface. |

## 16. Synthetic examples

All content is invented. `example.com` is a reserved documentation domain.

### 16.1 Minimal valid content (saves with warnings)

```json
{ "title": "Synthetic Toast" }
```

Result: saved. `validation.errors` is `[]`. `validation.warnings` contains `no_ingredients`, `no_instructions` and `no_sources`.

### 16.2 Full valid content

```json
{
  "title": "Synthetic Bean Stew",
  "yield": "Serves 4-6",
  "ingredients": [
    { "text": "2 cups dried example beans, soaked", "quantity": "2", "unit": "cups", "name": "dried example beans", "note": "soaked" },
    { "text": "1 onion, chopped" },
    { "text": "salt, to taste" }
  ],
  "instructions": [
    { "text": "Soak the beans overnight.", "phase": "prep_ahead",
      "phase_basis": { "kind": "agent_estimate", "note": "Typical for dried beans; not checked against a source." } },
    { "text": "Chop the onion.", "phase": "prep_tonight" },
    { "text": "Simmer everything until tender.", "phase": "cook_tonight" }
  ],
  "notes": "Synthetic example for documentation only.",
  "sources": [
    { "id": "s1", "kind": "url", "label": "Example page", "url": "https://example.com/recipes/synthetic-bean-stew" },
    { "id": "s2", "kind": "agent_generated", "label": "Drafted by the agent from pasted text" }
  ],
  "estimates": {
    "active_minutes": { "value": 20, "basis": { "kind": "agent_estimate" } },
    "total_minutes":  { "value": 90, "basis": { "kind": "sourced", "source_ref": "s1" } },
    "attention":      { "value": "low", "basis": { "kind": "agent_estimate" } }
  },
  "phase_estimates": {
    "cook_tonight": {
      "active_minutes": { "value": 5, "basis": { "kind": "agent_estimate" } },
      "total_minutes":  { "value": 60, "basis": { "kind": "agent_estimate" } }
    }
  },
  "guidance": [
    { "topic": "storage", "text": "Leftovers may keep for several days in a fridge.",
      "basis": { "kind": "agent_estimate", "note": "General suggestion; unverified." } },
    { "topic": "prep_ahead", "text": "Beans may be soaked the night before.",
      "basis": { "kind": "sourced", "source_ref": "s1" } }
  ]
}
```

Result: saved, no errors, no warnings. `prep_tonight` has no estimate, so its time is **unknown** (not zero); `cleanup` and `variability` are likewise unknown. To state a known zero (for example a no-cook recipe), supply `"active_minutes": { "value": 0, "basis": { "kind": "agent_estimate" } }`.

### 16.3 Invalid content

```json
{
  "title": "   ",
  "ingredients": [ { "text": "" } ],
  "instructions": [ { "text": "Stir.", "phase": "prep_ahead" } ],
  "sources": [
    { "id": "s1", "kind": "url" },
    { "id": "s1", "kind": "publication", "label": "Example Cookbook" }
  ],
  "estimates": { "active_minutes": { "value": -5, "basis": { "kind": "sourced", "source_ref": "s9" } } }
}
```

Result: not saved; `structurally_valid: false`; errors, in this order:

| Code | Path |
| --- | --- |
| `required` | `/title` |
| `required` | `/ingredients/0/text` |
| `basis_required` | `/instructions/0/phase_basis` |
| `required` | `/sources/0/url` |
| `duplicate_id` | `/sources/1/id` |
| `invalid_value` | `/estimates/active_minutes/value` |
| `unknown_source_ref` | `/estimates/active_minutes/basis/source_ref` |

### 16.4 Decode-stage failure

`{"title": "Synthetic Toast", "status": "reviewed"}` → one error, `unknown_field` (server-assigned and not client-settable; a reviewed state does not exist). `{"title": ` → one error, `malformed_json`. Empty input → `malformed_json`. `{"title": "A", "title": "B"}` → one error, `duplicate_key` (not last-value-wins). `{"Title": "Synthetic Toast"}` → one error, `unknown_field` (field names are exact-case).

### 16.5 Stored file

`<data-root>/drafts/drf_0123456789abcdef0123.json` for the minimal example, after one create:

```json
{
  "schema_version": 1,
  "id": "drf_0123456789abcdef0123",
  "revision": 1,
  "status": "draft",
  "created_at": "2026-11-04T09:30:00Z",
  "updated_at": "2026-11-04T09:30:00Z",
  "title": "Synthetic Toast",
  "ingredients": [],
  "instructions": [],
  "sources": [],
  "guidance": []
}
```

### 16.6 Update and conflict

1. `draft_create` with the minimal example returns `draft.revision: 1`.
2. `draft_update` with `{"id": "drf_0123456789abcdef0123", "expected_revision": 1, "draft": {"title": "Synthetic Toast", "ingredients": [{"text": "2 slices bread"}]}}` returns `draft.revision: 2`; the warning `no_ingredients` is gone.
3. Repeating step 2 verbatim fails as a tool error and writes nothing:

```json
{ "error": { "category": "conflict", "code": "revision_mismatch",
             "message": "expected revision 1 but the draft is at revision 2; read it again and retry",
             "current_revision": 2 } }
```

### 16.7 List with a skipped entry

```json
{
  "drafts": [
    { "id": "drf_0123456789abcdef0123", "title": "Synthetic Toast", "status": "draft", "revision": 2,
      "created_at": "2026-11-04T09:30:00Z", "updated_at": "2026-11-04T09:41:00Z",
      "ingredient_count": 1, "instruction_count": 0, "source_count": 0, "agent_estimate_count": 0 }
  ],
  "total": 1, "offset": 0, "limit": 25, "has_more": false,
  "skipped": [
    { "id": "drf_aaaaaaaaaaaaaaaaaaaa", "category": "unsupported_schema",
      "message": "draft was written by a newer schema version (2); this build reads [1]" }
  ],
  "skipped_total": 1
}
```

## 17. Conformance checklist by issue

Each behavior has one owner for its tests; others rely on it.

| Behavior | Owner |
| --- | --- |
| Field rules, limits, enums, error/warning codes, unknown vs zero, basis rules, decoder strictness, service semantics, ordering, paging, deep-copy isolation, no import of forbidden packages; `memstore`; `storetest` suite; `duplicate_key` and exact-case names; nil-to-`[]` and all-absent-estimate normalization; negative limit/offset codes; Unicode rune counting (multi-byte, combining and astral characters); no LLM or network use; `gofmt`, `go vet` and build clean | #21 |
| Data-dir precedence and rejection of relative paths, layout, permissions, atomic write, interruption, `Create` never overwrites, revision conflict under two processes, lock timeout, containment (traversal, symlinks), corruption and unsupported schema (get, list skip, no rewrite), read cap, store bound, restart persistence, build tags and `CGO_ENABLED=0` cross-compile; runs `storetest`; the store cap counting unreadable draft-named files; `O_NOFOLLOW` opens; `.tmp-*` cleanup older than 24 hours and nothing else; `Replace` of a vanished draft; errors after the rename not failing the call; `canceled` vs `lock_timeout`; `encoded_too_large`; read-only data directory, unknown ID, empty store, no payloads in logs, default directory outside the checkout, synthetic fixtures only | #22 |
| Tool names/schemas/annotations, result duplication in `structuredContent` and `text`, error mapping, protocol-clean stdout, stderr logging without payloads, shutdown bounds, `--version`, subprocess round trip with restart; input schemas are root-only with plain typed properties and `required` for required ones (no bounds, patterns or `oneOf`), no `outputSchema`, `draft` passed raw to `DecodeContent`, `server_status` `data_dir.source` from the adapter option; protocol-version compatibility (current and legacy versions through the pinned SDK); malformed or unknown requests do not mutate the store; arguments cannot request shell, file or fetch actions; `invalid_arguments` and top-level `duplicate_key` argument errors; the shutdown gate for tracked requests | #23 |
| Commands, flags, `--json` single-document stdout, human-output requirements, exit codes, no mutation from validate/list/show, empty/malformed/oversized input, same results as core for shared fixtures, `version`/`--version` unchanged; `--limit 0` rejected with exit 2; `--data-dir` accepted by `list`/`show` only; the category/code to exit-code table; `--help` content; unreadable files and directories; unknown commands; cross-compile | #24 |
| End-to-end in both hosts, stale update, invalid input, restart persistence, CLI inspection of the MCP-created draft, privacy statement, CI path coverage; record OS, Go, SDK and client versions; per-host pass, fail or unavailable with evidence; README and layout links | #25 |

Shared fixtures: the examples in section 16 are the seed. #21 copies them verbatim as `testdata` (valid, invalid, expected issue code/path lists) and #23/#24 reuse them so MCP, CLI and core are compared on identical inputs.

## 18. Privacy and safety boundaries

- **Local only.** The tools persist to the local data directory. They do not publish, sync, fetch, or call any hosted API, OneNote/Microsoft Graph, or an LLM. Source URLs are stored and displayed as text and are never requested.
- **Content is data.** Recipe text, notes, labels and URLs are stored and returned verbatim and are never interpreted as instructions, commands or paths. Text returned by `draft_get` is untrusted input to the calling agent, as any user-authored text would be; nothing in the server acts on it.
- **Logs and errors.** Ordinary logs, error messages returned to hosts, and `server_status` never contain recipe content, titles, URLs or filesystem paths (section 8, section 13.3). Do not put credentials or tokens in a source URL; URLs containing userinfo are rejected, but query strings are not inspected.
- **Agent hosts.** Content used in an agent conversation can be sent by the chosen host to its model provider under that host's settings. The server being local does not make the whole workflow offline; #25 must state this accurately.
- **No implied approval.** Nothing returned by this system says a recipe is safe, reviewed or approved (decision 2). Estimates and guidance are labeled with their basis wherever they are shown.
- **Public repository.** Fixtures, tests and docs use synthetic data only; no real recipes, household details, account identifiers or private paths.

## 19. Revision history

- 2026-11-04 — v1. Initial contract for issue #20.
- 2026-11-04 — rev 2: QA/judge fixes. MCP input schemas reduced to root `object` with plain typed properties and `draft` passed raw (no `outputSchema`, no `id` form of `draft_validate`, `ValidateStored` and `exactly_one_of` removed); strict decode adds exact-case names and `duplicate_key`, `MaxDraftBytes` measured on `json.Marshal` of normalized content; store cap counts every draft-named file; rename is the commit point and later errors do not fail the call; nil/empty normalization; paging (`invalid_offset`, core `Limit` 0 = default, CLI rejects `--limit 0`); exported `Code…` constants, `Option`, `MaxIngredientShort`, `MaxStoredFileBytes`, `encoded_too_large`, `canceled`; `Replace` existence re-check, `O_NOFOLLOW`, 24-hour `.tmp-*` cleanup; `server_status` `data_dir.source` via adapter option; exit-code table and `--data-dir` scope; CI note updated for PR #26; checklist items added. Follow-up to rev 2: the schema keyword `required` is kept for genuinely required properties (discoverability); a missing one may be rejected by the SDK, as may a type mismatch or unknown root property, with the error path left to the pinned SDK (#23 documents it); bounds and patterns stay description-only.
- 2026-11-04 — rev 3: as-built alignment. `DecodeContent` also enforces `MaxInputBytes`; non-object or `null` root is `invalid_type`; invalid UTF-8 and nesting more than 64 levels below the root are `malformed_json`; unpaired surrogate escapes are `invalid_characters`; parse-pass errors precede shape errors; `draft.ValidateRaw` added and used by CLI and MCP. The MCP adapter has two codes of its own, `invalid_arguments` (argument mismatches) and, as a top-level code, `duplicate_key` (same key twice in the `arguments` root). MCP: SDK pinned at `v1.8.0` (supports `2026-07-28`, `2025-11-25`, `2025-06-18`, `2025-03-26`, `2024-11-05`; `initialize` negotiates up to `2025-11-25`; the stateless `2026-07-28` flow is `server/discover` plus per-request `_meta`), session-ending behavior for non-JSON and batch lines, the shutdown gate for tracked requests, `--help`, out-of-order completion, empty `resources/list` and `prompts/list`, SDK-default `idempotentHint: false` on read-only tools, `net/http` linked but unused. `private_permissions` is true for a root that does not exist yet. CLI help goes to stdout with exit 0; `draft show` human output is a summary; human output escapes control, Cf and U+2028/2029 characters (and tabs in single-line fields; multi-line fields keep newlines and tabs), also in validation messages and stderr, with UTC RFC 3339 timestamps, while `--json` is complete and for machines. Core: a bare `context.Canceled`/`DeadlineExceeded` from a store is `io/canceled`. Store: a file that vanishes mid-`List` is skipped silently; Windows skips the post-rename directory sync; read-only drafts directory message carries a writability hint.
