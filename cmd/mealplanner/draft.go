package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/localstore"
)

const draftUsage = `usage: mealplanner draft <command>

commands:
  validate <file|->   structurally validate draft content from a file or stdin
  list                list saved drafts, newest first
  show <id>           show one saved draft

Every saved recipe is a draft. Validation is structural only; it is not food-safety
verification, review, or approval. Drafts are created and edited through the MCP server.

Run 'mealplanner draft <command> --help' for details.
`

const validateUsage = `usage: mealplanner draft validate <file|-> [--json]

Validate one JSON object of draft content (not a saved draft) from a file, or from stdin when the
argument is '-'. Nothing is saved and no data directory is read.

  --json   print one JSON document: {"validation": {...}}

Exit status: 0 structurally valid (warnings do not change it), 1 validation errors (including
malformed or empty input), 2 bad invocation, 3 the input file could not be read.

Example:
  echo '{"title": "Synthetic Toast"}' | mealplanner draft validate -
`

const listUsage = `usage: mealplanner draft list [--limit N] [--offset N] [--data-dir DIR] [--json]

List saved drafts, most recently updated first. Reads only; the data directory is never created.

  --limit N      page size, 1 to 100 (default 25)
  --offset N     number of drafts to skip (default 0)
  --data-dir DIR absolute path of the data directory
  --json         print one JSON document with drafts, total, offset, limit, has_more, skipped

Data directory: --data-dir, else the MEALPLANNER_DATA_DIR environment variable, else
<user config dir>/mealplanner (macOS: ~/Library/Application Support/mealplanner). Relative
paths are rejected. The MCP server uses the same rules.

Exit status: 0 ok, 2 bad invocation, 3 the data directory or a stored file could not be used.

Example:
  mealplanner draft list --data-dir /absolute/path/to/drafts --limit 10
`

const showUsage = `usage: mealplanner draft show <id> [--data-dir DIR] [--json]

Show one saved draft, including its sources and how each estimate or claim is based
(agent estimate or sourced). Reads only; the data directory is never created.

The human output is a summary for reading: it omits some detail, such as ingredient quantities,
units and notes, and it escapes control and formatting characters. Use --json for the complete
draft.

  --data-dir DIR absolute path of the data directory (default as for 'draft list')
  --json         print one JSON document: {"draft": {...}}, or {"error": {...}} if not found

Exit status: 0 ok, 1 no such draft, 2 bad invocation (including a malformed id),
3 the data directory or the stored draft could not be used.

Example:
  mealplanner draft show drf_0123456789abcdef0123 --json
`

func runDraft(args []string, e env) int {
	if len(args) == 0 {
		fmt.Fprint(e.stderr, draftUsage)
		return exitUsage
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(e.stdout, draftUsage)
		return exitOK
	case "validate":
		return runValidate(args[1:], e)
	case "list":
		return runList(args[1:], e)
	case "show":
		return runShow(args[1:], e)
	}
	fmt.Fprintf(e.stderr, "mealplanner draft: unknown command %q\n%s", args[0], draftUsage)
	return exitUsage
}

// parsed is the result of parsing one subcommand's arguments.
type parsed struct {
	fs   *flag.FlagSet
	pos  []string
	set  map[string]bool
	code int // exitOK means parsing succeeded; anything else means exit with code now
}

// parseArgs parses flags that may appear before or after positional arguments. A parse error or a
// request for help is reported here and parsed.code says whether to continue.
func parseArgs(name, usageText string, args []string, e env, define func(fs *flag.FlagSet)) parsed {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	define(fs)
	p := parsed{fs: fs, set: map[string]bool{}}
	rest := args
	for {
		err := fs.Parse(rest)
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(e.stdout, usageText)
			p.code = -1
			return p
		}
		if err != nil {
			fmt.Fprintf(e.stderr, "mealplanner %s: %v\nRun 'mealplanner %s --help' for usage.\n", name, err, name)
			p.code = exitUsage
			return p
		}
		after := fs.Args()
		if i := len(rest) - len(after) - 1; i >= 0 && rest[i] == "--" {
			p.pos = append(p.pos, after...) // everything after -- is positional
			break
		}
		if len(after) == 0 {
			break
		}
		p.pos = append(p.pos, after[0])
		rest = after[1:]
	}
	fs.Visit(func(f *flag.Flag) { p.set[f.Name] = true })
	return p
}

// done reports whether the caller should return p's exit code now.
func (p parsed) done() (int, bool) {
	switch {
	case p.code == -1:
		return exitOK, true
	case p.code != exitOK:
		return p.code, true
	}
	return 0, false
}

func usageError(e env, name, msg string) int {
	fmt.Fprintf(e.stderr, "mealplanner %s: %s\nRun 'mealplanner %s --help' for usage.\n", name, msg, name)
	return exitUsage
}

func runValidate(args []string, e env) int {
	const name = "draft validate"
	var asJSON bool
	p := parseArgs(name, validateUsage, args, e, func(fs *flag.FlagSet) { fs.BoolVar(&asJSON, "json", false, "") })
	if code, stop := p.done(); stop {
		return code
	}
	if len(p.pos) != 1 {
		return usageError(e, name, "expected exactly one argument: a file path, or - for stdin")
	}
	var src io.Reader
	if p.pos[0] == "-" {
		src = e.stdin
	} else {
		f, err := os.Open(p.pos[0])
		if err != nil {
			fmt.Fprintf(e.stderr, "mealplanner: cannot read the input file: %s\n", clean(err.Error(), false))
			return exitFailure
		}
		defer f.Close()
		src = f
	}
	// One byte over the limit is enough for DecodeContent to report input_too_large.
	raw, err := io.ReadAll(io.LimitReader(src, draft.MaxInputBytes+1))
	if err != nil {
		fmt.Fprintf(e.stderr, "mealplanner: cannot read the input: %s\n", clean(err.Error(), false))
		return exitFailure
	}
	res := draft.ValidateRaw(raw)
	if asJSON {
		writeJSON(e.stdout, map[string]any{"validation": res})
	} else {
		renderValidation(e.stdout, res)
	}
	if res.StructurallyValid {
		return exitOK
	}
	return exitRejected
}

func runList(args []string, e env) int {
	const name = "draft list"
	var asJSON bool
	var limit, offset int
	var dataDir string
	p := parseArgs(name, listUsage, args, e, func(fs *flag.FlagSet) {
		fs.BoolVar(&asJSON, "json", false, "")
		fs.IntVar(&limit, "limit", 0, "")
		fs.IntVar(&offset, "offset", 0, "")
		fs.StringVar(&dataDir, "data-dir", "", "")
	})
	if code, stop := p.done(); stop {
		return code
	}
	if len(p.pos) != 0 {
		return usageError(e, name, "unexpected argument")
	}
	if p.set["limit"] && (limit < 1 || limit > draft.MaxListLimit) {
		return usageError(e, name, fmt.Sprintf("--limit must be between 1 and %d (omit it for the default of %d)", draft.MaxListLimit, draft.DefaultListLimit))
	}
	if offset < 0 {
		return usageError(e, name, "--offset must be 0 or greater")
	}
	svc, code := openService(e, name, dataDir, p.set["data-dir"])
	if svc == nil {
		return code
	}
	page, err := svc.List(context.Background(), draft.ListOptions{Limit: limit, Offset: offset})
	if err != nil {
		return fail(e, err, asJSON)
	}
	if asJSON {
		writeJSON(e.stdout, page)
	} else {
		renderList(e.stdout, page)
	}
	return exitOK
}

func runShow(args []string, e env) int {
	const name = "draft show"
	var asJSON bool
	var dataDir string
	p := parseArgs(name, showUsage, args, e, func(fs *flag.FlagSet) {
		fs.BoolVar(&asJSON, "json", false, "")
		fs.StringVar(&dataDir, "data-dir", "", "")
	})
	if code, stop := p.done(); stop {
		return code
	}
	if len(p.pos) != 1 {
		return usageError(e, name, "expected exactly one argument: a draft id")
	}
	id := p.pos[0]
	if !draft.ValidID(id) {
		return usageError(e, name, "the draft id must look like drf_ followed by 20 lowercase hex characters")
	}
	svc, code := openService(e, name, dataDir, p.set["data-dir"])
	if svc == nil {
		return code
	}
	d, err := svc.Get(context.Background(), id)
	if err != nil {
		return fail(e, err, asJSON)
	}
	if asJSON {
		writeJSON(e.stdout, map[string]any{"draft": d})
	} else {
		renderDraft(e.stdout, d)
	}
	return exitOK
}

// openService resolves the data directory and returns a service over the file store. On failure it
// has already reported and returns a nil service and the exit code. Nothing is created on disk.
func openService(e env, name, flagValue string, flagSet bool) (*draft.Service, int) {
	dir, _, err := localstore.ResolveDataDir(flagValue, flagSet, e.getenv, e.userConfigDir)
	if err != nil {
		return nil, fail(e, err, false)
	}
	st, err := localstore.Open(dir, localstore.Options{})
	if err != nil {
		return nil, fail(e, err, false)
	}
	return draft.NewService(st), 0
}

// exitFor maps an error to the contract's exit code (section 14.2).
func exitFor(err error) int {
	var de *draft.Error
	if !errors.As(err, &de) {
		return exitFailure
	}
	switch de.Category {
	case draft.CategoryNotFound:
		return exitRejected
	case draft.CategoryInvalidArgument:
		return exitUsage
	}
	return exitFailure
}

// fail reports err and returns its exit code. In JSON mode only a rejected result (exit 1) is
// written to stdout; for exit 2 and 3 stdout stays empty.
func fail(e env, err error, asJSON bool) int {
	code := exitFor(err)
	var de *draft.Error
	if !errors.As(err, &de) {
		fmt.Fprintf(e.stderr, "mealplanner: %s\n", clean(err.Error(), false))
		return code
	}
	if asJSON && code == exitRejected {
		writeJSON(e.stdout, map[string]any{"error": de})
		return code
	}
	msg := de.Message
	// Only I/O causes are appended: they come from the operating system and may name a path, which
	// is acceptable on stderr. Other causes (such as a JSON decoder error on a corrupt file) can
	// echo file contents, so only the fixed core message is printed for them.
	if de.Err != nil && de.Category == draft.CategoryIO {
		msg += " (" + oneLine(de.Err.Error()) + ")"
	}
	fmt.Fprintf(e.stderr, "mealplanner: %s\n", clean(msg, false))
	return code
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// writeJSON writes exactly one JSON document and a newline.
func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
