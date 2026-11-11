package hosted

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A schema lists only fields this auth boundary interprets. Unknown provider
// extensions remain supported, including nested objects and nullable values.
// Children describe nested objects or objects in an array (the JWKS keys).
type authJSONFields map[string]authJSONFields

// Provider extensions need shallow JSON, not unbounded recursive work. Count
// both object and array nesting, including the top-level object.
const maxAuthJSONDepth = 64

var (
	authHeaderFields = authJSONFields{
		"alg": nil, "kid": nil, "typ": nil, "cty": nil, "crit": nil, "b64": nil,
		"jku": nil, "jwk": nil, "x5u": nil, "x5c": nil, "x5t": nil, "x5t#S256": nil,
	}
	authClaimFields = authJSONFields{
		"iss": nil, "sub": nil, "aud": nil, "exp": nil, "nbf": nil, "iat": nil, "jti": nil,
		"email": nil, "emailVerified": nil, "email_verified": nil,
	}
	authWebhookFields = authJSONFields{
		"event_id": nil, "event_type": nil,
		"context": {"endpoint_id": nil}, "user": {"email": nil},
	}
	authJWKSFields = authJSONFields{"keys": {
		"kty": nil, "crv": nil, "kid": nil, "x": nil, "alg": nil, "use": nil, "key_ops": nil,
	}}
	authEmailFields    = authJSONFields{"email": nil}
	errInvalidAuthJSON = errors.New("invalid or ambiguous authentication JSON")
)

// decodeAuthJSON prevents encoding/json's case-insensitive field matching,
// duplicate overwrite/merge, null coercion, and malformed-Unicode replacement
// from changing identity or policy. Escaped, correctly spelled names work.
// Check the complete JSON before typed decoding; never normalize signed bytes.
func decodeAuthJSON(raw []byte, fields authJSONFields, target any) error {
	if !validAuthJSONUnicode(raw) {
		return errInvalidAuthJSON
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errInvalidAuthJSON
	}
	if err := authJSONObject(decoder, fields, 1); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errInvalidAuthJSON
	}
	return json.Unmarshal(raw, target)
}

func authJSONObject(decoder *json.Decoder, fields authJSONFields, depth int) error {
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return errInvalidAuthJSON
		}
		seen[key] = true
		children, known := fields[key]
		if !known {
			for name := range fields {
				if strings.EqualFold(name, key) {
					return errInvalidAuthJSON
				}
			}
		}
		if err := authJSONValue(decoder, children, known, depth); err != nil {
			return err
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return errInvalidAuthJSON
	}
	return nil
}

func authJSONValue(decoder *json.Decoder, fields authJSONFields, rejectNull bool, depth int) error {
	token, err := decoder.Token()
	if err != nil || (rejectNull && token == nil) {
		return errInvalidAuthJSON
	}
	switch token {
	case json.Delim('{'):
		if depth >= maxAuthJSONDepth {
			return errInvalidAuthJSON
		}
		return authJSONObject(decoder, fields, depth+1)
	case json.Delim('['):
		if depth >= maxAuthJSONDepth {
			return errInvalidAuthJSON
		}
		for decoder.More() {
			if err := authJSONValue(decoder, fields, rejectNull, depth+1); err != nil {
				return err
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			return errInvalidAuthJSON
		}
	}
	return nil
}

// encoding/json replaces invalid UTF-8 and unpaired UTF-16 surrogates with
// U+FFFD. Reject both before that lossy conversion; valid surrogate pairs and
// literal replacement characters are still legal. json.Valid bounds all reads
// below and supplies the JSON syntax/depth check.
func validAuthJSONUnicode(raw []byte) bool {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		for i++; raw[i] != '"'; i++ {
			if raw[i] != '\\' {
				continue
			}
			i++
			if raw[i] != 'u' {
				continue
			}
			code, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
			i += 4
			if code < 0xD800 || code > 0xDFFF {
				continue
			}
			if code > 0xDBFF || i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return true
}
