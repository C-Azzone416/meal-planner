package localstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// Real multi-process tests use the standard helper-process pattern: the test binary re-executes
// itself with helperEnv set, and TestMain runs helperMain instead of the tests.
const helperEnv = "LOCALSTORE_TEST_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		os.Exit(helperMain(mode))
	}
	os.Exit(m.Run())
}

// helperMain waits for the start file (so all workers begin together), runs one scenario against the
// shared root and prints a one-line result.
func helperMain(mode string) int {
	root, id, worker := os.Getenv("LS_ROOT"), os.Getenv("LS_ID"), os.Getenv("LS_WORKER")
	start := os.Getenv("LS_START")
	for deadline := time.Now().Add(20 * time.Second); ; {
		if _, err := os.Stat(start); err == nil {
			break
		}
		if time.Now().After(deadline) {
			fmt.Println("error: start file never appeared")
			return 1
		}
		time.Sleep(time.Millisecond)
	}
	s, err := Open(root, Options{LockTimeout: 15 * time.Second})
	if err != nil {
		fmt.Println("error:", err)
		return 1
	}
	s.logf = func(string, ...any) {}
	ctx := context.Background()
	switch mode {
	case "create":
		err := s.Create(ctx, mkDraft(id, 1, minimal("worker "+worker)))
		fmt.Println(result(err))
	case "replace":
		err := s.Replace(ctx, id, 1, mkDraft(id, 2, minimal("worker "+worker)))
		fmt.Println(result(err))
	case "increment":
		for i := 0; i < 10; i++ {
			for {
				cur, err := s.Get(ctx, id)
				if err != nil {
					fmt.Println("error:", err)
					return 1
				}
				next := mkDraft(id, cur.Revision+1, minimal("t"))
				next.Notes = cur.Notes + worker + ";"
				err = s.Replace(ctx, id, cur.Revision, next)
				if err == nil {
					break
				}
				var de *draft.Error
				if !errors.As(err, &de) || de.Code != draft.CodeRevisionMismatch {
					fmt.Println("error:", err)
					return 1
				}
			}
		}
		fmt.Println("ok")
	}
	return 0
}

func result(err error) string {
	var de *draft.Error
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &de):
		return fmt.Sprintf("%s/%s/%d", de.Category, de.Code, de.CurrentRevision)
	default:
		return "error: " + err.Error()
	}
}

// runWorkers starts n helper processes in the given mode, releases them together, and returns each
// worker's output line.
func runWorkers(t *testing.T, mode, root, id string, n int) []string {
	t.Helper()
	start := filepath.Join(t.TempDir(), "go")
	type proc struct {
		cmd *exec.Cmd
		out *bytes.Buffer
	}
	var procs []proc
	for i := 0; i < n; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), helperEnv+"="+mode, "LS_ROOT="+root, "LS_ID="+id, fmt.Sprintf("LS_WORKER=%d", i), "LS_START="+start)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		procs = append(procs, proc{cmd, &out})
	}
	if err := os.WriteFile(start, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, p := range procs {
		if err := p.cmd.Wait(); err != nil {
			t.Fatalf("worker failed: %v\n%s", err, p.out)
		}
		lines = append(lines, strings.TrimSpace(p.out.String()))
	}
	return lines
}

func skipWithoutProcessLock(t *testing.T) {
	t.Helper()
	if !crossProcessLock {
		t.Skip("no cross-process lock on this platform (documented limitation)")
	}
}

func count(lines []string, want string) int {
	n := 0
	for _, l := range lines {
		if l == want {
			n++
		}
	}
	return n
}

func TestTwoProcessCreateRace(t *testing.T) {
	skipWithoutProcessLock(t)
	s, root := newStore(t)
	id := testID(1)
	lines := runWorkers(t, "create", root, id, 6)
	if count(lines, "ok") != 1 || count(lines, "conflict/already_exists/0") != 5 {
		t.Fatalf("results = %q, want one ok and five already_exists", lines)
	}
	got, err := s.Get(ctxb(), id)
	if err != nil || got.Revision != 1 {
		t.Fatalf("Get: %v %+v", err, got)
	}
	assertNoTemps(t, root)
}

func TestTwoProcessSameRevisionHasOneWinner(t *testing.T) {
	skipWithoutProcessLock(t)
	s, root := newStore(t)
	id := testID(1)
	mustCreate(t, s, mkDraft(id, 1, minimal("original")))
	lines := runWorkers(t, "replace", root, id, 6)
	if count(lines, "ok") != 1 || count(lines, "conflict/revision_mismatch/2") != 5 {
		t.Fatalf("results = %q, want one ok and five conflicts reporting revision 2", lines)
	}
	got, err := s.Get(ctxb(), id)
	if err != nil || got.Revision != 2 || !strings.HasPrefix(got.Title, "worker ") {
		t.Fatalf("Get: %v %+v", err, got)
	}
	assertNoTemps(t, root)
}

func TestTwoProcessNoLostUpdate(t *testing.T) {
	skipWithoutProcessLock(t)
	s, root := newStore(t)
	id := testID(1)
	mustCreate(t, s, mkDraft(id, 1, minimal("counter")))
	const workers, rounds = 4, 10
	lines := runWorkers(t, "increment", root, id, workers)
	if count(lines, "ok") != workers {
		t.Fatalf("results = %q", lines)
	}
	got, err := s.Get(ctxb(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1+workers*rounds {
		t.Fatalf("revision = %d, want %d: an update was lost", got.Revision, 1+workers*rounds)
	}
	if n := strings.Count(got.Notes, ";"); n != workers*rounds {
		t.Fatalf("notes hold %d entries, want %d: an update was lost", n, workers*rounds)
	}
	assertNoTemps(t, root)
}
