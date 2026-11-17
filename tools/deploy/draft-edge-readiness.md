# Draft edge-readiness evidence contract

This is a normalized, source-only contract for the clean provider adapters and
their eventual readiness handoff. It does not contact Vercel, publish WAF rules,
set function duration, authorize a deployment, or open either draft feature
gate.

The adapter must independently authenticate the exact Vercel project, reviewed
source SHA, Preview target and stable alias. It must return every firewall
rule intersecting either unauthenticated path, every bypass, and the project
plan, not merely the expected rule.
`verifyDraftEdgeObservation` accepts only:

- the Hobby plan and its one shared Preview-environment, POST-only rule using
  the `Is any of` operator to match exactly the create and poll paths across all
  project Preview hostnames, with no Hostname restriction;
- 150 requests per provider-derived source IP per ten-minute fixed window;
- fixed-window, provider-derived source-IP keys and 429 before invocation;
- no bypass, other intersecting rule or Production effect;
- 15-second maximum duration for only the create and poll Go functions; and
- evidence observed less than 15 minutes ago and bound to the approved source.

The normalized object is not self-authenticating. A target-supplied JSON file or
digest is not evidence. `draft-edge-firewall-adapter.mjs` now retrieves the
active rule and both conditional and system bypass classes through a read-only,
owner-approved authenticated channel; it emits only a bounded normalized
receipt. It has synthetic schema coverage but has not been run against the
project. The clean readiness handoff must attribute its fresh receipt and the
separate plan/project receipt to the reviewed source and deployed function
state. Reinspect after any firewall, function, project, alias or deployment
change. A rule limited to the stable alias is rejected
because each generated Preview deployment URL would otherwise be an
edge-throttle bypass. The Go handlers also reject alternate hosts and non-exact
paths before repository access; this is defense in depth, not a substitute for
provider coverage.

The rule bounds one source address per provider region, not distributed abuse.
One five-second poller consumes at most 120 of 150 requests during the
ten-minute pairing lifetime, plus one creation request. The application also
fails closed at 20 active, 60 hourly or 500 retained pairing rows per Preview
environment and issuer, accepting that cap exhaustion denies availability.
Shared-address clients can throttle each other. The 15-second function limit is
an execution ceiling, not proof of a request-upload deadline. Preview acceptance
must exercise 429 behavior and the intended client; Production remains out of
scope.

IP rotation and cross-region traffic can multiply the allowed rate. At 150 per
ten minutes, one stable IP can be allowed 21,600 matching requests per day in
each region. The application creation caps bound durable row growth but not
random poll compute, so this is an availability and free-allowance tradeoff,
not a global spend cap.

Vercel recommends first observing a custom rule with a non-blocking Log action.
That phase may use the exact conditions only while every draft gate remains
closed. It is not accepted by `verifyDraftEdgeObservation`; the final 429 action
must be independently observed after conversion. A 429 ends the current
polling attempt, clears its in-memory polling secrets and permits no immediate
retry. Do not depend on a `Retry-After`
header, which the current provider documentation does not promise.

Verification:

`node --test tools/deploy/draft-*.test.mjs`
