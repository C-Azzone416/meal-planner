# Preview migration-readiness inspection contract

Supported PostgreSQL majors: **17 and 18 only**. SQL fails closed outside this
range before inspection, and the major version is included in the exact policy
snapshot. PostgreSQL 18 was tested locally; the actual Neon major must be verified
and matching reviewed baseline generated before activation. MAINTAIN privileges
(introduced in PostgreSQL 17), their grant options and PUBLIC/unrelated-table
exposure are included. Other versions require explicit implementation/review.

This library and read-only SQL are **not an activated deployment gate**. The
existing unconditional guard stays unchanged until a reviewed trusted-runner
adapter can establish the facts below. They do not apply migrations, configure
roles, connect to a live database, or authorize a deployment.

## Trust and exact data flow

1. A trusted workflow revision, not target code, inventories all three known
   migration files (recipe 001, feedback 002, draft-ingest 004). Unknown/missing
   files, links, and nonregular files fail. All checksums bind even disabled
   features. `inventory(root)` computes the digest from exact source bytes;
   the caller must bind the root to the authenticated reviewed Git commit.
2. The independently reviewed plan binds exact source SHA, Vercel project ID,
   Neon branch ID, inventory hash, schema-policy hash, Preview target, and actual
   deployed feature flags. Recipes are always required. Disabled feedback or
   draft-ingest can omit their DB observations only when the clean deployment
   adapter proves the corresponding runtime/build configuration is disabled.
   A target-provided flag is not proof. Existing Go configuration returns before
   constructing those stores when disabled; provider runtime settings must also
   match the plan. No production mode exists in this contract.
3. Establish expected schema/grants snapshots in a disposable database by
   applying the exact reviewed migrations and separately reviewed runtime grants.
   Review those grants; **do not bless an arbitrary live snapshot as policy**.
   Snapshot hashes are part of the independently reviewed schema policy, never
   derived from the target's proposed evidence. The policy digest binds the full
   plan/document that specifies these expectations, not merely a label.
4. A separate clean inspector authenticates provider project/branch identity and
   obtains the connection for that branch through its authorized secret mechanism.
   It runs the fixed `inspect-migration-readiness.sql` with `psql -X -qAt
   -v ON_ERROR_STOP=1 -v component=recipes -v runtime_role=NAME -f ...`.
   Connection credentials belong in its private environment, never command-line
   DSNs or logs. Use verified TLS. Do not run target SQL/scripts, migrations, or
   grant statements. SQL is a timeout-bounded READ ONLY transaction, rolled back.
   The SQL database name is not a Neon branch identity and cannot replace step 4.
   A dedicated catalog-only LOGIN + CONNECT role is sufficient for this fixed
   inspection on the tested PostgreSQL 18 fixture: no schema USAGE or table SELECT
   was granted. The test compares its observation with the migrator observation
   and confirms it cannot SELECT recipes. Real provider defaults must still be
   inspected; creating this role/credential requires separate security approval.
5. Parse the one observation directly from that trusted psql process. The inspector
   records columns/defaults, constraints, indexes, RLS flags/policies, triggers and rewrite rules,
   role flags/memberships/ADMIN OPTION, effective table/column/schema grants and
   grant options, database privileges/grant options, non-system schema CREATE,
   PUBLIC grants, unrelated
   non-system relation access, and callable non-system security-definer functions.
   `verifyObservation` requires exact reviewed snapshot equality, rejects runtime
   ownership/elevated flags/memberships/ADMIN OPTION, all selected grant options,
   database CREATE, non-system schema CREATE, forbidden table/schema rights, PUBLIC
   table access, unrelated relation access, and security-definer access. It is
   deliberately conservative. Missing role or table fails, not an empty success.
   Database TEMPORARY is recorded and compared to the reviewed snapshot rather
   than denied automatically: PostgreSQL normally grants it through PUBLIC.
   This is not a comprehensive database security audit: arbitrary system/extension
   behavior needs separate policy assessment before activation. No claim of general
   privilege proof.
6. `createReadiness(plan, observations, authenticatedProviderIdentity, nowMs)`
   creates a canonical metadata record after those comparisons. Its 15-minute
   expiry is a maximum window, not a guarantee against changes after inspection.
   Transport through an authenticated clean reporter. Pin its digest independently
   in that trusted reporter's output, not beside attacker-selected bytes. A hash
   alone authenticates nothing. No private credentials or raw catalog dump needs
   to reach the build job/public artifact.
7. The clean deploy consumer uses `parseCanonicalRecord` on the bounded canonical
   record, then `verifyReadiness(record, approvedPlan, authenticatedRecordDigest,
   freshlyObservedSourceInventory, trustedNowMs)`. Revalidate runtime settings,
   provider identity and source immediately before deployment; retry inspection
   after configuration/role/schema changes. The return value is bounded identity
   metadata, not standalone authorization to deploy. Existing release approvals,
   CI evidence, output verification and alias checks remain required.

## What remains before wiring

- An independently reviewed expected snapshot policy generated from the exact
  migrations and intended least-privilege grants, including effective column
  privileges. The library enforces exact policy snapshots; it does not choose grants.
- Authenticated provider/secret adapter and clean inspector job; no new DB access
  or GitHub/Vercel security settings are created here.
- Authenticated receipt attribution to clean inspector, exact source/config
  verification and protected plan input. Never read record hashes from target code.
- Specific owner authorization and verification for actual feedback schema/role/
  key setup. Recipe runtime credentials must not be reused.
- Fresh independent QA then Judge before integration. No live inspection/deploy
  was performed by these tests.

## Verification

`node --test tools/deploy/migration-readiness.test.mjs`

`READINESS_PG_BIN=/absolute/postgres/bin node --test tools/deploy/migration-readiness-postgres.test.mjs`

The second command creates and destroys only its own temporary Unix-socket-only
PostgreSQL cluster, ignores ambient connection variables, applies synthetic local
migrations, and tests schema drift, extra rights, table/column/schema/database grant options,
role ADMIN OPTION, write-suppressing rewrite rules, and catalog-only inspection. It never uses a provided external DSN.
