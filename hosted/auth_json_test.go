package hosted

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuthJSONUnicodeAndDuplicates(t *testing.T) {
	for _, value := range []string{
		`"plain"`, `"🍽"`, `"\ud83c\udf7d"`, `"\u0061"`, `"\\ud800"`, `"\"escaped quote"`, `"�"`,
		`null`, `{"nullable":null,"number":9007199254740993}`, `[true,1,null,"value"]`,
	} {
		var target map[string]json.RawMessage
		if err := decodeAuthJSON([]byte(`{"extension":`+value+`}`), authClaimFields, &target); err != nil {
			t.Errorf("valid extra %s rejected: %v", value, err)
		}
	}
	for _, raw := range []string{
		`{"extension":"\ud800"}`, `{"extension":"\udfff"}`, `{"extension":"\ud800\ud800"}`,
		`{"extension":"\udfff\ud800"}`, `{"extension":"\ud800\u0061"}`, `{"extension":"\ud800\\udfff"}`,
		`{"\ud800":"value"}`, `{"extension":"` + string([]byte{0xff}) + `"}`,
		`{"extension":{"name":1,"name":2}}`, `{"extension":[{"name":1,"\u006eame":2}]}`,
		`{"extension":1,"extension":2}`, `{"extension":null,"extension":null}`,
		`{"nbf":1,"NBF":2}`, `{"NBF":1}`, `{"iſſ":"issuer"}`,
		`{"aud":[null,"expected"]}`, `{"emailVerified":null}`, `{"nbf":null}`,
		`null`, `[]`, `1`, `"string"`, `{"extra":1} {}`, `{"extra":1,}`, `{"extra":`,
	} {
		var target map[string]json.RawMessage
		if err := decodeAuthJSON([]byte(raw), authClaimFields, &target); err == nil {
			t.Errorf("ambiguous JSON accepted: %s", raw)
		}
	}
}

func TestAuthJSONBoundsUnknownExtensionDepth(t *testing.T) {
	for _, kind := range []string{"object", "array", "mixed"} {
		for _, depth := range []int{maxAuthJSONDepth - 1, maxAuthJSONDepth, maxAuthJSONDepth + 1, 1000} {
			value := `null`
			for i := 1; i < depth; i++ {
				if kind == "array" || (kind == "mixed" && i%2 == 0) {
					value = `[` + value + `]`
				} else {
					value = `{"extension":` + value + `}`
				}
			}
			var target map[string]json.RawMessage
			err := decodeAuthJSON([]byte(`{"extension":`+value+`}`), authClaimFields, &target)
			if (err == nil) != (depth <= maxAuthJSONDepth) {
				t.Errorf("%s depth %d: %v", kind, depth, err)
			}
		}
	}
}

func FuzzDecodeAuthJSON(f *testing.F) {
	for _, input := range []string{`{}`, `{"email":"sample@example.invalid"}`, `{"x":"\ud800"}`, `{"x":"\ud83c\udf7d"}`, `{"email":"x","EMAIL":"y"}`, `{"x":` + strings.Repeat(`[`, 65) + `0` + strings.Repeat(`]`, 65) + `}`} {
		f.Add([]byte(input))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > int(defaultWebhookBodyMax) {
			t.Skip()
		}
		var target map[string]json.RawMessage
		_ = decodeAuthJSON(raw, authClaimFields, &target)
	})
}
