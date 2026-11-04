# Local Draft Contract

Status: contract v1 (`schema_version` 1), 2026-11-04. Resolves the decisions in issue #20 for the local draft slice tracked by epic #19.

This document is the shared contract for the local recipe-draft workflow. Go core (#21), local store (#22), MCP adapter (#23) and CLI (#24) are implemented from it, and host verification (#25) tests against it. All examples are synthetic.

Local drafts are a **staging workflow** that exists before the hosted API and database are connected. The long-term direction is unchanged: structured recipes live in hosted Postgres behind the authenticated Go API (see [architecture](architecture.md)). Nothing here replaces that, and nothing here is a hosted save.

## 1. Scope and non-goals

In scope: create, get, list, update and validate a local recipe draft, plus a local status read, exposed through a Go core, a stdio MCP server and a small CLI.

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
- **Strings.** Stored exactly as supplied: no trimming, case folding or Unicode normalization. Length limits count Unicode code points (runes). A required text field must contain at least one non-whitespace character. Control characters are rejected (`invalid_characters`) in every string except that `notes`, instruction `text`, and `guidance.text` may contain `\n`, `\r` and `\t`. The U+0000 character is always rejected.
- **Lists.** On input, an absent field, `null` and `[]` are equivalent. On output (API results and stored files) list fields are always emitted as `[]` and never `null` or omitted.
- **Optional scalars and objects.** Absent and `null` are equivalent on input. Optional strings are omitted from output when empty. Optional objects (`estimates`, `phase_estimates`, `phase_basis`) are omitted from output when absent.
- **Unknown fields** anywhere in input are an error (`unknown_field`), as are duplicate source IDs. Server-assigned fields (`schema_version`, `id`, `revision`, `status`, `created_at`, `updated_at`) are not accepted in input content.
- **Issue paths** are [JSON Pointers](https://www.rfc-editor.org/rfc/rfc6901) into the *content* object, for example `/ingredients/2/text`. Whole-object issues use `/`. Decode-stage issues (`malformed_json`, `unknown_field`, `invalid_type`) carry a path when the decoder can determine one, otherwise `/`; tests must assert their `code`, not their `path`.

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
	StructurallyValid bool    `json:"structurally_valid"` // len(Errors) == 0
	Errors            []Issue `json:"errors"`             // never nil
	Warnings          []Issue `json:"warnings"`           // never nil
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
	Limit  int // 0 means DefaultListLimit; > MaxListLimit is invalid_argument
	Offset int // >= 0
}

type ListPage struct {
	Drafts       []Summary      `json:"drafts"`
	Total        int            `json:"total"`         // readable drafts in the store
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

`Validate(Content) ValidationResult` is a pure function: no store, no clock, no I/O. `DecodeContent([]byte) (Content, []Issue)` is the single strict decoder (unknown fields rejected, exactly one JSON value, no trailing data, empty input is `malformed_json`) used by CLI file/stdin input and by the MCP adapter, so that both produce identical results for identical input. If decoding fails, the decode issue is the whole error list and semantic validation is skipped (the decoder reports at most one issue). Otherwise `Validate` reports **all** findings in schema order (field order, then list index), capped at 50 errors and 50 warnings (`truncated: true` if capped).

### 5.1 Errors (block the save; nothing is written)

| Code | Meaning |
| --- | --- |
| `malformed_json` | Not parseable as a single JSON object (including empty input, trailing data). |
| `input_too_large` | Raw input exceeds 256 KiB (checked by CLI input readers before decoding). |
| `unknown_field` | A field not in this schema, including server-assigned fields. |
| `invalid_type` | Wrong JSON type for a field. |
| `required` | A required field is missing or blank (`title`, `ingredients[].text`, `instructions[].text`, `sources[].id`/`kind`, `sources[].url` for kind `url`, a label or locator for other source kinds, estimate `value`, `basis.kind`, `guidance[].text`/`topic`). |
| `invalid_value` | Value outside its enum, range or pattern; bad URL; `phase_basis` without `phase`; `url` on a non-`url` source. |
| `invalid_characters` | Control characters not allowed for that field. |
| `too_long` | A string exceeds its limit. |
| `too_many_items` | A list exceeds its limit. |
| `draft_too_large` | Compact JSON encoding of the content exceeds 128 KiB. |
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
| `MaxDraftBytes` | 131072 (128 KiB) | Compact JSON encoding of `Content`. |
| `MaxInputBytes` | 262144 (256 KiB) | Raw bytes the CLI reads from a file or stdin. Reading stops at one byte over; over-limit is `input_too_large`. |
| `MaxStoredDrafts` | 1000 | Readable draft files in a data root. A create beyond this fails `limit_exceeded`. |
| `MaxTitleRunes` | 200 | `title` |
| `MaxYieldRunes` | 100 | `yield` |
| `MaxNotesRunes` | 4000 | `notes` |
| `MaxIngredients` | 100 | `ingredients` items |
| `MaxInstructions` | 100 | `instructions` items |
| `MaxSources` | 20 | `sources` items |
| `MaxGuidance` | 20 | `guidance` items |
| `MaxIngredientText` | 300 | `ingredients[].text`, `.note` |
| `MaxIngredientField` | 200 | `ingredients[].name`; `.quantity` and `.unit` are 50 |
| `MaxStepRunes` | 2000 | `instructions[].text` |
| `MaxGuidanceRunes` | 1000 | `guidance[].text` |
| `MaxLabelRunes` | 200 | `sources[].label`, `.locator` |
| `MaxURLBytes` | 2048 | `sources[].url` |
| `MaxNoteRunes` | 300 | `basis.note` |
| `MaxMinutes` | 10080 | minute estimate values (7 days) |
| `DefaultListLimit` / `MaxListLimit` | 25 / 100 | `draft_list` and `draft list` page size |
| `MaxSkippedReported` | 20 | skipped entries returned per list call |
| `MaxIssuesReported` | 50 | errors and warnings each, per validation |
| (store read cap) | 524288 (512 KiB) | A file larger than this is treated as `corrupt` without being fully read. |

## 7. Identifiers, revisions, ordering and paging

- **ID format.** `drf_` followed by 20 lowercase hexadecimal characters (80 random bits from `crypto/rand`), validated everywhere an ID enters the system by `^drf_[0-9a-f]{20}$`. Lowercase only, so IDs are not aliased on case-insensitive filesystems such as default macOS volumes. The ID contains no path separators, so a validated ID can only name a file directly inside the drafts directory. The service generates IDs; clients never supply them on create.
- **Collisions.** The store's `Create` fails with `conflict`/`already_exists` if the file exists; the service regenerates the ID and retries up to 5 times, then returns `internal`/`id_generation_failed`. Create never overwrites.
- **Revision.** `revision` is an integer starting at 1. A successful update sets it to `expected_revision + 1`. Revisions are never reused or decremented. An update with identical content still increments the revision and `updated_at` (simple and predictable; no no-op detection).
- **List order.** `updated_at` descending, then `id` ascending. The service sorts, not the store, so every `Store` implementation yields the same order.
- **Paging.** `limit` (default 25, max 100; a larger value is `invalid_argument`/`invalid_limit`, not clamped) and `offset` (≥ 0). Offset paging is accepted: this is a single-user local store, and a concurrent write between pages can shift entries. `total` is the number of readable drafts; `has_more` is `offset + len(drafts) < total`. An offset at or beyond `total` returns an empty page, not an error.
- **Timestamps** come from the service's clock (injectable for tests), converted to UTC and truncated to whole seconds. Clients cannot set them.

## 8. Errors

One error type and one set of categories are shared by core, CLI and MCP.

| Category | Meaning | Codes |
| --- | --- | --- |
| `invalid_argument` | The request is not acceptable: bad input, bad ID format, bad paging, bad data directory. Nothing was changed. | `validation_failed`, `invalid_id`, `invalid_revision`, `invalid_limit`, `invalid_data_dir`, `exactly_one_of` |
| `not_found` | No draft with that ID. | `draft_not_found` |
| `conflict` | The request lost a race or is stale. Nothing was changed. | `revision_mismatch`, `already_exists` |
| `corrupt` | A stored file is damaged: unparseable, wrong shape, ID not matching its file name, not a regular file, over the read cap. | `invalid_json`, `invalid_content`, `id_mismatch`, `not_regular_file`, `file_too_large` |
| `unsupported_schema` | A stored file has a `schema_version` this build cannot read. | `schema_unsupported` |
| `limit_exceeded` | A store bound would be exceeded. | `store_full` |
| `io` | The environment failed: permissions, missing or read-only directory, disk, lock not acquired in time. | `io_error`, `lock_timeout`, `data_dir_unavailable` |
| `internal` | A bug or impossible state. | `internal`, `id_generation_failed` |

`validation_failed` (category `invalid_argument`) carries the validation `issues` (errors) and `warnings`. `revision_mismatch` carries `current_revision`. Malformed JSON, unknown fields and other decode-stage failures on create/update are reported as `invalid_argument`/`validation_failed` whose `issues` hold the decode issue; the specific cause is the issue `code`.

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

- Every error returned by the `Service` and by any `Store` is (or wraps, so `errors.As` works) a `*draft.Error`. Anything else reaching an adapter is mapped to `internal`.
- `Message` is safe to show to an agent: it MUST NOT contain filesystem paths, recipe text, titles or source URLs. Issue messages describe the rule and may name the field path, never echo the value. `Err` may contain paths and is for stderr logs only.
- Adapters pass `category`, `code`, `message`, `issues`, `warnings`, `current_revision` through unchanged. They do not invent categories.

## 9. Core API (`internal/core/draft`)

### 9.1 Service

The service owns validation, ID and timestamp assignment, revision logic, ordering and paging. It imports no MCP, CLI, HTTP, database or provider code.

```go
func NewService(store Store, opts ...Option) *Service
func WithClock(now func() time.Time) Option               // default: time.Now
func WithIDGenerator(gen func() (string, error)) Option  // default: "drf_" + 20 hex from crypto/rand

func (s *Service) Create(ctx context.Context, in Content) (WriteResult, error)
func (s *Service) Get(ctx context.Context, id string) (Draft, error)
func (s *Service) List(ctx context.Context, opts ListOptions) (ListPage, error)
func (s *Service) Update(ctx context.Context, id string, expectedRevision int64, in Content) (WriteResult, error)
func (s *Service) Validate(in Content) ValidationResult                              // pure, touches no store
func (s *Service) ValidateStored(ctx context.Context, id string) (ValidationResult, error)
func (s *Service) Info(ctx context.Context) (StoreInfo, error)                       // store status for server_status

func DecodeContent(raw []byte) (Content, []Issue)  // strict decoder, see section 5
func Validate(in Content) ValidationResult         // what Service.Validate calls
```

Semantics:

- **Create.** Validate (errors → `invalid_argument`/`validation_failed`, no store call). Assign ID, `schema_version`, `status: "draft"`, `revision: 1`, `created_at = updated_at = now`. Call `store.Create`. Return the stored draft and the validation result (warnings only). A repeat call makes another draft.
- **Get.** Validate the ID format (`invalid_id`), then `store.Get`. Does not modify anything.
- **List.** Validate options. Call `store.List` once; sort; page; build summaries; report up to `MaxSkippedReported` skipped entries and the full `skipped_total`. Does not modify anything.
- **Update.** In order: ID format; `expectedRevision >= 1` (`invalid_revision`); validate content (errors → `validation_failed`); `store.Get` (not found / corrupt / unsupported propagate); if the stored revision differs from `expectedRevision` return `conflict`/`revision_mismatch` with `current_revision`; build the next draft (same ID and `created_at`, `revision = expected + 1`, new `updated_at`, new content); call `store.Replace`, which repeats the revision check atomically. A failed or conflicting update never changes the stored draft.
- **Validate.** `Service.Validate` checks candidate content only. `ValidateStored` loads a stored draft and validates its content; because stored drafts passed validation when saved, an error here means the rules were tightened after the save, and the stored file is still left untouched.
- **Isolation.** Values passed in and returned are deep-copied at the service boundary: mutating a returned `Draft` or a previously passed `Content` never changes stored state.
- **Context.** All methods honor `ctx` cancellation before any commit point (see section 10.4).

### 9.2 Persistence interface

The interface lives in `internal/core/draft` so the domain depends on no store implementation. #22 implements it; #21 supplies an in-memory implementation.

```go
// StoreInfo is the store's status, without recipe content or paths.
type StoreInfo struct {
	DataDirExists      bool
	PrivatePermissions bool // data root and drafts directory have no group/other access bits (always false where not supported)
	CrossProcessLock   bool // true when writes are protected by the cross-process lock
	DraftCount         int  // readable drafts
	SkippedCount       int  // unreadable entries
}

// ListResult is every readable draft plus a description of each entry that could not be read.
type ListResult struct {
	Drafts  []Draft
	Skipped []SkippedEntry
}

type Store interface {
	// Create stores d as a new draft. d is fully formed (ID, revision 1, timestamps) and
	// already validated by the service. It NEVER overwrites: if a draft with d.ID exists it
	// returns conflict/already_exists. If the store already holds MaxStoredDrafts it returns
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

1. `--data-dir <path>` flag on `mealplanner` and `mealplanner-mcp`.
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

Both `cmd/mealplanner` and `cmd/mealplanner-mcp` call `ResolveDataDir` then `Open`; neither re-implements the rules. The MCP server resolves and opens at startup; if that fails it writes the error to stderr and exits with status 2 without speaking the protocol. `--version` and `version` never resolve a data directory.

### 10.2 Layout and permissions

```text
<data-root>/                  0700, created by the first Create
  .lock                       0600, empty, never deleted
  drafts/                     0700
    drf_0123456789abcdef0123.json   0600, one draft
    .tmp-<random>                   transient, same directory as the target
```

- `Open` never creates or modifies anything. Read operations on a missing data root behave as an empty store.
- Directories are created with `0700` and files with `0600`. An existing data root is **not** `chmod`ed; if it has group/other bits, `Info.PrivatePermissions` is `false` and `server_status` reports it, and operation continues. Draft files are always written `0600`.
- File name is exactly `<id>.json`. `List` considers only names matching `^drf_[0-9a-f]{20}\.json$`; other names (including `.tmp-*` and `.DS_Store`) are ignored and not reported. The file's `id` must equal its name, else `corrupt`/`id_mismatch`.
- A draft file is the `Draft` JSON object, indented with two spaces, with a trailing newline, fields in the declared struct order, so files are readable and diff-friendly.
- **Containment.** IDs are validated before any path is built, and have no separators or `.`, so a path can only name a file directly in `drafts/`. In addition, `drafts/`, `.lock` and each draft file are checked with `Lstat` and must be a directory or regular file respectively; a symlink or other type is refused (`corrupt`/`not_regular_file` for a draft file, `io`/`data_dir_unavailable` for the directory). The data root itself may be a symlink the user chose. No symlink is followed inside the root.
- Default location is outside any checkout. If a user points `--data-dir` inside a checkout, keeping it out of Git is their responsibility; tests must use `t.TempDir()`.

### 10.3 Writes

`Create` and `Replace` follow the same steps; the lock is held only for the check and the rename, not while writing the temp file.

1. Marshal the draft; refuse (`internal`) if the encoded file would exceed the 512 KiB read cap.
2. Create `drafts/` (`MkdirAll`, `0700`) and `.lock` if missing (`Create` only; `Replace` of an absent draft is `not_found` without creating anything).
3. Write the content to `drafts/.tmp-<random>` in the **same directory** (so rename cannot cross filesystems), mode `0600`, `Sync()` the file, close it.
4. Acquire the cross-process lock (section 10.5).
5. Under the lock:
   - `Create`: count existing draft files (must be `< MaxStoredDrafts`), `Lstat` the target (must not exist), `rename` temp → target.
   - `Replace`: read and decode the current file (`corrupt`/`unsupported_schema` abort and leave it alone), compare its revision to `expectedRevision` (mismatch → `conflict`), check `next` per the interface rules, `rename` temp → target.
6. `Sync()` the `drafts/` directory (best effort where the platform does not support it).
7. Release the lock.
8. On any failure before the rename, remove the temp file. A crash can leave a `.tmp-*` file; it is ignored by reads and listing, never promoted, and may be deleted by hand.

The rename is the commit point. Readers see either the complete previous file or the complete new file, never a partial file. After the rename succeeds, a cancelled context cannot undo it; before it, cancellation aborts with no change.

Go's `os.File.Sync` is used for durability (it requests a full flush on macOS).

### 10.4 Reads and corruption

- `Get` and `List` take no lock; atomic rename makes that safe.
- Every file read is limited to the 512 KiB cap; a larger file is `corrupt`/`file_too_large` without being read in full.
- Decoding first reads only `schema_version`. If it is missing or not an integer, the file is `corrupt`/`invalid_content`. If it is not in `SupportedSchemaVersions`, the file is `unsupported_schema`/`schema_unsupported` (a newer version of the tools wrote it). Only then is the rest decoded strictly (unknown fields are `corrupt`/`invalid_content`, because a same-version file must match the schema exactly). The stored content is then checked with the structural rules that do not depend on warnings: `status == "draft"`, `id` matches the file name, `revision >= 1`.
- `Get` of a damaged file returns the error. `List` omits it from `drafts`, adds a `SkippedEntry` (with `id` when the name is well-formed, category, and a message without paths or content), counts it in `skipped_total`, and continues. **Nothing is ever deleted, repaired, rewritten or moved automatically.** `Replace` of a damaged draft fails and leaves the file as it is; the user decides what to do with it by hand.
- A missing data root, an empty `drafts/` directory and a directory containing only ignored names are all an empty store.

### 10.5 Concurrency

Two layers, both required:

1. **Optimistic revision.** Every update states the revision it was based on. A stale update is `conflict`/`revision_mismatch` and writes nothing. The service checks early (to fail fast) and the store re-checks authoritatively under the lock.
2. **Cross-process advisory lock.** `flock(2)` with `LOCK_EX` on `<data-root>/.lock`, so the "read current revision, compare, rename" sequence is atomic across two MCP server processes (and the CLI). `flock` is used rather than `fcntl` byte-range locks because it is associated with the open file description: a fresh `open` of the lock file per operation gives mutual exclusion between goroutines in the same process as well as between processes. The lock is released by `LOCK_UN` and by the kernel if the process dies, so there is no stale-lock cleanup.

Lock scope and timeout:

- **Scope:** one lock for the whole data root, held by `Create` and `Replace` only, for the duration of steps 5–6 above (a few file operations, not the temp-file write). Reads never take it.
- **Acquisition:** non-blocking attempts (`LOCK_EX|LOCK_NB`) polled every 25 ms until acquired, `ctx` is done, or `Options.LockTimeout` (default 5 s) elapses. Timeout returns `io`/`lock_timeout` ("another process holds the draft lock; retry"), which is retryable and distinct from `conflict`. Context cancellation returns the context error mapped to `io`/`lock_timeout` if it occurred while waiting.
- **Limits:** the lock file must be on a local filesystem. Network filesystems are not supported (advisory locks are unreliable there).

### 10.6 Portability

- The code is pure Go (stdlib only), builds with `CGO_ENABLED=0`, and the existing cross-compile checks (windows/amd64, linux/arm64, darwin/arm64) must keep passing.
- Locking is behind build tags: `lock_flock.go` with `//go:build darwin || linux` uses `syscall.Flock`; `lock_other.go` with `//go:build !darwin && !linux` uses only an in-process mutex. On those platforms `Info.CrossProcessLock` is `false` and `server_status` reports it: the optimistic-revision check still catches most stale writers, but two processes can race between the check and the rename, so concurrent multi-process writes are **not supported** there. This limitation is documented, not hidden. Directory `Sync` errors and `chmod` bits are best effort on Windows, where `Info.PrivatePermissions` reports `false`.
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
| `internal/mcpadapter` | MCP server construction, tool schemas, handlers, error and result mapping. | `internal/core/draft`, `internal/core/version`, the MCP SDK, stdlib. | `internal/localstore` (the store is injected via `*draft.Service`). |
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

`.github/scripts/classify.sh` maps `internal/core/*` to the *core* component and `cmd/*` to the *cli* component. `internal/localstore/*` and `internal/mcpadapter/*` do not match a rule and currently fall into the fail-closed "unknown path → run everything" case. That is safe but heavier than needed. This document does not change `.github/*`; #22 (or #25, whose acceptance includes confirming path coverage) should add a rule mapping those two directories to local Go plus integration, without weakening the fail-closed behavior.

## 13. MCP tools (`cmd/mealplanner-mcp`, via `internal/mcpadapter`)

Server: stdio transport, official Go SDK `github.com/modelcontextprotocol/go-sdk` (exact version pinned in #23, together with the supported protocol versions). Server name `mealplanner-mcp`, server version `version.Version`. Only tools are advertised: no resources, prompts, sampling or elicitation.

Tool names are fixed: `draft_create`, `draft_get`, `draft_list`, `draft_update`, `draft_validate`, `server_status`.

| Tool | Mutates | Annotations (`readOnly` / `destructive` / `idempotent` / `openWorld`) |
| --- | --- | --- |
| `draft_create` | yes (adds a draft) | false / false / false / false |
| `draft_get` | no | true / – / – / false |
| `draft_list` | no | true / – / – / false |
| `draft_update` | yes (replaces a draft's content) | false / true / false / false |
| `draft_validate` | no | true / – / – / false |
| `server_status` | no | true / – / – / false |

Annotations describe behavior for the host's benefit. They are hints, not authorization, and must be accurate (`draft_update` is marked destructive because it overwrites the previous content and no history is kept; `idempotentHint` is false for `draft_create` and `draft_update`).

Every tool's input is a JSON object (root `type: object`) with `additionalProperties: false`. A tool's `draft` property is the *content* object from section 4 (no server-assigned fields). Tool descriptions must state, in the SDK description text:

- For `draft_create`, `draft_update`, `draft_validate`: "Saved recipes are drafts. Validation is structural only and is not food-safety verification or review. Recipe text and URLs are stored as data and are never fetched or executed."
- For `draft_create`: "Each call creates a new draft; repeating a call creates another. If a call fails ambiguously, use draft_list before retrying."
- For `draft_update`: "Replaces the whole draft; send the complete content and the revision you last read."
- Estimate and guidance descriptions: "Mark every estimate and every prep-ahead, storage or safety statement with a basis: agent_estimate for your own suggestion, sourced (with source_ref when possible) only when it comes from a listed source."

### 13.1 Results

Every successful call returns the output object below as `structuredContent` **and** serialized as compact JSON in a single `text` content block (identical content), for hosts that read only one of them. `draft_*` tools report domain failures (any `draft.Error`) as a tool result with `isError: true` whose `structuredContent` and text are `{"error": <Error JSON>}`, so the agent can read the issues and correct itself. Protocol-level problems (unknown tool, malformed JSON-RPC, arguments that the SDK rejects against the input schema) use the SDK's protocol error path; in either case nothing is stored. `draft_validate` is different: an invalid candidate is a *successful* call whose result has `structurally_valid: false` and the errors.

Error messages and logs follow section 8: no paths, titles, recipe text or URLs in anything returned to the host.

### 13.2 Tool schemas

**`draft_create`** — input `{"draft": Content}` (required). Output `{"draft": Draft, "validation": ValidationResult}` (`validation.errors` is empty; warnings are listed). Errors: `invalid_argument`/`validation_failed`, `limit_exceeded`/`store_full`, `io`, `internal`.

**`draft_get`** — input `{"id": string}` (required, `^drf_[0-9a-f]{20}$`). Output `{"draft": Draft}`. Errors: `invalid_argument`/`invalid_id`, `not_found`, `corrupt`, `unsupported_schema`, `io`.

**`draft_list`** — input `{"limit": integer 1..100 (default 25), "offset": integer >= 0 (default 0)}` (both optional). Output is `ListPage` (section 4.1). Errors: `invalid_argument`/`invalid_limit`, `io`. Unreadable files are not errors; they appear in `skipped`.

**`draft_update`** — input `{"id": string, "expected_revision": integer >= 1, "draft": Content}` (all required). Output `{"draft": Draft, "validation": ValidationResult}`. Errors: `invalid_argument` (`invalid_id`, `invalid_revision`, `validation_failed`), `not_found`, `conflict`/`revision_mismatch` (with `current_revision`), `corrupt`, `unsupported_schema`, `io` (including `lock_timeout`), `internal`.

**`draft_validate`** — input is **exactly one** of `{"draft": Content}` (a candidate, not saved) or `{"id": string}` (validate a stored draft); both or neither is `invalid_argument`/`exactly_one_of`. Output `{"validation": ValidationResult}`. Never writes. Decode-stage problems in a candidate (unknown field, wrong type) are returned as validation errors, not as tool errors. Errors (for the `id` form): `invalid_id`, `not_found`, `corrupt`, `unsupported_schema`, `io`.

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

`data_dir.source` is `flag`, `env` or `default`. The data directory **path is deliberately not returned**: tool output is placed in the agent conversation and may be sent to a model provider, and the path can contain a user name. The user knows the path they configured. `server_status` does not fail because the store has a problem; it sets `store.available: false` and adds `store.error: {"category": ..., "message": ...}` instead. It makes no network calls and contains no recipe content, titles, URLs or secrets. Update checks and remote release information are out of scope for this slice.

### 13.3 Process behavior

- **stdout is protocol only.** No banner, notice, log line or stray `fmt.Print` may reach stdout. Diagnostics go to stderr.
- **Logging.** One structured line per tool call to stderr: tool name, outcome (`ok` or error category and code), duration, and the draft `id` when there is one (IDs are random and carry no content). Never titles, text, URLs, issue values, or the data directory path.
- **Shutdown.** On stdin EOF, `SIGINT` or `SIGTERM` the server stops accepting calls, lets in-flight calls finish for up to 5 seconds, and exits 0 on EOF or signal with no call outstanding, 1 if the deadline passed. Context cancellation of an in-flight call aborts it before its commit point; a persisted draft is never partial (section 10.3).
- **Flags.** `--data-dir <abs path>` and the existing `--version` (prints `mealplanner-mcp <version>` and exits 0 without resolving a data directory). Unknown flags exit 2 with usage on stderr.
- **No capability beyond the tools above**: no tool takes a path, command, or URL to act on, and none fetches, publishes or calls out.

## 14. CLI (`cmd/mealplanner`)

Existing behavior is unchanged: `mealplanner version`, `mealplanner --version`, no arguments prints usage to stderr and exits 2, an unknown command exits 2.

```text
mealplanner draft validate <file|-> [--json]
mealplanner draft list   [--limit N] [--offset N] [--data-dir DIR] [--json]
mealplanner draft show   <id>       [--data-dir DIR] [--json]
```

- `draft validate` reads one JSON object of **content** (section 4) from the named file, or from stdin when the argument is `-`. A missing argument is a usage error (so an interactive shell never hangs on stdin). It reads at most `MaxInputBytes`, uses `DecodeContent` and `Validate`, never touches the data directory or any store, and never writes. It does not accept `--data-dir`.
- `draft list` and `draft show` read the same store, with the same data-directory rules, as the MCP server. They never write. `--limit`/`--offset` follow section 7.
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
- `draft show` starts with the ID, revision and the line `status: draft (not reviewed or verified)`. Every item that has a basis is printed with it: agent estimates as `[agent estimate, unverified]`, sourced items as `[sourced: <source_ref or "no reference">]`. Unknown estimates are shown as `unknown`, never as `0`. Provenance sources are listed with their kind and URL (printed as text, never fetched).
- `draft list` prints one line per draft with ID, revision, `updated_at` and title (and a final line noting any skipped unreadable entries and their count).
- Data written to stdout never includes the data directory path. Error text on stderr may.

### 14.2 Exit codes

| Code | Meaning | Examples |
| --- | --- | --- |
| 0 | Success. **Warnings do not change the exit code.** | `draft validate` of a draft with only warnings; `draft list` of an empty store. |
| 1 | The operation ran and the subject was rejected on its merits. | `draft validate` found errors (including malformed JSON, empty input, input over 256 KiB, unknown fields); `draft show <id>` for an ID that does not exist. |
| 2 | Bad invocation: anything detectable from the arguments and environment alone. | Unknown command or flag; missing argument; `show` with an ID that fails the format check; `--limit` out of range; relative or empty data directory; no default data directory available. |
| 3 | Operational failure: the environment or the stored data failed. | Input file missing or unreadable; data directory unreadable; corrupt or unsupported-schema draft (`show`); lock timeout; internal error. |

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

`{"title": "Synthetic Toast", "status": "reviewed"}` → one error, `unknown_field` (server-assigned and not client-settable; a reviewed state does not exist). `{"title": ` → one error, `malformed_json`. Empty input → `malformed_json`.

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
| Field rules, limits, enums, error/warning codes, unknown vs zero, basis rules, decoder strictness, service semantics, ordering, paging, deep-copy isolation, no import of forbidden packages; `memstore`; `storetest` suite | #21 |
| Data-dir precedence and rejection of relative paths, layout, permissions, atomic write, interruption, `Create` never overwrites, revision conflict under two processes, lock timeout, containment (traversal, symlinks), corruption and unsupported schema (get, list skip, no rewrite), read cap, store bound, restart persistence, build tags and `CGO_ENABLED=0` cross-compile; runs `storetest` | #22 |
| Tool names/schemas/annotations, result duplication in `structuredContent` and `text`, error mapping, protocol-clean stdout, stderr logging without payloads, shutdown bounds, `--version`, subprocess round trip with restart | #23 |
| Commands, flags, `--json` single-document stdout, human-output requirements, exit codes, no mutation from validate/list/show, empty/malformed/oversized input, same results as core for shared fixtures, `version`/`--version` unchanged | #24 |
| End-to-end in both hosts, stale update, invalid input, restart persistence, CLI inspection of the MCP-created draft, privacy statement, CI path coverage | #25 |

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
