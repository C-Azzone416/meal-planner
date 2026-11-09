package localstore

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func TestDecodeStoredDraftHeaderAndMembers(t *testing.T) {
	const id = "drf_00000000000000000001"
	const fields = `,"id":"` + id + `","revision":1,"status":"draft","title":"first"`
	for _, tc := range []struct {
		name, raw string
		category  draft.Category
		code      string
	}{
		{"escaped canonical version", `{"schema_vers\u0069on":1` + fields + "}", "", ""},
		{"duplicate version", `{"schema_version":2,"schema_version":1` + fields + "}", draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"escaped duplicate version", `{"schema_version":1,"schema_vers\u0069on":2}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"mixed case version", `{"schema_version":2,"Schema_Version":1` + fields + "}", draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"noncanonical version", `{"Schema_Version":2}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"null version", `{"schema_version":null}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"quoted version", `{"schema_version":"2"}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"future unknown field", `{"schema_version":2,"NewField":{"title":"a","Title":"b","title":"c"}}`, draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported},
		{"future deep payload", `{"new":` + strings.Repeat("[", 70) + "0" + strings.Repeat("]", 70) + `,"schema_version":2}`, draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported},
		{"future enormous integer", `{"schema_version":922337203685477580800}`, draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported},
		{"future noninteger", `{"schema_version":922337203685477580800e1}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"future malformed payload", `{"schema_version":2,"new":[}`, draft.CategoryCorrupt, draft.CodeInvalidJSON},
		{"future ambiguous root", `{"schema_version":2,"new":1,"New":2}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"duplicate title", `{"schema_version":1` + fields + `,"title":"second"}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"case variant title", `{"schema_version":1` + fields + `,"Title":"second"}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"case variant envelope", `{"schema_version":1` + fields + `,"Created_At":null}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"case variant nested singleton", `{"schema_version":1` + fields + `,"ingredients":[{"Text":"salt"}]}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"escaped duplicate nested key", `{"schema_version":1` + fields + `,"ingredients":[{"text":"salt","t\u0065xt":"pepper"}]}`, draft.CategoryCorrupt, draft.CodeInvalidContent},
		{"malformed UTF-8", `{"schema_version":1` + fields + `,"notes":"` + "\xff" + `"}`, draft.CategoryCorrupt, draft.CodeInvalidJSON},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := decode([]byte(tc.raw), id)
			if tc.category == "" {
				if err != nil || d.ID != id {
					t.Fatalf("decode = %+v, %v", d, err)
				}
				return
			}
			wantErr(t, err, tc.category, tc.code, "")
		})
	}
}

func TestReplaceRechecksAmbiguityUnderLock(t *testing.T) {
	store, root := newStore(t)
	current := mkDraft(testID(1), 1, minimal("first"))
	mustCreate(t, store, current)
	path := filepath.Join(root, "drafts", current.ID+".json")
	original := readFile(t, path)
	ambiguous := bytes.Replace(original, []byte(`"title": "first"`), []byte(`"title": "first", "Title": "second"`), 1)
	if bytes.Equal(ambiguous, original) {
		t.Fatal("could not construct ambiguous fixture")
	}
	store.hooks.beforeLock = func() {
		if err := os.WriteFile(path, ambiguous, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	err := store.Replace(ctxb(), current.ID, 1, mkDraft(current.ID, 2, minimal("replacement")))
	wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, root)
	if got := readFile(t, path); !bytes.Equal(got, ambiguous) {
		t.Fatal("Replace changed the ambiguous file")
	}
	assertNoTemps(t, root)

	list, err := store.List(ctxb())
	if err != nil || len(list.Drafts) != 0 || len(list.Skipped) != 1 || list.Skipped[0].Category != draft.CategoryCorrupt {
		t.Fatalf("List = %+v, err=%v", list, err)
	}
}
