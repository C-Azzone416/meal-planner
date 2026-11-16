# Native macOS Keychain adapter

Status: source-complete, runtime-disabled, real Keychain behavior unverified.

The local-client package contains an unwired credential-store adapter backed by
Apple Security.framework generic-password items. It exists to make the final
hookup review concrete; the shipped command does not construct it, no MCP
registration is changed, and both the source and environment workflow gates
remain closed.

## Security contract

- The adapter calls `SecItemCopyMatching`, `SecItemUpdate`, `SecItemAdd`, and
  `SecItemDelete` directly. It never invokes `/usr/bin/security` or another
  child process, so a credential is never placed in argv or an environment
  variable by this implementation.
- The Keychain service is the constant `mealplanner.local-client.v1`. The
  account label is a non-secret `preview/` digest over a fixed-width,
  length-framed tuple of the domain label, exact environment, exact API origin,
  exact web origin, and account binding ID. The store accepts only the compiled
  Preview origins and rejects altered identities before a native call.
- Values use `kSecAttrAccessibleWhenUnlockedThisDeviceOnly`. Add/update races
  resolve to one item: update is attempted first; an add that loses a race to a
  duplicate retries as an update.
- Native result buffers, cgo input buffers, and Go temporary copies are cleared
  after use. A `Credential` remains opaque and all formatting and serialization
  stays redacted.
- Missing items map to the fixed `ErrCredentialMissing`. Other native errors
  map to the fixed `ErrUnavailable`; native or secret-bearing error text is not
  returned. Delete is idempotent.
- There is no file, command-line, environment-variable, log, or exported-secret
  fallback.

## Build behavior

The native backend is compiled only with `darwin && cgo`. A Darwin build with
`CGO_ENABLED=0`, and every non-Darwin build, returns `ErrUnavailable` from the
constructor. Construction itself performs no Keychain operation.

The native seam is deliberately injectable. Automated tests instantiate the
store only with a synthetic backend and cover create, read, update, delete,
duplicate/update semantics, invalid and hostile identities, backend errors,
malformed stored values, cancellation, account isolation, error redaction, and
temporary-buffer clearing. The tests make zero Security.framework calls.

Constructing the native adapter is side-effect-free. If a future caller
deliberately opts in and calls `Get`, `Put`, or `Delete`, that method does
perform the corresponding Security.framework operation. No shipped command
currently constructs the adapter.

## Runtime blockers before hookup

None of the following is proven by repository tests, and enabling the adapter
must remain blocked until the owner reviews evidence from the exact packaged
binary:

1. Code-signing identity, designated requirement stability, notarization, and
   behavior after an upgrade or signing-identity change.
2. Keychain access-control/ACL behavior when the same user invokes a different
   binary, including `/usr/bin/security`, from a non-interactive shell. In
   particular, determine whether access prompts, is denied, or silently returns
   the value.
3. A real-machine canary test that samples argv and environment and uses process
   execution tracing while Put/Get/Delete run, confirming the canary never
   appears outside the intended process memory/Keychain value.
4. Locked-Keychain, user-cancel, entitlement, migration, duplicate legacy item,
   and corrupted-item behavior for the signed package.

These checks require a separately approved runtime exercise. This change does
not touch the real Keychain, issue a credential, install a binary, register MCP,
or enable ingestion.
