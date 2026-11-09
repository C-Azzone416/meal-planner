// Command mealplanner-mcp is a stdio MCP server for local recipe drafts
// (docs/local-draft-contract.md, section 13). Stdout carries protocol frames only; diagnostics go
// to stderr and never include recipe content or filesystem paths.
//
// Usage: mealplanner-mcp [--data-dir <absolute path>]   |   mealplanner-mcp --version
//
// Shutdown: on stdin EOF, SIGINT or SIGTERM the server stops reading, but first answers every
// request it has already read (see frameGate): the SDK is not told about the EOF until each such
// request has a response on stdout, or until shutdownGrace (5 s) has passed. It exits 0 when
// everything was answered and 1 if the grace period ran out. Malformed or ambiguous frames,
// unsupported or reused request IDs and frames over 4 MiB are rejected before SDK dispatch.
// Earlier accepted requests drain within the grace period, then the process exits 1 with a
// fixed diagnostic (see ids.go); the rejected frame and later frames are not executed.
// Data-directory resolution or store errors exit 2 before any protocol traffic. --help prints usage
// to stdout and exits 0.
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

// shutdownGrace bounds how long requests already read may take to be answered after EOF or a
// signal. A variable only so a test-tagged build can shorten it.
var shutdownGrace = 5 * time.Second

// wrapStore, when set (test-tagged builds only), wraps the store, for example to stall a call.
var wrapStore func(draft.Store) draft.Store

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
	fs.SetOutput(io.Discard)
	const usage = "usage: mealplanner-mcp [--data-dir <absolute path>] | --version | --help"
	dataDir := fs.String("data-dir", "", "absolute path of the draft data directory")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, usage)
			fmt.Fprintln(stdout, "  --data-dir <absolute path>  draft data directory (default: the user config directory; env MEALPLANNER_DATA_DIR)")
			fmt.Fprintln(stdout, "  --version                   print the version and exit")
			return 0
		}
		fmt.Fprintf(stderr, "mealplanner-mcp: %v\n%s\n", err, usage)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, usage)
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
	var st draft.Store = store
	if wrapStore != nil {
		st = wrapStore(st)
	}
	srv := mcpadapter.New(draft.NewService(st),
		mcpadapter.WithDataDirSource(string(source)),
		mcpadapter.WithLogger(logger))

	gate := newFrameGate(shutdownGrace)
	gate.start(stdin)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			gate.Stop() // a signal ends input like EOF: answer what was read, then exit
		case <-done:
		}
	}()

	transport := &mcp.IOTransport{Reader: io.NopCloser(gate), Writer: gate.wrapWriter(stdout)}
	// The server context is not ctx: cancelling it would abandon requests that are still running.
	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(context.Background(), transport) }()

	select {
	case runErr := <-runDone:
		code := 0
		if reportInputError(gate, logger) {
			code = 1
		} else if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, io.EOF) {
			logger.Error("server stopped", "error_type", fmt.Sprintf("%T", runErr))
			code = 1
		}
		if gate.Expired() {
			logger.Error("shutdown grace period elapsed with requests unanswered")
			code = 1
		}
		return code
	case <-gate.DeadlineC():
		reportInputError(gate, logger)
		// A cancelled call may no longer owe a response but still keep Run
		// waiting. Bound session completion independently of pending IDs.
		logger.Error("shutdown grace period elapsed before session completed")
		return 1
	}
}

func reportInputError(gate *frameGate, logger *slog.Logger) bool {
	err := gate.InputError()
	switch err {
	case nil:
		return false
	case errUnsupportedID, errFrameTooLarge, errAmbiguousEnvelope, errAmbiguousToolParams, errDuplicateID:
		logger.Error(err.Error()) // fixed text, independent of the frame
	default:
		logger.Error("could not read protocol input")
	}
	return true
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
