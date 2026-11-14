-- Clean trusted inspector only; psql -X -v ON_ERROR_STOP=1 -At
-- Supply component/runtime_role as psql variables; connection via private env.
-- No credentials in argv/output. No target SQL or migration is executed.
BEGIN READ ONLY;
SET LOCAL search_path = pg_catalog;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '1s';
-- Explicitly supported major versions; older/future servers must be reviewed.
SELECT 1 / CASE WHEN current_setting('server_version_num')::integer BETWEEN 170000 AND 189999 THEN 1 ELSE 0 END AS supported_version \gset

WITH selected AS (
 SELECT unnest(CASE :'component'
 WHEN 'recipes' THEN ARRAY['meal_planner.recipe_creates','meal_planner.recipe_updates','meal_planner.recipes','meal_planner.schema_migrations']
 WHEN 'feedback' THEN ARRAY['public.framebriar_enrollments']
 WHEN 'draft' THEN ARRAY['public.draft_ingest_grants','public.draft_ingest_submissions']
 ELSE ARRAY[]::text[] END) AS name
), runtime AS (SELECT * FROM pg_roles WHERE rolname=:'runtime_role'), objects AS (
 SELECT s.name,c.*,n.nspname,n.nspacl,n.nspowner FROM selected s
 JOIN pg_namespace n ON n.nspname=split_part(s.name,'.',1) JOIN pg_class c ON c.relnamespace=n.oid AND c.relname=split_part(s.name,'.',2)
), details AS (
 SELECT o.name,jsonb_build_object(
 'name',o.name,'kind',o.relkind::text,'ownerIsRuntime',o.relowner=r.oid,'rowSecurity',o.relrowsecurity,'forceRowSecurity',o.relforcerowsecurity,
 'triggers',COALESCE((SELECT jsonb_agg(pg_get_triggerdef(oid,true) ORDER BY tgname) FROM pg_trigger WHERE tgrelid=o.oid AND NOT tgisinternal),'[]'::jsonb),
 'rules',COALESCE((SELECT jsonb_agg(jsonb_build_object('name',rulename,'enabled',ev_enabled::text,'definition',pg_get_ruledef(oid,true)) ORDER BY rulename) FROM pg_rewrite WHERE ev_class=o.oid),'[]'::jsonb),
 'policies',COALESCE((SELECT jsonb_agg(jsonb_build_object('name',polname,'command',polcmd::text,'permissive',polpermissive,'roles',polroles::text,'using',pg_get_expr(polqual,polrelid),'check',pg_get_expr(polwithcheck,polrelid)) ORDER BY polname) FROM pg_policy WHERE polrelid=o.oid),'[]'::jsonb),
 'columns',(SELECT jsonb_agg(jsonb_build_object('name',a.attname,'type',format_type(a.atttypid,a.atttypmod),'notNull',a.attnotnull,'identity',a.attidentity::text,'generated',a.attgenerated::text,'default',pg_get_expr(d.adbin,d.adrelid),'privileges',(SELECT jsonb_object_agg(p,has_column_privilege(r.oid,o.oid,a.attnum,p)) FROM unnest(ARRAY['SELECT','INSERT','UPDATE','REFERENCES']) p),'grantOptions',(SELECT jsonb_object_agg(p,has_column_privilege(r.oid,o.oid,a.attnum,p||' WITH GRANT OPTION')) FROM unnest(ARRAY['SELECT','INSERT','UPDATE','REFERENCES']) p),'publicPrivileges',(SELECT jsonb_object_agg(p,EXISTS(SELECT 1 FROM aclexplode(a.attacl) x WHERE x.grantee=0 AND x.privilege_type=p)) FROM unnest(ARRAY['SELECT','INSERT','UPDATE','REFERENCES']) p)) ORDER BY a.attnum) FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=o.oid AND a.attnum>0 AND NOT a.attisdropped),
 'constraints',COALESCE((SELECT jsonb_agg(jsonb_build_object('name',conname,'definition',pg_get_constraintdef(oid,true),'validated',convalidated) ORDER BY conname) FROM pg_constraint WHERE conrelid=o.oid),'[]'::jsonb),
 'indexes',COALESCE((SELECT jsonb_agg(jsonb_build_object('definition',pg_get_indexdef(indexrelid),'valid',indisvalid,'ready',indisready) ORDER BY pg_get_indexdef(indexrelid)) FROM pg_index WHERE indrelid=o.oid),'[]'::jsonb),
 'privileges',(SELECT jsonb_object_agg(p,has_table_privilege(r.oid,o.oid,p)) FROM unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER','MAINTAIN']) p),
 'grantOptions',(SELECT jsonb_object_agg(p,has_table_privilege(r.oid,o.oid,p||' WITH GRANT OPTION')) FROM unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER','MAINTAIN']) p),
 'schemaGrantOptions',jsonb_build_object('USAGE',has_schema_privilege(r.oid,o.relnamespace,'USAGE WITH GRANT OPTION'),'CREATE',has_schema_privilege(r.oid,o.relnamespace,'CREATE WITH GRANT OPTION')),
 'schemaPrivileges',jsonb_build_object('USAGE',has_schema_privilege(r.oid,o.relnamespace,'USAGE'),'CREATE',has_schema_privilege(r.oid,o.relnamespace,'CREATE')),
 'publicPrivileges',(SELECT jsonb_object_agg(p,EXISTS(SELECT 1 FROM aclexplode(COALESCE(o.relacl,acldefault('r',o.relowner))) a WHERE a.grantee=0 AND a.privilege_type=p)) FROM unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER','MAINTAIN']) p),
 'schemaPublicPrivileges',jsonb_build_object('CREATE',EXISTS(SELECT 1 FROM aclexplode(COALESCE(o.nspacl,acldefault('n',o.nspowner))) a WHERE a.grantee=0 AND a.privilege_type='CREATE'))
 ) AS detail FROM objects o CROSS JOIN runtime r
)
SELECT jsonb_build_object('serverMajor',current_setting('server_version_num')::integer/10000,'databaseName',current_database(),'runtimeRole',r.rolname,
 'role',jsonb_build_object('superuser',r.rolsuper,'createRole',r.rolcreaterole,'createDatabase',r.rolcreatedb,'replication',r.rolreplication,'bypassRls',r.rolbypassrls,'login',r.rolcanlogin,'adminOptions',COALESCE((SELECT jsonb_agg(p.rolname ORDER BY p.rolname) FROM pg_auth_members m JOIN pg_roles p ON p.oid=m.roleid WHERE m.member=r.oid AND m.admin_option),'[]'::jsonb),'memberships',COALESCE((SELECT jsonb_agg(p.rolname ORDER BY p.rolname) FROM pg_roles p WHERE p.oid<>r.oid AND pg_has_role(r.oid,p.oid,'MEMBER')),'[]'::jsonb)),
 'databasePrivileges',(SELECT jsonb_object_agg(p,has_database_privilege(r.oid,current_database(),p)) FROM unnest(ARRAY['CONNECT','CREATE','TEMPORARY']) p),
 'databaseGrantOptions',(SELECT jsonb_object_agg(p,has_database_privilege(r.oid,current_database(),p||' WITH GRANT OPTION')) FROM unnest(ARRAY['CONNECT','CREATE','TEMPORARY']) p),
 'schemaCreateAccess',COALESCE((SELECT jsonb_agg(nspname ORDER BY nspname) FROM pg_namespace WHERE nspname NOT LIKE 'pg_%' AND nspname<>'information_schema' AND has_schema_privilege(r.oid,oid,'CREATE')),'[]'::jsonb),
 'unrelatedAccess',COALESCE((SELECT jsonb_agg(n.nspname||'.'||c.relname ORDER BY n.nspname,c.relname) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND c.relkind IN ('r','p','v','m','f','S') AND NOT EXISTS (SELECT 1 FROM selected WHERE name=n.nspname||'.'||c.relname) AND CASE WHEN c.relkind='S' THEN has_sequence_privilege(r.oid,c.oid,'USAGE,SELECT,UPDATE') ELSE has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN') OR has_any_column_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,REFERENCES') END),'[]'::jsonb),
 'securityDefinerAccess',COALESCE((SELECT jsonb_agg(p.oid::regprocedure::text ORDER BY p.oid::regprocedure::text) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND p.prosecdef AND has_function_privilege(r.oid,p.oid,'EXECUTE')),'[]'::jsonb),
 'relations',COALESCE((SELECT jsonb_agg(detail ORDER BY name) FROM details),'[]'::jsonb)) FROM runtime r;
ROLLBACK;
