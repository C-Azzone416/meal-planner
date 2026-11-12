// Package localstore is the file-backed draft.Store: one versioned JSON file per draft under a
// private data directory. It is specified by docs/local-draft-contract.md (section 10).
//
// Layout under the data root: .lock, and drafts/<id>.json. Writes go through a same-directory
// temp file, fsync and atomic rename; the revision check and rename run under an exclusive lock
// (flock where available). Reads take no lock. The store logs only fixed text (no content or
// paths), and error messages never carry paths or recipe content.
package localstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

const (
	dirName       = "drafts"
	lockName      = ".lock"
	tempPrefix    = ".tmp-"
	defaultLockTO = 5 * time.Second
)

// Options configures Open.
type Options struct {
	LockTimeout time.Duration // 0 means 5 seconds
}

// Store implements draft.Store on the local filesystem.
type Store struct {
	root        string
	lockTimeout time.Duration
	hooks       hooks
	// logf reports non-fatal events. It is called with fixed text only, never recipe content or
	// paths. The default writes to stderr, which is never a protocol stream.
	logf func(format string, args ...any)
}

// hooks are test seams for failure injection.
type hooks struct {
	// beforeRename runs under the lock, immediately before the commit rename.
	beforeRename func() error
	// beforeLock runs after the temp file is written and before the lock is taken.
	beforeLock func()
	// syncDir replaces the directory sync after the commit rename.
	syncDir func(path string) error
	// afterReadDir runs in List after the directory is read and before its entries are read.
	afterReadDir func()
}

var _ draft.Store = (*Store)(nil)

// Open returns a store rooted at dataDir, which must be absolute. It creates nothing on disk:
// directories and the lock file appear on the first Create.
func Open(dataDir string, opts Options) (*Store, error) {
	if runtime.GOOS == "js" || runtime.GOOS == "plan9" {
		return nil, ioError(draft.CodeDataDirUnavailable, "secure local storage is unavailable on this platform", nil)
	}
	if !filepath.IsAbs(dataDir) {
		return nil, newError(draft.CategoryInvalidArgument, draft.CodeInvalidDataDir, "the data directory must be an absolute path", nil)
	}
	to := opts.LockTimeout
	if to <= 0 {
		to = defaultLockTO
	}
	return &Store{root: filepath.Clean(dataDir), lockTimeout: to, logf: stderrf}, nil
}

func (s *Store) draftsDir() string         { return filepath.Join(s.root, dirName) }
func (s *Store) lockPath() string          { return filepath.Join(s.root, lockName) }
func (s *Store) filePath(id string) string { return filepath.Join(s.draftsDir(), id+".json") }

// openDirs pins both directories for the entire operation. All subsequent filesystem
// operations are relative to these handles, never to a re-resolved pathname. The
// user-selected root may be a symlink; nothing below it may escape the boundary.
func (s *Store) openDirs(create bool) (*os.Root, *os.Root, error) {
	if create {
		if err := os.MkdirAll(s.root, 0700); err != nil {
			return nil, nil, ioError(draft.CodeDataDirUnavailable, "cannot create the data directory", err)
		}
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*os.Root, *os.Root, error) { root.Close(); return nil, nil, err }
	if create {
		if err := root.Mkdir(dirName, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return fail(err)
		}
	}
	fi, err := root.Lstat(dirName)
	if err != nil {
		return fail(err)
	}
	if !fi.IsDir() {
		return fail(errors.New("drafts location is not a plain directory"))
	}
	dir, err := root.OpenRoot(dirName)
	if err != nil {
		return fail(err)
	}
	actual, err := dir.Stat(".")
	if err != nil || !os.SameFile(fi, actual) {
		dir.Close()
		return fail(errors.New("drafts directory changed while opening"))
	}
	return root, dir, nil
}

// Create implements draft.Store.
func (s *Store) Create(ctx context.Context, d draft.Draft) error {
	if err := ctx.Err(); err != nil {
		return ctxError(err)
	}
	if !idRe.MatchString(d.ID) {
		return invalidID()
	}
	if err := checkEnvelope(d); err != nil {
		return err
	}
	if d.Revision != 1 {
		return internal("a new draft must have revision 1", nil)
	}
	data, err := s.prepare(d)
	if err != nil {
		return err
	}
	root, dir, err := s.openDirs(true)
	if err != nil {
		return ioError(draft.CodeDataDirUnavailable, "cannot open the drafts directory", err)
	}
	defer root.Close()
	defer dir.Close()
	return s.commit(ctx, root, dir, d.ID, data, func() error {
		n, err := countFiles(dir)
		if err != nil {
			return err
		}
		if n >= draft.MaxStoredDrafts {
			return newError(draft.CategoryLimitExceeded, draft.CodeStoreFull, "the draft store is full", nil)
		}
		if _, err := dir.Lstat(d.ID + ".json"); err == nil {
			return newError(draft.CategoryConflict, draft.CodeAlreadyExists, "a draft with that id already exists", nil)
		} else if !errors.Is(err, os.ErrNotExist) {
			return ioError(draft.CodeIOError, "cannot check for an existing draft", err)
		}
		return nil
	})
}

// Replace implements draft.Store.
func (s *Store) Replace(ctx context.Context, id string, expectedRevision int64, next draft.Draft) error {
	if err := ctx.Err(); err != nil {
		return ctxError(err)
	}
	if !idRe.MatchString(id) {
		return invalidID()
	}
	if expectedRevision >= draft.MaxRevision {
		return newError(draft.CategoryLimitExceeded, draft.CodeRevisionLimit, "the draft is at the maximum revision and cannot be updated", nil)
	}
	if next.ID != id || expectedRevision < 1 || next.Revision != expectedRevision+1 {
		return internal("replacement does not match the id and expected revision", nil)
	}
	if err := checkEnvelope(next); err != nil {
		return err
	}
	root, dir, err := s.openDirs(false)
	if errors.Is(err, os.ErrNotExist) {
		return notFound()
	}
	if err != nil {
		return ioError(draft.CodeDataDirUnavailable, "cannot open the drafts directory", err)
	}
	defer root.Close()
	defer dir.Close()
	data, err := s.prepare(next)
	if err != nil {
		return err
	}
	return s.commit(ctx, root, dir, id, data, func() error {
		cur, err := readDraftFile(dir, id+".json", id)
		if err != nil {
			return err
		}
		if cur.Revision != expectedRevision {
			e := newError(draft.CategoryConflict, draft.CodeRevisionMismatch, "the draft changed since the revision you based this update on; read it again and retry", nil)
			e.CurrentRevision = cur.Revision
			return e
		}
		if !next.CreatedAt.Equal(cur.CreatedAt) {
			return internal("replacement changes created_at", nil)
		}
		return nil
	})
}

func checkEnvelope(d draft.Draft) error {
	if d.SchemaVersion != draft.SchemaVersion || d.Status != draft.StatusDraft {
		return internal("draft has an invalid schema version or status", nil)
	}
	if !validEnvelope(d, time.Now()) {
		return internal("draft has an invalid revision or timestamp", nil)
	}
	return nil
}

// prepare encodes d, enforces the stored-file size cap and validates d's content.
func (s *Store) prepare(d draft.Draft) ([]byte, error) {
	data, err := encode(d)
	if err != nil {
		return nil, internal("cannot encode the draft", err)
	}
	if len(data) > draft.MaxStoredFileBytes {
		return nil, newError(draft.CategoryInternal, draft.CodeEncodedTooLarge, "the encoded draft exceeds the stored file size limit", nil)
	}
	// Never write what the decoder would refuse to read: apply the same shared validator.
	if !draft.Validate(d.Content).StructurallyValid {
		return nil, internal("draft has invalid content", nil)
	}
	return data, nil
}

// commit writes data to a temp file, takes the lock, runs check, then renames the temp file over
// the target. Everything before the rename leaves the target untouched; the temp file is removed on
// any failure.
func (s *Store) commit(ctx context.Context, root, dir *os.Root, id string, data []byte, check func() error) (err error) {
	tmp, err := s.writeTemp(dir, data)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = dir.Remove(tmp)
		}
	}()
	if h := s.hooks.beforeLock; h != nil {
		h()
	}
	release, err := acquireRootLock(ctx, root, s.lockTimeout)
	if err != nil {
		return asDraftError(err)
	}
	defer release()
	s.sweepTemps(dir)
	if err := check(); err != nil {
		return asDraftError(err)
	}
	if err := ctx.Err(); err != nil {
		return ctxError(err)
	}
	if h := s.hooks.beforeRename; h != nil {
		if err := h(); err != nil {
			return ioError(draft.CodeIOError, "the draft could not be written", err)
		}
	}
	if err := dir.Rename(tmp, id+".json"); err != nil {
		return ioError(draft.CodeIOError, "the draft could not be written", err)
	}
	// The rename is the commit point. Nothing after it may report failure.
	var syncErr error
	if s.hooks.syncDir != nil {
		syncErr = s.hooks.syncDir(s.draftsDir())
	} else if runtime.GOOS != "windows" {
		f, err := dir.Open(".")
		syncErr = err
		if err == nil {
			syncErr = f.Sync()
			f.Close()
		}
	}
	if syncErr != nil {
		s.logf("localstore: directory sync after commit failed; the draft was written")
	}
	return nil
}

// stderrf writes a diagnostic line to stderr.
func stderrf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// staleTempAge is how old one of the store's own temp files must be before it is swept.
const staleTempAge = 24 * time.Hour

var tempNameRe = regexp.MustCompile(`^\.tmp-[0-9a-f]{16}$`)

// sweepTemps removes the store's own stale temp files (a crash can leave them). It removes only
// regular files matching the temp-name pattern and older than 24 hours, never anything else, and
// ignores errors. Called under the lock so a live writer's fresh temp file is never touched.
func (s *Store) sweepTemps(dir *os.Root) {
	entries, err := fs.ReadDir(dir.FS(), ".")
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleTempAge)
	for _, e := range entries {
		if !tempNameRe.MatchString(e.Name()) {
			continue
		}
		p := e.Name()
		if fi, err := dir.Lstat(p); err == nil && fi.Mode().IsRegular() && fi.ModTime().Before(cutoff) {
			_ = dir.Remove(p)
		}
	}
}

// writeTemp writes data to a new 0600 file in the drafts directory and syncs it.
func (s *Store) writeTemp(dir *os.Root, data []byte) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", internal("cannot generate a temp file name", err)
	}
	path := tempPrefix + hex.EncodeToString(b[:])
	f, err := dir.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", ioError(draft.CodeIOError, "cannot write to the drafts directory; check that it is writable", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = dir.Remove(path)
		return "", ioError(draft.CodeIOError, "cannot write the draft", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = dir.Remove(path)
		return "", ioError(draft.CodeIOError, "cannot flush the draft to disk", err)
	}
	if err := f.Close(); err != nil {
		_ = dir.Remove(path)
		return "", ioError(draft.CodeIOError, "cannot write the draft", err)
	}
	return path, nil
}

// countFiles counts entries named like draft files. Called under the lock.
func (s *Store) countFiles() (int, error) {
	root, dir, err := s.openDirs(false)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	defer dir.Close()
	return countFiles(dir)
}
func countFiles(dir *os.Root) (int, error) {
	entries, err := fs.ReadDir(dir.FS(), ".")
	if err != nil {
		return 0, ioError(draft.CodeIOError, "cannot read the drafts directory", err)
	}
	n := 0
	for _, e := range entries {
		if fileNameRe.MatchString(e.Name()) {
			n++
		}
	}
	return n, nil
}

// Get implements draft.Store.
func (s *Store) Get(ctx context.Context, id string) (draft.Draft, error) {
	if err := ctx.Err(); err != nil {
		return draft.Draft{}, ctxError(err)
	}
	if !idRe.MatchString(id) {
		return draft.Draft{}, invalidID()
	}
	root, dir, err := s.openDirs(false)
	if errors.Is(err, os.ErrNotExist) {
		return draft.Draft{}, notFound()
	}
	if err != nil {
		return draft.Draft{}, ioError(draft.CodeDataDirUnavailable, "cannot open the drafts directory", err)
	}
	defer root.Close()
	defer dir.Close()
	d, err := readDraftFile(dir, id+".json", id)
	if err != nil {
		return draft.Draft{}, asDraftError(err)
	}
	return d, nil
}

// List implements draft.Store. Entries that cannot be used are described, never repaired.
func (s *Store) List(ctx context.Context) (draft.ListResult, error) {
	res := draft.ListResult{Drafts: []draft.Draft{}, Skipped: []draft.SkippedEntry{}}
	root, dir, err := s.openDirs(false)
	if errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return res, ioError(draft.CodeIOError, "cannot read the drafts directory", err)
		}
		return res, ioError(draft.CodeDataDirUnavailable, "cannot open the drafts directory", err)
	}
	defer root.Close()
	defer dir.Close()
	entries, err := fs.ReadDir(dir.FS(), ".")
	if err != nil {
		return res, ioError(draft.CodeIOError, "cannot read the drafts directory", err)
	}
	if h := s.hooks.afterReadDir; h != nil {
		h()
	}
	for _, e := range entries { // sorted by name, so the result is deterministic
		m := fileNameRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return res, ctxError(err)
		}
		id := m[1]
		d, err := readDraftFile(dir, id+".json", id)
		if err != nil {
			de := asDraftError(err)
			if de.Category == draft.CategoryNotFound {
				continue // the file vanished between ReadDir and the read: not an unreadable entry
			}
			res.Skipped = append(res.Skipped, draft.SkippedEntry{ID: id, Category: de.Category, Message: de.Message})
			continue
		}
		res.Drafts = append(res.Drafts, d)
	}
	return res, nil
}

// Info implements draft.Store.
func (s *Store) Info(ctx context.Context) (draft.StoreInfo, error) {
	info := draft.StoreInfo{CrossProcessLock: crossProcessLock}
	fi, err := os.Stat(s.root)
	switch {
	case err == nil && fi.IsDir():
		info.DataDirExists = true
	case err == nil || errors.Is(err, os.ErrNotExist):
	default:
		return info, ioError(draft.CodeDataDirUnavailable, "the data directory cannot be read", err)
	}
	info.PrivatePermissions = s.privatePermissions()
	res, err := s.List(ctx)
	if err != nil {
		return info, err
	}
	info.DraftCount = len(res.Drafts)
	info.SkippedCount = len(res.Skipped)
	return info, nil
}

// privatePermissions reports that the data root and drafts directory have no group/other bits.
// Where permission bits are not meaningful it is false. A directory that does not exist yet will
// be created 0700, so it does not count against the answer.
func (s *Store) privatePermissions() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	for _, p := range []string{s.root, s.draftsDir()} {
		fi, err := os.Stat(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return false
		}
	}
	return true
}
