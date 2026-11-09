# Repository Layout

Component map for the UI/authentication preview and retained local-tool scaffolds. All commands run from the repository root. Manual recipe API/storage and reviewed migration files exist; hosted planning and the executable local recipe-draft surface are not included.

| Path | Component | Purpose | Check commands |
| --- | --- | --- | --- |
| `go.mod`, `go.sum` | all Go | Module `github.com/C-Azzone416/meal-planner`; the `go` directive is the Go version source for CI | `test -z "$(gofmt -l .)"`, `go vet ./...` |
| `internal/core/version/` | local core | `Version` variable (default `dev`), set via `-ldflags "-X github.com/C-Azzone416/meal-planner/internal/core/version.Version=<v>"` | `go test ./internal/core/...` |
| `hosted/migrations/` | database | Reviewed, explicitly applied manual-recipe schema; never startup migration | `go test -tags=integration ./hosted/...` against disposable localhost Postgres |
| `hosted/` | hosted API | Manual recipe ownership/storage plus plain `net/http` handlers for health, public auth configuration, signup policy, and reusable bearer-token protection | `go test ./api/... ./hosted/...` |
| `api/` | hosted API | Vercel Go functions for `/api/health`, `/api/auth-config`, and the signed blocking signup webhook | `go test ./api/... ./hosted/...` |
| `cmd/mealplanner/` | local tools | CLI scaffold: `version`, `--version`; no args prints usage and exits 2; no draft commands | `go test ./cmd/...`, `go build ./cmd/...` |
| `cmd/mealplanner-mcp/` | local tools | MCP scaffold: `--version`, otherwise drains stdin and exits 0 without stdout; no protocol tools or file store | `go test ./cmd/...`, `go build ./cmd/...` |
| `contracts/` | contract | `openapi.yaml` (OpenAPI 3.1) and `contract_test.go` checking the hosted handler against it | `go test ./contracts/...` |
| `web/` | web client | Vite + React + TypeScript; protected responsive preview routes and email-and-password sign-in; Node version in `web/.nvmrc` | `npm ci --prefix web`, then `npm run --prefix web lint` / `typecheck` / `test` / `build` (output `web/dist`) |
| `vercel.json` | hosted deploy | Builds `web/` to static `web/dist`; `api/*.go` are detected as Go functions | none locally |

Shared draft core retained for the hosted release:

| Path | Component | Purpose |
| --- | --- | --- |
| `internal/core/draft/` (+ `memstore/`, `storetest/`) | shared core | Draft model, strict JSON-string validation reused by hosted recipes, structural validation, service, persistence interface, in-memory store and conformance suite |

All core tests and synthetic fixtures remain. The [local draft contract](local-draft-contract.md) specifies the core and the deferred epic #19 design. `internal/localstore/`, `internal/mcpadapter/`, local draft CLI commands and MCP draft tools are excluded from this release; no local setup is required. CI continues to test all remaining internal and command packages with `go test ./internal/... ./cmd/...`. Reserved local-tool classifier rules remain for future separately reviewed development.

Additional checks:

- Integration: `go test -tags=integration ./...` (manual-recipe tests require a disposable loopback `TEST_DATABASE_URL`; never a live Neon URL).
- Cross-compile sanity: `GOOS=<os> GOARCH=<arch> CGO_ENABLED=0 go build ./cmd/...` for windows/amd64, linux/arm64, darwin/arm64.

Note: `web/go.mod` is intentionally empty of code; it makes `web/` a separate Go module so `./...` and `go vet` skip Go files shipped inside `web/node_modules`.
