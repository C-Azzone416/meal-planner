# Repository Layout

Component map for the UI and authentication preview and the separate #23 local draft restore candidate. Run commands from the repository root. The candidate is pending #25 host acceptance.

| Path | Component | Purpose | Check commands |
| --- | --- | --- | --- |
| `go.mod`, `go.sum` | all Go | Module `github.com/C-Azzone416/meal-planner`; the `go` directive is the Go version source for CI | `test -z "$(gofmt -l .)"`, `go vet ./...` |
| `internal/core/version/` | local core | `Version` variable (default `dev`), set via `-ldflags "-X github.com/C-Azzone416/meal-planner/internal/core/version.Version=<v>"` | `go test ./internal/core/...` |
| `hosted/migrations/` | database | Reviewed, explicitly applied manual-recipe schema; never startup migration | `go test -tags=integration ./hosted/...` against disposable localhost Postgres |
| `hosted/` | hosted API | Manual recipe ownership/storage plus plain `net/http` handlers for health, public auth configuration, signup policy, and reusable bearer-token protection | `go test ./api/... ./hosted/...` |
| `api/` | hosted API | Vercel Go functions for `/api/health`, `/api/auth-config`, and the signed blocking signup webhook | `go test ./api/... ./hosted/...` |
| `cmd/mealplanner/` | local tools | Candidate CLI with version, draft validation, listing and inspection | `go test ./cmd/...`, `go build ./cmd/...` |
| `cmd/mealplanner-mcp/` | local tools | Candidate stdio server with draft tools, status and a wire gate | `go test ./cmd/...`, `go build ./cmd/...` |
| `internal/localstore/` and `internal/mcpadapter/` | local tools | Candidate file persistence and MCP tool schemas and handlers | `go test ./internal/localstore ./internal/mcpadapter` |
| `contracts/` | contract | `openapi.yaml` (OpenAPI 3.1) and `contract_test.go` checking the hosted handler against it | `go test ./contracts/...` |
| `web/` | web client | Vite + React + TypeScript; protected responsive preview routes and email-and-password sign-in; Node version in `web/.nvmrc` | `npm ci --prefix web`, then `npm run --prefix web lint` / `typecheck` / `test` / `build` (output `web/dist`) |
| `vercel.json` | hosted deploy | Builds `web/` to static `web/dist`; `api/*.go` are detected as Go functions | none locally |

Shared draft core retained for the hosted release:

| Path | Component | Purpose |
| --- | --- | --- |
| `internal/core/draft/` (+ `memstore/`, `storetest/`) | shared core | Draft model, strict JSON-string validation reused by hosted recipes, structural validation, service, persistence interface, in-memory store and conformance suite |

The local Go job includes the candidate commands, store and adapter. [Local draft contract](local-draft-contract.md) · [Local draft workflow](local-workflow.md)

Additional checks:

- Integration: `go test -tags=integration ./...` (manual-recipe tests require a disposable loopback `TEST_DATABASE_URL`; never a live Neon URL).
- Cross-compile sanity: `GOOS=<os> GOARCH=<arch> CGO_ENABLED=0 go build ./cmd/...` for windows/amd64, linux/arm64, darwin/arm64.

Note: `web/go.mod` is intentionally empty of code; it makes `web/` a separate Go module so `./...` and `go vet` skip Go files shipped inside `web/node_modules`.
