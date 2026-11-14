package localstore

import (
	"errors"
	"path/filepath"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// EnvDataDir is the environment variable that overrides the default data directory.
const EnvDataDir = "MEALPLANNER_DATA_DIR"

// DirSource says where a resolved data directory came from.
type DirSource string

const (
	SourceFlag    DirSource = "flag"
	SourceEnv     DirSource = "env"
	SourceDefault DirSource = "default"
)

// ResolveDataDir applies the data directory precedence: the --data-dir flag, then
// MEALPLANNER_DATA_DIR, then os.UserConfigDir()/mealplanner.
//
// flagSet reports whether --data-dir was provided at all, so an explicitly empty value is
// distinguishable from an absent flag. An empty environment value counts as unset. A bad value from
// a higher-precedence source is an error and never falls through to a lower one. Relative paths are
// rejected rather than resolved against the working directory, which an MCP host controls. getenv
// and userConfigDir are injected so tests never touch the process environment.
func ResolveDataDir(flagValue string, flagSet bool, getenv func(string) string,
	userConfigDir func() (string, error)) (dir string, source DirSource, err error) {
	if flagSet {
		dir, err = checkDataDir(flagValue, "--data-dir")
		return dir, SourceFlag, err
	}
	if v := getenv(EnvDataDir); v != "" {
		dir, err = checkDataDir(v, EnvDataDir)
		return dir, SourceEnv, err
	}
	cfg, cerr := userConfigDir()
	if cerr != nil || cfg == "" {
		return "", SourceDefault, &draft.Error{
			Category: draft.CategoryInvalidArgument,
			Code:     draft.CodeInvalidDataDir,
			Message:  "cannot determine the default data directory; provide an absolute path with --data-dir or " + EnvDataDir,
			Err:      cerr,
		}
	}
	dir, err = checkDataDir(filepath.Join(cfg, "mealplanner"), "default data directory")
	return dir, SourceDefault, err
}

// checkDataDir requires an absolute path and returns it cleaned. The message names the source but
// never echoes the value.
func checkDataDir(v, what string) (string, error) {
	if v == "" {
		return "", invalidDataDir(what + " must not be empty")
	}
	if !filepath.IsAbs(v) {
		return "", invalidDataDir(what + " must be an absolute path (relative paths and ~ are not expanded)")
	}
	return filepath.Clean(v), nil
}

func invalidDataDir(msg string) error {
	return &draft.Error{
		Category: draft.CategoryInvalidArgument,
		Code:     draft.CodeInvalidDataDir,
		Message:  msg,
		Err:      errors.New(msg),
	}
}
