# Hosted manual recipes

## What this slice does

Signed-in users can create, read and edit private manual recipes through `/api/recipes`. The Go API verifies the existing environment-specific Neon Auth policy before touching the database. Ownership is the pair `(verified JWT issuer, verified JWT subject)`, never email or request data. Recreated accounts do not inherit an old subject's recipes. Preview and Production use separate database branches and auth issuers.

Manual recipes are saved with `status=approved`, `origin=manual`. This records that a person deliberately saved their recipe, not a food-safety certification. The database reserves `draft/approved` and `manual/agent` states for future work. This endpoint does not accept those fields and never changes draft state. MCP #55 remains paused; local MCP files and tools are unchanged.

Recipe persistence is separate from browser-local week planning, draft previews, checklist progress and house rules. Existing browser data is preserved and never automatically uploaded. Unowned legacy data is not assigned to an account. Hosted recipes load from the server after a fresh sign-in/reload, even in a different browser; local planning does not sync. Do not call the whole app synced or backed up by this slice.

## Deployment state and explicit gates

Code, migration files and passing local/CI tests do **not** mean the live Neon database is ready. Application startup never runs migrations. A missing runtime URL, missing schema, invalid TLS configuration or database outage fails closed with a generic 503; no browser-only fallback can claim a hosted save.

Roll out to protected Preview first, after UI/auth integration and review. Production setup is separate and must not copy Preview auth identities or credentials. Provider setup, creating credentials/persistent access and security settings require the applicable action-time approvals. This runbook is not that approval. Do not provision accounts, enable public signup, weaken authentication or expose preview deployments as a workaround.

## Migration review and application

1. Privately verify the selected branch/database and inventory existing tables and migration history. Keep provider URLs, account identifiers and connection strings outside this public repo. Reconcile any schema drift; do not run a migration against an assumed empty database.
2. Review `hosted/migrations/001_recipes.sql` and its final checksum/commit. It creates only the dedicated `meal_planner` schema, product tables, receipt tables and migration record. It does not touch `public`, `neon_auth`, existing recipes or unrelated enrollment tables. Its version is scoped to this schema, not the separate Framebriar migration directory.
3. Use a migration owner separate from the runtime login. Apply the reviewed SQL with `psql -X -v ON_ERROR_STOP=1 -f hosted/migrations/001_recipes.sql` using a privately supplied migration connection. Never print the connection string or put it in a command argument/log. The transaction rolls back on failure; a repeated application deliberately fails instead of masking drift.
4. Privately verify the migration record, table constraints and privileges. Runtime must not own the schema/tables, create/drop objects, delete records, mutate auth tables or manage roles. Do not apply a destructive down migration. If rollout fails, stop application writes and retain the additive schema/data for diagnosis.

## Narrow runtime access

After explicit approval, provision a dedicated login in the intended Preview branch. Only grant:

- CONNECT to the intended database and USAGE on `meal_planner`.
- SELECT, INSERT, UPDATE on `meal_planner.recipes`.
- SELECT, INSERT on `meal_planner.recipe_creates` and `meal_planner.recipe_updates`.

The role must not inherit broad roles, own objects, have `BYPASSRLS`, create databases/roles or have superuser privileges. Default/public grants must not expand this scope. The application uses account-scoped parameterized queries for every operation. RLS is not implemented in this slice; the server is the trusted ownership boundary. Runtime connection leakage would grant access to all recipe rows, so keep the credential server-only. A future RLS change needs its own reviewed pooled-session identity design/tests.

Store the pooled Neon connection URL as **Secret**, named exactly `MEAL_RECIPES_DATABASE_URL`, in Vercel **Preview** only. Never use a `VITE_` variable or expose it to a client. Require `sslmode=verify-full`; use the provider-supported pooled hostname. The pinned lib/pq driver does not support `channel_binding`; omit that optional provider URL parameter rather than claiming channel-binding enforcement. The only additional allowed URL option is `sslrootcert`. The runtime validates its URL, limits each warm function to four open/two idle connections, uses bounded request/connection/statement/lock timeouts and reuses the pool. Aggregate serverless concurrency still needs provider capacity monitoring. Server statement/lock limits are set with `SET LOCAL` inside each explicit transaction, never in the connection startup packet or session-wide `SET`. [Neon documents](https://neon.com/docs/connect/connection-errors#unsupported-startup-parameter) that its PgBouncer pool rejects untracked startup parameters. Transaction-local settings disappear when the transaction ends; tests verify this reset on a reused connection.

`vercel pull` must return only the Secret placeholder `[SENSITIVE]` for this variable. The deployment guard rejects literal URLs, lookalike names and frontend-prefixed names. Existing isolated pull/build/deploy jobs delete pulled env files; no credentials go to public artifacts or the env-free build. Runtime secret injection still needs end-to-end Preview verification.

## API and retry contract

All requests carry a short-lived `Authorization: Bearer ...` token for the intended deployment. Responses use `Cache-Control: no-store`; credential or recipe content is never logged by these handlers. JSON bodies are at most 256 KiB. Unknown properties, case-variant property names, duplicate keys at every object level, missing required properties and invalid quantities/times are rejected before storage. Property names are matched exactly after JSON escape decoding. Malformed query encoding, unknown or repeated query keys and empty query values are rejected; `id=` cannot turn a single-recipe read into a list. The byte limit includes leading/trailing whitespace and any extra document; oversized bodies return 413 even with an unknown Content-Length. IDs are server-assigned UUIDs. Owner/state/origin/IDs/timestamps are not accepted in content.

- `GET /api/recipes?limit=100&after=<uuid>` lists at most 100 account-owned manual approved recipes, ordered by UUID. It returns `{recipes, nextCursor}`. The cursor is nullable. Pages are stable across edits but not a snapshot of concurrent inserts; refresh to see recipes added during pagination.
- `GET /api/recipes?id=<uuid>` returns one envelope. A foreign or nonexistent ID returns the same 404.
- `POST /api/recipes` accepts only `{content}` (no `expectedRevision`, including null) and requires a UUID `Idempotency-Key`. First commit returns 201; an identical retry returns 200 with the original committed receipt. Reusing the key for changed content returns 409. Recipe and receipt commit atomically. Keep the same key and payload after an uncertain outcome.
- `PUT /api/recipes?id=<uuid>` accepts `{content, expectedRevision}`. The whole content object replaces the current content only at that revision. Successful update increments revision atomically. A stale conflicting edit returns 409. An identical retry can reconcile the immediately committed revision; if later edits advanced it, reload rather than returning stale content.

An envelope contains `id`, `revision`, `status`, `origin`, `createdAt`, `updatedAt` and `content`. Content mirrors the manual app recipe fields, including ingredients and prep/cook steps; nullable quantity means unspecified, not zero. Explicit empty arrays are required for optional step sections and tags. This manual contract does not losslessly represent all incomplete/provenance-bearing MCP drafts and must not be reused for #55 without a separate contract.

Errors use `{error:{code,message}}`: 400 invalid request, 401 unauthorized, 404 not found, 409 conflict/idempotency conflict, 413 request too large, 415 unsupported media type, 503 unavailable. The shared auth wrapper may return plain-text 401/503; clients must not depend on a JSON body for authentication failure. A 503/connection loss is not proof that a transaction did not commit. Retry the same operation identity. Receipts are retained for the recipe lifetime; no deletion or retention cleanup is implemented.

## Required Preview acceptance

Use synthetic recipes only. Record evidence privately against the exact commit, migration and deployed target:

- Missing/expired/wrong-environment tokens cannot list, read or write.
- Save a manual recipe, reload, sign out/in and reopen in another browser. Edit and reload again.
- Repeat a create after a lost response and verify one database row. Replay an update after a lost response; stale different edits conflict without overwriting either person's form.
- Cross-account and cross-issuer IDs never leak content or revisions. Verify actual auth issuer and subject privately; do not log tokens.
- Cancel, repeated Save, interrupted navigation, load failures, offline/outage and session expiry show honest states and preserve unsaved fields where possible. No success navigation before confirmed save.
- Samples/legacy records are not inserted automatically; explicit copy-to-account retains the local source.
- Confirm the least-privilege login can perform required operations and cannot create/drop/delete or access auth data. Confirm the browser bundle and public build artifacts have no database credentials.
- Existing local planner/MCP behavior, auth gates and deployment protection remain intact.

Only after these checks and the separate release approval should Production setup be considered. PostgreSQL integration tests run against a disposable local/CI database, not Neon; they do not count as live-provider acceptance.
