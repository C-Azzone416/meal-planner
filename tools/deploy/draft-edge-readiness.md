# Draft edge-readiness evidence contract

This is a normalized, source-only contract for a future clean provider adapter.
It does not contact Vercel, publish WAF rules, set function duration, authorize a
deployment, or open either draft feature gate.

The adapter must independently authenticate the exact Vercel project, reviewed
source SHA, Preview target and stable hostname. It must return every firewall
rule intersecting either unauthenticated path, not merely the expected rules.
`verifyDraftEdgeObservation` accepts only:

- exact-host, POST-only create rules at 3/source-IP/minute and
  10/source-IP/hour;
- an exact-host, POST-only poll rule at 150/source-IP/15 minutes;
- fixed-window, provider-derived source-IP keys and 429 before invocation;
- no other intersecting rule or Production effect;
- 15-second maximum duration for only the create and poll Go functions; and
- evidence observed less than 15 minutes ago and bound to the approved source.

The normalized object is not self-authenticating. A target-supplied JSON file or
digest is not evidence. A future adapter must retrieve provider state through an
owner-approved authenticated channel and attribute the resulting digest through
the clean readiness handoff. Reinspect after any firewall, function, project,
hostname or deployment change.

These rules bound one source address, not distributed abuse. The poll budget is
for one five-second poller during one ten-minute pairing window; shared-address
clients can throttle each other. The 15-second function limit is an execution
ceiling, not proof of a request-upload deadline. Preview acceptance must exercise
429 behavior and the intended client; Production remains out of scope.

Verification:

`node --test tools/deploy/draft-edge-readiness.test.mjs`
