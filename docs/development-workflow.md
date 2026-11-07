# Development workflow

This repository keeps local feedback targeted while CI remains the exact-head authority.

## One-time setup

```sh
bash scripts/install-hooks.sh
npm ci --prefix web
```

The hook is opt-in because this repository has Go at the root rather than a root package install step. For each ref being pushed it checks the commits that push adds, not the worktree: the paths and blob sizes in the pushed tip and in every commit of the pushed range (no root or nested `node_modules`, `dist`, `coverage`, `.env*`, database files, or blobs over 1 MiB), and, when gitleaks (8.19 or newer) is installed, a redacted secret scan of only the pushed range, merge commits included. A failure to list or read objects fails the push. It then reuses the CI path classifier and runs only the affected Go or web checks. Documentation-only pushes do not take the heavy-work lock or run component tests.

## Fast loop

Run the smallest relevant command while editing:

```sh
go test ./internal/core/...
npm run --prefix web test -- --run <test-file>
```

Before a coherent push, the hook selects the same component boundaries as CI. Integration tests remain in CI because they use disposable Postgres there; Meal Planner has no browser E2E suite, so this workflow does not invent one.

## Shared heavy-work lock

The affected local checks wrap Go tests/builds and web tests/builds with `scripts/with-heavy-lock.sh`. Packed Smart uses the same `$TMPDIR/caroline-development-heavy.lock` in the user's private macOS temporary directory, so the Mac runs only one resource-heavy job at a time while editing, Git operations, formatting, linting, typechecking, and documentation work remain free.

The wrapper takes a `flock` on a descriptor it then passes to the command, and replaces itself with the command (`exec`). The lock is therefore held for exactly as long as the command, or any process the command started that still has that descriptor open, is alive:

- Killing the command (SIGTERM, SIGKILL, a crash) releases the lock immediately. There is no separate outer process whose death could release the lock while the work keeps running.
- A background process the command leaves behind keeps the lock until it exits. That is deliberate: the Mac is still busy.
- Nothing runs after the command, so there is no "released" message or cleanup step. The `.owner` file next to the lock is informational only (repo name, pid, start time), is written atomically, and may be stale after a crash. Waiters read it best-effort, only name an owner whose pid is alive, and never fail because of it.
- A waiter retries in one-second slices. If it is killed while waiting it leaves nothing running that could take or keep the lock, but waiters are not served in strict arrival order.
- Nested wrappers see `CAROLINE_HEAVY_LOCK_HELD` and run the command directly. CI and Vercel skip the lock.
- The lock needs macOS `/usr/bin/lockf`; its presence does not by itself mean the file is locked.

`scripts/with-heavy-lock.test.sh` checks serialization, nesting, exit status, and that after SIGKILL or SIGTERM a waiting job never starts while any process of the first job is alive:

```sh
bash scripts/with-heavy-lock.test.sh
```

The other local script tests are `check-repo-hygiene.test.sh`, `pre-push.test.sh`, `check-changed.test.sh` and `secret-scan.test.sh` (the last two skip when `gofmt` or `gitleaks` is missing). CI runs the hygiene, pre-push and secret-scan tests; the lock test needs macOS.

## CI scans

CI runs the secret scan and repository hygiene as their own jobs, `secrets` and `hygiene`, rather than as steps of `affected`, so a scan failure never hides the component results. `CI / required` fails unless both succeed. The secret scan uses a pinned, checksum-verified gitleaks CLI over the whole event range (`base..head` for a pull request, `before..sha` for a push, full history if there is no usable base), with merge commits expanded (`-m`) so changes that only appear in a merge are scanned, and with redacted output. It writes nothing back to the pull request. Hygiene checks the checked-out commit's tree and every file the event adds.

## Merge evidence

The GitHub rulesets were audited on 2026-11-06. They require one approval and resolved review conversations for `develop` and `main`, but they do not require `CI / required`. Until that setting is deliberately changed, the team gate is:

1. Push a coherent head and wait for `CI / required` on that exact SHA.
2. Obtain approval for the same SHA and resolve every review conversation.
3. If the head changes, wait for CI and approval again.
4. Merge only after both are current and green.

For web UI changes, attach rendered phone and desktop evidence for each affected view/state. Add widths, themes, loading/empty/error states, or large-text variants only when the change can affect them. Keep repeatable screenshot scripts in the repository, attach images to the PR, identify the head SHA, and use synthetic data.
