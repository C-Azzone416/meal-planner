// Command mealplanner-mcp is a stdio MCP server for local recipe drafts
// (docs/local-draft-contract.md, section 13). Stdout carries protocol frames only; diagnostics go
// to stderr and never include recipe content or filesystem paths.
//
// Usage: mealplanner-mcp [--data-dir <absolute path>]   |   mealplanner-mcp --version
//
// Shutdown: on stdin EOF, SIGINT or SIGTERM the server stops accepting calls and waits up to
// shutdownGrace for calls already running. It exits 0 when none was outstanding or all finished,
// and 1 if the grace period ran out. Data-directory resolution or store errors exit 2 before any
// protocol traffic.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/core/version"
	"github.com/C-Azzone416/meal-planner/internal/localstore"
	"github.com/C-Azzone416/meal-planner/internal/mcpadapter"
)

// shutdownGrace bounds how long in-flight tool calls may run after EOF or a signal.
const shutdownGrace = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "--version" {
		fmt.Fprintf(stdout, "mealplanner-mcp %s\n", version.Version)
		return 0
	}

	fs := flag.NewFlagSet("mealplanner-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, "usage: mealplanner-mcp [--data-dir <absolute path>] | --version") }
	dataDir := fs.String("data-dir", "", "absolute path of the draft data directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return 2
	}
	flagSet := false
	fs.Visit(func(f *flag.Flag) { flagSet = flagSet || f.Name == "data-dir" })

	dir, source, err := localstore.ResolveDataDir(*dataDir, flagSet, getenv, os.UserConfigDir)
	if err != nil {
		fmt.Fprintf(stderr, "mealplanner-mcp: %s\n", messageOf(err))
		return 2
	}
	store, err := localstore.Open(dir, localstore.Options{})
	if err != nil {
		fmt.Fprintf(stderr, "mealplanner-mcp: %s\n", messageOf(err))
		return 2
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	srv := mcpadapter.New(draft.NewService(store),
		mcpadapter.WithDataDirSource(string(source)),
		mcpadapter.WithLogger(logger))

	transport := &mcp.IOTransport{Reader: io.NopCloser(stdin), Writer: nopWriteCloser{stdout}}
	runErr := srv.Run(ctx, transport)

	code := 0
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, io.EOF) {
		logger.Error("server stopped", "error_type", fmt.Sprintf("%T", runErr))
		code = 1
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Drain(drainCtx); err != nil {
		logger.Error("shutdown grace period elapsed with calls still running")
		code = 1
	}
	return code
}

// messageOf returns the safe message of a draft error, which never contains a path or a value.
func messageOf(err error) string {
	var e *draft.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return "could not open the draft store"
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
