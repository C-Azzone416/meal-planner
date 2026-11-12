# Preview recipe persistence: live-readiness packet (#61)

Status: **preparation only.** Refs #61. Nothing in this document has been run. The author had no database, Neon, Vercel or credential access and handled no secret. Every live step below is for the owner (Caroline) or someone she explicitly approves. This packet is not an approval.

Related: [recipes.md](recipes.md) (contract and gates), [hosted-deploy.md](hosted-deploy.md) (deploy and env guard), [authentication.md](authentication.md).

Ground rules for every step below:

- Preview only. No Production schema, role, Secret, auth or deployment.
- Never put a DSN, password, token, project/branch/endpoint ID or role name in a command argument, ticket, PR, chat or log. Use placeholders; supply real values through the shell environment or a prompt.
- Do **not** reapply `hosted/migrations/001_recipes.sql` (it deliberately fails on a second run) and do **not** create a second runtime role. The issue records that both already exist on Preview.
- Read-only means `SELECT` on catalogs only. Run the section 2 queries as a privileged *owner/admin* login, never as the runtime login (the runtime role cannot see everything and must not be given more to make the checks pass).

## 1. Repo-side readiness (verified from code at develop `ef7e561`)

### 1.1 Environment variable

| Item | Value | Source |
|---|---|---|
| Name read by the store | `MEAL_RECIPES_DATABASE_URL` (the only one) | `api/recipes/index.go:20`, `os.Getenv(...)` passed to `hosted.OpenRecipeStore` |
| Vercel type / scope | Secret, **Preview only**, server-only, never `VITE_` | `recipes.md`; `tools/deploy/check-pulled-env.sh:41,116` accepts only the literal `[SENSITIVE]` for this name and rejects lookalike or frontend-prefixed names |
| Missing or invalid | fails closed, generic 503; no browser-only fallback | `recipes.md` |

The migration is never run by the application (`hosted/recipe_store.go` header comment, `001_recipes.sql` header).

### 1.2 DSN shape (no real values)

```
postgres://<runtime_role>:<password>@<endpoint>-pooler.<region>.aws.neon.tech/<database>?sslmode=verify-full
```

Enforced by `recipeDatabaseURL` in `hosted/recipe_store.go`:

- Scheme `postgres` or `postgresql`; non-empty host, username, and database path.
- `sslmode=verify-full` is **required**. Anything else is rejected.
- The only other query option allowed is `sslrootcert`; each key must appear once. Everything else (including `channel_binding`, `options`, `host`) is rejected. Omit `channel_binding`: the pinned lib/pq does not support it.
- The code appends `connect_timeout=5` and `application_name=meal-planner-recipes` itself. Do not add them.
- Pool: 4 open / 2 idle connections per warm function, 5 min max lifetime, 1 min idle. `statement_timeout=10s` and `lock_timeout=5s` are sent as `SET LOCAL` inside each transaction, not as startup parameters.
- Host must be the **pooled** hostname (the endpoint ID plus `-pooler`). The Neon pooler is PgBouncer in transaction mode and rejects startup options outside `client_encoding`, `datestyle`, `timezone`, `standard_conforming_strings`, `application_name`; session-level `SET` is unsupported. This is why the code uses `SET LOCAL`.
- Trust roots: `sslrootcert` is optional. Neon's certificates chain to ISRG Root X1, normally in the OS store. Whether the Vercel Go runtime image trusts it without `sslrootcert`, and how the pinned lib/pq locates system roots in that image: **UNVERIFIED** (confirmed only by the Preview acceptance in section 5). If needed, the fix is to bundle a root file and set `sslrootcert` to its in-function path, which is a code change and out of scope here.

### 1.3 Exact privileges the runtime role needs

Derived from `001_recipes.sql` and every SQL string in `hosted/recipe_store.go`:

| Object | Privilege | Used by |
|---|---|---|
| database | `CONNECT` | any connection |
| schema `meal_planner` | `USAGE` | all statements |
| `meal_planner.recipes` | `SELECT`, `INSERT`, `UPDATE` | create (INSERT), get/list (SELECT), update (`SELECT ... FOR UPDATE` + UPDATE; `FOR UPDATE` needs UPDATE) |
| `meal_planner.recipe_creates` | `SELECT`, `INSERT` | idempotent-create receipt lookup and write |
| `meal_planner.recipe_updates` | `SELECT`, `INSERT` | update receipt lookup and write |
| `meal_planner.schema_migrations` | **none** | the app never reads it |

The foreign keys from the two receipt tables to `recipes` need no extra grant: PostgreSQL runs referential-integrity checks with the referenced table owner's rights. (If a deviation appears here at acceptance time, treat it as a finding, not a reason to broaden grants.) No sequences exist (UUID keys, no serials), so no sequence grants are needed. No functions or triggers are used.

### 1.4 What the role must NOT have

- Ownership of the database, the schema, any table, index, sequence or function (the migration owner owns them; separate login).
- Schema `CREATE` on `meal_planner`, `public`, or anything else; `CREATE` on the database; `TEMPORARY` is not needed (PUBLIC has it by default; see query 8).
- `DELETE`, `TRUNCATE`, `REFERENCES`, `TRIGGER` on any of the four tables; any privilege on `schema_migrations`; any column-level extras.
- Superuser, `CREATEROLE`, `CREATEDB`, `REPLICATION`, `BYPASSRLS`; membership in any other role (in particular `neon_superuser`, `pg_read_all_data`, `pg_write_all_data`).
- Any access to `neon_auth`/`auth` schemas or `public` tables. Default privileges or PUBLIC grants must not reach `meal_planner`.

**Highest-risk suspicion.** Neon documents that roles created through the Console or API are made members of `neon_superuser` (which carries `CREATEDB`, `CREATEROLE`, `BYPASSRLS`), while roles created by SQL are not. If the existing runtime role was created via Console/API, it likely fails section 1.4 regardless of the migration's grants. Queries 1 and 2 test this directly. Whether it can be removed (`REVOKE neon_superuser FROM ...`) on the owner's plan: **UNVERIFIED**; see approval item A1b.

### 1.5 Provider references (accessed 2026-11-11)

| Claim | Source |
|---|---|
| `-pooler` host, PgBouncer transaction mode, `SET`/`RESET` unsupported | https://neon.com/docs/connect/connection-pooling |
| Pooler rejects non-allowlisted startup parameters | https://neon.com/docs/connect/connection-errors (Unsupported startup parameter) |
| Console/API roles join `neon_superuser`; SQL-created roles do not; docs give no SQL for listing role attributes | https://neon.com/docs/manage/roles |
| `verify-full` recommended; ISRG Root X1; `PGSSLROOTCERT` | https://neon.com/docs/connect/connect-securely |
| Secret type is write-only after save; cannot be converted to Config in place | https://vercel.com/docs/environment-variables/sensitive-environment-variables |

These were read by a research helper through page fetches, not by hand-checking in the console. Anything not in this table (CLI flags, whether an env change needs a redeploy, Neon password-reset behavior) is **UNVERIFIED** and is marked where used.

## 2. Read-only verification SQL (owner-run, Preview)

Connect as a privileged admin/owner login to the Preview branch **database**, using the *direct* (unpooled) host and libpq environment variables (see 3.1 for the safe pattern). Every statement is `SELECT`. Set the role name privately; do not paste it anywhere:

```bash
psql -X -v ON_ERROR_STOP=1 -v rt="$RUNTIME_ROLE" -f preview-readiness-checks.sql   # your local copy of the queries below
```

`:'rt'` is psql variable substitution. Output stays on the owner's terminal; record only PASS/FAIL per query, never role names or identifiers.

**Q1. Role attributes**
```sql
SELECT rolcanlogin, rolsuper, rolcreaterole, rolcreatedb, rolreplication,
       rolbypassrls, rolconnlimit, rolvaliduntil IS NOT NULL AS has_expiry
FROM pg_roles WHERE rolname = :'rt';
```
Expect one row: `rolcanlogin=t`, all others `f`; `rolconnlimit` -1 or a sane cap. Zero rows: wrong name or role absent (stop; do not create it, report). `rolsuper/createrole/createdb/replication/bypassrls = t`: over-privileged (A1b). `rolcanlogin=f`: credential cannot work. `has_expiry=t`: a password expiry may silently break Preview later.

**Q2. Role memberships**
```sql
SELECT g.rolname AS granted_role, m.admin_option
FROM pg_auth_members m
JOIN pg_roles g ON g.oid = m.roleid
JOIN pg_roles u ON u.oid = m.member
WHERE u.rolname = :'rt';
```
Expect zero rows. Any row (`neon_superuser`, `pg_*_all_data`, an app/owner role) means inherited privileges beyond section 1.3: over-privileged (A1b).

**Q3. Ownership**
```sql
SELECT 'schema' AS kind, nspname AS name, pg_get_userbyid(nspowner) AS owner
  FROM pg_namespace WHERE nspname = 'meal_planner'
UNION ALL
SELECT CASE relkind WHEN 'r' THEN 'table' WHEN 'i' THEN 'index' ELSE relkind::text END,
       c.relname, pg_get_userbyid(c.relowner)
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = 'meal_planner'
ORDER BY 1, 2;
```
Expect: schema, 4 tables, their indexes (`*_pkey`, unique constraints, `recipes_owner_page`), all owned by the **same migration-owner role, not the runtime role**. Compare owners by eye or `owner <> :'rt'`. Any object owned by the runtime role: the role can DROP/GRANT it (A1c, owner transfer). Mixed owners: schema drift from the reviewed migration; stop and report.

**Q3b. Anything the runtime role owns anywhere in this database**
```sql
SELECT n.nspname, c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relowner = (SELECT oid FROM pg_roles WHERE rolname = :'rt')
UNION ALL SELECT nspname, '(schema)' FROM pg_namespace
WHERE nspowner = (SELECT oid FROM pg_roles WHERE rolname = :'rt')
UNION ALL SELECT 'database', datname FROM pg_database
WHERE datdba = (SELECT oid FROM pg_roles WHERE rolname = :'rt');
```
Expect zero rows. Any row is an ownership deviation (note: a Neon Console role that is also the project's default role would own the default database).

**Q4. Table privilege matrix (effective, includes PUBLIC and inherited)**
```sql
SELECT t.tbl,
  has_table_privilege(:'rt', 'meal_planner.'||t.tbl, 'SELECT')     AS sel,
  has_table_privilege(:'rt', 'meal_planner.'||t.tbl, 'INSERT')     AS ins,
  has_table_privilege(:'rt', 'meal_planner.'||t.tbl, 'UPDATE')     AS upd,
  has_table_privilege(:'rt', 'meal_planner.'||t.tbl, 'DELETE')     AS del,
  has_table_privilege(:'rt', 'meal_planner.'||t.tbl, 'TRUNCATE')   AS trunc,
  has_table_privilege(:'rt', 'meal_planner.'||t.tbl, 'REFERENCES') AS refs,
  has_table_privilege(:'rt', 'meal_planner.'||t.tbl, 'TRIGGER')    AS trig
FROM (VALUES ('recipes'),('recipe_creates'),('recipe_updates'),('schema_migrations')) t(tbl)
ORDER BY 1;
```
Expected exactly:

| table | sel | ins | upd | del | trunc | refs | trig |
|---|---|---|---|---|---|---|---|
| recipe_creates | t | t | f | f | f | f | f |
| recipe_updates | t | t | f | f | f | f | f |
| recipes | t | t | t | f | f | f | f |
| schema_migrations | f | f | f | f | f | f | f |

A missing `t` means the feature fails (503 at runtime); a grant fix is A1a. An extra `t` (especially `del`, `trunc`, `upd` on receipts, any on `schema_migrations`) violates least privilege (A1a revoke). Zero-row/error "relation does not exist": schema drift, stop.

**Q5. Raw ACLs (shows direct vs PUBLIC grants)**
```sql
SELECT c.relname,
       CASE a.grantee WHEN 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END AS grantee,
       a.privilege_type, a.is_grantable
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
CROSS JOIN LATERAL aclexplode(c.relacl) a
WHERE n.nspname = 'meal_planner' AND c.relkind = 'r'
ORDER BY 1, 2, 3;
```
Expect: owner rows (all privileges), runtime role rows exactly as the table in Q4, **no `PUBLIC` rows** (the migration revokes them), no `is_grantable = true` for the runtime role. A PUBLIC row means the migration's `REVOKE` was undone or a default privilege applies (see Q7). `c.relacl IS NULL` for a table would mean default owner-only ACL, which would also fail Q4 for the runtime role.

**Q5b. Column-level grants**
```sql
SELECT c.relname, a.attname, a.attacl
FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'meal_planner' AND a.attacl IS NOT NULL AND NOT a.attisdropped;
```
Expect zero rows. Rows mean unreviewed column-level grants.

**Q6. Schema and database access, and reach into other schemas**
```sql
SELECT has_database_privilege(:'rt', current_database(), 'CONNECT') AS db_connect,
       has_database_privilege(:'rt', current_database(), 'CREATE')  AS db_create,
       has_schema_privilege(:'rt', 'meal_planner', 'USAGE')         AS mp_usage,
       has_schema_privilege(:'rt', 'meal_planner', 'CREATE')        AS mp_create,
       has_schema_privilege(:'rt', 'public', 'CREATE')              AS public_create;
```
Expect `t, f, t, f, f`. `db_connect=f` or `mp_usage=f`: not usable (A1a). `db_create`, `mp_create` or `public_create = t`: can create objects (A1a revoke; `public_create=t` may stem from an older default or inheritance: investigate Q2).

```sql
SELECT n.nspname, c.relname,
       concat_ws(',',
         CASE WHEN has_table_privilege(:'rt', c.oid, 'SELECT')   THEN 'SELECT' END,
         CASE WHEN has_table_privilege(:'rt', c.oid, 'INSERT')   THEN 'INSERT' END,
         CASE WHEN has_table_privilege(:'rt', c.oid, 'UPDATE')   THEN 'UPDATE' END,
         CASE WHEN has_table_privilege(:'rt', c.oid, 'DELETE')   THEN 'DELETE' END) AS privs
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r','p','v','m')
  AND n.nspname NOT IN ('pg_catalog','information_schema','pg_toast','meal_planner')
  AND (has_table_privilege(:'rt', c.oid, 'SELECT') OR has_table_privilege(:'rt', c.oid, 'INSERT')
    OR has_table_privilege(:'rt', c.oid, 'UPDATE') OR has_table_privilege(:'rt', c.oid, 'DELETE'))
ORDER BY 1, 2;
```
Expect zero rows. Rows in `neon_auth`/`auth` are a serious deviation (the runtime login could read auth data; likely via `neon_superuser` membership, Q2). Rows in `public` mean existing public tables are readable or writable by the runtime role (directly or via PUBLIC); report the table names privately and decide whether to revoke; do not change `public` as part of this work without a separate approval.

**Q7. Default privileges**
```sql
SELECT pg_get_userbyid(d.defaclrole) AS for_role,
       COALESCE(d.defaclnamespace::regnamespace::text, '(all schemas)') AS schema,
       d.defaclobjtype AS objtype, d.defaclacl
FROM pg_default_acl d ORDER BY 1, 2, 3;
```
Expect no entry whose ACL mentions the runtime role or `=` (PUBLIC) for the migration owner in `meal_planner` or all schemas (an unscoped default granting the runtime role table rights would silently give it DELETE etc. on future tables; one for `PUBLIC` with `r` would undo the migration's REVOKE). Entries not touching the runtime role or PUBLIC are fine.

**Q8. Database owner, TEMP, and role-level settings**
```sql
SELECT pg_get_userbyid(datdba) AS db_owner,
       has_database_privilege(:'rt', datname, 'TEMPORARY') AS temp
FROM pg_database WHERE datname = current_database();

SELECT setrole::regrole::text AS role, setconfig FROM pg_db_role_setting
WHERE setrole = (SELECT oid FROM pg_roles WHERE rolname = :'rt');
```
Expect `db_owner` is not the runtime role. `temp=t` is the PostgreSQL default for PUBLIC and is tolerated (the app does not need it; revoking it is optional hardening, not part of this approval). Expect no role settings, or only benign ones. A `search_path` or `statement_timeout` setting could conflict with the code, which uses schema-qualified names and `SET LOCAL`; report it.

**Q9. Migration version and schema shape**
```sql
SELECT version, applied_at FROM meal_planner.schema_migrations ORDER BY version;

SELECT relname, relrowsecurity, relforcerowsecurity FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'meal_planner' AND relkind = 'r' ORDER BY 1;

SELECT conrelid::regclass AS tbl, conname, contype FROM pg_constraint
WHERE connamespace = 'meal_planner'::regnamespace ORDER BY 1, 2;
```
Expect exactly one row, `version=1`; four tables, all `relrowsecurity=f` (RLS is intentionally not implemented; the server is the ownership boundary); constraints consistent with `001_recipes.sql` (primary keys on all four tables, unique `(owner_issuer, owner_subject, id)` plus check constraints on `recipes`, two foreign keys). A different or extra version means the issue's inventory is stale: stop and reconcile against the reviewed migration; never reapply it. Missing constraints mean drift.

**Q10. Row counts (private; do not paste)**
```sql
SELECT (SELECT count(*) FROM meal_planner.recipes) AS recipes,
       (SELECT count(*) FROM meal_planner.recipe_creates) AS creates,
       (SELECT count(*) FROM meal_planner.recipe_updates) AS updates;
```
Expect zeros (or only the owner's own synthetic test rows). Unexpected rows: report, do not delete under this approval.

## 3. Credential usability check (no secret revealed)

Run by the owner on her own machine, never in CI, a shared terminal or a recorded session. It proves: pooled host, verified TLS, role can log in, TLS actually is `verify-full`. It prints only PASS/FAIL, the server version, and booleans.

### 3.1 Safe pattern

Do not use a URL on the command line (visible in `ps`, history). Use libpq environment variables fed by silent prompts:

```bash
# begin credential usability check
set +x
unset PGSSLMODE PGSSLROOTCERT PGOPTIONS
read -r  -p 'pooled host (<endpoint>-pooler...): ' PGHOST ; export PGHOST
read -r  -p 'database: '                           PGDATABASE ; export PGDATABASE
read -r  -p 'runtime role: '                       PGUSER ; export PGUSER
read -rs -p 'password (hidden): '                  PGPASSWORD ; echo ; export PGPASSWORD
export PGSSLMODE=verify-full PGCONNECT_TIMEOUT=5 PGAPPNAME=meal-planner-readiness-check
# add: export PGSSLROOTCERT=/path/to/system-or-isrg-root.pem   only if the OS store is not picked up
if psql -X -w -At -c "SELECT 'PASS server=' || current_setting('server_version') || ' ssl=' || (SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid())" ; then :; else echo FAIL; fi 2>&1 | sed 's/^psql: error: .*/FAIL (connection or auth error; details withheld)/'
unset PGHOST PGDATABASE PGUSER PGPASSWORD PGSSLMODE PGSSLROOTCERT
# end credential usability check
```

Notes:

- `pg_stat_ssl` shows a boolean for the session; no secret. `sslmode=verify-full` makes libpq itself fail when the certificate chain or hostname is wrong, so a PASS implies verified TLS. `psql` error text can echo the host: the `sed` line suppresses it. Do not remove it.
- Run it twice, first with the pooled host, expecting PASS. A direct (unpooled) host is not what the app uses and is not a substitute. Whether `pg_stat_ssl` is readable by a non-privileged role on Neon is **UNVERIFIED**; if that column returns an error, drop the `ssl=` part, because `verify-full` already enforces TLS.
- Optional least-privilege probe with the same environment (SELECT-only, harmless): `SELECT has_table_privilege('meal_planner.recipes','INSERT'), has_table_privilege('meal_planner.recipes','DELETE'), has_schema_privilege('meal_planner','CREATE');` expecting `t, f, f`. This repeats Q4/Q6 from the runtime role's own point of view and also confirms the login sees the schema.
- Do **not** test DELETE/DDL by actually running them on Preview. The denied-statement behavior is covered against a disposable database in `hosted/recipe_store_integration_test.go`; live proof is Q4/Q6 plus the section 5 acceptance.
- The pooled connection holds an unprivileged login only; it cannot show whether the password stored in Vercel later matches. That is proven by section 5 (end-to-end save) and by an exact-name-and-scope check (A3).

## 4. Approval packet

Nothing here is authorized. Each item needs Caroline's explicit approval at action time. Do the section 2 and 3 checks first; items A1x are conditional on their findings. Rollback and verification are per item.

### A0. Recheck immediately before setup (read-only; no approval beyond running section 2)
- Re-run Q1 to Q10 the same day. The 2026-11-09 inventory is stale evidence.
- Confirm the deployed Preview commit is `ef7e561` or newer; the 2026-11-09 inventory said it was `aaff49c`, 27 commits behind reviewed main. Pushing to develop already triggers an automatic Preview deploy (hosted-deploy.md section 2); nothing manual is needed beyond confirming.

### A1. Grant fixes (only if Q1 to Q9 deviate)

**Who:** the migration owner / project admin, on a trusted machine, via a privately supplied admin connection (the pattern in recipes.md "Migration review and application"). Not an agent.

**A1a. Align the grants to section 1.3.** Template; substitute names privately, review the diff against the Q4 matrix, and run only the statements that are needed:
```sql
BEGIN;
GRANT CONNECT ON DATABASE <db> TO <runtime_role>;
GRANT USAGE ON SCHEMA meal_planner TO <runtime_role>;
GRANT SELECT, INSERT, UPDATE ON meal_planner.recipes TO <runtime_role>;
GRANT SELECT, INSERT ON meal_planner.recipe_creates, meal_planner.recipe_updates TO <runtime_role>;
-- revoke excess (only if Q4/Q5/Q6 showed it):
-- REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER ON ALL TABLES IN SCHEMA meal_planner FROM <runtime_role>;
-- REVOKE ALL ON meal_planner.schema_migrations FROM <runtime_role>;
-- REVOKE CREATE ON SCHEMA meal_planner FROM <runtime_role>;
COMMIT;
```
Rollback: the inverse `REVOKE` of exactly the statements applied (keep the pre-change Q4/Q5 output privately to know the starting state). Rolling back leaves the feature failing closed with 503, which is safe.
Verify: re-run Q4, Q5, Q6; the matrix must match exactly.

**A1b. Remove over-privilege (Q1/Q2 show superuser-like attributes or memberships, e.g. `neon_superuser`).** Exact action depends on how the role was created: **UNVERIFIED** whether `REVOKE <role> FROM <runtime_role>` and `ALTER ROLE ... NOBYPASSRLS NOCREATEDB NOCREATEROLE` are permitted on Neon for a Console-created role. If not removable, the least-privilege role needs to be a SQL-created role, which means a new role and a new credential; the issue forbids that without a decision, so **stop and ask Caroline** (it also changes A2). Do not weaken the requirement in the runbook to fit the role.
Rollback: re-grant the membership/attributes removed (record them first).
Verify: Q1, Q2, Q6 (second query).

**A1c. Ownership transfer** (Q3/Q3b show the runtime role owning objects): `ALTER ... OWNER TO <migration_owner>` for each, or reassign. Record original owners first; rollback is the reverse `ALTER`. Verify: Q3, Q3b.

### A2. Credential issuance or rotation (only if section 3 fails, the password is unknown, or Q1 showed expiry)

**Who:** Caroline in the Neon console (or CLI/API), on the Preview branch only. Not an agent. The password is shown once, so copy it straight to a password manager / the Vercel Secret form; never to chat, shell history or this repo.
**Action:** reset the existing runtime role's password (no new role). Exact Neon UI/API action and whether a reset invalidates open sessions: **UNVERIFIED** (not in the pages read). Prefer verifying on the Neon docs/console before acting.
**Rollback:** the old password cannot be restored once reset; rollback is to reset again and re-set the Vercel Secret. If Preview already had a working Secret, rotate by updating the Secret immediately after the reset, accepting a short window of 503s on Preview.
**Verify:** section 3 with the new password, then A3 and section 5.

### A3. Set the Vercel Preview Secret `MEAL_RECIPES_DATABASE_URL` (currently absent)

**Who:** Caroline, in the Vercel dashboard (Settings, Environment Variables) for the Meal Planner project. If she approves the CLI instead, the value must come from stdin or the interactive prompt, never argv; the exact CLI flags for marking a Secret are **UNVERIFIED** (the repo pins `vercel@65.9.0`; check its `env add --help` locally).
**Action:**
- Name exactly `MEAL_RECIPES_DATABASE_URL`.
- Type **Secret** (the write-only type; Vercel documents Secrets as unreadable after save).
- Environment: **Preview only**. Not Production, not Development.
- Value: the pooled DSN of section 1.2 with `?sslmode=verify-full` (plus `sslrootcert` only if section 3 needed it). No `channel_binding`. Do not apply the Preview scope to a single branch unless Caroline decides to (UNVERIFIED how the repo's Preview deployments are branched; default is all Preview).
- Never `VITE_`-prefixed.
**Then:** a new Preview deployment is needed for the function to see the variable. Whether Vercel applies a changed env var to existing deployments: **UNVERIFIED** (assume not). Trigger it by the normal path (a push to develop), not by manual deploy.
**Rollback:** delete the variable in the dashboard (or replace it); the API returns the generic 503 and the app keeps its fail-closed behavior. A Secret cannot be converted to Config; recreate if a mistake was made.
**Verify (no values revealed):**
1. Dashboard/`vercel env ls`: lists `MEAL_RECIPES_DATABASE_URL` for Preview only, type Secret, no Production or Development entry.
2. The Preview pull job passes `tools/deploy/check-pulled-env.sh` (it accepts only `[SENSITIVE]` and fails on a literal URL or lookalike names); the CI log names the variable, never the value.
3. Section 5.

### A4. Nothing else
No Production changes, no migration, no new role (unless A1b forces a decision), no auth changes (#74 owns that), no database branch operations.

## 5. Post-setup acceptance

Preconditions: A0 to A3 done; the new Preview deployment is READY; you have approved Preview auth for the test identity (auth readiness is #74; identities for cross-account tests are #53/#74). Use synthetic recipe data only, and record evidence privately against the exact commit SHA, the migration version, and the deployed URL.

### 5.1 Manual-recipe end-to-end test (this packet; satisfies the #61 remaining checklist)
1. Signed out: `GET /api/recipes` is rejected (401), no data.
2. Sign in on the Preview URL; open Recipes; the list loads from the server (empty is fine).
3. Create a synthetic recipe; success appears only after the server confirms; note that the label does not call local planning "synced".
4. Reload: the recipe persists. Sign out, sign in, reopen: still there.
5. Open a second browser/profile, sign in as the same account: the recipe appears. Edit there; reload in the first; the edit shows.
6. Lost-response retry: with network throttle or request blocking, repeat a create with the same operation identity; verify (privately, owner query) exactly one `recipes` row and one `recipe_creates` receipt.
7. Stale revision: edit the same recipe in two windows; the second save conflicts (409) and neither person's form is silently overwritten.
8. Failure states: offline, session expiry, repeated Save, cancel: honest messages, unsaved fields preserved, no success navigation before confirmation.
9. Account switch: sign out and in as another approved identity: no recipes of the first account visible (cache isolation). Cross-account/cross-issuer ID access returns the same 404 (needs #53/#74 identities; synthetic tests can proceed first).
10. Runtime privileges: after steps 3 to 7, re-run Q4 to Q7 from section 2 (nothing should have changed) and confirm the browser bundle and public build artifacts contain no DSN/`postgres://` string (search the deployed static assets for `postgres`).
11. Owner-side row check (Q10): row counts match the test actions; delete nothing under this approval.
12. Record Preview acceptance in the private evidence log; record remaining Production requirements separately. No Production step is implied.

### 5.2 How this differs from the #55 Preview MCP test
| | This acceptance (#61) | #55 owner test |
|---|---|---|
| Client | The web app, signed in, manual form | The local MCP server to hosted Preview draft/review workflow |
| API | `/api/recipes` manual approved path only | Draft submission/review (not implemented; paused) |
| Runner | Any approved tester or agent with synthetic data | Caroline personally, hands-on |
| Gate | Prerequisite for #55 persistence | Required before Production shipment of #25/#55 |

Passing section 5.1 **does not** satisfy or replace Caroline's personal #55 test, and the manual model is not a lossless draft contract (#72).

## 6. Open items and unverified claims

- Whether the existing role is Console/API-created, and so inherits `neon_superuser` (section 1.4): unknown until Q1/Q2.
- Trust roots for verify-full in the Vercel Go runtime without `sslrootcert`: UNVERIFIED until acceptance.
- Neon password-reset semantics, Vercel CLI `env add` Secret flags, whether env changes apply without a redeploy, `pg_stat_ssl` visibility for the runtime role: UNVERIFIED.
- Aggregate serverless concurrency (4 open connections per warm function) versus Neon capacity: needs provider monitoring (recipes.md).
- None of this packet was executed. All "Expect" values are derived from the migration and code, not from observing Preview.
