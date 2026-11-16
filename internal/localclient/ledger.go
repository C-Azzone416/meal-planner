package localclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

const (
	ledgerDirName = "outbound-ledger-v1"
	ledgerVersion = 1
)

// Ledger keeps only routing hashes, identifiers and receipt metadata. Recipe
// content and credentials are never written to it.
type Ledger struct {
	root string
	now  func() time.Time
}

func OpenLedger(dataDir string) (*Ledger, error) {
	if !filepath.IsAbs(dataDir) {
		return nil, ErrUnavailable
	}
	return &Ledger{root: filepath.Join(filepath.Clean(dataDir), ledgerDirName), now: time.Now}, nil
}

func scopeFilename(scope ledgerScope) (string, error) {
	raw, err := json.Marshal(scope)
	if err != nil {
		return "", ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]) + ".json", nil
}

func newOperationKey() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", ErrUnavailable
	}
	return "snd_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func (l *Ledger) ensureDir() error {
	if err := os.MkdirAll(l.root, 0700); err != nil {
		return ErrUnavailable
	}
	info, err := os.Lstat(l.root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return ErrUnavailable
	}
	return nil
}

func (l *Ledger) withLock(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := l.ensureDir(); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(l.root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return ErrUnavailable
	}
	defer lock.Close()
	if err := acquireLedgerLock(ctx, lock); err != nil {
		return err
	}
	defer releaseLedgerLock(lock)
	return fn()
}

func (l *Ledger) Begin(ctx context.Context, scope ledgerScope) (ledgerRecord, error) {
	var record ledgerRecord
	name, err := scopeFilename(scope)
	if err != nil {
		return record, err
	}
	err = l.withLock(ctx, func() error {
		path := filepath.Join(l.root, name)
		if existing, readErr := readLedgerRecord(path); readErr == nil {
			if existing.Scope != scope {
				return ErrUnavailable
			}
			record = existing
			return nil
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		key, keyErr := newOperationKey()
		if keyErr != nil {
			return keyErr
		}
		record = ledgerRecord{Version: ledgerVersion, Scope: scope, OperationKey: key, State: "new", UpdatedAt: l.now().UTC().Truncate(time.Second)}
		return l.writeRecord(path, record)
	})
	return record, err
}

func (l *Ledger) Update(ctx context.Context, next ledgerRecord) error {
	name, err := scopeFilename(next.Scope)
	if err != nil {
		return err
	}
	return l.withLock(ctx, func() error {
		path := filepath.Join(l.root, name)
		current, readErr := readLedgerRecord(path)
		if readErr != nil || current.OperationKey != next.OperationKey || current.Scope != next.Scope {
			return ErrUnavailable
		}
		next.Version = ledgerVersion
		next.UpdatedAt = l.now().UTC().Truncate(time.Second)
		return l.writeRecord(path, next)
	})
}

func readLedgerRecord(path string) (ledgerRecord, error) {
	var record ledgerRecord
	file, err := os.Open(path)
	if err != nil {
		return record, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 2 || info.Size() > 16*1024 {
		return record, ErrUnavailable
	}
	decoder := json.NewDecoder(io.LimitReader(file, 16*1024+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || decoder.Decode(new(any)) != io.EOF || !validLedgerRecord(record) {
		return ledgerRecord{}, ErrUnavailable
	}
	return record, nil
}

func validLedgerRecord(r ledgerRecord) bool {
	return r.Version == ledgerVersion && r.OperationKey != "" &&
		r.Scope.Environment == PreviewEnvironment && r.Scope.APIOrigin == PreviewOrigin &&
		safeBindingID(r.Scope.BindingID) && draft.ValidID(r.Scope.LocalID) &&
		r.Scope.Revision > 0 && len(r.Scope.ContentHash) == 64
}

func (l *Ledger) writeRecord(path string, record ledgerRecord) error {
	if !validLedgerRecord(record) {
		return ErrUnavailable
	}
	raw, err := json.Marshal(record)
	if err != nil || bytes.Contains(raw, []byte("Authorization")) {
		return ErrUnavailable
	}
	tmp, err := os.CreateTemp(l.root, ".tmp-ledger-")
	if err != nil {
		return ErrUnavailable
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(raw)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return ErrUnavailable
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return ErrUnavailable
	}
	committed = true
	if dir, openErr := os.Open(l.root); openErr == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
