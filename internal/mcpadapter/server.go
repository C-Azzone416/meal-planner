// Package mcpadapter exposes the local draft operations of internal/core/draft as MCP tools
// (docs/local-draft-contract.md, section 13). It owns tool schemas, argument handling and result
// and error mapping only; every recipe rule lives in the core. It does not import the store: the
// store arrives inside the *draft.Service.
package mcpadapter

import (
	"context"
	"io"
	"log/slog"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/core/version"
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
)

// Operations lists the advertised tools in a stable order.
var Operations = []string{ToolDraftCreate, ToolDraftGet, ToolDraftList, ToolDraftUpdate, ToolDraftValidate, ToolServerStatus}

// CodeInvalidArguments is the error code (category invalid_argument) for arguments that fail the
// tool's input schema before any handler logic runs.
const CodeInvalidArguments = "invalid_arguments"

type config struct {
	source string
	logger *slog.Logger
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

// Server is an MCP server for the draft tools. It embeds the SDK server (use Run) and tracks
// in-flight tool calls so a caller can wait for them to finish before exiting.
type Server struct {
	*mcp.Server
	inflight *sync.WaitGroup
}

// Drain waits until every tool call that has started has finished, or ctx is done. It returns
// ctx.Err() on timeout.
func (s *Server) Drain(ctx context.Context) error {
	done := make(chan struct{})
	go func() { s.inflight.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// New returns an MCP server exposing exactly the six draft tools over svc. Only the tools
// capability is advertised: no resources, prompts, logging, sampling or elicitation.
func New(svc *draft.Service, opts ...Option) *Server {
	cfg := &config{source: "unknown", logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, o := range opts {
		o(cfg)
	}
	srv := mcp.NewServer(
		&mcp.Implementation{Name: ServerName, Version: version.Version},
		&mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}}},
	)
	h := &handlers{svc: svc, cfg: cfg, wg: &sync.WaitGroup{}}
	h.register(srv)
	return &Server{Server: srv, inflight: h.wg}
}
