package draft

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// StoredSchemaVersion reads the version from a syntactically valid UTF-8 JSON
// document. It checks the root object for ambiguous keys and inspects only the
// version value, so the current schema's shape and depth rules cannot reject a
// future schema's payload.
func StoredSchemaVersion(raw []byte) (json.Number, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return "", false
	}

	const versionKey = "schema_version"
	seen := make(map[string]struct{})
	var version json.Number
	var value json.RawMessage
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return "", false
		}
		folded := foldedJSONKey(key)
		if _, exists := seen[folded]; exists {
			return "", false
		}
		seen[folded] = struct{}{}
		if err := dec.Decode(&value); err != nil {
			return "", false
		}
		if strings.EqualFold(key, versionKey) {
			value = bytes.TrimSpace(value)
			if key != versionKey || !isJSONInteger(value) {
				return "", false
			}
			version = json.Number(value)
		}
	}
	return version, version != ""
}

// The input is one already validated JSON value. Check the integer spelling
// without restricting its magnitude to the machine's integer range.
func isJSONInteger(value []byte) bool {
	digits := bytes.TrimPrefix(value, []byte("-"))
	if len(digits) == 0 {
		return false
	}
	for _, b := range digits {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

// ValidStoredDraftShape checks the current schema's field spelling, unique
// keys at every depth, content shape and allowed content characters. Call it after checking JSON syntax,
// UTF-8 and schema version. The caller still decodes the typed envelope and
// checks store invariants such as the ID matching its filename.
func ValidStoredDraftShape(raw []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	p := &parser{dec: dec, raw: raw}
	root, err := p.parse(0)
	if err != nil || root.kind != nodeObject {
		return false
	}

	// Draft embeds Content without a JSON tag. The tagged fields on Draft
	// are the envelope, including time.Time fields handled by typed decoding.
	envelope := jsonFields(reflect.TypeFor[Draft]())
	content := jsonFields(reflect.TypeFor[Content]())
	for i, key := range root.keys {
		value := root.items[i]
		if _, ok := envelope[key]; ok {
			if value.badUnicode {
				return false
			}
			continue
		}
		field, ok := content[key]
		if !ok || checkShape(value, field, "/"+key, true) != nil {
			return false
		}
	}
	return true
}
