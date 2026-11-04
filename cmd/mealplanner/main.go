// Command mealplanner is the meal-planner CLI.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/C-Azzone416/meal-planner/internal/core/version"
)

// Exit codes (contract section 14.2).
const (
	exitOK       = 0 // success; validation warnings do not change it
	exitRejected = 1 // the operation ran and the subject was rejected on its merits
	exitUsage    = 2 // bad invocation, detectable from the arguments and environment alone
	exitFailure  = 3 // operational failure: the environment or the stored data failed
)

const usage = `usage: mealplanner <command>

commands:
  version    print the version
  draft      validate, list and show local recipe drafts (mealplanner draft --help)
`

// env is everything the CLI takes from the process, so tests can supply their own.
type env struct {
	stdin         io.Reader
	stdout        io.Writer
	stderr        io.Writer
	getenv        func(string) string
	userConfigDir func() (string, error)
}

func main() {
	os.Exit(runEnv(os.Args[1:], env{
		stdin:         os.Stdin,
		stdout:        os.Stdout,
		stderr:        os.Stderr,
		getenv:        os.Getenv,
		userConfigDir: os.UserConfigDir,
	}))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runEnv(args, env{stdin: os.Stdin, stdout: stdout, stderr: stderr, getenv: os.Getenv, userConfigDir: os.UserConfigDir})
}

func runEnv(args []string, e env) int {
	if len(args) == 0 {
		fmt.Fprint(e.stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(e.stdout, "mealplanner %s\n", version.Version)
		return exitOK
	case "help", "--help", "-h":
		fmt.Fprint(e.stdout, usage)
		return exitOK
	case "draft":
		return runDraft(args[1:], e)
	}
	fmt.Fprintf(e.stderr, "mealplanner: unknown command %q\n%s", args[0], usage)
	return exitUsage
}
