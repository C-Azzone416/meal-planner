package localstore

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func TestResolveDataDir(t *testing.T) {
	abs := func(parts ...string) string { return filepath.Join(append([]string{t.TempDir()}, parts...)...) }
	flagDir, envDir, cfgDir := abs("flag"), abs("env"), abs("cfg")
	env := func(v string) func(string) string {
		return func(k string) string {
			if k != EnvDataDir {
				t.Errorf("unexpected env lookup %q", k)
			}
			return v
		}
	}
	okCfg := func() (string, error) { return cfgDir, nil }
	failCfg := func() (string, error) { return "", errors.New("HOME is not set") }

	tests := []struct {
		name     string
		flag     string
		flagSet  bool
		env      string
		cfg      func() (string, error)
		wantDir  string
		wantSrc  DirSource
		wantCode string
	}{
		{name: "flag beats env and default", flag: flagDir, flagSet: true, env: envDir, cfg: okCfg, wantDir: flagDir, wantSrc: SourceFlag},
		{name: "env beats default", env: envDir, cfg: okCfg, wantDir: envDir, wantSrc: SourceEnv},
		{name: "default", cfg: okCfg, wantDir: filepath.Join(cfgDir, "mealplanner"), wantSrc: SourceDefault},
		{name: "empty env counts as unset", env: "", cfg: okCfg, wantDir: filepath.Join(cfgDir, "mealplanner"), wantSrc: SourceDefault},
		{name: "flag is cleaned", flag: flagDir + "/a/../b/", flagSet: true, cfg: okCfg, wantDir: filepath.Join(flagDir, "b"), wantSrc: SourceFlag},
		{name: "relative flag rejected", flag: "drafts", flagSet: true, cfg: okCfg, wantSrc: SourceFlag, wantCode: draft.CodeInvalidDataDir},
		{name: "tilde flag rejected", flag: "~/drafts", flagSet: true, cfg: okCfg, wantSrc: SourceFlag, wantCode: draft.CodeInvalidDataDir},
		{name: "dot-relative flag rejected", flag: "./drafts", flagSet: true, cfg: okCfg, wantSrc: SourceFlag, wantCode: draft.CodeInvalidDataDir},
		{name: "explicit empty flag rejected", flag: "", flagSet: true, env: envDir, cfg: okCfg, wantSrc: SourceFlag, wantCode: draft.CodeInvalidDataDir},
		{name: "bad flag does not fall through to env", flag: "zz-relpath", flagSet: true, env: envDir, cfg: okCfg, wantSrc: SourceFlag, wantCode: draft.CodeInvalidDataDir},
		{name: "relative env rejected", env: "rel/dir", cfg: okCfg, wantSrc: SourceEnv, wantCode: draft.CodeInvalidDataDir},
		{name: "bad env does not fall through to default", env: "zz-relenv", cfg: okCfg, wantSrc: SourceEnv, wantCode: draft.CodeInvalidDataDir},
		{name: "no config dir", cfg: failCfg, wantSrc: SourceDefault, wantCode: draft.CodeInvalidDataDir},
		{name: "flag works without config dir", flag: flagDir, flagSet: true, cfg: failCfg, wantDir: flagDir, wantSrc: SourceFlag},
		{name: "relative config dir rejected", cfg: func() (string, error) { return "relative", nil }, wantSrc: SourceDefault, wantCode: draft.CodeInvalidDataDir},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir, src, err := ResolveDataDir(tc.flag, tc.flagSet, env(tc.env), tc.cfg)
			if src != tc.wantSrc {
				t.Errorf("source = %q, want %q", src, tc.wantSrc)
			}
			if tc.wantCode != "" {
				var de *draft.Error
				if !errors.As(err, &de) || de.Category != draft.CategoryInvalidArgument || de.Code != tc.wantCode {
					t.Fatalf("err = %v, want invalid_argument/%s", err, tc.wantCode)
				}
				if dir != "" {
					t.Errorf("dir = %q on error", dir)
				}
				for _, bad := range []string{tc.flag, tc.env} {
					if bad != "" && contains(de.Message, bad) {
						t.Errorf("message echoes the value %q: %s", bad, de.Message)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if dir != tc.wantDir {
				t.Errorf("dir = %q, want %q", dir, tc.wantDir)
			}
		})
	}
}

func TestOpenRejectsRelative(t *testing.T) {
	for _, p := range []string{"", "rel", "./rel", "~/x"} {
		if _, err := Open(p, Options{}); err == nil {
			t.Errorf("Open(%q) succeeded", p)
		}
	}
	s, err := Open(t.TempDir(), Options{})
	if err != nil || s == nil {
		t.Fatal(err)
	}
}
