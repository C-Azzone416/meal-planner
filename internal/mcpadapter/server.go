// Package mcpadapter exposes the local draft operations of internal/core/draft as MCP tools
// (docs/local-draft-contract.md, section 13). It owns tool schemas, argument handling and result
// and error mapping only; every recipe rule lives in the core. It does not import the store: the
// store arrives inside the *draft.Service.
package mcpadapter

import (
	"io"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/core/version"
	"github.com/C-Azzone416/meal-planner/internal/localclient"
)

// ServerName is the MCP server name reported at initialize.
const ServerName = "mealplanner-mcp"

// Tool names (contract section 13). They are fixed.
const (
	ToolDraftCreate   = "draft_create"
	ToolDraftGet      = "draft_get"
	ToolDraftList     = "draft_list"
	ToolDraftUpdate   = "draft_update"
	ToolDraftValidate = "draft_validate"
	ToolServerStatus  = "server_status"
	ToolDraftSend     = "draft_send"
)

// Operations lists the advertised tools in a stable order.
var Operations = []string{ToolDraftCreate, ToolDraftGet, ToolDraftList, ToolDraftUpdate, ToolDraftValidate, ToolServerStatus}

// CodeInvalidArguments is the error code (category invalid_argument) for arguments that fail the
// tool's input schema before any handler logic runs.
const CodeInvalidArguments = "invalid_arguments"

type config struct {
	source string
	logger *slog.Logger
	sender localclient.Sender
}

// Option configures New.
type Option func(*config)

// WithDataDirSource sets the data_dir.source label reported by server_status ("flag", "env" or
// "default"). cmd passes the DirSource the store resolver returned, as a plain string, so this
// package does not import the store. Unset reports "unknown".
func WithDataDirSource(source string) Option { return func(c *config) { c.source = source } }

// WithLogger sets the logger for the one-line-per-call diagnostics. Default: discard. The adapter
// logs the tool name, outcome, duration and draft ID only, never payloads or paths.
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }

// WithDraftSender enables the explicit outbound send tool. The command does
// not use this option until the approved OS credential adapter and runtime gates
// are hooked up, so repository builds remain local-only by default.
func WithDraftSender(sender localclient.Sender) Option { return func(c *config) { c.sender = sender } }

// New returns an MCP server exposing the six local tools, plus draft_send only
// when an outbound sender is explicitly injected. Only the tools capability is
// advertised: no resources, prompts, logging, sampling or elicitation.
func New(svc *draft.Service, opts ...Option) *mcp.Server {
	cfg := &config{source: "unknown", logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, o := range opts {
		o(cfg)
	}
	srv := mcp.NewServer(
		&mcp.Implementation{Name: ServerName, Version: version.Version},
		&mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}}},
	)
	h := &handlers{svc: svc, cfg: cfg}
	h.register(srv)
	return srv
}
