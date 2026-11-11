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
		{"future content characters", `{"schema_version":2,"title":"\u001b\u0085\u202e"}`, draft.CategoryUnsupportedSchema, draft.CodeSchemaUnsupported},
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

func TestDecodeStoredDraftContentCharacters(t *testing.T) {
	const id = "drf_00000000000000000001"
	const header = `{"schema_version":1,"id":"` + id + `","revision":1,"status":"draft",`
	for _, tc := range []struct {
		name, fields string
		valid        bool
	}{
		{"escape in title", `"title":"first\u001b"`, false},
		{"C1 in ingredient", `"ingredients":[{"text":"salt\u0085"}]`, false},
		{"bidi isolate in guidance", `"guidance":[{"text":"notes\u2066"}]`, false},
		{"bidi mark in source URL", `"sources":[{"url":"https://example.test/\u200f"}]`, false},
		{"C1 in notes", `"notes":"one\u009b"`, false},
		{"NUL in step", `"instructions":[{"text":"one\u0000"}]`, false},
		{"tab in nested basis note", `"instructions":[{"text":"ok","phase_basis":{"note":"one\ttwo"}}]`, false},
		{"bidi in enum", `"instructions":[{"text":"ok","phase":"prep_ahead\u202e"}]`, false},
		{"multiline and valid Unicode", `"title":"قهوة � 👩‍🍳","notes":"one\r\ntwo\tthree","instructions":[{"text":"one\r\ntwo\tthree"}],"guidance":[{"text":"one\r\ntwo\tthree"}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := decode([]byte(header+tc.fields+"}"), id)
			if !tc.valid {
				wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, "")
				return
			}
			if err != nil || d.Title != "قهوة � 👩‍🍳" || d.Notes != "one\r\ntwo\tthree" ||
				d.Instructions[0].Text != d.Notes || d.Guidance[0].Text != d.Notes {
				t.Fatalf("decode = %+v, %v", d, err)
			}
		})
	}
}

func TestReplaceRechecksCorruptionUnderLock(t *testing.T) {
	for _, tc := range []struct{ name, field string }{
		{"ambiguous", `"title": "first", "Title": "second"`},
		{"invalid characters", `"title": "first\u001b\u0085\u202e"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, root := newStore(t)
			current := mkDraft(testID(1), 1, minimal("first"))
			mustCreate(t, store, current)
			path := filepath.Join(root, "drafts", current.ID+".json")
			original := readFile(t, path)
			damaged := bytes.Replace(original, []byte(`"title": "first"`), []byte(tc.field), 1)
			if bytes.Equal(damaged, original) {
				t.Fatal("could not construct corrupt fixture")
			}
			store.hooks.beforeLock = func() {
				if err := os.WriteFile(path, damaged, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			err := store.Replace(ctxb(), current.ID, 1, mkDraft(current.ID, 2, minimal("replacement")))
			wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, root)
			assertNoTemps(t, root)
			_, err = store.Get(ctxb(), current.ID)
			wantErr(t, err, draft.CategoryCorrupt, draft.CodeInvalidContent, root)

			list, err := store.List(ctxb())
			if err != nil || len(list.Drafts) != 0 || len(list.Skipped) != 1 || list.Skipped[0].Category != draft.CategoryCorrupt {
				t.Fatalf("List = %+v, err=%v", list, err)
			}
			if got := readFile(t, path); !bytes.Equal(got, damaged) {
				t.Fatal("operations changed the corrupt file")
			}
		})
	}
}
