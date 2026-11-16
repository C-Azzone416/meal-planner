# Outbound Preview sender implementation

Status: source-complete, runtime-disabled. This code does not register an MCP
server, install a binary, write the macOS Keychain, open a listener, or enable a
hosted endpoint. Production is not a supported profile.

## Fixed interfaces

The local client accepts only the compiled Preview origin
`https://athome-meals-dev.vercel.app` for both API and browser navigation. It
never derives a destination from a response, `Host`, an environment variable or
a command argument, and its HTTP client does not follow redirects.

Pairing is outbound HTTPS polling:

1. `POST /api/draft-pairings` sends `client_label`, a generated PKCE
   `code_challenge`, and `code_challenge_method: "S256"`.
2. The `local-client-v1` response has an eight-character human code, a
   query-free `/connect` verification URI, an in-memory poll token, and a poll
   interval of at least five seconds.
3. `POST /api/draft-pairings/poll` sends the pairing ID, poll token and verifier.
   The only accepted grant response is Preview-scoped `draft:propose`, with the
   same pinned origins and a binding ID. The opaque grant moves directly into a
   `CredentialStore`; it is not a return value.

Sending is an explicit, revision-bound MCP operation:

1. `draft_send {id, expected_revision}` re-reads the local draft and refuses a
   stale revision.
2. The first call creates an immutable intent with `POST
   /api/draft-send-intents`. Its durable operation key is reused after an
   ambiguous failure.
3. The result is only a safe browser approval URL. The grant cannot approve its
   own intent. A signed-in owner must review and approve the exact snapshot.
4. Repeating the same tool call performs one outbound `POST
   /api/draft-send-intents/poll`. There is no timer, daemon or background sync.
5. An approved receipt is committed to the local ledger before success is
   reported. Subsequent retries return that receipt without another network
   request.

The ledger namespace includes environment, exact API origin, account binding,
local draft ID, revision and canonical content hash. It stores no recipe content
or grant. Account changes and content revisions create independent operations.

## Closed gates and tomorrow's hookup

`mcpadapter.WithDraftSender` is the only source gate. The shipped command does
not call it, so it continues to advertise exactly the existing six local tools.
The only repository credential implementations are a fail-closed default and an
in-memory test fake. There is intentionally no plaintext fallback.

Before enabling the option, an owner must separately approve and verify:

- a native macOS Keychain adapter that never shells out or places a secret in
  argv/env, plus signed-binary ACL behavior under a shell-capable agent;
- the exact Preview pairing and send-intent endpoints and their feature flags;
- the browser intent approval page and membership/grant-revocation behavior;
- packaging identity and code-signing continuity for Keychain access.

Password reset is not claimed to revoke a send grant. Membership removal and
re-add behavior is a server contract, not something the local client guesses.
