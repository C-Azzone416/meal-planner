# Hosted draft contract

Status: proposed contract HD1, 2026-11-11, issue #72. Based on reviewed main
`e73fab6`. Requires independent review and Caroline's product decisions below.
This is design and synthetic test data, not an implemented API or permission to
resume #55. #73 must coordinate auth/environment readiness; #25 coordinates owner
Preview execution. #55 remains paused and cannot ship Production until Caroline
personally completes and confirms the owner test.

## 1. Boundary, versions, and authority

The hosted draft is a persisted, account-owned **unreviewed** work item. Its
canonical `content` is exactly `internal/core/draft.Content` schema 1, as specified
in [the local contract](local-draft-contract.md), sections 3–6. It is not
`hosted.RecipeContent` and must not be decoded into the current web `Draft` type.
The title-only minimum and structural validation rules remain unchanged.

| Version | Meaning | Rule |
| --- | --- | --- |
| `/api/draft-v1` | Proposed hosted HTTP API major version | Separate from `/api/recipes`; never register these routes in this issue. |
| `api_version: "draft-v1"` | Server response protocol marker | Required on draft/receipt responses. |
| `content_schema_version: 1` | Submitted content version | Required request selector, not an assertion about stored state; supported value is 1 only. |
| `schema_version: 1` | Server envelope version | Read-only; independent of local envelope and application build. |
| `review.policy_version` | Approved review/projection policy | Server-configured version or null while unresolved; no client may choose it. |

Breaking content changes require a new content schema and explicit migration/read
support; breaking HTTP changes require a new API major. Never downgrade, coerce an
unsupported version, strip unknown fields, or rewrite old snapshots with a newer
schema. Deployment must advertise compatible versions before a future local send.

Ownership is exactly `(verified token issuer, verified token subject)`, reusing
#61 `RecipeOwner`; email, display name, local draft ID, tool host, Framebriar and
caller JSON are not authority. Validate the configured environment's token issuer,
audience and session before looking up data. Preview and Production have separate
storage, auth configuration, keys and receipts, even for the same subject. Every
read/write/list/receipt/approval query includes both owner fields. After
authentication, check operation capability **before** owner-scoped object
lookup. A valid credential lacking that capability returns 403
`operation_not_allowed` for every target, including absent and foreign IDs. For a
capable credential, another owner's well-formed ID returns the same 404 as an
absent ID. Never return their revision,
title, receipt or existence. Signed out returns 401. Responses use `no-store`.

The send credential may submit and read its own drafts/receipts, but cannot review,
approve, or write manual recipes. Review and approval require the signed-in app
owner through an app-authorized operation. This capability boundary needs #73's
implementation design; the current manual token alone does not prove this split.
Thus a send credential attempting approval receives 403 even for another owner's
ID; an app-owner credential attempting that same foreign ID receives 404.

## 2. Submission, envelope, and receipts

A submission is an explicit send of one captured local content snapshot. POST
`/api/draft-v1/submissions`, with `Content-Type: application/json` and required
`Idempotency-Key` (1–200 ASCII `[A-Za-z0-9_-]` characters), accepts **only**:

```json
{"content_schema_version":1,"content":{"title":"Synthetic draft","ingredients":[],"instructions":[],"sources":[],"guidance":[]}}
```

HD1 deliberately differs from #61's lowercase UUID-only Idempotency-Key: bounded
opaque keys allow a local sender to retain an operation identifier without a UUID
representation dependency. Keys are case-sensitive, never IDs or auth, and must
not contain recipe text. Exactly one header is required on every mutation; no
trimming or joining repeated headers. Missing/empty is 400
`missing_idempotency_key`; repeated, non-ASCII, overlong or nonmatching is 400
`invalid_idempotency_key`. #61's existing endpoints keep their UUID requirement.

All object properties are exact-case. Reject duplicate keys at every depth,
unknown properties, invalid Unicode, trailing JSON, wrong types, and root null.
Use the core raw decoder and structural validator on `content`, never a map that
has already dropped duplicate keys. Root fields are both required and non-null;
version must use JSON integer syntax (not a string, boolean, decimal, or exponent).
Only `1` is supported: **any other integer token**, including values beyond machine
integer range, returns 422 `unsupported_content_schema`; do not parse it into a
fixed-width integer before this comparison. Noninteger syntax/null returns 400
`invalid_type`.
Absent content returns 400 `invalid_request`; null content returns 400
`invalid_type`; canonical content's optional null/absence rules still follow the
local contract. Unknown fields are errors even if null.

Caller-supplied `owner`, `owner_issuer`, `owner_subject`, `approval`, `approved`,
`status`, `origin`, `id`, `receipt_id`, `recipe_id`, `revision`, `created_at`,
`updated_at`, `schema_version`, `review` or envelope fields in either root or
content are rejected with 400 `unknown_field`. They are not silently ignored.
Content `sources[].id` and `basis.source_ref` are legitimate provenance labels,
not authoritative IDs. A local `Draft` envelope is never a submission body.

A successful first send returns 201 with this response shape (examples in the
fixture pack are authoritative synthetic values):

| Object/field | Type / ownership |
| --- | --- |
| root `api_version`, `draft`, `receipt`, `validation` | All required, server-produced. |
| `draft.schema_version`, `draft.content_schema_version` | Integers, both 1. |
| `draft.id` | Server-generated lowercase UUID; distinct from local `drf_…`. |
| `draft.revision` | Positive JS-safe integer, initially 1; content/candidate/lifecycle concurrency token. Never caller-assigned. |
| `draft.status` | `draft` initially; HD1 default (proposed), pending P4: `approved`, `discarded` or `archived` terminally; undo may add pending states (section 4). |
| `draft.origin` | Always `agent`; describes ingress, not verification. |
| `draft.owner` | `{issuer, subject}` from verified session, server read-only, owner-only response. Never use a label as an auth check. |
| `draft.created_at`, `draft.updated_at` | Server UTC RFC3339 whole seconds. Creation never changes. Revision, not clock order, resolves races. |
| `draft.content` | Editable canonical Content; initially equal to immutable submission content. Terminal purging may remove it only under the agreed retention policy. |
| `draft.review` | `{revision, state, draft_revision, policy_version, candidate, confirmations}`; initially revision 1, state `not_started`, draft_revision 1, null policy/candidate, empty confirmations. Optional `confirmations_next_cursor` only if another page exists (section 3.1). |
| `draft.approved_recipe_id` | UUID or null; initially null, set only by atomic approval. |
| `receipt.id`, `receipt.draft_id` | Server UUIDs. Receipt links to original work item, never a new one on replay. |
| `receipt.submitted_at`, `receipt.accepted_revision` | Immutable acceptance time and initial revision (1). |
| `receipt.content_schema_version`, `receipt.submission_content` | Immutable canonical version and content snapshot; never overwritten by web or local edits. |
| `receipt.links.receipt`, `receipt.links.draft` | Server-generated relative owner-authenticated app paths `/draft-receipts/{receipt UUID}` and `/drafts/{draft UUID}`; resolve only against the configured send destination. |
| `validation` | Core `ValidationResult`, scope `structural`, fixed notice, errors/warnings arrays, `truncated` when capped; never approval. |

The immutable receipt does not contain a frozen editable draft envelope. On replay,
return 200 with the **same receipt** and the **current draft** (or terminal summary),
plus current structural validation when content remains. Response header
`Idempotency-Replayed: true` identifies replay (false on 201). Receipt acceptance
is historical; its links may now lead to approved/discarded/archived work. A send
success uses the proposed P1 label "Saved to Preview as an unreviewed draft"
(**HD1 default (proposed), pending P1**). Success must communicate unreviewed
acceptance; exact copy awaits P1 and must never claim approval.

GET `/api/draft-v1/drafts/{id}` returns 200 with the current envelope and validation,
or 200 with the owner-scoped terminal summary after content purge while its
identifier/tombstone remains. If P4 removes that identifier after namespace
retirement/account deletion, GET returns 404; it never returns 410 for a read.
410 `submission_retired` applies to submission replay when content is gone;
a committed approval replay still identifies the same approved recipe.
GET `/api/draft-v1/receipts/{id}` likewise returns 200 with the immutable receipt
plus current status and approved recipe link if any, or the retained summary after
purge; 404 after identifier retirement. Links require owner authentication on
every load.
A receipt is proof of acceptance into that environment, not proof of recipe safety,
approval, current content, or Production storage. Purged terminal content uses a
summary response with no content/validation, explicitly `content_retained: false`;
never substitute an empty recipe. Exact expiry is an open product decision.

## 3. Complete content mapping and bounds

The following mapping is identity from canonical local Content into hosted
`content`, immutable `receipt.submission_content`, and app edit-state `content`.
All nested fields survive a no-edit API → app → API round trip. App editing uses a
new content model beside the existing planner seam. Never use `recipeForm.ts`
`toInt` (blank → 0), trim strings, infer phases, normalize quantities, sort lists,
or reconstruct content from the current five guess rows. Omitted values render
**Unknown**, not 0, false, "nothing required", or "no limit". Manual projection
requirements in this table describe the **HD1 default (proposed), pending P3**;
the canonical lossless mapping is required for every P3 option.

| Local field (all nested fields enumerated) | Hosted / app display and editing | Existing manual/planner incompatibility and approval treatment |
| --- | --- | --- |
| `title` | Same string; text editor, preserving spaces and Unicode. Required, 1–200 runes. | Manual max 120. Keep full title in draft; explicit owner edit to ≤120 or a separately approved broader recipe schema. No truncation. |
| `yield` | Optional text, ≤100 runes; display original yield. | Not numeric `defaultServings`. Owner supplies 1–12 servings for projection; never parse `Serves 4–6` into 4. Preserve original yield. |
| `ingredients[]` order, `text` | Raw line ≤300 runes, always retained and editable, even with structured parts. | Manual has no raw text; keep alongside projected ingredient. At least one projected ingredient before approval. |
| `ingredients[].quantity` | Optional string ≤50, including `1 1/2`, `to taste`, `0`, or unknown. | Manual positive numeric quantity or null; never cast, round or substitute null for an unparsed amount. Explicit owner mapping/exclusion decision, retaining raw amount. |
| `ingredients[].unit`, `.name`, `.note` | Independent optional strings, maxima 50/200/300; raw text is not a fallback write to name. | Owner supplies required manual name and unit (empty unit allowed); note remains in canonical content. No automatic ingredient normalization. |
| `instructions[]` order, `.text` | One ordered list, text ≤2000; optional phase editor with Unclassified choice. | Manual separates lists and requires a cook step; retain original order and explicit mapping to generated row IDs. Owner resolves unclassified steps; never default to cook. |
| `instructions[].phase` | Absent or `prep_ahead`, `prep_tonight`, `cook_tonight`; exact value retained. | Explicit phase groups are a view only. No per-step minutes or timer is implied. |
| `instructions[].phase_basis` | Required for prep_ahead, optional for other present phases, forbidden without phase. Show basis beside that step. | No manual basis field; retained with per-claim confirmation, never flattened into storage text. |
| `notes` | Optional multiline text ≤4000, preserved line breaks. | No manual field; read-only original plus editor in new content view, retained after approval. |
| `sources[]` order, `.id`, `.kind`, `.label`, `.url`, `.locator` | Same provenance entries. ID matches `[a-z][a-z0-9_-]{0,31}`, unique. Kind `url`, `publication`, `user_supplied`, `agent_generated`. Label/locator ≤200. URL ≤2048 bytes, core HTTP(S) validation. Source link/locator displayed with claims. | No manual provenance fields; immutable original and reviewed canonical content retained alongside projection. Do not fetch URLs or imply a source was checked. |
| `estimates.active_minutes`, `.total_minutes` | Optional objects `{value: integer 0..10080, basis}`; preserve present zero, show missing as Unknown. | Manual requires active 0..600, total 0..10080 and total ≥active. Require explicit reviewed values in range, never clamp or use 0 for absence. |
| `estimates.attention`, `.cleanup`, `.variability` | Optional `{value: low/medium/high, basis}` with separate per-claim review. | Only attention exists in manual model and is required there. Cleanup/variability remain canonical metadata; no silent loss. |
| `phase_estimates.prep_ahead`, `.prep_tonight`, `.cook_tonight` | Each optionally contains all five Estimates fields above, with identical bounds and bases. | No manual phase aggregate. Do not distribute totals among steps or sum them into whole-recipe times. Retain all five values per phase. |
| Every `basis.kind`, `.source_ref`, `.note` | Kind `agent_estimate` or `sourced`; optional source_ref points to same-content source ID; optional note ≤300. Display "Agent estimate · unverified" or "Submitter cites source · unverified", source and note. | Confirmation records owner's review, not a fact-check or food-safety certification. `sourced` without ref stays a warning, never becomes verified. |
| `guidance[]` order, `.topic`, `.text`, `.basis` | Topic `prep_ahead`, `storage`, `food_safety`, `other`, text ≤1000, basis required. Each displayed and confirmed separately. | No direct mapping to `keepsDays`/`storageNote`. Explicit owner association and sourced-policy decision required; never extract days or invent unlimited keeping. |
| Local `schema_version`, `id`, `revision`, `status`, `created_at`, `updated_at` | Not content and never copied into authoritative hosted fields. Future local send ledger may record `(local ID, revision, content snapshot, destination, operation key, receipt)` locally. | Local timestamps/IDs do not authorize ownership or edits. HD1 default (proposed), pending P5: a local update never updates an existing hosted item; explicit replacement is an alternative. |
| Manual-only `shortName`, `icon`, `tags` | New owner-edited projection fields, absent until supplied or an explicit product-approved presentation policy applies. | No fabricated recipe data. Cosmetic fallback shown in UI must not be serialized as authored content. |
| Manual ingredient `id`, `aisle`, `isPantry` | Server generates projected row IDs; owner supplies classification in projection. Canonical array index + draft revision links back to original line. | No automatic "Other"/false/pantry exclusion until product policy allows it. Current pantry false/true directly affects shopping. |
| Manual prep `label`, `minutes`, `keepsDays`, `storageNote`; cook `timerSeconds` | Owner fills explicit projection fields, preserving raw step/guidance mappings. Per-step numeric claims are separately reviewed. | Missing minutes ≠0; unknown keepsDays ≠null (current planner null means no limit worth warning about). Optional timers can be null only by explicit "no timer" decision. Policy P3 blocks these conversions. |

Canonicalization is exactly the existing core's semantic equivalence: absent,
null and empty lists normalize to `[]`; absent/null optional values are unknown;
empty optional strings and empty estimates objects are omitted. Lossless means
preserving all **canonical content**, not byte-for-byte whitespace/key order or
these intentionally equivalent input forms. No Unicode normalization. Unknown
estimate is an omitted object; a present minutes object requires both `value`
and `basis`, and a present zero remains 0. Source refs and bases are never removed
because a field has been confirmed.

Limits are engineering limits of HD1: submission body ≤262144 raw bytes; canonical content
≤131072 encoded bytes; ≤100 ingredients, ≤100 steps, ≤20 sources, ≤20 guidance.
All per-field rune/byte, enum, control-character, reference, relation and warning
rules are the core schema 1 validator's rules (`types.go`, `limits.go`,
`validate.go`); issue codes and pointers are reused. Up to 50 errors and 50 warnings
with `truncated: true` at the cap. These are not food-safety checks. Local file-store
1000-draft capacity is not a hosted account quota; quota policy is P6.

### 3.1 List and errors

GET `/api/draft-v1/drafts?limit=25&after=…` returns only the authenticated owner's
**active** drafts (`status == draft`, regardless of review progress,
normally `not_started`, `in_progress` or `ready`; exclude approved, discarded and
archived under the proposed P4 lifecycle)
as summaries `{id,title,status,revision,created_at,updated_at,
review_state}` and `next_cursor` (opaque string or null). Limit is integer 1–100,
default 25. Reject repeated/unknown query keys and invalid/foreign-filter cursors.
Cursor binds API major, owner, environment, filter and last ordering key.
**HD1 default (proposed), pending P1:** stable ascending UUID keyset ordering.
P1 may instead choose newest-created or most-recently-edited first, each with UUID
as a tie-breaker; that changes the cursor key and list UX acceptance. Pages are not
snapshots of concurrent sends/discards. Terminal receipt lookup remains possible outside this
active list. Never skip unreadable rows silently; fail 503 with no partial page.

All responses ≤3,500,000 encoded bytes (decimal, matching #61); pages stop before
adding a whole item that crosses that bound and return a continuation cursor. Summaries omit content; detail and
receipt responses have bounded content, no recursive history. No splitting one
item or cursor inferred from returned count. Empty result is `[]`, null cursor.
No unbounded receipt/history list API in HD1. Candidate plus projection mappings
is bounded to 262144 canonical encoded bytes (413 on a replacement overage).
Review audit/confirmations are stored separately, not charged against that cap.

| Mutation | Maximum raw body bytes | Additional bound |
| --- | --- | --- |
| Submission POST | 262144 | Canonical content ≤131072 bytes; existing core encoding for this size check. |
| Content/candidate PUT | 3,500,000 | Canonical content ≤131072; canonical candidate + mappings ≤262144. This admits both maxima together, including up to sixfold JSON escaping plus wrapper. |
| Review POST | 262144 | At most 256 confirmation deltas per request; path ≤256 UTF-8 bytes, claim_kind ≤64 ASCII bytes. Send multiple batches using returned review revision. |
| Approve POST | 1024 | Only the two revision tokens; no resubmission of content/candidate/confirmations. |
| Discard/archive POST | 1024 | Only the two revision tokens. |

There is **no total required-review-item cap**: the former 512-item cap is removed.
A valid maximum-size draft cannot become unapprovable because of its claim count.
GET `/api/draft-v1/drafts/{id}/review?limit=100&after=…` enumerates all required items
and stored confirmations with owner/draft/review/policy-bound cursors, limit 1–256,
and the response byte bound above. Stale cursors return 409 `revision_mismatch`.
The envelope's `review.confirmations` is the first bounded page; optional
`confirmations_next_cursor` is present only when another page exists. Never derive
readiness from this first page: approval checks every persisted item. Pagination
stops on whole items before the byte bound. Each review record is bounded to 4096
encoded bytes (IDs/policy/reviewer stored as bounded server IDs; no copied claim
text), so one record always fits. A future policy must enumerate bounded items
from bounded content/candidate fields and allow batching; it cannot impose a lower
total-item storage cap. Review grouping/count UX remains pending P1–P3. Histories,
if added, also need separate pagination.

Errors use `{api_version:"draft-v1",error:{code,message,issues:[],retryable,
outcome}}` where `outcome` is `not_committed` or `unknown`. Messages never echo
recipe text, tokens, database errors, foreign IDs or secrets. Content issue paths
are core JSON pointers relative to content; root issues use request pointers.

| HTTP / code | Meaning and caller behavior |
| --- | --- |
| 400 `invalid_request`, `unknown_field`, `duplicate_key`, `invalid_type`, `invalid_characters` | Malformed/ambiguous request; no write; fix input. |
| 400 `missing_idempotency_key` / `invalid_idempotency_key` | Missing/empty key / malformed or repeated key; no write. Applies to every mutation. |
| 400 `invalid_content` | Structurally invalid content; issues use core codes; no write. |
| 400 `invalid_cursor` | Cursor invalid for this owner/environment/filter; no foreign metadata. |
| 401 `unauthorized` / 403 `operation_not_allowed` | Missing session / valid credential lacks operation capability; no write. |
| 404 `not_found` | Absent or other owner's object, including receipt. |
| 409 `idempotency_conflict` | Same owner/environment/operation key, different canonical request (version+content for submission); no write. |
| 409 `revision_mismatch` | Stale draft or review revision; return own current revision tokens only. Preserve unsaved client work, reload and resolve. |
| 409 `review_incomplete`, `policy_unresolved`, `already_terminal` | Approval gate unmet / product policy absent / forbidden lifecycle transition; no write. |
| 410 `submission_retired` | Key belongs to purged/discarded work whose content response is no longer retained; terminal receipt summary only, no new draft. |
| 413 `input_too_large` / 415 `unsupported_media_type` | Limit exceeded / not JSON; no write. |
| 422 `unsupported_content_schema` | Any integer-token version other than 1, including overflowing values; no write. Future negotiation cannot treat this as a successful send. |
| 429 `rate_limited` | Retry-After, same key/payload; no acceptance claimed. |
| 503 `unavailable` | Commit outcome may be unknown; retry exact key and captured content, never allocate a new key automatically. |

Raw-body error distinctions: invalid UTF-8 bytes and JSON syntax failures (including
literal unescaped control bytes) are 400 `invalid_request`; a syntactically valid
JSON string containing an unpaired surrogate escape is 400 `invalid_characters`.
An escaped NUL or other prohibited control decodes as Unicode but fails semantic
validation: 400 `invalid_content`, issue `invalid_characters`. Allowed multiline
newline/carriage return/tab follow core field rules. Enforce the raw body cap
before decoding (413 takes precedence); never repair or re-encode malformed bytes.

## 4. Snapshots, replay, concurrent edits, and retirement

The scenarios below use HD1 defaults (proposed), pending P1/P3/P4/P5 wherever
review invalidation, projection, lifecycle or new-send policy is involved. The
section 7 impact rows identify what must change for another option.

Persist immutable canonical submission version+content and a request digest with
the operation key, under an owner/environment namespace. Canonical digest input is
UTF-8 `draft-v1\n1\n` followed by HD1 canonical JSON of core-normalized Content;
SHA-256. This encoding is language-independent: recursively sort object member
names by UTF-8 byte order; emit no whitespace; decimal integers without leading
zeros; retain array order. Strings escape quote and reverse solidus as `\"` and
`\\`, use `\b`, `\t`, `\n`, `\f`, `\r` for those controls, and lowercase
`\u00xx` for remaining U+0000–001F. All other scalar values are literal UTF-8,
including `<`, `>`, `&`, U+2028 and U+2029; never HTML-escape, escape solidus, or
normalize Unicode. Optional-field normalization precedes this encoding. This
formula does not depend on Go struct order or `json.Marshal` escaping. Different
whitespace, key order, equivalent nulls and omitted empty lists produce the same digest. Strings
and array order remain significant. The local sender retains the captured content
and key until definitive resolution; a revision change during a send cannot change
the payload used for its retry. Submission does not modify the local draft.

Every proposed mutating endpoint below also requires an Idempotency-Key. Mutation
keys are scoped by owner, environment and operation kind; the request digest binds
the method, target ID and complete canonical typed body, including both expected
revision tokens. An identical retry returns the recorded acknowledgment and
current object without applying the mutation again; changed target/body under
that key is 409 idempotency_conflict. Store the operation receipt in the mutation's
transaction. A replay never resets newer edits, confirmations or terminal state.
Retirement protections below apply to these keys as well. The submission digest
formula above is specific to submission content, not a substitute for hashing
the complete body of a review/edit/approval operation.

| Scenario | Required outcome |
| --- | --- |
| First key K / content A | One transaction creates draft D, immutable snapshot S and receipt R; 201. Any failure before commit creates none. |
| Retry K / canonical A (including concurrent calls) | 200 R + current D; exactly one D/R. Never reset review, status or content. |
| Retry K / B | 409 idempotency_conflict; D unchanged, even if local revision changed. |
| Explicit "Send as new draft" with new key K2 / A or B | **HD1 default (proposed), pending P5:** 201 separate D2/R2, unreviewed. Content equality is not a dedupe rule. UI must make duplicate creation explicit; warning policy P5. |
| Local edit after send | Existing S/R/D unchanged by local editing. **HD1 default (proposed), pending P5:** explicit new send uses a new key and creates an independent draft without inherited confirmations; no upsert by local ID. Alternative: explicit replace with target/revision preconditions and review reset; never automatic sync. |
| Web edit of D followed by retry K/A | R still contains A; D returns edited content and current review. No overwrite by old snapshot. |
| Two web editors read draft rev 1/review rev 1 | First replacement wins, creates draft rev 2/review rev 2; stale writer gets 409, nothing merged/dropped. |
| Confirmation races with edit or approval | Serialize on D; compare both draft and review revisions in the same transaction. A stale confirmation cannot attach to new content. |
| Discard D then late retry K/A | Return same terminal D/R with status discarded while retained, or 410 retired summary after purge. Never INSERT another draft or re-open D. |
| Approval D then retry K/A | Return R + terminal D and approved_recipe_id; no second recipe, no new draft. |
| Discard races with send replay/approval | Same owner+draft serialization; one terminal transition wins. Losing transition gets already_terminal or revision_mismatch. No half-approved recipe. |

A body edit is PUT `/api/draft-v1/drafts/{id}`, accepting only
`{expected_revision,expected_review_revision,content,candidate}`.
**HD1 default (proposed), pending P3:** candidate is a complete manual-shaped
planner projection validated against #61 bounds, or null. A broader versioned projection is the P3 alternative.
Both expected tokens are positive JS-safe integers and are compare conditions, never desired revisions.
Replacement updates only mutable content/candidate; submission snapshot and receipt
stay untouched. Every newly committed replacement increments draft revision and
review revision by one, including identical content under a new operation key.
An identical retry of the same operation key does not increment either token.
Require explicit conflict
resolution; no last-write-wins. Stable row IDs belong to the server; a new candidate
is a proposed projection, and reserved fields cannot set authority. Details of the
projection mapping wire shape await P3; this endpoint remains blocked until settled.

**HD1 default (proposed), pending P4:** discard is terminal removal from the active
queue; archive is terminal read-only hiding of unapproved work, with no restore.
Neither deletes a local draft or approved recipe. Approved/discarded work cannot
be archived via a second terminal transition under this default. Alternative undo
window: add `discard_pending` (and optionally `archive_pending`) plus a server
`undo_until`; remove pending work from the active queue, retain content, allow an
owner-capable revision-checked restore to `draft` before expiry, increment both
revisions, and reset confirmations under the P1 default. Expiry commits the
terminal status atomically; purge cannot run before expiry. Send replays always
return the same pending/restored/terminal ID, never create work. A restore is a new
explicit mutation, never a submission replay. Section 2 status/summary shapes,
section 5 transitions and Preview-7 must adopt this alternative if P4 chooses it.
Archive availability, undo/recovery, content expiry and account deletion are P4.
Until P4 is approved, no purge job or retention promise is implementable.
Proposed POST `/api/draft-v1/drafts/{id}/discard` and, only if P4 enables it,
`/api/draft-v1/drafts/{id}/archive` accept only
`{expected_revision,expected_review_revision}`. Require the app-owner capability,
compare both tokens, and commit the terminal status plus operation receipt
atomically. Increment both revisions, freeze review, and return the terminal
envelope. A stale/different terminal transition cannot delete an approved recipe.

**Non-negotiable retry invariant:** a retired key never becomes a fresh key.
Retain an owner-scoped consumed-key tombstone (key digest, request digest, terminal
outcome, receipt/draft references) after content purge. Never cascade-delete retry
protection when removing draft content. If finite tombstones are chosen, add a
server-enforced expiring send authorization namespace: expired namespaces must
reject all submissions, including keys no longer remembered. Simply deleting old
receipts after N days is insufficient. Account deletion must revoke its send
namespace before removing receipts so an old token/key cannot recreate data. P4
must settle retention/privacy and namespace expiry before deletion ships.

## 5. Persisted draft, review progress, and approval

| Layer | States / meaning |
| --- | --- |
| Persisted hosted draft | `draft` initially. **HD1 default (proposed), pending P4:** then exactly one terminal `approved`, `discarded`, or `archived`, no restore. Undo-window alternative adds pending states and restore as described in section 4. Draft persistence is not recipe approval. |
| Review progress | `not_started`, `in_progress`, `ready`, `completed`; server-derived. `ready` requires all applicable claims and completeness gates for the configured policy, never just all visible boxes. Terminal discard/archive freezes progress without marking completed. |
| Approved recipe | `approved` / `agent`, created atomically with completed review. Only this projection may enter Recipes/planner/shopping/prep. Drafts and receipt snapshots never do. |

Persist editable canonical content, candidate projection, review progress and
immutable original separately. Candidate is not a recipe until committed by
approval. App must distinguish "Saved draft · unreviewed", "Review in progress",
and "Approved recipe · owner reviewed"; current "Draft · not saved" and "nothing
is saved" copy would be false for hosted persistence. These are test-pack label
proposals requiring Caroline's copy sign-off (P1).

The server enumerates review items; clients cannot omit claims to make a draft
ready. An item identifies `(draft_revision, JSON pointer, claim_kind,
policy_version)` and a digest of that value/basis/referenced source plus the full
revision binding. Paths identify an array entry only within that revision.
**HD1 default (proposed), pending P1/P2/P3:** proposed required candidate review
groups include recipe content/ingredients/step order,
yield→servings, all whole and phase estimates (five each), every present step
phase_basis/prep-ahead assertion, every guidance item (including `other`), and
all added projection time/storage/pantry/scaling claims. Structural warnings and
unknowns each need a policy-defined disposition, not a fictitious confirmation of
an absent value. Missing source refs cannot be treated as verified by confirmation.
Exact required/optional policy and absence acknowledgments are P1/P2/P3.

POST `/api/draft-v1/drafts/{id}/review` accepts only
`{expected_revision,expected_review_revision,confirmations:[{path,claim_kind,confirmed}]}`.
These are review requests, not caller-supplied approval facts. Require the app
owner capability. The server checks the current enumerated items and stores
`{path,claim_kind,draft_revision,policy_version,value_digest,confirmed,reviewer,
confirmed_at}` with authenticated reviewer and server timestamp. Clients cannot
supply reviewer/time/digest. Review writes increment only review.revision and derive
review.state. Confirmation is reversible while still a draft. No bulk
"confirm all" operation is implied. Unknown/stale paths, duplicates and arbitrary
IDs are rejected; all-or-nothing review write.

**HD1 default (proposed), pending P1:** any content, candidate, list order, source,
basis or mapping edit increments draft revision and invalidates **all**
confirmations (review revision also increments, state resets to not_started).
This conservative recommendation avoids stale index identities and source
dependencies. P1 may instead choose dependency-scoped invalidation with stable
claim identities and explicit rules for unchanged confirmations. Policy-version
changes also invalidate every confirmation and readiness and increment
review.revision, even if content did not change.

POST `/api/draft-v1/drafts/{id}/approve` accepts only
`{expected_revision,expected_review_revision}` and an Idempotency-Key. It must:

1. Verify app-owner capability, lock owner-scoped draft, replay any committed
   identical approval key, then compare both revision tokens and active status.
2. Require a configured Caroline-approved policy version. While absent, return
   409 policy_unresolved; fixtures being structurally full does not bypass this.
3. Revalidate canonical content, candidate, mappings, all server-enumerated claims,
   warning/unknown dispositions, sourcing rules and confirmations bound to this
   revision/policy. Reject if anything is missing/stale. No inference at this step.
4. **HD1 default (proposed), pending P3:** validate the complete projection against
   #61 manual content bounds (including all required fields and numeric bounds);
   retain canonical content/provenance and mapping separately. No requirement to coerce a draft to fit those bounds.
5. In one transaction create exactly one account-owned approved agent recipe with
   server UUID/row IDs/timestamps, immutable approved canonical content, projection,
   provenance and review audit; mark D approved, set approved_recipe_id, increment
   draft and review revisions, set review completed, persist approval receipt.
   Commit all or none. Never rely on a browser removing the draft after a second
   request succeeds. A retry returns that same recipe even after subsequent edits.

Candidate required fields under the **HD1 default (proposed), pending P3**:
nonblank ≤120 title; explicit icon; active/total times, attention, servings 1–12;
tags (may be explicitly empty);
≥1 ingredient with name/quantity disposition/unit/aisle/pantry decision; ≥1 cook
step; explicit prep lists, per-step minutes, timer and keeping/storage dispositions
where applicable. `shortName` and prep labels are optional. If P3 cannot represent
unknown keeping/time honestly within #61, approval of affected drafts stays blocked
until a versioned approved-recipe/planner model supports it. No zero/null workaround.

## 6. Extension of #61 and app implementation seams

#61 `001_recipes.sql` reserves `status=draft` and `origin=agent`, but its existing
Go API always creates/reads/updates `approved/manual`. Preserve those filters and
strict manual input schema, including rejecting reserved states and agent IDs.
No new draft fields or state transitions are added to `/api/recipes` by this design.
Do not store local Content in `recipes.content` and let the existing manual decoder
or planner guess its shape. Under the **HD1 default (proposed), pending P3**,
current `RecipeContent` is the projection type only; a broader P3 model would require a versioned adapter.

A future reviewed migration can add separate owner-keyed `draft_submissions`
(immutable content/version/digest), `hosted_drafts` (editable content/candidate and
revision/status), `draft_reviews` (policy/progress/confirmations),
`draft_submission_receipts`/consumed keys (survive content retirement), and
`draft_mutation_receipts` (edit/review/discard/archive retries) plus
`draft_approval_receipts`/approved canonical metadata. Use owner-composite foreign
keys and unique constraints for one receipt per owner/key and one recipe per draft.
Approval may reuse `recipes` reserved approved/agent envelope with its complete
projection, plus versioned canonical metadata linked by owner+recipe. A dedicated
approved-agent read adapter is required before planning from it; the manual
repository remains approved/manual only. Exact SQL/grants/migration is later work.

Reuse #61 verified owner extraction, bounded transactions, owner-scoped queries,
operation serialization, optimistic revisions, digest/receipt pattern and response
bounds. Do not reuse its manual-create receipt lifetime mechanically: drafts can
be discarded/purged, and retry protection must outlive their content. Server roles
remain narrow; no browser/local DB credentials or schema-owner privileges.

Reuse `web/src/pages/DraftsPage.tsx` review layout, discard dialog, navigation and
planner recipe seam, with a new lossless hosted-draft model and owner-scoped server
state. The five `DRAFT_GUESS_KEYS` and current `canApproveDraft`/`draftToRecipe`
are inadequate for arbitrary claims/provenance/unknowns and must not authorize
hosted approval. `recipeForm.ts` trims text and fills blank minutes with zero;
it remains for manual forms, not lossless draft serialization. App reload must
fetch authoritative draft/review/recipe state, clear previous account caches on
account change, and keep drafts out of `assignRecipe`/planner until approved.

No field, URL, auth capability or lifecycle rule depends on Framebriar. MCP, CLI
or another host can later call the same reusable send core. The remediation branch
`origin/codex/23-mcp-remediation` tools are local-only create/get/list/update/validate
and status, with non-idempotent local create. This contract adds no hosted tool
and does not mislabel that existing local save as a hosted send.

## 7. Open product decisions

**Owner: Caroline. All entries are unresolved.** Recommendations are design input,
not authorization or silently selected product behavior. Record the chosen options
and policy version before dependent implementation; engineering invariants above
(lossless content, owner isolation, atomicity, no invented defaults, no resurrecting
retired keys) are required regardless of choice.

| ID / decision | Options | Recommendation | Blocks / change impact if another option is chosen |
| --- | --- | --- | --- |
| P1 Required review, copy and list order | Confirm every present claim vs five groups; unknown acknowledgment vs completion; invalidate all confirmations on edit vs dependency-scoped invalidation; exact labels; ascending UUID vs newest-created vs recently-edited list order. | HD1 default (proposed), pending P1: every claim separately; invalidate all on any edit; section 5 labels and send-success label "Saved to Preview as an unreviewed draft"; ascending UUID keyset as a simple deterministic first slice. Owner review never implies safety verification. | Sections 2, 3.1, 4–5 and 8; review `not_started/in_progress/ready/completed`, revision rebinding and cursor keys. Alternatives change Preview-1/2/4/6 and list-order acceptance; `preview-context.json`, `full.hosted.json` and `incomplete.hosted.json` must track chosen copy/state fields; app fixtures change if edit-state review binding changes. Invalidation changes require stable identities/dependency fixtures; list-order choice requires ordered pagination fixtures before implementation. |
| P2 Sourcing/safety policy | Require owner-checked qualified sources for prep-ahead/storage/safety; allow labeled unverified guidance after acknowledgment; exclude such guidance from approved use. Decide acceptable sources, evidence and reviewer meaning. | Keep all guidance unverified until owner checks an acceptable reference; exclude unresolved guidance from planner/prep use with explicit visible disposition. | Approval of safety/storage/prep-ahead claims, evidence UI and any reviewed/safe wording. Structural validation cannot settle this. |
| P3 Planner projection and unknowns | Owner completes #61 fields explicitly; expand approved recipe/planner schema for unknowns/text quantities; limit first slice to representable recipes. Decide time/keeping/quantity/phase/classification/cosmetic defaults. | HD1 default (proposed), pending P3: PUT accepts a complete manual-shaped projection or null; PUT and approve apply #61 bounds; retain full canonical content. Broaden the schema before planning with honest unknowns. | Sections 3 mapping/bounds, 4 PUT, 5 approval/candidate, 6 adapters and 8 Preview-4; candidate wire format, `ready` and approved recipe shape. Broader schema changes require versioned projection/approval fixtures and mapping tests; full/incomplete accepted content stays lossless but planner-readiness expectations and Preview-2/4 change. No approved candidate fixture currently selects a policy. |
| P4 Discard/archive/privacy/retention | Permanent discard vs undo window; archive vs omit; indefinite vs finite content/receipt retention; lifetime tombstones vs expiring namespaces; account deletion. | HD1 default (proposed), pending P4: terminal discard/archive, no restore, finite content retention chosen by Caroline and consumed-key/namespace protection. | Sections 2 envelope/GET, 3.1 active filter, 4 transitions/replay/purge, 5 states, 6 storage and 8 Preview-7; undo adds `discard_pending`/optional `archive_pending`, `undo_until` and revision-checked restore. Lifecycle/replay/retirement acceptance fixtures must change for undo/expiry or omitted archive; `full.hosted.json`/`incomplete.hosted.json` initial `draft` fixtures remain unchanged unless pending/deadline envelope fields become required. Retention duration and purge/account-deletion work remain blocked. |
| P5 Duplicate/new revision UX | Explicit independent new send; explicit replace/review-reset workflow; hash warning before new send. | HD1 default (proposed), pending P5: independent new draft, no upsert; duplicate warning and new receipt; no implicit local-ID dedupe. | Sections 2 submission/receipt, 3 local-ID mapping, 4 scenario/key/PUT contract, 5 review reset and 8 Preview-5; replacement changes target/revision preconditions, new-vs-existing ID/state and immutable snapshot lineage. Duplicate/revised-send acceptance fixtures and Preview-5 expectations must change; `full.submit.json`/`incomplete.submit.json` and their hosted pairs remain initial sends unless replacement becomes part of the submission shape. Replacement needs an explicit reviewed concurrency/receipt contract. |
| P6 Hosted quotas/abuse limits | Active draft cap only; total retained snapshots/bytes cap; rate limits and retention-based quota. | Bounded bytes + active/retained counts + owner rate limits; tell the user the limit, never silently delete work. | Hosted capacity, 429 policy, quota errors and operational provisioning. The local 1000-file cap is not a product choice here. |

## 8. Shared fixtures and executable owner Preview test pack (#25)

`contracts/testdata/fixtures/hosted-drafts-v1/` is all synthetic. `incomplete` and
`full` each contain a local saved envelope, submission body, hosted acceptance
response and app edit-state content. Full means all canonical field families are
exercised, **not approved or safe or planner-ready**. Both remain unreviewed.
Rejection files exercise owner/approval/authoritative ID/time/revision injection
at root and content, unsupported version, duplicates and structural errors.
`contracts/hosted_draft_fixtures_test.go` tests canonical core validity, semantic
content equality through typed encode/decode at every hop, snapshot isolation
through the core deep clone used to model app editing, missing-vs-zero and
per-claim provenance, and strict request rejection.
Its wrapper validator exists only in `_test.go`; it is not an endpoint or production
bridge. It does not execute auth, concurrency, policy, database or browser behavior.
The scenarios in sections 4–5 and the pack below are later integration acceptance,
not claims that synthetic Go checks prove a hosted system works.

### 8.1 Run record and prerequisites

The shared dry-run context in `preview-context.json` supplies literal synthetic
labels `Preview · https://preview.example.invalid` and
`Account · synthetic-owner-a`, exact synthetic absolute receipt/draft links,
Preview API origin and separate synthetic owner B. They are test data only, not
a deployment or an actual owner account. The fixture check resolves all returned
relative links against that exact web origin.

#25 obtains a reviewed contract, P1–P6 decisions for the exercised behaviors, #73
identity/environment readiness, explicit later bridge implementation authorization,
and an isolated Preview build. This issue performs none of that setup. Before an
owner run, record exact resolved values in a private execution report:

- Preview web origin, Preview API origin and build commit; local tool/core version,
  hosted API `draft-v1` and content schema 1; no Production destination.
- Exact visible destination label `Preview · {web origin}` and account label
  `Account · {authenticated display label}` in both send confirmation and app.
  Record the verified issuer and subject privately to disambiguate same-name
  accounts; use synthetic `owner-a`/`owner-b` in shared evidence. P1 signs off the
  literal labels. Do not invent a live URL/account for this design pack.
- Exact server-returned absolute links resolved from `receipt.links.receipt` and
  `.draft`, receipt ID, draft ID and revisions. Never construct a Production link
  from an ID or substitute a local storage link. Example fixture paths are
  `/draft-receipts/22222222-2222-4222-8222-222222222222` and
  `/drafts/11111111-1111-4111-8111-111111111111` for incomplete;
  full uses receipt `44444444-4444-4444-8444-444444444444` and draft
  `33333333-3333-4333-8333-333333333333` (not live URLs).

### 8.2 Owner-operated acceptance sequence

| Test | Owner action / exact expected evidence |
| --- | --- |
| Preview-1 Local → explicit send | Caroline creates incomplete fixture through the local MCP draft tool. Local-only save shows no hosted receipt. She sees exact Preview origin/account label and chooses the explicit send action. Capture one 201 receipt and working owner-authenticated draft/receipt links. No background send on create/update/validate. |
| Preview-2 Honest incomplete display | Open receipt link in Preview app. Show "Saved draft · unreviewed" (P1 copy), original title/notes, unknown times/servings as Unknown, no numeric placeholders serialized, missing-content warnings and structural-only notice. It cannot enter Recipes/planner or approve while required values/policy are missing. Reload: same ID/content/review state. |
| Preview-3 Full provenance and zero | Send full fixture. Display every ingredient raw/structured component, ordered/unclassified steps, source kind/label/locator/link, basis note and source_ref next to every whole/phase estimate and guidance. Explicit zero phase active time shows 0 min; incomplete unknown shows Unknown. `sourced` remains unverified; no URLs are fetched by ingestion. |
| Preview-4 Review/edit/approval/reload | Caroline edits full draft into an explicitly representable candidate under approved P1–P3, confirms each required claim across all review pages/batches (include a maximum-content case exceeding 512 items), then edits a source/step/order. Under the HD1 default (proposed), pending P1, all confirmations become stale; adapt to the chosen dependency policy. Stale approve fails; she re-reviews and approves. Capture one recipe ID, completed audit and receipt→recipe link; reload app and receipt, confirm approved recipe remains and draft is out of review queue. Planner sees only the approved projection, with canonical provenance available. |
| Preview-5 Duplicate send and revised snapshot | Retry exact captured key/body before and after web edit and after approval: 200 same receipt/current work, no reset, no extra draft/recipe. Same key with changed content: 409. HD1 default (proposed), pending P5: local edit + explicit new send with new key creates a distinct unreviewed draft/receipt, original snapshot unchanged; exercise explicit replacement instead if P5 chooses it. Double-click/concurrent identical sends create exactly one. |
| Preview-6 Concurrent review | Two app tabs read same revisions. One edits, the other confirms/edits/approves: 409, preserve unsaved input; reload shows first writer and invalidated confirmations. Approve twice/race discard: one terminal winner and no orphan or duplicate recipe. |
| Preview-7 Discard and late retry | Discard an unapproved draft, reload and follow receipt. HD1 default (proposed), pending P4: active queue excludes it, terminal status remains, no restore. If P4 selects undo, observe pending status/deadline, restore before expiry and test expiry/restore race; no purge during the undo window. Retry old captured send: same discarded receipt or 410 retired summary, never recreated. Repeat after any P4 retention purge/expiry test clock; archive if enabled. Explicit new key creates new work only after a new send decision. |
| Preview-8 Cross-user and environment isolation | Sign out, links/API yield 401. Sign in as independently provisioned owner B with app-owner capability; A's draft/receipt/recipe IDs yield 404 across get/review/edit/discard/approve. A send-only credential gets 403 on review/approve for own, foreign and absent IDs, before lookup; B's list is empty of A. Same operation key under B creates only B-owned work, with no A content. Switch back A: A data unchanged, no B cache bleed. Test equal subject/different trusted issuer in isolated auth tests; Preview token/receipt is unusable in Production. |
| Preview-9 Rejected inputs and failure | Run each rejected fixture through later submit boundary; match section 3 errors, no write/receipt. Simulate timeout/503 after possible commit; UI says outcome unknown, retries same captured key/body and converges on one receipt. Unsupported version cannot say saved. |

#25 records expected/actual, private exact labels/links, owner-operated steps,
reload evidence and any failures; shared issue evidence remains synthetic/redacted.
Caroline explicitly confirms the result herself. A developer dry run, CI pass,
design approval or #25 coordination does not satisfy that owner requirement.
#55's Production gate stays closed until that confirmation and its other release
gates; this document alone only enables reviewed implementation planning.

## 9. Downstream work assignment

| Owner slice (later, not authorized here) | Contract / fixtures to consume | Exit evidence |
| --- | --- | --- |
| Backend #55 persistence/API | Sections 1–5; all submission/rejection fixtures | Strict decode, owner/capability isolation, atomic/retry/retirement integration tests; reviewed migration only under separate authorization. |
| Local send core + MCP/CLI adapters | Sections 1–4 and 8; local/submission pairs | Explicit account/environment confirmation, captured snapshot+key ledger, receipt links, no automatic sync. Coordinate #23/#73. |
| Web review/projection/planner | Sections 3, 5–7; hosted/app pairs | Lossless editor, per-claim review, honest unknowns, stale-write handling, server approval, approved-only planning. |
| #25 owner verification | Section 8 + section 4 scenario matrix | Caroline's personally executed Preview run and explicit confirmation. |
| PM/independent review | P1–P6, canonical mapping, fixture limitations | Record decisions, independent contract review, coordinate #73; only then assign #55 slices 2–5 planning. |
