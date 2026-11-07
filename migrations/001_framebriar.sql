-- Apply independently in Preview and Production, using separate databases and
-- credentials. The application never runs migrations or provisions storage.
-- No invitations, raw account identifiers, or feedback payloads belong here.
BEGIN;
CREATE TABLE framebriar_enrollments (
    owner text PRIMARY KEY CHECK (length(owner) BETWEEN 1 AND 256),
    generation uuid NOT NULL,
    state text NOT NULL DEFAULT '' CHECK (length(state) <= 32),
    key_id text NOT NULL DEFAULT '' CHECK (length(key_id) <= 128),
    sealed bytea CHECK (octet_length(sealed) <= 32768),
    expires_at timestamptz,
    attempt text NOT NULL DEFAULT '' CHECK (length(attempt) <= 128),
    attempt_at timestamptz,
    attempt_digest text NOT NULL DEFAULT '' CHECK (length(attempt_digest) <= 64),
    cooldown_until timestamptz,
    rate_window timestamptz,
    rate_count integer NOT NULL DEFAULT 0 CHECK (rate_count >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE framebriar_enrollments IS
 'Server-only encrypted Framebriar enrollment, redemption coordination and durable rate limits. Account erasure must revoke first and rotate generation.';
-- Grant only SELECT, INSERT, UPDATE to the runtime role. Do not expose this table
-- through browser/public data APIs. Use a separate owner role for migration.
COMMIT;
