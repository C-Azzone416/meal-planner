-- Additive, reviewed migration-owner operation only. Never run at startup.
-- Source and environment feature gates remain closed after this migration.
BEGIN;
ALTER TABLE public.draft_ingest_grants DROP CONSTRAINT draft_ingest_grants_scope_check;
ALTER TABLE public.draft_ingest_grants ADD CONSTRAINT draft_ingest_grants_scope_check CHECK (scope IN ('draft:create','draft:propose'));
CREATE TABLE public.draft_send_intents (
 environment text NOT NULL CHECK (environment='preview'),
 owner_issuer text NOT NULL CHECK (length(owner_issuer) BETWEEN 1 AND 2048),
 owner_subject text NOT NULL CHECK (length(owner_subject) BETWEEN 1 AND 512),
 id uuid NOT NULL,
 operation_key text NOT NULL CHECK (octet_length(operation_key) BETWEEN 16 AND 127),
 originating_grant_hash bytea NOT NULL CHECK (octet_length(originating_grant_hash)=32),
 request_hash bytea NOT NULL CHECK (octet_length(request_hash)=32),
 submission jsonb NOT NULL CHECK (jsonb_typeof(submission)='object'),
 state text NOT NULL CHECK (state IN ('pending','denied','accepted')),
 revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 decided_at timestamptz,
 draft_id uuid,
 receipt_id uuid,
 PRIMARY KEY(environment,owner_issuer,owner_subject,id),
 UNIQUE(environment,owner_issuer,owner_subject,operation_key),
 CHECK (expires_at>created_at AND expires_at<=created_at+interval '30 minutes'),
 CHECK ((state='pending' AND decided_at IS NULL AND draft_id IS NULL AND receipt_id IS NULL)
     OR (state='denied' AND decided_at IS NOT NULL AND draft_id IS NULL AND receipt_id IS NULL)
     OR (state='accepted' AND decided_at IS NOT NULL AND draft_id IS NOT NULL AND receipt_id IS NOT NULL))
);
CREATE INDEX draft_send_intents_owner_pending ON public.draft_send_intents(environment,owner_issuer,owner_subject,id) WHERE state='pending';

CREATE TABLE public.draft_reviews (
 environment text NOT NULL CHECK (environment='preview'),
 owner_issuer text NOT NULL CHECK (length(owner_issuer) BETWEEN 1 AND 2048),
 owner_subject text NOT NULL CHECK (length(owner_subject) BETWEEN 1 AND 512),
 draft_id uuid NOT NULL,
 revision bigint NOT NULL CHECK (revision BETWEEN 2 AND 9007199254740991),
 state text NOT NULL CHECK (state IN ('ready','approved')),
 draft_revision bigint NOT NULL CHECK (draft_revision BETWEEN 1 AND 9007199254740991),
 approved_recipe_id uuid,
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(environment,owner_issuer,owner_subject,draft_id),
 CHECK ((state='ready' AND approved_recipe_id IS NULL) OR (state='approved' AND approved_recipe_id IS NOT NULL))
);
REVOKE ALL ON public.draft_send_intents,public.draft_reviews FROM PUBLIC;
-- A separately provisioned workflow role needs only the explicit statement
-- rights used by hosted/draft_workflow_store.go. It must not mint/revoke grants,
-- alter auth membership, migrate, delete drafts, or receive recipe-table rights.
COMMIT;
