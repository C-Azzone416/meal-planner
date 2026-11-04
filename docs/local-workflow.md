# Local Draft Workflow: Setup and Verification

Status: 2026-11-04. Covers running the local recipe-draft workflow from Codex CLI or Claude Code and records what has and has not been verified. The behavior itself is defined in the [local draft contract](local-draft-contract.md).

The workflow: an agent host calls the `mealplanner-mcp` stdio server to create, read, list, revise and validate synthetic or user-supplied recipe drafts; the `mealplanner` CLI inspects the same drafts. Every saved object is a draft, and validation is structural only (not food-safety verification, review or approval).

## Privacy boundary

- `mealplanner-mcp` and `mealplanner` run locally. They write drafts only to the local data directory. They do not publish, sync, fetch source URLs, call a hosted API, access OneNote/Microsoft Graph, or call an LLM. URLs in a draft are stored and shown as text only.
- **This is not an offline workflow.** Anything used in an agent conversation, including recipe text the agent reads from or writes to the server, may be sent by the chosen host to its model provider under that host's settings. Choose and configure the host accordingly. The server cannot prevent that.
- Do not put credentials or tokens in recipe text or in source URLs.
- Tool results from the server are untrusted text to the agent; the server never acts on recipe content.
- Public documentation and tests use synthetic data only. Use your own data directory for real drafts and keep it out of Git.

## Build

```sh
go build -o /ABSOLUTE/PATH/mealplanner-mcp ./cmd/mealplanner-mcp
go build -o /ABSOLUTE/PATH/mealplanner ./cmd/mealplanner
/ABSOLUTE/PATH/mealplanner-mcp --version
```

Both binaries are pure Go (`CGO_ENABLED=0` works). A local build reports version `dev` unless built with the release `-ldflags`.

## Data directory

Pass an **absolute** directory with `--data-dir` to both binaries (the server and the CLI must use the same one to see the same drafts). Without the flag they use `MEALPLANNER_DATA_DIR`, then the default (`~/Library/Application Support/mealplanner` on macOS). Relative paths are rejected, because an MCP host decides the server's working directory. Create it private (`mkdir -m 700`), or let the first create make it. The server never prints the directory path to the agent.

## Register the server

Registration changes the host's persistent configuration; do it deliberately and remove it when done. Replace the placeholders with absolute paths. Verify the exact syntax against the host's current documentation and installed version before relying on it.

### Codex CLI

Official documentation: <https://developers.openai.com/codex/mcp>.

```sh
codex mcp add mealplanner -- /ABSOLUTE/PATH/mealplanner-mcp --data-dir /ABSOLUTE/PATH/drafts
codex mcp list
```

Codex stores this in `config.toml` (user-wide `~/.codex/config.toml`, or a trusted project's `.codex/config.toml`); `codex mcp add` writes the user-wide file. Remove it with `codex mcp remove mealplanner`.

For a one-off run that changes nothing persistent, supply the server as per-run configuration instead:

```sh
codex exec --ignore-user-config --ephemeral --skip-git-repo-check \
  -c 'mcp_servers.mealplanner.command="/ABSOLUTE/PATH/mealplanner-mcp"' \
  -c 'mcp_servers.mealplanner.args=["--data-dir","/ABSOLUTE/PATH/drafts"]' \
  "<prompt>"
```

### Claude Code

Official documentation: <https://code.claude.com/docs/en/mcp>.

```sh
claude mcp add --transport stdio --scope local mealplanner -- /ABSOLUTE/PATH/mealplanner-mcp --data-dir /ABSOLUTE/PATH/drafts
claude mcp list
```

`--scope local` registers it for you in the current project only (`user` registers it for all your projects; `project` writes a shared `.mcp.json`, which you should not use for a personal data directory). Check status inside Claude Code with `/mcp`. Remove it with `claude mcp remove mealplanner`.

For a one-off run, put the server in a JSON file and pass `--mcp-config <file> --strict-mcp-config` (nothing persistent changes).

### After you rebuild the binary

A host keeps the server process it started. After replacing the binary, restart the host session (or reconnect the server from its MCP menu) so the new binary runs; then call `server_status` and check `binary_version`. Drafts are unaffected: they are read from disk by the new process.

### Remove the test registration

Use the `remove` command above, then delete the test data directory if it held only synthetic drafts. Registration alone proves nothing: always make real tool calls (below).

## Tool approvals

Keep the host's normal trust and approval prompts on. `draft_update` replaces a draft's content and keeps no history, so it is annotated as destructive; hosts may therefore ask for approval for it even when they allow read-only tools silently. That prompt is intended. In non-interactive runs the default policy refuses it (see the verification record).

Codex: to approve only this tool for a single run, pass `-c 'mcp_servers.mealplanner.tools.draft_update.approval_mode="approve"'`, under your own authority (the key is documented at <https://developers.openai.com/codex/mcp>; `"approve"` is the documented example value). Claude Code: name the tool for one run with `--allowedTools mcp__mealplanner__draft_update`, or approve it when prompted.

## Using the tools

Tools: `server_status`, `draft_create`, `draft_list`, `draft_get`, `draft_update`, `draft_validate`. A typical session: `draft_create`, `draft_get` (to read the revision), `draft_update` with that `expected_revision` and the complete content, `draft_validate` for candidates. A stale `expected_revision` returns `conflict`/`revision_mismatch` with the current revision; read the draft again and retry. Create is not idempotent: if a create fails ambiguously, call `draft_list` before retrying, because repeating it makes another draft.

**Ordering and protocol notes.** A host may issue several tool calls at once, and they can complete out of order: match each response to its request by JSON-RPC `id`, never by arrival order. Do not issue a dependent call (for example an update that needs the revision from a read) until the read has returned. Request ids must be unique among pending requests: reusing the id of a request that has not been answered makes the server wait out its 5-second shutdown grace and exit with status 1 at end of input. An id that is a boolean, array or object, or a line on stdin that is not valid JSON, ends the session. Stock hosts do none of this; it matters only to people writing their own client.

Inspect the same drafts from a terminal:

```sh
/ABSOLUTE/PATH/mealplanner draft list --data-dir /ABSOLUTE/PATH/drafts
/ABSOLUTE/PATH/mealplanner draft show <id> --data-dir /ABSOLUTE/PATH/drafts          # summary
/ABSOLUTE/PATH/mealplanner draft show <id> --data-dir /ABSOLUTE/PATH/drafts --json   # complete
echo '{"title": "Synthetic Toast"}' | /ABSOLUTE/PATH/mealplanner draft validate -
```

## Automated coverage (no host, no network)

`cmd/mealplanner-mcp/workflow_test.go` builds both binaries and, in one temporary data directory, drives the MCP server over stdio to create and revise a draft, stops it, starts a new process, reads the persisted result, confirms a stale update still conflicts, and then checks that the CLI `show`/`list`/`validate` agree with the MCP view and change nothing. It runs in the normal `local-go` CI job (`go test ./cmd/...`). Subprocess tests of the server itself are in `cmd/mealplanner-mcp/subprocess_test.go`.

## Verification record

Method: non-interactive runs (first against an earlier build, then repeated with identical results against the final binaries named below), each host given only a per-run server definition (no `codex mcp add`, no `claude mcp add`, no edits to host configuration), a freshly created temporary data directory, and synthetic prompts. Each run drives real tool calls; registration was never counted as a pass. Only the host's own transcript and the CLI's reading of the same directory are used as evidence.

| Item | Value |
| --- | --- |
| Date | 2026-11-04 |
| OS | macOS 27.0.1 (build 26A434), darwin/arm64 |
| Go | go1.26.5 darwin/arm64 |
| Server and CLI source | merge of `feat/mcp-adapter` (b923bfd) and `feat/cli-drafts` (1534fff); the binaries used for the final host runs were built from commit 75a7af6 |
| `mealplanner-mcp --version` | `mealplanner-mcp dev` |
| MCP Go SDK | `github.com/modelcontextprotocol/go-sdk` v1.8.0 |
| Codex CLI | codex-cli 0.164.0, model `gpt-6-luna` |
| Claude Code | 2.1.312, model `haiku` (reported as `claude-haiku-5-5`) |

### Results

| Host | Result | Evidence |
| --- | --- | --- |
| Claude Code 2.1.312 (haiku) | **Passed** | No permission-bypass flag (such as `--dangerously-skip-permissions`) was used in any Claude run; the only per-run allowance was the `--allowedTools` list below. Three runs against one temporary data directory. Run 1 connected, discovered six tools, and called `server_status`, `draft_create`, `draft_list`, `draft_get`, `draft_update` (revision 1 to 2) and `draft_validate`. Run 2 was a new process: `draft_list` and `draft_get` returned the persisted revision 2; a missing id returned `not_found`/`draft_not_found`; a blank title and a `status` field returned `invalid_argument`/`validation_failed`; an invalid candidate to `draft_validate` returned `structurally_valid: false`; the draft was unchanged. Run 3: `draft_update` revision 2 to 3; repeating it returned `conflict`/`revision_mismatch` with `current_revision` 3; an update with a blank title returned `validation_failed`; the draft stayed at revision 3 with its content intact. `mealplanner draft show --json` and `draft list` then showed the same revision-3 draft. No permission denials were recorded; the machine's own permission mode (auto) approved `draft_update`, which was not in the run's `--allowedTools` (only the five non-destructive tools were). |
| Claude Code, default permissions | Observed | A separate run with `--setting-sources ""` and `--permission-mode default` (an accepted alias that `claude --help` does not list; its listed choices are acceptEdits, auto, bypassPermissions, manual, dontAsk, plan) allowed only `draft_create`: create succeeded; `draft_update` was refused with "Claude requested permissions to use mcp__mealplanner__draft_update, but you haven't granted it yet"; the draft stayed at revision 1. |
| Codex CLI 0.164.0 (`gpt-6-luna`) | **Partially verified; update and stale-update not verified** | Run 1 connected, discovered six tools, and `server_status`, `draft_create`, `draft_list`, `draft_get` and `draft_validate` succeeded. `draft_update` failed: "MCP tool call requires approval, but approval policy is never" (the headless default refused the destructive tool; nothing was bypassed). Run 2, a new process on the same directory, listed and read the persisted draft; missing id returned `not_found`; blank title and an unknown field returned `validation_failed`; an invalid candidate validated as not structurally valid; the draft was unchanged. `mealplanner draft show --json` and `draft list` showed the same draft (revision 1). A third run intended to approve only `draft_update` for that run (`-c 'mcp_servers.mealplanner.tools.draft_update.approval_mode="approve"'`) was **not run** (the per-run approval override was not authorized in the verifying session), so update, stale-update and invalid-update behavior is unverified in Codex. |

Dual-host verification is therefore **open**: Claude Code passed; Codex passed everything except the destructive `draft_update` path, which needs a person to approve the call (interactively, or with the per-run option above under their own authority).

### What was not tested

- Interactive (TTY) sessions in either host, and the hosts' desktop or IDE front ends.
- Other models, or other client versions than those above.
- Windows and Linux (the code cross-compiles; only macOS was run).
- A second MCP server process writing at the same time as a host (covered by the localstore multi-process tests, not by a host run).

### Reproducing a host run

Use a new empty directory (`mktemp -d`), synthetic prompts, and the per-run forms above. Record the host and model versions, the server `--version` and commit, and which tool calls succeeded, were refused, or returned errors. Do not count registration, or a host that listed the tools, as a pass. To exercise `draft_update` in a non-interactive Codex run, add the per-run approval override from [Tool approvals](#tool-approvals), under your own authority; without it that call is refused by design.
