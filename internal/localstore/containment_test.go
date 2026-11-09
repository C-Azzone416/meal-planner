package localstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Swap the parent after discovery, not merely the final file. Reads must retain
// the original snapshot directory; writes, locking and cleanup must also stay
// attached to the original handles, including when the user-selected root moves.
func TestOperationsStayInPinnedDirectories(t *testing.T) {
	for _, target := range []string{"drafts", "root"} {
		for _, stage := range []string{"list", "before-lock", "before-rename", "failed-rename"} {
			t.Run(target+"/"+stage, func(t *testing.T) {
				s, root := newStore(t)
				outside, outsideRoot := newStore(t)
				d := mkDraft(testID(1), 1, minimal("original"))
				mustCreate(t, s, d)
				mustCreate(t, outside, mkDraft(d.ID, 1, minimal("outside")))
				stale := ".tmp-0123456789abcdef"
				stalePath := filepath.Join(outsideRoot, "drafts", stale)
				if err := os.WriteFile(stalePath, []byte("outside sentinel"), 0600); err != nil {
					t.Fatal(err)
				}
				old := time.Now().Add(-48 * time.Hour)
				if err := os.Chtimes(stalePath, old, old); err != nil {
					t.Fatal(err)
				}
				source, dest := filepath.Join(root, "drafts"), filepath.Join(outsideRoot, "drafts")
				if target == "root" {
					source, dest = root, outsideRoot
				}
				moved := source + "-original"
				swap := func() {
					if err := os.Rename(source, moved); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(dest, source); err != nil {
						t.Fatal(err)
					}
				}
				switch stage {
				case "list":
					s.hooks.afterReadDir = swap
					got, err := s.List(ctxb())
					if err != nil || len(got.Drafts) != 1 || got.Drafts[0].Title != "original" {
						t.Fatalf("read escaped pinned directory: %+v, %v", got, err)
					}
				default:
					if stage == "before-lock" {
						s.hooks.beforeLock = swap
					} else {
						s.hooks.beforeRename = func() error {
							swap()
							if stage == "failed-rename" {
								return errors.New("injected")
							}
							return nil
						}
					}
					err := s.Replace(ctxb(), d.ID, 1, mkDraft(d.ID, 2, minimal("updated")))
					if stage == "failed-rename" {
						if err == nil {
							t.Fatal("expected injected failure")
						}
					} else if err != nil {
						t.Fatal(err)
					}
					originalDir := moved
					if target == "root" {
						originalDir = filepath.Join(moved, "drafts")
					}
					want := "updated"
					if stage == "failed-rename" {
						want = "original"
					}
					data := readFile(t, filepath.Join(originalDir, d.ID+".json"))
					got, err := decode(data, d.ID)
					if err != nil || got.Title != want {
						t.Fatalf("original directory write: %+v, %v", got, err)
					}
					entries, err := os.ReadDir(originalDir)
					if err != nil {
						t.Fatal(err)
					}
					for _, e := range entries {
						if tempNameRe.MatchString(e.Name()) {
							t.Errorf("leaked temp %s", e.Name())
						}
					}
				}
				got, err := outside.Get(ctxb(), d.ID)
				if err != nil || got.Title != "outside" || got.Revision != 1 {
					t.Fatalf("outside draft changed: %+v %v", got, err)
				}
				if string(readFile(t, stalePath)) != "outside sentinel" {
					t.Fatal("outside temp changed")
				}
			})
		}
	}
}
