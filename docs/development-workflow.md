# Development workflow

This repository keeps local feedback targeted while CI remains the exact-head authority.

## One-time setup

```sh
bash scripts/install-hooks.sh
npm ci --prefix web
```

The hook is opt-in because this repository has Go at the root rather than a root package install step. It always checks repository hygiene and runs gitleaks when installed. It then reuses the CI path classifier and runs only the affected Go or web checks. Documentation-only pushes do not take the heavy-work lock or run component tests.

## Fast loop

Run the smallest relevant command while editing:

```sh
go test ./internal/core/...
npm run --prefix web test -- --run <test-file>
```

Before a coherent push, the hook selects the same component boundaries as CI. Integration tests remain in CI because they use disposable Postgres there; Meal Planner has no browser E2E suite, so this workflow does not invent one.

## Shared heavy-work lock

The affected local checks wrap Go tests/builds and web tests/builds with `scripts/with-heavy-lock.sh`. Packed Smart uses the same `$TMPDIR/caroline-development-heavy.lock` in the user's private macOS temporary directory, so the Mac runs only one resource-heavy job at a time while editing, Git operations, formatting, linting, typechecking, and documentation work remain free.

The lock uses macOS `lockf`. The kernel releases it automatically if a command exits or is killed; the file's existence alone does not mean it is locked. Nested wrappers inherit a token and do not deadlock. Verify the behavior with:

```sh
bash scripts/with-heavy-lock.test.sh
```

## Merge evidence

The GitHub rulesets were audited on 2026-11-06. They require one approval and resolved review conversations for `develop` and `main`, but they do not require `CI / required`. Until that setting is deliberately changed, the team gate is:

1. Push a coherent head and wait for `CI / required` on that exact SHA.
2. Obtain approval for the same SHA and resolve every review conversation.
3. If the head changes, wait for CI and approval again.
4. Merge only after both are current and green.

For web UI changes, attach rendered phone and desktop evidence for each affected view/state. Add widths, themes, loading/empty/error states, or large-text variants only when the change can affect them. Keep repeatable screenshot scripts in the repository, attach images to the PR, identify the head SHA, and use synthetic data.
