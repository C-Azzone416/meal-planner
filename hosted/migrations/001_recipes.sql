-- Explicit migration-owner operation, never applied by application startup.
-- Supply libpq connection settings privately in the environment, not command arguments.
-- psql -X -v ON_ERROR_STOP=1 -f hosted/migrations/001_recipes.sql
-- Duplicate application deliberately fails rather than hiding schema drift.
-- No roles, extensions, public-schema changes, or existing-data modifications.
BEGIN;
CREATE SCHEMA meal_planner;
REVOKE ALL ON SCHEMA meal_planner FROM PUBLIC;
CREATE TABLE meal_planner.schema_migrations (
 version integer PRIMARY KEY,
 applied_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO meal_planner.schema_migrations (version) VALUES (1);
CREATE TABLE meal_planner.recipes (
 id uuid PRIMARY KEY,
 owner_issuer text NOT NULL CHECK (length(owner_issuer) BETWEEN 1 AND 2048),
 owner_subject text NOT NULL CHECK (length(owner_subject) BETWEEN 1 AND 512),
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 status text NOT NULL CHECK (status IN ('draft', 'approved')),
 origin text NOT NULL CHECK (origin IN ('manual', 'agent')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 content jsonb NOT NULL CHECK (jsonb_typeof(content) = 'object'),
 UNIQUE (owner_issuer, owner_subject, id)
);
CREATE INDEX recipes_owner_page ON meal_planner.recipes (owner_issuer, owner_subject, id);
-- Immutable receipts return the original result even after subsequent edits.
-- Retain these for the recipe lifetime to preserve safe retries.
CREATE TABLE meal_planner.recipe_creates (
 owner_issuer text NOT NULL,
 owner_subject text NOT NULL,
 operation_key text NOT NULL CHECK (length(operation_key) BETWEEN 1 AND 200),
 request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
 recipe_id uuid NOT NULL,
 receipt jsonb NOT NULL,
 PRIMARY KEY (owner_issuer, owner_subject, operation_key),
 FOREIGN KEY (owner_issuer, owner_subject, recipe_id) REFERENCES meal_planner.recipes (owner_issuer, owner_subject, id)
);
CREATE TABLE meal_planner.recipe_updates (
 owner_issuer text NOT NULL,
 owner_subject text NOT NULL,
 recipe_id uuid NOT NULL,
 expected_revision bigint NOT NULL CHECK (expected_revision > 0),
 request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
 receipt jsonb NOT NULL,
 PRIMARY KEY (owner_issuer, owner_subject, recipe_id, expected_revision),
 FOREIGN KEY (owner_issuer, owner_subject, recipe_id) REFERENCES meal_planner.recipes (owner_issuer, owner_subject, id)
);
REVOKE ALL ON ALL TABLES IN SCHEMA meal_planner FROM PUBLIC;
-- Separately grant the pre-provisioned runtime role USAGE on this schema,
-- SELECT/INSERT/UPDATE on recipes, SELECT/INSERT on both receipt tables.
-- Runtime must not own this schema or have CREATE, DELETE, or migration rights.
COMMIT;
