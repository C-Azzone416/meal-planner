// Command mealplanner is the meal-planner CLI.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/C-Azzone416/meal-planner/internal/core/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: mealplanner <command>\n\ncommands:\n  version    print the version")
		return 2
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(stdout, "mealplanner %s\n", version.Version)
		return 0
	}
	fmt.Fprintf(stderr, "mealplanner: unknown command %q\nusage: mealplanner <command>\n", args[0])
	return 2
}
