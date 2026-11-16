-- Private, clean-inspector aggregate only; psql -X -qAt -v ON_ERROR_STOP=1.
-- Supply as_of as an ISO-8601 instant. No identifier, owner, token hash,
-- credential, draft content or recipe content is returned or modified.
BEGIN READ ONLY;
SET LOCAL search_path = pg_catalog;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '1s';

WITH instant AS (SELECT :'as_of'::timestamptz AS now),
pairing_stats AS (
 SELECT count(*) AS total,
  count(*) FILTER (WHERE status='pending') AS pending,
  count(*) FILTER (WHERE status='approved') AS approved,
  count(*) FILTER (WHERE status='denied') AS denied,
  count(*) FILTER (WHERE status='consumed') AS consumed,
  count(*) FILTER (WHERE status IN ('pending','approved') AND expires_at<=instant.now) AS expired_open,
  count(*) FILTER (WHERE status IN ('denied','consumed') AND user_code IS NOT NULL) AS terminal_codes
 FROM public.draft_pairings CROSS JOIN instant
), grant_stats AS (
 SELECT count(*) AS total,
  count(*) FILTER (WHERE revoked_at IS NULL AND expires_at>instant.now) AS active,
  count(*) FILTER (WHERE revoked_at IS NOT NULL) AS revoked,
  count(*) FILTER (WHERE expires_at<=instant.now) AS expired,
  (SELECT count(*) FROM (SELECT 1 FROM public.draft_send_grants GROUP BY environment,owner_issuer,owner_subject HAVING count(*)>=50) owners) AS owners_at_retained_cap
 FROM public.draft_send_grants CROSS JOIN instant
), intent_stats AS (
 SELECT count(*) AS total,
  count(*) FILTER (WHERE state='pending') AS pending,
  count(*) FILTER (WHERE state='pending' AND expires_at<=instant.now) AS expired_pending,
  count(*) FILTER (WHERE state='denied') AS denied,
  count(*) FILTER (WHERE state='accepted') AS accepted,
  (SELECT count(*) FROM (SELECT 1 FROM public.draft_send_intents GROUP BY environment,owner_issuer,owner_subject HAVING count(*)>=100 OR sum(retained_bytes)>=16777216) owners) AS owners_at_retained_cap
 FROM public.draft_send_intents CROSS JOIN instant
), integrity_stats AS (
 SELECT
  (SELECT count(*) FROM public.draft_send_intents i LEFT JOIN public.draft_ingest_submissions s ON s.environment=i.environment AND s.owner_issuer=i.owner_issuer AND s.owner_subject=i.owner_subject AND s.operation_key=i.id::text WHERE i.state='accepted' AND s.operation_key IS NULL) AS accepted_without_submission,
  (SELECT count(*) FROM public.draft_reviews r LEFT JOIN meal_planner.recipes p ON p.owner_issuer=r.owner_issuer AND p.owner_subject=r.owner_subject AND p.id=r.approved_recipe_id WHERE r.state='approved' AND p.id IS NULL) AS approved_without_recipe
), submission_stats AS (
 SELECT count(*) AS total,
  (SELECT count(*) FROM (SELECT 1 FROM public.draft_ingest_submissions GROUP BY environment,owner_issuer,owner_subject HAVING count(*)>=100 OR sum(retained_bytes)>=16777216) owners) AS owners_at_retained_cap
 FROM public.draft_ingest_submissions
), review_stats AS (
 SELECT count(*) FILTER (WHERE state='ready') AS ready,
  count(*) FILTER (WHERE state='approved') AS approved
 FROM public.draft_reviews
)
SELECT jsonb_build_object(
 'version',1,'asOf',to_char(instant.now AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),
 'pairings',to_jsonb(pairing_stats),'grants',to_jsonb(grant_stats),
 'intents',to_jsonb(intent_stats),'submissions',to_jsonb(submission_stats),
 'reviews',to_jsonb(review_stats),'integrity',to_jsonb(integrity_stats)
) FROM instant,pairing_stats,grant_stats,intent_stats,submission_stats,review_stats,integrity_stats;
ROLLBACK;
