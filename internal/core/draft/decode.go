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
	"unicode"
	"unicode/utf8"
)

// DecodeContent is the single strict decoder for content supplied as JSON (CLI file or stdin
// input, and the MCP adapter), so identical input gives identical results everywhere.
//
// Exactly one JSON object is accepted: no trailing data, no duplicate keys at any depth, no unknown fields
// (matched case-sensitively, unlike encoding/json), no values of the wrong JSON type, and no
// invalid UTF-8. Absent and null are equivalent. On failure it returns the zero Content and
// exactly one issue; semantic validation is then skipped. Syntax errors, duplicate keys and
// over-long nesting are found first, while the document is parsed; only if it parses are unknown
// fields, wrong types and unpaired surrogate escapes reported, and then the first one in document
// order.
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
	p := &parser{dec: dec, raw: raw}
	root, err := p.parse(0)
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

// ValidUniqueJSONMembers checks one Unicode JSON value for duplicate object members,
// including escaped names and case aliases under encoding/json's field matching.
// When the root is an object, reservedRootKeys also require canonical casing.
// This rejects invalid UTF-8 and unpaired surrogate escapes in keys or values.
// It uses the draft parser's nesting limit. Callers validate schema and types.
func ValidUniqueJSONMembers(raw []byte, reservedRootKeys ...string) bool {
	return validUniqueJSONMembers(raw, 0, reservedRootKeys)
}

// ValidUniqueJSONEnvelope applies the same ambiguity and Unicode checks to a
// protocol envelope, allowing each member value the same nesting depth as a
// standalone params value. The caller checks the envelope's schema and types.
func ValidUniqueJSONEnvelope(raw []byte, reservedRootKeys ...string) bool {
	return validUniqueJSONMembers(raw, -1, reservedRootKeys)
}

func validUniqueJSONMembers(raw []byte, rootDepth int, reservedRootKeys []string) bool {
	if !utf8.Valid(raw) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	p := &parser{dec: dec, raw: raw, membersOnly: true, rootDepth: rootDepth, reservedRootKeys: reservedRootKeys}
	if _, err := p.parse(rootDepth); err != nil {
		return false
	}
	_, err := dec.Token()
	return errors.Is(err, io.EOF)
}

// ValidateRaw decodes raw with DecodeContent and validates the result, so every adapter reports
// the same thing for the same input. A decode failure is a ValidationResult whose only error is
// the decode issue (no warnings, since semantic validation is skipped); otherwise it is Validate of
// the decoded content. It never touches a store.
func ValidateRaw(raw []byte) ValidationResult {
	c, issues := DecodeContent(raw)
	if len(issues) > 0 {
		return ValidationResult{
			Scope:             ValidationScope,
			StructurallyValid: false,
			Errors:            issues,
			Warnings:          []Issue{},
			Notice:            ValidationNotice,
		}
	}
	return Validate(c)
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
	kind       nodeKind
	text       string // decoded string or original number spelling
	badUnicode bool   // a string with an unpaired surrogate escape, which encoding/json would silently replace
	keys       []string
	items      []node // array elements, or object values parallel to keys
}

type duplicateKeyError struct{}

func (*duplicateKeyError) Error() string { return "duplicate key" }

const maxDecodeDepth = 64

var errTooDeep = errors.New("nesting too deep")
var errUnpairedSurrogate = errors.New("unpaired surrogate escape")

// parser builds a node tree from the token stream. It also looks at the raw text of every string
// token, because encoding/json replaces an unpaired \uD800-\uDFFF escape with U+FFFD and so hides it.
type parser struct {
	dec              *json.Decoder
	raw              []byte
	last             int64 // input offset after the previous token
	membersOnly      bool  // fold member names and skip building the node tree
	rootDepth        int   // envelope members get the standalone value depth budget
	reservedRootKeys []string
}

// foldedJSONKey uses the same Unicode equivalence as encoding/json's field
// lookup. SimpleFold walks a cycle, so use the smallest rune in each cycle.
func foldedJSONKey(key string) string {
	return strings.Map(func(r rune) rune {
		for {
			next := unicode.SimpleFold(r)
			if next <= r {
				return next
			}
			r = next
		}
	}, key)
}

// token returns the next token and the raw text consumed for it (the token plus any delimiters and
// whitespace before it).
func (p *parser) token() (json.Token, []byte, error) {
	tok, err := p.dec.Token()
	if err != nil {
		return nil, nil, err
	}
	end := p.dec.InputOffset()
	seg := p.raw[p.last:end]
	p.last = end
	return tok, seg, nil
}

func (p *parser) parse(depth int) (node, error) {
	if depth > maxDecodeDepth {
		return node{}, errTooDeep
	}
	tok, seg, err := p.token()
	if err != nil {
		return node{}, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := node{kind: nodeObject}
			seen := map[string]bool{}
			for p.dec.More() {
				kt, keySeg, err := p.token()
				if err != nil {
					return node{}, err
				}
				key, ok := kt.(string)
				if !ok {
					return node{}, errors.New("object key is not a string")
				}
				identity := key
				if p.membersOnly {
					if hasUnpairedSurrogate(keySeg) {
						return node{}, errUnpairedSurrogate
					}
					identity = foldedJSONKey(key)
					if depth == p.rootDepth {
						for _, reserved := range p.reservedRootKeys {
							if key != reserved && strings.EqualFold(key, reserved) {
								return node{}, errors.New("noncanonical object key")
							}
						}
					}
				}
				if seen[identity] {
					return node{}, &duplicateKeyError{}
				}
				seen[identity] = true
				v, err := p.parse(depth + 1)
				if err != nil {
					return node{}, noEOF(err)
				}
				if !p.membersOnly {
					n.keys = append(n.keys, key)
					n.items = append(n.items, v)
				}
			}
			if _, _, err := p.token(); err != nil { // the closing brace
				return node{}, noEOF(err)
			}
			return n, nil
		case '[':
			n := node{kind: nodeArray}
			for p.dec.More() {
				v, err := p.parse(depth + 1)
				if err != nil {
					return node{}, noEOF(err)
				}
				if !p.membersOnly {
					n.items = append(n.items, v)
				}
			}
			if _, _, err := p.token(); err != nil {
				return node{}, noEOF(err)
			}
			return n, nil
		}
		return node{}, errors.New("unexpected delimiter")
	case json.Number:
		return node{kind: nodeNumber, text: t.String()}, nil
	case string:
		badUnicode := hasUnpairedSurrogate(seg)
		if p.membersOnly && badUnicode {
			return node{}, errUnpairedSurrogate
		}
		return node{kind: nodeString, text: t, badUnicode: badUnicode}, nil
	case bool:
		return node{kind: nodeBool}, nil
	case nil:
		return node{kind: nodeNull}, nil
	}
	return node{}, errors.New("unexpected token")
}

// ValidJSONString reports whether raw is one JSON string whose Unicode scalar
// values survive encoding/json unchanged. JSON syntax validation alone accepts
// invalid UTF-8 and unpaired surrogate escapes, replacing them with U+FFFD.
// This syntax-only helper imposes no content-field limits: transport IDs may
// contain controls (including NUL) and exceed the draft-content size limit.
func ValidJSONString(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) >= 2 && raw[0] == '"' && json.Valid(raw) && utf8.Valid(raw) && !hasUnpairedSurrogate(raw)
}

// hasUnpairedSurrogate reports whether the JSON text seg contains a \uD800-\uDBFF escape not
// immediately followed by a \uDC00-\uDFFF escape, or a \uDC00-\uDFFF escape not preceded by one.
func hasUnpairedSurrogate(seg []byte) bool {
	hex4 := func(i int) (rune, bool) { // the four hex digits after "\u" at seg[i:i+2]
		if i+6 > len(seg) || seg[i] != '\\' || seg[i+1] != 'u' {
			return 0, false
		}
		var r rune
		for _, c := range seg[i+2 : i+6] {
			switch {
			case c >= '0' && c <= '9':
				r = r<<4 | rune(c-'0')
			case c >= 'a' && c <= 'f':
				r = r<<4 | rune(c-'a'+10)
			case c >= 'A' && c <= 'F':
				r = r<<4 | rune(c-'A'+10)
			default:
				return 0, false
			}
		}
		return r, true
	}
	for i := 0; i < len(seg); {
		if seg[i] != '\\' {
			i++
			continue
		}
		r, ok := hex4(i)
		if !ok {
			i += 2 // some other escape, or an escaped backslash
			continue
		}
		switch {
		case r >= 0xD800 && r <= 0xDBFF:
			lo, ok := hex4(i + 6)
			if !ok || lo < 0xDC00 || lo > 0xDFFF {
				return true
			}
			i += 12
		case r >= 0xDC00 && r <= 0xDFFF:
			return true
		default:
			i += 6
		}
	}
	return false
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

// checkShape verifies the JSON shape and string Unicode, returning the first
// problem in document order. Content rules belong to Validate for both input
// and stored drafts.
// Null is accepted except as a list element.
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
		if n.badUnicode {
			if path == "" {
				path = "/"
			}
			return &Issue{Code: IssueInvalidChars, Path: path, Message: "contains an unpaired surrogate escape, which is not valid text"}
		}
		return nil
	case reflect.Int, reflect.Int64:
		if n.kind != nodeNumber {
			return typeIssue(path, "an integer")
		}
		if _, err := strconv.ParseInt(n.text, 10, 64); err != nil {
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
