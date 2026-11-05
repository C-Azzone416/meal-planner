def deployment_evidence($prod_names; $inventory_complete; $expect; $sha; $pid):
  {id, url, projectId, target, readyState, aliasAssigned, alias: (.alias // []), meta: (.meta // {}),
   production_domains_known: $prod_names,
   production_domains_inventory_complete: $inventory_complete,
   checks: {
     production_domains_known: ($inventory_complete and (($prod_names | length) > 0)),
     project_matches: (.projectId == $pid),
     sha_matches: ((.meta.sourceSha // "") == $sha),
     target_matches: (if $expect == "production" then .target == "production" else (.target == null or .target == "preview") end),
     ready: (.readyState == "READY"),
     no_production_domain_alias: (((.alias // []) | map(select(. as $a | $prod_names | index($a))) | length) == 0),
     # `aliasAssigned` is diagnostic only. Vercel reports it as true for some
     # `--skip-domain` deployments whose authoritative alias array is empty.
     no_aliases_when_production: (if $expect == "production" then ((.alias // []) | length) == 0 else true end)
   }};

deployment_evidence($prod_names; $inventory_complete; $expect; $sha; $pid)
