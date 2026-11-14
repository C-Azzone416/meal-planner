-- Reviewed migration-owner operation only. Never run by application startup.
-- Preview only: no credential creation, no runtime grants, no existing data edits.
-- Duplicate application deliberately fails. This slice has no purge/retention job.
BEGIN;
CREATE TABLE public.draft_ingest_grants (
 token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash)=32),
 environment text NOT NULL CHECK (environment='preview'),
 owner_issuer text NOT NULL CHECK (length(owner_issuer) BETWEEN 1 AND 2048),
 owner_subject text NOT NULL CHECK (length(owner_subject) BETWEEN 1 AND 512),
 email_at_approval text NOT NULL CHECK (length(email_at_approval) BETWEEN 1 AND 254),
 scope text NOT NULL CHECK (scope='draft:create'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 last_used_at timestamptz,
 CHECK (expires_at>created_at AND expires_at<=created_at+interval '7 days')
);
CREATE TABLE public.draft_ingest_submissions (
 environment text NOT NULL CHECK (environment='preview'),
 owner_issuer text NOT NULL CHECK (length(owner_issuer) BETWEEN 1 AND 2048),
 owner_subject text NOT NULL CHECK (length(owner_subject) BETWEEN 1 AND 512),
 operation_key text NOT NULL CHECK (octet_length(operation_key) BETWEEN 1 AND 200),
 request_hash bytea NOT NULL CHECK (octet_length(request_hash)=32),
 -- Retained forever with receipt/key: deliberately no grant FK/cascade.
 originating_grant_hash bytea NOT NULL CHECK (octet_length(originating_grant_hash)=32),
 request json NOT NULL CHECK (json_typeof(request)='object'),
 response json NOT NULL CHECK (json_typeof(response)='object'),
 retained_bytes bigint NOT NULL CHECK (retained_bytes>0 AND retained_bytes<=16777216),
 created_at timestamptz NOT NULL,
 PRIMARY KEY (environment,owner_issuer,owner_subject,operation_key),
 CHECK (retained_bytes >= octet_length(request::text)+octet_length(response::text)+octet_length(operation_key)+octet_length(environment)+octet_length(owner_issuer)+octet_length(owner_subject)+64+4096)
);
CREATE INDEX draft_ingest_owner_time ON public.draft_ingest_submissions (environment,owner_issuer,owner_subject,created_at);
REVOKE ALL ON public.draft_ingest_grants,public.draft_ingest_submissions FROM PUBLIC;
-- Separately provision a dedicated non-owner runtime role. Minimum privileges:
-- USAGE on public; SELECT on grants; UPDATE(last_used_at) on grants (FOR SHARE
-- needs an UPDATE privilege); SELECT, INSERT on submissions. Do not grant any
-- grant-issuance/revocation fields, UPDATE/DELETE on submissions, schema CREATE,
-- recipes, or auth tables. Revocation is an administrative UPDATE revoked_at;
-- it conflicts with the submit transaction's SHARE lock.
-- No automatic grant provisioning: pairing, active-grant quotas, membership epoch
-- invalidation and private credential delivery require their own reviewed slice.
COMMIT;
