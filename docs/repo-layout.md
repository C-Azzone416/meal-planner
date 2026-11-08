# Repository Layout

Component map for the UI/authentication preview and local tooling. All commands run from the repository root. Manual recipe API/storage and reviewed migration files exist; hosted planning is not implemented; the local recipe-draft slice is described below.

| Path | Component | Purpose | Check commands |
| --- | --- | --- | --- |
| `go.mod`, `go.sum` | all Go | Module `github.com/C-Azzone416/meal-planner`; the `go` directive is the Go version source for CI | `test -z "$(gofmt -l .)"`, `go vet ./...` |
| `internal/core/version/` | local core | `Version` variable (default `dev`), set via `-ldflags "-X github.com/C-Azzone416/meal-planner/internal/core/version.Version=<v>"` | `go test ./internal/core/...` |
| `hosted/migrations/` | database | Reviewed, explicitly applied manual-recipe schema; never startup migration | `go test -tags=integration ./hosted/...` against disposable localhost Postgres |
| `hosted/` | hosted API | Manual recipe ownership/storage plus plain `net/http` handlers for health, public auth configuration, signup policy, and reusable bearer-token protection | `go test ./api/... ./hosted/...` |
| `api/` | hosted API | Vercel Go functions for `/api/health`, `/api/auth-config`, and the signed blocking signup webhook | `go test ./api/... ./hosted/...` |
| `cmd/mealplanner/` | local tools | CLI: `version`, `--version`, `draft validate/list/show` (read-only; see [local-draft-contract.md](local-draft-contract.md)); no args prints usage and exits 2 | `go test ./cmd/...`, `go build ./cmd/...` |
| `cmd/mealplanner-mcp/` | local tools | stdio MCP server for local recipe drafts (`--data-dir`, `--version`); stdout is protocol-only. `workflow_test.go` is the offline MCP-to-CLI round trip described in [local-workflow.md](local-workflow.md) | `go test ./cmd/...`, `go build ./cmd/...` |
| `contracts/` | contract | `openapi.yaml` (OpenAPI 3.1) and `contract_test.go` checking the hosted handler against it | `go test ./contracts/...` |
| `web/` | web client | Vite + React + TypeScript; protected responsive preview routes and passwordless sign-in; Node version in `web/.nvmrc` | `npm ci --prefix web`, then `npm run --prefix web lint` / `typecheck` / `test` / `build` (output `web/dist`) |
| `vercel.json` | hosted deploy | Builds `web/` to static `web/dist`; `api/*.go` are detected as Go functions | none locally |

Local recipe-draft slice (contract in [local-draft-contract.md](local-draft-contract.md); host setup and verification record in [local-workflow.md](local-workflow.md)):

| Path | Component | Purpose |
| --- | --- | --- |
| `internal/core/draft/` (+ `memstore/`, `storetest/`) | local core | Draft model, structural validation, service, persistence interface, in-memory store, store conformance suite |
| `internal/localstore/` | local tools | Versioned JSON file store, data-directory resolution, cross-process lock |
| `internal/mcpadapter/` | local tools | MCP tool definitions and handlers over the core |
| `cmd/mealplanner-mcp/`, `cmd/mealplanner/` | local tools | MCP server and `draft validate/list/show` commands; `--version` behavior is preserved |

CI path classification for these directories is described in [local-draft-contract.md section 12.2](local-draft-contract.md#122-ci-classification-note).

Additional checks:

- Integration: `go test -tags=integration ./...` (manual-recipe tests require a disposable loopback `TEST_DATABASE_URL`; never a live Neon URL).
- Cross-compile sanity: `GOOS=<os> GOARCH=<arch> CGO_ENABLED=0 go build ./cmd/...` for windows/amd64, linux/arm64, darwin/arm64.

Note: `web/go.mod` is intentionally empty of code; it makes `web/` a separate Go module so `./...` and `go vet` skip Go files shipped inside `web/node_modules`.
