# Preview readiness adapter: activation checklist

This candidate implements a clean postbuild runtime/catalog gate. It does not
create database roles, apply migrations, set credentials or enable a live app.
Those steps require owner confirmation. Production is unsupported.

## Protected configuration

In the existing main-only, owner-approved `framebriar-ci` environment:

- Secret `FRAMEBRIAR_PREVIEW_CATALOG_DATABASE_URL`: a dedicated catalog inspector's
  PostgreSQL URI, exact verified endpoint/database/role, port 5432 and only
  `sslmode=verify-full`. Never use the migrator or recipe runtime credential.
- Variable `FRAMEBRIAR_PREVIEW_PROVIDER_MAP`: JSON with exactly `version: 1`,
  `target: "preview"`, `vercelProjectId`, `neonProjectId`, `neonBranchId`,
  `endpointHost`, `databaseName`, `inspectorRole`, `recipeRuntimeRole`,
  `feedbackRuntimeRole`, `postgresMajor: 18`, `feedback: true`, `draft: false`.
- `VERCEL_PROJECT_ID` must resolve to the same project as the Preview environment.

An owner must verify the full endpoint-to-project/branch mapping using Neon,
not infer it from SQL database/role names. TLS verifies that exact endpoint;
this mapping is the separately approved provider identity. Unknown/missing
configuration fails closed. Hostname, roles and database are metadata, not
substitutes for authentication. No broad provider-admin API token is required.

The proposed inspector gets explicit LOGIN and CONNECT only. Verify its effective
PUBLIC and inherited privileges too. The adapter compares its own effective
privileges against the disposable-database baseline, including absence of table
access and role memberships. PostgreSQL's default PUBLIC TEMPORARY and public
schema USAGE are recorded in that baseline, not misrepresented as absent.

## Runtime setup, separately approved

Apply the exact reviewed feedback migration with a migration owner. Provision a
separate feedback runtime role with USAGE on public and SELECT/INSERT/UPDATE on
`public.framebriar_enrollments` only, no ownership or grant options. Do not reuse
`meal_recipes_runtime`. Vercel Preview receives `FRAMEBRIAR_DATABASE_URL` and an
independent 32-byte encryption key ring in `FRAMEBRIAR_CREDENTIAL_KEYS` as Secret
type. These never enter the build or inspection artifacts.

The enabled Preview configuration must match the fixed exact app/origin/capture
settings. Secret pulls must contain only Vercel's `[SENSITIVE]` placeholders.
Legacy runtime `FRAMEBRIAR_BUILD_SHA` is rejected; provenance belongs to the exact
build. Draft ingestion remains disabled and its runtime settings absent.
Invitations and synthetic live receipt checks remain separate steps.

Placeholder checks confirm configured secret names/type only; they do not prove
the hidden connection uses the inspected branch/role or that the key is valid.
Before operational acceptance, observe the authenticated app’s actual connection
metadata and a complete receipt.

## Implemented boundaries

1. Full backend/frontend checks and compilation run without runtime credentials.
2. A fresh postbuild `runtime-check` job in the Preview environment pulls provider
   configuration with the existing Vercel token, checks fixed public settings and
   secret placeholders, binds the actual project ID, emits only a digest/time/ID,
   and deletes pulled values. No target code runs there.
3. A separate protected `readiness` job checks exact source migration bytes without
   executing them. It runs only the fixed trusted read-only SQL through psql with
   a minimal environment, verified TLS, no inherited PGOPTIONS/.pgpass, bounded
   stdout/timeouts, and generic errors. It checks PostgreSQL major, its own
   effective privileges, and both runtime roles' reviewed schema/grants.
4. Record and digest come directly from that clean job's outputs. The clean
   deployment consumer reconstructs the approved plan from trusted policy and
   those protected map outputs, rechecks exact migration bytes and project,
   requires the record and runtime observation to be younger than 15 minutes,
   and only then allows the prebuilt Preview deployment.

Schema/configuration can change after observation; these are fresh bounded checks,
not a transactional lock spanning deployment. Reinspect after any role, schema,
provider mapping or runtime configuration change. If owner approval or runner
queuing exceeds the 15-minute window, rerun the protected workflow with fresh
observations rather than extending or bypassing the expiry.

## Baseline provenance and limits

`readiness-baseline-pg18.json` was generated from the exact reviewed three
migration files in `readiness-migration-hashes.json`, using a disposable
Unix-socket-only PostgreSQL 18 cluster and explicit intended least-privilege grants.
Only `databaseName` and `runtimeRole` are replaced by reviewed map values before
hashing. All remaining schema/effective-grant details must match exactly. It is
not a snapshot copied from a live database and cannot approve unknown migration
bytes. Baseline regeneration is an offline development tool, never run beside a
real credential. See `generate-readiness-baseline.mjs` for reproducibility.

A provider-specific extension/default difference fails closed and requires review;
it must not be fixed by accepting whatever live snapshot appears. The contract is
not a comprehensive proof of every PostgreSQL/extension behavior. Only PG18 is
currently exercised end-to-end by this adapter. The underlying SQL's wider
version parsing does not expand the adapter's approved baseline.

## Verification still required

Independent QA then Judge for this exact integration head; normal green CI;
explicit trusted workflow promotion/pin; confirmed endpoint mapping and least-
privilege role setup; protected clean install/build; actual provider runtime and
catalog inspection; live Preview sign-in and complete feedback/image receipt.
No local fixture result is a claim that those live steps already passed.

The real PostgreSQL 18 fixture passed locally. It is not exercised by ordinary
CI because `READINESS_PG_BIN` is opt-in; synthetic contract/adapter tests do run
in ordinary CI. Live Neon inspection and authenticated app verification remain
activation gates.
