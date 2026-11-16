-- Reviewed migration-owner operation only. Never run by application startup.
-- Additive Preview pairing state. No credentials are created by this migration.
BEGIN;

CREATE TABLE public.draft_membership_epochs (
 environment text NOT NULL CHECK (environment='preview'),
 owner_issuer text NOT NULL CHECK (length(owner_issuer) BETWEEN 1 AND 2048),
 owner_subject text NOT NULL CHECK (length(owner_subject) BETWEEN 1 AND 512),
 membership_epoch text NOT NULL CHECK (membership_epoch ~ '^[A-Za-z0-9_-]{16,64}$'),
 email text NOT NULL CHECK (length(email) BETWEEN 1 AND 254),
 updated_at timestamptz NOT NULL,
 PRIMARY KEY (environment,owner_issuer,owner_subject)
);

CREATE TABLE public.draft_pairings (
 id uuid PRIMARY KEY,
 environment text NOT NULL CHECK (environment='preview'),
 issuer text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 2048),
 user_code text UNIQUE CHECK (user_code IS NULL OR user_code ~ '^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{8}$'),
 poll_token_hash bytea NOT NULL UNIQUE CHECK (octet_length(poll_token_hash)=32),
 code_challenge text NOT NULL CHECK (code_challenge ~ '^[A-Za-z0-9_-]{43}$'),
 challenge_fingerprint text NOT NULL CHECK (challenge_fingerprint ~ '^[0-9a-f]{8}$'),
 client_label text NOT NULL CHECK (length(client_label) BETWEEN 1 AND 80),
 status text NOT NULL CHECK (status IN ('pending','approved','denied','consumed')),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 last_polled_at timestamptz,
 approved_at timestamptz,
 consumed_at timestamptz,
 owner_issuer text CHECK (owner_issuer IS NULL OR length(owner_issuer) BETWEEN 1 AND 2048),
 owner_subject text CHECK (owner_subject IS NULL OR length(owner_subject) BETWEEN 1 AND 512),
 owner_email text CHECK (owner_email IS NULL OR length(owner_email) BETWEEN 1 AND 254),
 approval_epoch text CHECK (approval_epoch IS NULL OR approval_epoch ~ '^[A-Za-z0-9_-]{16,64}$'),
 CHECK (expires_at>created_at AND expires_at<=created_at+interval '10 minutes'),
 CHECK ((status='pending' AND approved_at IS NULL AND consumed_at IS NULL AND owner_issuer IS NULL AND owner_subject IS NULL AND owner_email IS NULL AND approval_epoch IS NULL)
     OR (status='approved' AND approved_at IS NOT NULL AND consumed_at IS NULL AND owner_issuer IS NOT NULL AND owner_subject IS NOT NULL AND owner_email IS NOT NULL AND approval_epoch IS NOT NULL)
     OR (status='consumed' AND approved_at IS NOT NULL AND consumed_at IS NOT NULL AND owner_issuer IS NOT NULL AND owner_subject IS NOT NULL AND owner_email IS NOT NULL AND approval_epoch IS NOT NULL AND user_code IS NULL)
     OR status='denied')
);
CREATE INDEX draft_pairings_expiry ON public.draft_pairings (expires_at);

CREATE TABLE public.draft_send_grants (
 id uuid PRIMARY KEY,
 token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash)=32),
 environment text NOT NULL CHECK (environment='preview'),
 owner_issuer text NOT NULL CHECK (length(owner_issuer) BETWEEN 1 AND 2048),
 owner_subject text NOT NULL CHECK (length(owner_subject) BETWEEN 1 AND 512),
 email_at_approval text NOT NULL CHECK (length(email_at_approval) BETWEEN 1 AND 254),
 membership_epoch text NOT NULL CHECK (membership_epoch ~ '^[A-Za-z0-9_-]{16,64}$'),
 client_label text NOT NULL CHECK (length(client_label) BETWEEN 1 AND 80),
 scope text NOT NULL CHECK (scope='draft:propose'),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 last_used_at timestamptz,
 CHECK (expires_at>created_at AND expires_at<=created_at+interval '7 days')
);
CREATE INDEX draft_send_grants_owner ON public.draft_send_grants (environment,owner_issuer,owner_subject,created_at DESC);

REVOKE ALL ON public.draft_membership_epochs,public.draft_pairings,public.draft_send_grants FROM PUBLIC;
-- Provision separate server roles tomorrow. The pairing role needs only the
-- exact SELECT/INSERT/UPDATE columns exercised by the reviewed statements and
-- no recipe, approval, migration, schema-CREATE, DELETE or TRUNCATE rights.
-- The future intent role may SELECT draft_send_grants and UPDATE last_used_at;
-- it must not mint, revoke, or expose raw credentials.

COMMIT;
