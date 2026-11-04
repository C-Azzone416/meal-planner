// Command mealplanner-mcp is a stub MCP adapter. Stdout is reserved for the
// protocol, so diagnostics go to stderr only.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/C-Azzone416/meal-planner/internal/core/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout))
}

func run(args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) > 0 && args[0] == "--version" {
		fmt.Fprintf(stdout, "mealplanner-mcp %s\n", version.Version)
		return 0
	}
	// Stub: drain stdin until EOF; write nothing to stdout.
	_, _ = io.Copy(io.Discard, stdin)
	return 0
}
