package hosted

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func recipeUnicodeBody(t *testing.T) string {
	t.Helper()
	c := syntheticRecipeContent()
	c.ShortName = "Test dish"
	c.Tags = []string{"Test tag"}
	c.Ingredients[0].Unit = "cup"
	c.PrepAhead = []RecipePrepAhead{{ID: "a1", Text: "Prepare ahead", Label: "Preparation", Minutes: 5, StorageNote: "Keep chilled"}}
	c.PrepTonight = []RecipePrepTonight{{ID: "p1", Text: "Prepare tonight", Minutes: 5}}
	return recipeBody(t, c)
}

// Work with RawMessage so malformed Unicode escapes reach the handler intact,
// rather than being silently repaired while constructing the test request.
func recipeJSONChange(t *testing.T, raw json.RawMessage, path []string, change func(json.RawMessage) json.RawMessage) json.RawMessage {
	t.Helper()
	if len(path) == 0 {
		return change(raw)
	}
	var value any
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			t.Fatal(err)
		}
		i, err := strconv.Atoi(path[0])
		if err != nil || i < 0 || i >= len(items) {
			t.Fatalf("invalid test array path: %v", path)
		}
		items[i] = recipeJSONChange(t, items[i], path[1:], change)
		value = items
	} else {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		next := recipeJSONChange(t, fields[path[0]], path[1:], change)
		if next == nil {
			delete(fields, path[0])
		} else {
			fields[path[0]] = next
		}
		value = fields
	}
	result, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func recipeJSONReplace(t *testing.T, body string, path []string, value string) string {
	t.Helper()
	return string(recipeJSONChange(t, json.RawMessage(body), path, func(json.RawMessage) json.RawMessage {
		if value == "" { // remove a property, as distinct from JSON null
			return nil
		}
		return json.RawMessage(value)
	}))
}

type recipeJSONField struct {
	path     []string
	typeOf   reflect.Type
	optional bool
}

// Reflect the schema so each current (and future) string field, nullable field,
// container and array element participates in the validation matrices.
func recipeJSONFields() []recipeJSONField {
	var fields []recipeJSONField
	var walk func(reflect.Type, []string, bool)
	walk = func(typ reflect.Type, path []string, optional bool) {
		fields = append(fields, recipeJSONField{append([]string(nil), path...), typ, optional})
		switch typ.Kind() {
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				name, options, _ := strings.Cut(f.Tag.Get("json"), ",")
				walk(f.Type, append(append([]string(nil), path...), name), options == "omitempty")
			}
		case reflect.Slice:
			walk(typ.Elem(), append(append([]string(nil), path...), "0"), false)
		}
	}
	walk(reflect.TypeFor[RecipeContent](), []string{"content"}, false)
	return fields
}

func assertRecipeWrite(t *testing.T, body string, accepted bool, check func(*recipeFake)) {
	t.Helper()
	for _, method := range []string{"POST", "PUT"} {
		t.Run(method, func(t *testing.T) {
			path, request, want := "/api/recipes", body, 201
			if method == "PUT" {
				path += "?id=" + recipeTestID
				request = body[:len(body)-1] + `,"expectedRevision":1}`
				want = 200
			}
			calls := 1
			if !accepted {
				want, calls = 400, 0
			}
			store := &recipeFake{}
			rec := recipeRequest(t, store, method, path, request)
			if rec.Code != want || store.calls != calls {
				t.Fatalf("status=%d (want %d), storage calls=%d (want %d), response=%s", rec.Code, want, store.calls, calls, rec.Body)
			}
			if check != nil && accepted {
				check(store)
			}
		})
	}
}

func TestRecipeAPIRejectsMalformedUnicodeInEveryString(t *testing.T) {
	valid := recipeUnicodeBody(t)
	cases := map[string]string{
		"high surrogate":                `"\ud800"`,
		"last high surrogate":           `"\uDBFF"`,
		"low surrogate":                 `"\udc00"`,
		"last low surrogate":            `"\uDFFF"`,
		"two high surrogates":           `"\ud800\ud801"`,
		"reversed pair":                 `"\udc00\ud800"`,
		"high then non-surrogate":       `"\ud800\ufffd"`,
		"interrupted pair":              `"\ud800x\udc00"`,
		"escaped low-surrogate text":    `"\ud800\\udc00"`,
		"escaped prefix then high":      `"\\\ud800"`,
		"valid pair then low":           `"\ud83d\ude00\udc00"`,
		"valid pair then high":          `"\ud83d\ude00\ud800"`,
		"literal replacement then high": `"�\ud800"`,
		"invalid UTF-8":                 "\"\xff\"",
		"UTF-8 encoded surrogate":       "\"\xed\xa0\x80\"",
		"overlong UTF-8":                "\"\xc0\xaf\"",
		"out of range UTF-8":            "\"\xf4\x90\x80\x80\"",
	}
	for _, field := range recipeJSONFields() {
		if field.typeOf.Kind() != reflect.String {
			continue
		}
		t.Run(strings.Join(field.path, "/"), func(t *testing.T) {
			for name, raw := range cases {
				t.Run(name, func(t *testing.T) {
					body := recipeJSONReplace(t, valid, field.path, raw)
					if !strings.Contains(body, raw) {
						t.Fatal("test construction changed the raw Unicode input")
					}
					assertRecipeWrite(t, body, false, nil)
				})
			}
		})
	}
}

func TestRecipeAPIRejectsWrongTypesAndUndocumentedNulls(t *testing.T) {
	valid := recipeUnicodeBody(t)
	for _, field := range recipeJSONFields() {
		t.Run(strings.Join(field.path, "/"), func(t *testing.T) {
			kind := field.typeOf.Kind()
			if kind == reflect.Pointer {
				kind = field.typeOf.Elem().Kind()
			}
			for _, tc := range []struct {
				name, raw string
				kind      reflect.Kind
			}{
				{"string", `"\ud800"`, reflect.String},
				{"number", `1`, reflect.Int},
				{"boolean", `true`, reflect.Bool},
				{"object", `{}`, reflect.Struct},
				{"array", `[]`, reflect.Slice},
			} {
				if tc.kind == kind || (kind == reflect.Float64 && tc.kind == reflect.Int) {
					continue
				}
				t.Run(tc.name, func(t *testing.T) {
					assertRecipeWrite(t, recipeJSONReplace(t, valid, field.path, tc.raw), false, nil)
				})
			}
			t.Run("null", func(t *testing.T) {
				assertRecipeWrite(t, recipeJSONReplace(t, valid, field.path, "null"), field.typeOf.Kind() == reflect.Pointer, nil)
			})
			if field.path[len(field.path)-1] != "0" {
				t.Run("missing", func(t *testing.T) {
					assertRecipeWrite(t, recipeJSONReplace(t, valid, field.path, ""), field.optional, nil)
				})
			}
			if kind == reflect.Slice {
				for _, raw := range []string{`[null]`, `[[]]`, `[true]`, `[1]`, `["\ud800"]`} {
					t.Run("invalid element "+raw, func(t *testing.T) {
						assertRecipeWrite(t, recipeJSONReplace(t, valid, field.path, raw), false, nil)
					})
				}
			}
		})
	}
}

func TestRecipeAPIPreservesValidUnicodeInEveryTextField(t *testing.T) {
	valid := recipeUnicodeBody(t)
	cases := []struct{ name, raw, want string }{
		{"literal replacement", `"�"`, "�"},
		{"escaped replacement", `"\ufffd"`, "�"},
		{"literal emoji", `"😀"`, "😀"},
		{"surrogate pair", `"\ud83d\ude00"`, "😀"},
		{"minimum pair", `"\uD800\uDC00"`, "\U00010000"},
		{"maximum pair", `"\uDBFF\uDFFF"`, "\U0010ffff"},
		{"non-surrogate boundaries", `"\ud7ff\ue000"`, "\ud7ff\ue000"},
		{"escaped high-surrogate text", `"\\ud800"`, `\ud800`},
		{"escaped low-surrogate text", `"\\udc00"`, `\udc00`},
		{"escaped backslash before pair", `"\\\ud83d\ude00"`, `\😀`},
		{"two escaped backslashes", `"\\\\ud800"`, `\\ud800`},
		{"escaped quote", `"\"quoted\""`, `"quoted"`},
		{"whitespace around string", " \n\t\"text\"\r ", "text"},
	}
	for _, field := range recipeJSONFields() {
		name := field.path[len(field.path)-1]
		if field.typeOf.Kind() != reflect.String || name == "attention" || name == "aisle" {
			continue // these two fields are enums, not arbitrary text
		}
		t.Run(strings.Join(field.path, "/"), func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					assertRecipeWrite(t, recipeJSONReplace(t, valid, field.path, tc.raw), true, func(store *recipeFake) {
						raw, err := json.Marshal(store.content)
						if err != nil {
							t.Fatal(err)
						}
						recipeJSONChange(t, raw, field.path[1:], func(value json.RawMessage) json.RawMessage {
							var got string
							if err := json.Unmarshal(value, &got); err != nil || got != tc.want {
								t.Fatalf("stored value=%q, want %q, error=%v", got, tc.want, err)
							}
							return value
						})
					})
				})
			}
		})
	}
	for _, tc := range []struct {
		path []string
		raw  string
	}{
		{[]string{"content", "attention"}, `"\u006cow"`},
		{[]string{"content", "ingredients", "0", "aisle"}, `"\u004fther"`},
	} {
		assertRecipeWrite(t, recipeJSONReplace(t, valid, tc.path, tc.raw), true, nil)
	}
}

func TestRecipeAPIUnicodeDoesNotBypassContentLimits(t *testing.T) {
	valid := recipeUnicodeBody(t)
	limits := map[string]int{
		"content/title": 120, "content/shortName": 120, "content/icon": 16,
		"content/tags/0":           80,
		"content/ingredients/0/id": 160, "content/ingredients/0/name": 500, "content/ingredients/0/unit": 80,
		"content/prepAhead/0/id": 160, "content/prepAhead/0/text": 4000, "content/prepAhead/0/label": 500, "content/prepAhead/0/storageNote": 2000,
		"content/prepTonight/0/id": 160, "content/prepTonight/0/text": 4000,
		"content/cookTonight/0/id": 160, "content/cookTonight/0/text": 4000,
	}
	for path, limit := range limits {
		t.Run(path, func(t *testing.T) {
			for _, count := range []int{limit, limit + 1} {
				t.Run(strconv.Itoa(count)+" runes", func(t *testing.T) {
					raw := `"` + strings.Repeat(`\ud83d\ude00`, count) + `"`
					assertRecipeWrite(t, recipeJSONReplace(t, valid, strings.Split(path, "/"), raw), count == limit, nil)
				})
			}
			t.Run("NUL", func(t *testing.T) {
				assertRecipeWrite(t, recipeJSONReplace(t, valid, strings.Split(path, "/"), `"valid\u0000text"`), false, nil)
			})
		})
	}
}

func TestRecipeAPIRejectsAmbiguousOrMalformedKeysAtEveryDepth(t *testing.T) {
	valid := recipeUnicodeBody(t)
	for _, tc := range []struct {
		path         []string
		key, escaped string
	}{
		{nil, "content", `\u0063ontent`},
		{[]string{"content"}, "title", `\u0074itle`},
		{[]string{"content", "ingredients", "0"}, "name", `\u006eame`},
		{[]string{"content", "prepAhead", "0"}, "text", `\u0074ext`},
		{[]string{"content", "prepTonight", "0"}, "text", `\u0074ext`},
		{[]string{"content", "cookTonight", "0"}, "text", `\u0074ext`},
	} {
		t.Run(strings.Join(tc.path, "/"), func(t *testing.T) {
			for _, key := range []string{tc.key, tc.escaped, strings.ToUpper(tc.key), `\ud800`, `\udc00`, `�`, `\\ud800`} {
				t.Run(key, func(t *testing.T) {
					body := recipeJSONChange(t, json.RawMessage(valid), tc.path, func(object json.RawMessage) json.RawMessage {
						return json.RawMessage(`{"` + key + `":null,` + string(object[1:]))
					})
					assertRecipeWrite(t, string(body), false, nil)
				})
			}
			t.Run("valid escaped key", func(t *testing.T) {
				body := recipeJSONChange(t, json.RawMessage(valid), tc.path, func(object json.RawMessage) json.RawMessage {
					return json.RawMessage(strings.Replace(string(object), `"`+tc.key+`":`, `"`+tc.escaped+`":`, 1))
				})
				assertRecipeWrite(t, string(body), true, nil)
			})
		})
	}
}

func TestRecipeObjectKeysCheckUnicodeBeforeMatchingDecodedNames(t *testing.T) {
	// Production names are ASCII; ensure key validation itself is strict rather
	// than relying on a malformed decoded key being absent from the allowlist.
	for _, raw := range []string{`{"\ud800":1}`, `{"\udc00":1}`, `{"known":1, "\ud800":2}`} {
		if _, err := recipeObjectFields(json.RawMessage(raw), map[string]bool{"�": true, "known": true}); err == nil {
			t.Fatalf("accepted malformed key: %s", raw)
		}
	}
	for _, raw := range []string{`{"�":1}`, `{"\ufffd":1}`, `{"known":1, "\ufffd":2}`} {
		if _, err := recipeObjectFields(json.RawMessage(raw), map[string]bool{"�": true, "known": true}); err != nil {
			t.Fatalf("rejected valid key: %s: %v", raw, err)
		}
	}
}
