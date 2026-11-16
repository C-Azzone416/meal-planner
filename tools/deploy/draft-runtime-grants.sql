-- Reviewed least-privilege policy for the one shared MEAL_DRAFTS_DATABASE_URL
-- role. The caller must supply an existing, independently approved role as the
-- psql identifier variable runtime_role. This file creates no role or credential.
GRANT USAGE ON SCHEMA public TO :"runtime_role";
GRANT SELECT ON public.draft_ingest_submissions, public.draft_membership_epochs, public.draft_pairings, public.draft_send_grants, public.draft_send_intents, public.draft_reviews TO :"runtime_role";
GRANT INSERT (environment,owner_issuer,owner_subject,operation_key,request_hash,originating_grant_hash,request,response,retained_bytes,created_at) ON public.draft_ingest_submissions TO :"runtime_role";
GRANT INSERT (environment,owner_issuer,owner_subject,membership_epoch,email,updated_at) ON public.draft_membership_epochs TO :"runtime_role";
GRANT UPDATE (membership_epoch,email,updated_at) ON public.draft_membership_epochs TO :"runtime_role";
GRANT INSERT (id,environment,issuer,user_code,poll_token_hash,code_challenge,challenge_fingerprint,client_label,status,created_at,expires_at) ON public.draft_pairings TO :"runtime_role";
GRANT UPDATE (user_code,status,last_polled_at,approved_at,consumed_at,owner_issuer,owner_subject,owner_email,approval_epoch) ON public.draft_pairings TO :"runtime_role";
GRANT INSERT (id,token_hash,environment,owner_issuer,owner_subject,email_at_approval,membership_epoch,client_label,scope,created_at,expires_at) ON public.draft_send_grants TO :"runtime_role";
GRANT UPDATE (revoked_at,last_used_at) ON public.draft_send_grants TO :"runtime_role";
GRANT INSERT (environment,owner_issuer,owner_subject,id,operation_key,originating_grant_hash,request_hash,submission,retained_bytes,state,revision,created_at,expires_at) ON public.draft_send_intents TO :"runtime_role";
GRANT UPDATE (state,revision,decided_at,draft_id,receipt_id,accepted_revision,submitted_at) ON public.draft_send_intents TO :"runtime_role";
GRANT INSERT (environment,owner_issuer,owner_subject,draft_id,revision,state,draft_revision,updated_at) ON public.draft_reviews TO :"runtime_role";
GRANT UPDATE (revision,state,draft_revision,approved_recipe_id,updated_at) ON public.draft_reviews TO :"runtime_role";
