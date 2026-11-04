package draft

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// DecodeContent is the single strict decoder for content supplied as JSON (CLI file or stdin
// input, and the MCP adapter), so identical input gives identical results everywhere.
//
// Exactly one JSON object is accepted: no trailing data, no duplicate keys at any depth, no unknown fields
// (matched case-sensitively, unlike encoding/json), no values of the wrong JSON type, and no
// invalid UTF-8. Absent and null are equivalent. On failure it returns the zero Content and
// exactly one issue (the first problem in document order); semantic validation is then skipped.
// On success the returned Content is in canonical form (see Service.Create) and has not been
// validated: call Validate.
//
// No issue message echoes input. An issue path names the offending location when the decoder can
// tell and is "/" otherwise.
func DecodeContent(raw []byte) (Content, []Issue) {
	fail := func(code, path, msg string) (Content, []Issue) {
		return Content{}, []Issue{{Code: code, Path: path, Message: msg}}
	}
	if len(raw) > MaxInputBytes {
		return fail(IssueInputTooLarge, "/", "the input exceeds the size limit")
	}
	if !utf8.Valid(raw) {
		return fail(IssueMalformedJSON, "/", "the input is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	root, err := parseNode(dec, 0)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return fail(IssueMalformedJSON, "/", "the input is empty")
		}
		var dup *duplicateKeyError
		if errors.As(err, &dup) {
			return fail(IssueDuplicateKey, "/", "an object has a duplicate key")
		}
		return fail(IssueMalformedJSON, "/", "the input is not a single valid JSON value")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fail(IssueMalformedJSON, "/", "the input has data after the first JSON value")
	}
	if root.kind != nodeObject {
		return fail(IssueInvalidType, "/", "the input must be a JSON object")
	}
	if iss := checkShape(root, reflect.TypeOf(Content{}), ""); iss != nil {
		return Content{}, []Issue{*iss}
	}
	var c Content
	if err := json.Unmarshal(raw, &c); err != nil {
		// checkShape accepted the document, so this is not expected.
		return fail(IssueInvalidType, "/", "the input does not match the draft schema")
	}
	return normalize(c), nil
}

type nodeKind uint8

const (
	nodeNull nodeKind = iota
	nodeBool
	nodeNumber
	nodeString
	nodeArray
	nodeObject
)

// node is a parsed JSON value with object keys kept in document order. Scalars keep only what the
// shape check needs.
type node struct {
	kind  nodeKind
	num   string
	keys  []string
	items []node // array elements, or object values parallel to keys
}

type duplicateKeyError struct{}

func (*duplicateKeyError) Error() string { return "duplicate key" }

const maxDecodeDepth = 64

var errTooDeep = errors.New("nesting too deep")

func parseNode(dec *json.Decoder, depth int) (node, error) {
	if depth > maxDecodeDepth {
		return node{}, errTooDeep
	}
	tok, err := dec.Token()
	if err != nil {
		return node{}, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := node{kind: nodeObject}
			seen := map[string]bool{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return node{}, err
				}
				key, ok := kt.(string)
				if !ok {
					return node{}, errors.New("object key is not a string")
				}
				if seen[key] {
					return node{}, &duplicateKeyError{}
				}
				seen[key] = true
				v, err := parseNode(dec, depth+1)
				if err != nil {
					return node{}, noEOF(err)
				}
				n.keys = append(n.keys, key)
				n.items = append(n.items, v)
			}
			if _, err := dec.Token(); err != nil { // the closing brace
				return node{}, noEOF(err)
			}
			return n, nil
		case '[':
			n := node{kind: nodeArray}
			for dec.More() {
				v, err := parseNode(dec, depth+1)
				if err != nil {
					return node{}, noEOF(err)
				}
				n.items = append(n.items, v)
			}
			if _, err := dec.Token(); err != nil {
				return node{}, noEOF(err)
			}
			return n, nil
		}
		return node{}, errors.New("unexpected delimiter")
	case json.Number:
		return node{kind: nodeNumber, num: t.String()}, nil
	case string:
		return node{kind: nodeString}, nil
	case bool:
		return node{kind: nodeBool}, nil
	case nil:
		return node{kind: nodeNull}, nil
	}
	return node{}, errors.New("unexpected token")
}

// noEOF turns a premature end of input inside a value into a syntax error, so only an empty
// document is reported as empty.
func noEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

var fieldCache sync.Map // reflect.Type -> map[string]reflect.Type

// jsonFields returns the JSON field name -> Go type of a struct, from its json tags.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	if m, ok := fieldCache.Load(t); ok {
		return m.(map[string]reflect.Type)
	}
	m := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		m[name] = f.Type
	}
	fieldCache.Store(t, m)
	return m
}

// checkShape verifies that n has the JSON shape of t, returning the first problem in document
// order. null is accepted wherever a value is optional (anywhere except as a list element).
func checkShape(n node, t reflect.Type, path string) *Issue {
	if n.kind == nodeNull {
		return nil
	}
	switch t.Kind() {
	case reflect.Pointer:
		return checkShape(n, t.Elem(), path)
	case reflect.Struct:
		if n.kind != nodeObject {
			return typeIssue(path, "an object")
		}
		fields := jsonFields(t)
		for i, k := range n.keys {
			ft, ok := fields[k]
			if !ok {
				return &Issue{Code: IssueUnknownField, Path: unknownFieldPath(path, k), Message: "unknown field"}
			}
			if iss := checkShape(n.items[i], ft, path+"/"+escapePointer(k)); iss != nil {
				return iss
			}
		}
		return nil
	case reflect.Slice:
		if n.kind != nodeArray {
			return typeIssue(path, "a list")
		}
		for i, it := range n.items {
			p := path + "/" + strconv.Itoa(i)
			if it.kind == nodeNull {
				return typeIssue(p, "an object, not null")
			}
			if iss := checkShape(it, t.Elem(), p); iss != nil {
				return iss
			}
		}
		return nil
	case reflect.String:
		if n.kind != nodeString {
			return typeIssue(path, "a string")
		}
		return nil
	case reflect.Int, reflect.Int64:
		if n.kind != nodeNumber {
			return typeIssue(path, "an integer")
		}
		if _, err := strconv.ParseInt(n.num, 10, 64); err != nil {
			return typeIssue(path, "an integer")
		}
		return nil
	}
	return typeIssue(path, "a supported value")
}

func typeIssue(path, want string) *Issue {
	if path == "" {
		path = "/"
	}
	return &Issue{Code: IssueInvalidType, Path: path, Message: "the value must be " + want}
}

// unknownFieldPath returns the path of an unknown key when the key is short and plain enough to
// show; otherwise the path of its parent, so arbitrary input is never echoed.
func unknownFieldPath(parent, key string) string {
	plain := len(key) > 0 && len(key) <= 64
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			plain = false
			break
		}
	}
	if plain {
		return parent + "/" + key
	}
	if parent == "" {
		return "/"
	}
	return parent
}

func escapePointer(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}
