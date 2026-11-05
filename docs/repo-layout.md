# Repository Layout

Component map for the verification skeleton. All commands run from the repository root. The skeleton contains no product functionality yet; the local recipe-draft slice described below is planned, not present.

| Path | Component | Purpose | Check commands |
| --- | --- | --- | --- |
| `go.mod`, `go.sum` | all Go | Module `github.com/C-Azzone416/meal-planner`; the `go` directive is the Go version source for CI | `test -z "$(gofmt -l .)"`, `go vet ./...` |
| `internal/core/version/` | local core | `Version` variable (default `dev`), set via `-ldflags "-X github.com/C-Azzone416/meal-planner/internal/core/version.Version=<v>"` | `go test ./internal/core/...` |
| `hosted/` | hosted API | Plain `net/http` handlers (`GET /api/health`) | `go test ./api/... ./hosted/...` |
| `api/health.go` | hosted API | Vercel Go function wrapping `hosted`, served at `/api/health` | `go test ./api/... ./hosted/...` |
| `cmd/mealplanner/` | local tools | CLI: `version`, `--version`, `draft validate/list/show` (read-only; see [local-draft-contract.md](local-draft-contract.md)); no args prints usage and exits 2 | `go test ./cmd/...`, `go build ./cmd/...` |
| `cmd/mealplanner-mcp/` | local tools | MCP stub: `--version`, otherwise drains stdin and exits 0; stdout stays protocol-clean | `go test ./cmd/...`, `go build ./cmd/...` |
| `contracts/` | contract | `openapi.yaml` (OpenAPI 3.1) and `contract_test.go` checking the hosted handler against it | `go test ./contracts/...` |
| `web/` | web client | Vite + React + TypeScript; Node version in `web/.nvmrc` | `npm ci --prefix web`, then `npm run --prefix web lint` / `typecheck` / `test` / `build` (output `web/dist`) |
| `vercel.json` | hosted deploy | Builds `web/` to static `web/dist`; `api/*.go` are detected as Go functions | none locally |

Planned for the local recipe-draft slice (not yet created; contract in [local-draft-contract.md](local-draft-contract.md)):

| Path | Component | Purpose |
| --- | --- | --- |
| `internal/core/draft/` (+ `memstore/`, `storetest/`) | local core | Draft model, structural validation, service, persistence interface, in-memory store, store conformance suite |
| `internal/localstore/` | local tools | Versioned JSON file store, data-directory resolution, cross-process lock |
| `internal/mcpadapter/` | local tools | MCP tool definitions and handlers over the core |
| `cmd/mealplanner-mcp/`, `cmd/mealplanner/` | local tools | Gain the MCP server and `draft validate/list/show` commands; `--version` behavior is preserved |

CI path classification for these directories is described in [local-draft-contract.md section 12.2](local-draft-contract.md#122-ci-classification-note).

Additional checks:

- Integration: `go test -tags=integration ./...` (no integration tests yet).
- Cross-compile sanity: `GOOS=<os> GOARCH=<arch> CGO_ENABLED=0 go build ./cmd/...` for windows/amd64, linux/arm64, darwin/arm64.

Note: `web/go.mod` is intentionally empty of code; it makes `web/` a separate Go module so `./...` and `go vet` skip Go files shipped inside `web/node_modules`.
