//go:build integration

package hosted

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Owns a unique private directory and server. TCP is disabled; cleanup stops
// only the process identified by this cluster's data directory before removal.
func feedbackDisposablePostgres(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "feedback-postgres-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	binary := func(name string) string {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		if config, err := exec.LookPath("pg_config"); err == nil {
			if out, err := exec.Command(config, "--bindir").Output(); err == nil {
				candidate := filepath.Join(strings.TrimSpace(string(out)), name)
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					return candidate
				}
			}
		}
		return "/opt/homebrew/bin/" + name
	}
	run := func(name string, args ...string) {
		t.Helper()
		if output, err := exec.Command(binary(name), args...).CombinedOutput(); err != nil {
			t.Fatalf("temporary %s: %v: %s", name, err, output)
		}
	}
	data := filepath.Join(root, "data")
	run("initdb", "-D", data, "-A", "trust", "-U", "feedback_test", "--no-locale")
	run("pg_ctl", "-D", data, "-l", filepath.Join(root, "server.log"), "-o", "-k "+root+" -p 55484 -h '' -c unix_socket_permissions=0700", "-w", "start")
	t.Cleanup(func() {
		run("pg_ctl", "-D", data, "-m", "fast", "-w", "stop")
		t.Log("Temporary fixture PostgreSQL stopped")
	})
	return "postgres:///postgres?host=" + root + "&port=55484&user=feedback_test&sslmode=disable"
}
