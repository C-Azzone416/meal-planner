package localstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func mustCreate(t *testing.T, s *Store, d draft.Draft) {
	t.Helper()
	if err := s.Create(ctxb(), d); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestRoundTripAndRestart(t *testing.T) {
	s, root := newStore(t)
	want := mkDraft(testID(1), 1, fullContent())
	mustCreate(t, s, want)

	got, err := s.Get(ctxb(), want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip lost data:\n got %+v\nwant %+v", got, want)
	}
	if *got.Estimates.ActiveMinutes.Value != 0 || got.Estimates.Cleanup != nil {
		t.Fatal("zero and unknown estimates must stay distinct")
	}

	// A new Store instance on the same root (a process restart) sees the same draft.
	s2, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got2, err := s2.Get(ctxb(), want.ID)
	if err != nil || !reflect.DeepEqual(got2, want) {
		t.Fatalf("after restart: %v %+v", err, got2)
	}
	res, err := s2.List(ctxb())
	if err != nil || len(res.Drafts) != 1 || len(res.Skipped) != 0 {
		t.Fatalf("List after restart: %v %+v", err, res)
	}

	// Replace persists across a restart too.
	upd := mkDraft(want.ID, 2, minimal("Renamed"))
	if err := s2.Replace(ctxb(), want.ID, 1, upd); err != nil {
		t.Fatal(err)
	}
	s3, _ := Open(root, Options{})
	got3, err := s3.Get(ctxb(), want.ID)
	if err != nil || !reflect.DeepEqual(got3, upd) {
		t.Fatalf("after replace+restart: %v %+v", err, got3)
	}
}

func TestEmptyListsAreWrittenAsEmptyNotNull(t *testing.T) {
	s, root := newStore(t)
	d := mkDraft(testID(1), 1, draft.Content{Title: "Bare"}) // nil lists
	mustCreate(t, s, d)
	raw := string(readFile(t, filepath.Join(root, "drafts", d.ID+".json")))
	for _, key := range []string{`"ingredients": []`, `"instructions": []`, `"sources": []`, `"guidance": []`} {
		if !contains(raw, key) {
			t.Errorf("stored file lacks %s:\n%s", key, raw)
		}
	}
	if contains(raw, "null") {
		t.Errorf("stored file contains null:\n%s", raw)
	}
	got, err := s.Get(ctxb(), d.ID)
	if err != nil || got.Ingredients == nil || got.Instructions == nil || got.Sources == nil || got.Guidance == nil {
		t.Fatalf("lists must read back as empty, not nil: %v %+v", err, got)
	}
	if raw[len(raw)-1] != '\n' {
		t.Error("stored file lacks trailing newline")
	}
}

func TestCreateNeverOverwrites(t *testing.T) {
	s, root := newStore(t)
	d := mkDraft(testID(1), 1, minimal("First"))
	mustCreate(t, s, d)
	before := snapshot(t, root)
	err := s.Create(ctxb(), mkDraft(d.ID, 1, minimal("Second")))
	wantErr(t, err, draft.CategoryConflict, draft.CodeAlreadyExists, root, "Second")
	equalSnapshots(t, before, snapshot(t, root))
}

func TestCreateAndReplaceRejectBadInput(t *testing.T) {
	s, root := newStore(t)
	for _, id := range []string{"", "../x", "/etc/passwd", "drf_0123456789ABCDEF0123", "drf_0123456789abcdef012", "drf_0123456789abcdef0123.json", "drf_0123456789abcdef0123/../a", "drf_0123456789abcdef0123\x00"} {
		wantErr(t, s.Create(ctxb(), mkDraft(id, 1, minimal("x"))), draft.CategoryInvalidArgument, draft.CodeInvalidID, root)
		_, err := s.Get(ctxb(), id)
		wantErr(t, err, draft.CategoryInvalidArgument, draft.CodeInvalidID, root)
		wantErr(t, s.Replace(ctxb(), id, 1, mkDraft(id, 2, minimal("x"))), draft.CategoryInvalidArgument, draft.CodeInvalidID, root)
	}
	if _, err := os.Lstat(root); err == nil {
		t.Error("rejected input created the data root")
	}

	id := testID(1)
	bad := mkDraft(id, 2, minimal("x"))
	wantErr(t, s.Create(ctxb(), bad), draft.CategoryInternal, draft.CodeInternal, root) // not revision 1
	bad = mkDraft(id, 1, minimal("x"))
	bad.Status = "reviewed"
	wantErr(t, s.Create(ctxb(), bad), draft.CategoryInternal, draft.CodeInternal, root)
	bad = mkDraft(id, 1, minimal("x"))
	bad.SchemaVersion = 2
	wantErr(t, s.Create(ctxb(), bad), draft.CategoryInternal, draft.CodeInternal, root)
	if _, err := os.Lstat(root); err == nil {
		t.Error("rejected input created the data root")
	}
}

func TestReplace(t *testing.T) {
	s, root := newStore(t)
	d := mkDraft(testID(1), 1, fullContent())
	mustCreate(t, s, d)

	t.Run("success", func(t *testing.T) {
		next := mkDraft(d.ID, 2, minimal("Second"))
		if err := s.Replace(ctxb(), d.ID, 1, next); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Get(ctxb(), d.ID)
		if !reflect.DeepEqual(got, next) {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("stale revision conflicts and writes nothing", func(t *testing.T) {
		before := snapshot(t, root)
		err := s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal("Third")))
		de := wantErr(t, err, draft.CategoryConflict, draft.CodeRevisionMismatch, root, "Third")
		if de.CurrentRevision != 2 {
			t.Errorf("CurrentRevision = %d, want 2", de.CurrentRevision)
		}
		equalSnapshots(t, before, snapshot(t, root))
	})
	t.Run("future revision conflicts", func(t *testing.T) {
		err := s.Replace(ctxb(), d.ID, 7, mkDraft(d.ID, 8, minimal("x")))
		de := wantErr(t, err, draft.CategoryConflict, draft.CodeRevisionMismatch, root)
		if de.CurrentRevision != 2 {
			t.Errorf("CurrentRevision = %d", de.CurrentRevision)
		}
	})
	t.Run("interface violations are internal and write nothing", func(t *testing.T) {
		before := snapshot(t, root)
		other := testID(9)
		wantErr(t, s.Replace(ctxb(), d.ID, 2, mkDraft(other, 3, minimal("x"))), draft.CategoryInternal, draft.CodeInternal, root)
		wantErr(t, s.Replace(ctxb(), d.ID, 2, mkDraft(d.ID, 5, minimal("x"))), draft.CategoryInternal, draft.CodeInternal, root)
		wantErr(t, s.Replace(ctxb(), d.ID, 0, mkDraft(d.ID, 1, minimal("x"))), draft.CategoryInternal, draft.CodeInternal, root)
		changed := mkDraft(d.ID, 3, minimal("x"))
		changed.CreatedAt = ts.Add(time.Hour)
		wantErr(t, s.Replace(ctxb(), d.ID, 2, changed), draft.CategoryInternal, draft.CodeInternal, root)
		equalSnapshots(t, before, snapshot(t, root))
	})
	t.Run("unknown id", func(t *testing.T) {
		id := testID(5)
		before := snapshot(t, root)
		wantErr(t, s.Replace(ctxb(), id, 1, mkDraft(id, 2, minimal("x"))), draft.CategoryNotFound, draft.CodeDraftNotFound, root)
		equalSnapshots(t, before, snapshot(t, root)) // no temp file left behind
	})
}

func TestReplaceOnMissingRootCreatesNothing(t *testing.T) {
	s, root := newStore(t)
	id := testID(1)
	wantErr(t, s.Replace(ctxb(), id, 1, mkDraft(id, 2, minimal("x"))), draft.CategoryNotFound, draft.CodeDraftNotFound, root)
	if _, err := os.Lstat(root); err == nil {
		t.Error("Replace created the data root")
	}
}

func TestReplaceRechecksExistenceUnderLock(t *testing.T) {
	s, root := newStore(t)
	d := mkDraft(testID(1), 1, minimal("x"))
	mustCreate(t, s, d)
	// The draft vanishes after the temp file is written but before the lock is taken.
	s.hooks.beforeLock = func() { _ = os.Remove(filepath.Join(root, "drafts", d.ID+".json")) }
	err := s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal("y")))
	wantErr(t, err, draft.CategoryNotFound, draft.CodeDraftNotFound, root)
	assertNoTemps(t, root)
	if _, err := os.Lstat(filepath.Join(root, "drafts", d.ID+".json")); err == nil {
		t.Error("Replace resurrected a missing draft")
	}
}

func assertNoTemps(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "drafts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if tempNameRe.MatchString(e.Name()) {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestInterruptedWriteLeavesPreviousDraftIntact(t *testing.T) {
	s, root := newStore(t)
	d := mkDraft(testID(1), 1, fullContent())
	mustCreate(t, s, d)
	before := snapshot(t, root)

	s.hooks.beforeRename = func() error { return errors.New("injected failure before rename") }
	err := s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal("Interrupted")))
	wantErr(t, err, draft.CategoryIO, draft.CodeIOError, root, "Interrupted")
	equalSnapshots(t, before, snapshot(t, root)) // old file byte-identical, no temp left
	got, err := s.Get(ctxb(), d.ID)
	if err != nil || !reflect.DeepEqual(got, d) {
		t.Fatalf("draft changed: %v", err)
	}

	// An interrupted Create leaves nothing.
	id2 := testID(2)
	err = s.Create(ctxb(), mkDraft(id2, 1, minimal("Never")))
	wantErr(t, err, draft.CategoryIO, draft.CodeIOError, root)
	equalSnapshots(t, before, snapshot(t, root))

	// And the store keeps working afterwards.
	s.hooks.beforeRename = nil
	if err := s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal("After"))); err != nil {
		t.Fatal(err)
	}
}

func TestPostCommitErrorsDoNotFailTheWrite(t *testing.T) {
	s, root := newStore(t)
	var logged atomic.Int32
	s.logf = func(format string, args ...any) {
		logged.Add(1)
		if msg := fmt.Sprintf(format, args...); contains(msg, root) || contains(msg, "Synthetic") {
			t.Errorf("log line leaks payload or path: %q", msg)
		}
	}
	s.hooks.syncDir = func(string) error { return errors.New("injected directory sync failure") }
	d := mkDraft(testID(1), 1, fullContent())
	if err := s.Create(ctxb(), d); err != nil {
		t.Fatalf("Create failed after commit: %v", err)
	}
	if err := s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal("Two"))); err != nil {
		t.Fatalf("Replace failed after commit: %v", err)
	}
	if logged.Load() != 2 {
		t.Errorf("logged %d lines, want 2", logged.Load())
	}
	got, err := s.Get(ctxb(), d.ID)
	if err != nil || got.Revision != 2 {
		t.Fatalf("committed write missing: %v %+v", err, got)
	}
}

func TestContextCancelledBeforeCommit(t *testing.T) {
	s, root := newStore(t)
	ctx, cancel := context.WithCancel(ctxb())
	cancel()
	err := s.Create(ctx, mkDraft(testID(1), 1, minimal("x")))
	wantErr(t, err, draft.CategoryIO, draft.CodeCanceled, root)
	if _, err := os.Lstat(root); err == nil {
		t.Error("cancelled Create created the data root")
	}
	_, err = s.Get(ctx, testID(1))
	wantErr(t, err, draft.CategoryIO, draft.CodeCanceled, root)

	// Cancelled after the temp write, before the lock: nothing committed, temp removed.
	d := mkDraft(testID(2), 1, minimal("y"))
	mustCreate(t, s, d)
	ctx2, cancel2 := context.WithCancel(ctxb())
	s.hooks.beforeLock = cancel2
	err = s.Replace(ctx2, d.ID, 1, mkDraft(d.ID, 2, minimal("z")))
	wantErr(t, err, draft.CategoryIO, draft.CodeCanceled, root)
	assertNoTemps(t, root)
	got, _ := s.Get(ctxb(), d.ID)
	if got.Revision != 1 {
		t.Errorf("revision = %d, want 1", got.Revision)
	}
}

func TestLockTimeoutAndCancellationWhileWaiting(t *testing.T) {
	s, root := newStore(t)
	s.lockTimeout = 150 * time.Millisecond
	d := mkDraft(testID(1), 1, minimal("x"))
	mustCreate(t, s, d)
	before := snapshot(t, root)

	release, err := acquireLock(ctxb(), s.lockPath(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal("y")))
	wantErr(t, err, draft.CategoryIO, draft.CodeLockTimeout, root)
	if el := time.Since(start); el < 100*time.Millisecond || el > 3*time.Second {
		t.Errorf("lock wait took %v", el)
	}

	// Cancellation while waiting is io/canceled, distinct from lock_timeout.
	s.lockTimeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(ctxb(), 100*time.Millisecond)
	defer cancel()
	err = s.Create(ctx, mkDraft(testID(2), 1, minimal("z")))
	wantErr(t, err, draft.CategoryIO, draft.CodeCanceled, root)

	release()
	assertNoTemps(t, root)
	equalSnapshots(t, before, snapshot(t, root))
	if err := s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal("y"))); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

func TestConcurrentReplaceInProcess(t *testing.T) {
	s, _ := newStore(t)
	d := mkDraft(testID(1), 1, minimal("x"))
	mustCreate(t, s, d)
	const n = 16
	var wg sync.WaitGroup
	var ok, conflict atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal(fmt.Sprint("writer ", i))))
			var de *draft.Error
			switch {
			case err == nil:
				ok.Add(1)
			case errors.As(err, &de) && de.Code == draft.CodeRevisionMismatch:
				conflict.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || conflict.Load() != n-1 {
		t.Fatalf("ok=%d conflict=%d, want 1 and %d", ok.Load(), conflict.Load(), n-1)
	}
}

func TestGetMissing(t *testing.T) {
	s, root := newStore(t)
	_, err := s.Get(ctxb(), testID(1)) // no data root at all
	wantErr(t, err, draft.CategoryNotFound, draft.CodeDraftNotFound, root)
	mustCreate(t, s, mkDraft(testID(2), 1, minimal("x")))
	_, err = s.Get(ctxb(), testID(1))
	wantErr(t, err, draft.CategoryNotFound, draft.CodeDraftNotFound, root)
}

func TestEmptyStore(t *testing.T) {
	s, root := newStore(t)
	for name, prep := range map[string]func(){
		"no root":      func() {},
		"empty root":   func() { _ = os.MkdirAll(root, 0o700) },
		"empty drafts": func() { _ = os.MkdirAll(filepath.Join(root, "drafts"), 0o700) },
		"only ignored names": func() {
			dir := filepath.Join(root, "drafts")
			_ = os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o600)
			_ = os.WriteFile(filepath.Join(dir, ".tmp-0123456789abcdef"), []byte("x"), 0o600)
			_ = os.WriteFile(filepath.Join(dir, "notes.json"), []byte("x"), 0o600)
			_ = os.WriteFile(filepath.Join(dir, "DRF_0123456789abcdef0123.json"), []byte("x"), 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			prep()
			res, err := s.List(ctxb())
			if err != nil || len(res.Drafts) != 0 || len(res.Skipped) != 0 || res.Drafts == nil || res.Skipped == nil {
				t.Fatalf("List = %+v, %v", res, err)
			}
			info, err := s.Info(ctxb())
			if err != nil || info.DraftCount != 0 || info.SkippedCount != 0 {
				t.Fatalf("Info = %+v, %v", info, err)
			}
		})
	}
}

func TestInfo(t *testing.T) {
	s, root := newStore(t)
	info, err := s.Info(ctxb())
	if err != nil || info.DataDirExists || info.CrossProcessLock != crossProcessLock {
		t.Fatalf("Info before create = %+v, %v", info, err)
	}
	mustCreate(t, s, mkDraft(testID(1), 1, minimal("x")))
	mustCreate(t, s, mkDraft(testID(2), 1, minimal("y")))
	_ = os.WriteFile(filepath.Join(root, "drafts", testID(3)+".json"), []byte("{broken"), 0o600)
	info, err = s.Info(ctxb())
	if err != nil {
		t.Fatal(err)
	}
	if !info.DataDirExists || info.DraftCount != 2 || info.SkippedCount != 1 {
		t.Fatalf("Info = %+v", info)
	}
	if runtime.GOOS != "windows" && !info.PrivatePermissions {
		t.Error("PrivatePermissions = false for a store this package created")
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if !info.CrossProcessLock {
			t.Error("CrossProcessLock must be true on darwin and linux")
		}
		if err := os.Chmod(root, 0o755); err != nil {
			t.Fatal(err)
		}
		info, _ = s.Info(ctxb())
		if info.PrivatePermissions {
			t.Error("PrivatePermissions = true for a 0755 root")
		}
		_ = os.Chmod(root, 0o700)
	}
}

func TestPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on windows")
	}
	s, root := newStore(t)
	mustCreate(t, s, mkDraft(testID(1), 1, minimal("x")))
	if err := s.Replace(ctxb(), testID(1), 1, mkDraft(testID(1), 2, minimal("y"))); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{
		root:                          0o700,
		filepath.Join(root, "drafts"): 0o700,
		filepath.Join(root, ".lock"):  0o600,
		filepath.Join(root, "drafts", testID(1)+".json"): 0o600,
	} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", filepath.Base(p), got, want)
		}
	}
}

func TestExistingRootPermissionsAreNotChanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on windows")
	}
	s, root := newStore(t)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(root, 0o755)
	mustCreate(t, s, mkDraft(testID(1), 1, minimal("x")))
	if fi, _ := os.Stat(root); fi.Mode().Perm() != 0o755 {
		t.Errorf("existing root was chmodded to %o", fi.Mode().Perm())
	}
	info, _ := s.Info(ctxb())
	if info.PrivatePermissions {
		t.Error("PrivatePermissions must report the broader existing root")
	}
}

func TestReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on windows")
	}
	skipIfRoot(t)
	s, root := newStore(t)
	d := mkDraft(testID(1), 1, minimal("x"))
	mustCreate(t, s, d)
	dir := filepath.Join(root, "drafts")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)

	wantErr(t, s.Create(ctxb(), mkDraft(testID(2), 1, minimal("y"))), draft.CategoryIO, draft.CodeIOError, root)
	wantErr(t, s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal("z"))), draft.CategoryIO, draft.CodeIOError, root)
	if got, err := s.Get(ctxb(), d.ID); err != nil || got.Revision != 1 {
		t.Fatalf("read of a read-only store: %v", err)
	}
	// A read-only parent: the root cannot be created.
	ro := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(ro, 0o700)
	s2, _ := Open(filepath.Join(ro, "data"), Options{})
	wantErr(t, s2.Create(ctxb(), mkDraft(testID(3), 1, minimal("x"))), draft.CategoryIO, draft.CodeDataDirUnavailable, ro)

	// An unreadable draft directory fails List and Get with io, not a panic or empty result.
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	_, err := s.List(ctxb())
	wantErr(t, err, draft.CategoryIO, draft.CodeIOError, root)
}

func TestCorruptAndUnsupportedFiles(t *testing.T) {
	s, root := newStore(t)
	good := mkDraft(testID(1), 1, minimal("Good"))
	mustCreate(t, s, good)
	dir := filepath.Join(root, "drafts")
	goodJSON := string(readFile(t, filepath.Join(dir, good.ID+".json")))
	put := func(id, content string) { _ = os.WriteFile(filepath.Join(dir, id+".json"), []byte(content), 0o600) }
	repl := func(old, new string) string {
		if !contains(goodJSON, old) {
			t.Fatalf("fixture lacks %q", old)
		}
		return replaceOnce(goodJSON, old, new)
	}
	idOf := func(n int) string { return testID(n) }

	cases := []struct {
		id        string
		content   string
		cat       draft.Category
		code      string
		isSymlink bool
	}{
		{idOf(10), "", draft.CategoryCorrupt, draft.CodeInvalidJSON, false},
		{idOf(11), "{not json", draft.CategoryCorrupt, draft.CodeInvalidJSON, false},
		{idOf(12), `{"title":"no version"}`, draft.CategoryCorrupt, draft.CodeInvalidContent, false},
		{idOf(13), `{"schema_version":"1"}`, draft.CategoryCorrupt, draft.CodeInvalidContent, false},
		{idOf(14), `{"schema_version":1.5}`, draft.CategoryCorrupt, draft.CodeInvalidContent, false},
		{idOf(15), `{"schema_version":2,"title":"future"}`, draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported, false},
		{idOf(16), `{"schema_version":0}`, draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported, false},
		{idOf(17), repl(`"id": "`+good.ID+`"`, `"id": "`+idOf(99)+`"`), draft.CategoryCorrupt, draft.CodeIDMismatch, false},
		{idOf(18), repl(`"title": "Good"`, `"title": "Good", "surprise": 1`), draft.CategoryCorrupt, draft.CodeInvalidContent, false},
		{idOf(19), repl(`"status": "draft"`, `"status": "reviewed"`), draft.CategoryCorrupt, draft.CodeInvalidContent, false},
		{idOf(20), repl(`"revision": 1`, `"revision": 0`), draft.CategoryCorrupt, draft.CodeInvalidContent, false},
		{idOf(21), goodJSON + `{"trailing":true}`, draft.CategoryCorrupt, draft.CodeInvalidJSON, false},
		{idOf(22), string(make([]byte, draft.MaxStoredFileBytes+1)), draft.CategoryCorrupt, draft.CodeFileTooLarge, false},
	}
	for _, c := range cases {
		put(c.id, c.content)
	}
	// Wrong ids need their own id: fix the two cases whose id field must equal the file name.
	put(idOf(18), replaceOnce(replaceOnce(goodJSON, good.ID, idOf(18)), `"title": "Good"`, `"title": "Good", "surprise": 1`))
	put(idOf(19), replaceOnce(replaceOnce(goodJSON, good.ID, idOf(19)), `"status": "draft"`, `"status": "reviewed"`))
	put(idOf(20), replaceOnce(replaceOnce(goodJSON, good.ID, idOf(20)), `"revision": 1`, `"revision": 0`))
	put(idOf(21), replaceOnce(goodJSON, good.ID, idOf(21))+`{"trailing":true}`)

	// A directory with a draft-like name, and a symlink to a file outside the root.
	if err := os.Mkdir(filepath.Join(dir, idOf(30)+".json"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	_ = os.WriteFile(outside, []byte(replaceOnce(goodJSON, good.ID, idOf(31))), 0o600)
	symlinkOK := os.Symlink(outside, filepath.Join(dir, idOf(31)+".json")) == nil

	before := snapshot(t, dir)

	for _, c := range cases {
		_, err := s.Get(ctxb(), c.id)
		wantErr(t, err, c.cat, c.code, root, "no version", "future")
		// Replace never overwrites a damaged draft.
		err = s.Replace(ctxb(), c.id, 1, mkDraft(c.id, 2, minimal("Overwrite")))
		wantErr(t, err, c.cat, c.code, root, "Overwrite")
	}
	_, err := s.Get(ctxb(), idOf(30))
	wantErr(t, err, draft.CategoryCorrupt, draft.CodeNotRegularFile, root)
	if symlinkOK {
		_, err = s.Get(ctxb(), idOf(31))
		wantErr(t, err, draft.CategoryCorrupt, draft.CodeNotRegularFile, root)
		err = s.Replace(ctxb(), idOf(31), 1, mkDraft(idOf(31), 2, minimal("Overwrite")))
		wantErr(t, err, draft.CategoryCorrupt, draft.CodeNotRegularFile, root)
		if string(readFile(t, outside)) != replaceOnce(goodJSON, good.ID, idOf(31)) {
			t.Error("a symlinked draft target was written through")
		}
	}

	res, err := s.List(ctxb())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Drafts) != 1 || res.Drafts[0].ID != good.ID {
		t.Fatalf("good drafts = %+v", res.Drafts)
	}
	wantSkipped := len(cases) + 1
	if symlinkOK {
		wantSkipped++
	}
	if len(res.Skipped) != wantSkipped {
		t.Fatalf("skipped %d entries, want %d: %+v", len(res.Skipped), wantSkipped, res.Skipped)
	}
	byID := map[string]draft.SkippedEntry{}
	for _, e := range res.Skipped {
		byID[e.ID] = e
		if contains(e.Message, root) || contains(e.Message, "future") {
			t.Errorf("skipped message leaks: %q", e.Message)
		}
	}
	for _, c := range cases {
		if byID[c.id].Category != c.cat {
			t.Errorf("%s: category %q, want %q", c.id, byID[c.id].Category, c.cat)
		}
	}
	if byID[idOf(30)].Category != draft.CategoryCorrupt {
		t.Errorf("directory entry not reported corrupt: %+v", byID[idOf(30)])
	}

	// Nothing was deleted, repaired, rewritten or moved.
	equalSnapshots(t, before, snapshot(t, dir))
	assertNoTemps(t, root)
}

func replaceOnce(s, old, new string) string {
	i := indexOf(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestSymlinkedDraftsDirectoryIsRefused(t *testing.T) {
	s, root := newStore(t)
	outside := t.TempDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "drafts")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	d := mkDraft(testID(1), 1, minimal("x"))
	wantErr(t, s.Create(ctxb(), d), draft.CategoryIO, draft.CodeDataDirUnavailable, root, outside)
	_, err := s.Get(ctxb(), d.ID)
	wantErr(t, err, draft.CategoryIO, draft.CodeDataDirUnavailable, root, outside)
	_, err = s.List(ctxb())
	wantErr(t, err, draft.CategoryIO, draft.CodeDataDirUnavailable, root, outside)
	wantErr(t, s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal("y"))), draft.CategoryIO, draft.CodeDataDirUnavailable, root, outside)
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("files were written through the symlink: %v", entries)
	}
}

func TestSymlinkedLockFileIsRefused(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("lock file is only used with flock")
	}
	s, root := newStore(t)
	if err := os.MkdirAll(filepath.Join(root, "drafts"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Symlink(target, filepath.Join(root, ".lock")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	err := s.Create(ctxb(), mkDraft(testID(1), 1, minimal("x")))
	wantErr(t, err, draft.CategoryIO, draft.CodeDataDirUnavailable, root, target)
	if _, err := os.Lstat(target); err == nil {
		t.Error("the lock file was created through the symlink")
	}
	assertNoTemps(t, root)
}

func TestStoreCapIsEnforcedUnderTheLock(t *testing.T) {
	s, root := newStore(t)
	mustCreate(t, s, mkDraft(testID(0), 1, minimal("seed"))) // creates the directories
	dir := filepath.Join(root, "drafts")
	// Fill to one below the cap with files of any content: every matching name counts.
	for i := 1; i < draft.MaxStoredDrafts-1; i++ {
		if err := os.WriteFile(filepath.Join(dir, testID(i)+".json"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Names that do not match never count.
	_ = os.WriteFile(filepath.Join(dir, ".tmp-0123456789abcdef"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "other.json"), []byte("x"), 0o600)

	var wg sync.WaitGroup
	var ok, full atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Create(ctxb(), mkDraft(testID(2000+i), 1, minimal("x")))
			var de *draft.Error
			switch {
			case err == nil:
				ok.Add(1)
			case errors.As(err, &de) && de.Code == draft.CodeStoreFull && de.Category == draft.CategoryLimitExceeded:
				full.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || full.Load() != 7 {
		t.Fatalf("ok=%d full=%d, want exactly one create to fit under the cap", ok.Load(), full.Load())
	}
	if n, _ := s.countFiles(); n != draft.MaxStoredDrafts {
		t.Fatalf("store holds %d files, want %d", n, draft.MaxStoredDrafts)
	}
	// Replace still works on a full store.
	if err := s.Replace(ctxb(), testID(0), 1, mkDraft(testID(0), 2, minimal("seed2"))); err != nil {
		t.Fatalf("Replace on a full store: %v", err)
	}
}

func TestStaleTempFiles(t *testing.T) {
	s, root := newStore(t)
	mustCreate(t, s, mkDraft(testID(1), 1, minimal("x")))
	dir := filepath.Join(root, "drafts")
	old := filepath.Join(dir, ".tmp-aaaaaaaaaaaaaaaa")
	fresh := filepath.Join(dir, ".tmp-bbbbbbbbbbbbbbbb")
	foreign := filepath.Join(dir, ".tmp-notours")
	oldDraft := filepath.Join(dir, testID(7)+".json")
	for _, p := range []string{old, fresh, foreign, oldDraft} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	longAgo := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{old, foreign, oldDraft} {
		if err := os.Chtimes(p, longAgo, longAgo); err != nil {
			t.Fatal(err)
		}
	}
	// Stale temp files never appear in List, count toward the cap, or get promoted.
	res, _ := s.List(ctxb())
	if len(res.Drafts) != 1 || len(res.Skipped) != 1 { // the draft and the junk-named draft file
		t.Fatalf("List = %+v", res)
	}
	if err := s.Replace(ctxb(), testID(1), 1, mkDraft(testID(1), 2, minimal("y"))); err != nil {
		t.Fatal(err)
	}
	exists := func(p string) bool { _, err := os.Lstat(p); return err == nil }
	if exists(old) {
		t.Error("a stale .tmp-* file of the store's own naming was not swept")
	}
	if !exists(fresh) {
		t.Error("a recent temp file (possibly a live writer's) was removed")
	}
	if !exists(foreign) {
		t.Error("a file that is not the store's temp naming was removed")
	}
	if !exists(oldDraft) {
		t.Error("an old draft file was removed")
	}
}

func TestListIsDeterministic(t *testing.T) {
	s, _ := newStore(t)
	for _, n := range []int{5, 3, 9, 1} {
		mustCreate(t, s, mkDraft(testID(n), 1, minimal(fmt.Sprint(n))))
	}
	first, _ := s.List(ctxb())
	for i := 0; i < 5; i++ {
		again, _ := s.List(ctxb())
		if !reflect.DeepEqual(first, again) {
			t.Fatal("List order is not stable")
		}
	}
	var ids []string
	for _, d := range first.Drafts {
		ids = append(ids, d.ID)
	}
	if !reflect.DeepEqual(ids, []string{testID(1), testID(3), testID(5), testID(9)}) {
		t.Fatalf("order = %v", ids)
	}
}

func TestReadsDoNotModifyTheStore(t *testing.T) {
	s, root := newStore(t)
	mustCreate(t, s, mkDraft(testID(1), 1, fullContent()))
	_ = os.WriteFile(filepath.Join(root, "drafts", testID(2)+".json"), []byte("{broken"), 0o600)
	before := snapshot(t, root)
	_, _ = s.Get(ctxb(), testID(1))
	_, _ = s.Get(ctxb(), testID(2))
	_, _ = s.List(ctxb())
	_, _ = s.Info(ctxb())
	equalSnapshots(t, before, snapshot(t, root))
}

func TestOversizeEncodingIsRefused(t *testing.T) {
	s, root := newStore(t)
	c := minimal("x")
	c.Notes = repeat("n", draft.MaxStoredFileBytes)
	err := s.Create(ctxb(), mkDraft(testID(1), 1, c))
	wantErr(t, err, draft.CategoryInternal, draft.CodeEncodedTooLarge, root)
	if _, err := os.Lstat(root); err == nil {
		t.Error("an oversize create touched the disk")
	}
}

func repeat(s string, n int) string {
	b := make([]byte, 0, n*len(s))
	for i := 0; i < n; i++ {
		b = append(b, s...)
	}
	return string(b)
}

// A draft file that disappears between ReadDir and the read is not an unreadable entry: it is not
// reported as skipped, and the rest of the list is unaffected.
func TestListSilentlySkipsFileThatVanishesMidList(t *testing.T) {
	s, root := newStore(t)
	for _, n := range []int{1, 2, 3} {
		mustCreate(t, s, mkDraft(testID(n), 1, minimal(fmt.Sprint(n))))
	}
	s.hooks.afterReadDir = func() {
		if err := os.Remove(filepath.Join(root, "drafts", testID(2)+".json")); err != nil {
			t.Fatal(err)
		}
	}
	res, err := s.List(ctxb())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range res.Drafts {
		ids = append(ids, d.ID)
	}
	if !reflect.DeepEqual(ids, []string{testID(1), testID(3)}) {
		t.Fatalf("listed %v", ids)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("a vanished file was reported as skipped: %+v", res.Skipped)
	}
	// A genuinely unreadable entry is still reported.
	s.hooks.afterReadDir = nil
	if err := os.WriteFile(filepath.Join(root, "drafts", testID(4)+".json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, _ = s.List(ctxb())
	if len(res.Skipped) != 1 || res.Skipped[0].ID != testID(4) || res.Skipped[0].Category != draft.CategoryCorrupt {
		t.Fatalf("skipped = %+v", res.Skipped)
	}
}

// Writing into a read-only drafts directory gives a path-free message with a writability hint.
func TestReadOnlyDraftsDirectoryHint(t *testing.T) {
	skipIfRoot(t)
	s, root := newStore(t)
	mustCreate(t, s, mkDraft(testID(1), 1, minimal("one")))
	dir := filepath.Join(root, "drafts")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	err := s.Create(ctxb(), mkDraft(testID(2), 1, minimal("two")))
	de := wantErr(t, err, draft.CategoryIO, draft.CodeIOError, root)
	if !contains(de.Message, "check that it is writable") {
		t.Fatalf("message lacks the hint: %q", de.Message)
	}
	if _, err := s.Get(ctxb(), testID(2)); err == nil {
		t.Fatal("a failed create left a draft")
	}
}
