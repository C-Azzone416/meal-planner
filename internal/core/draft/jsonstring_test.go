package draft_test

import (
	"strings"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

func TestValidJSONString(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"empty", `""`, true},
		{"unicode", `"café 😀"`, true},
		{"pair", `"\ud83d\ude00"`, true},
		{"first pair", `"\uD800\uDC00"`, true},
		{"last pair", `"\uDBFF\uDFFF"`, true},
		{"controls", `"\u0000\n\r\t\b\f"`, true},
		{"replacement literal", `"�"`, true},
		{"replacement escape", `"\ufffd"`, true},
		{"escaped slash", `"\\ud800"`, true},
		{"escaped quote", `"\"\ud83d\ude00"`, true},
		{"padded", " \n\t\"a\"\r ", true},
		{"large", `"` + strings.Repeat("x", 2<<20) + `"`, true},
		{"high", `"\ud800"`, false},
		{"low", `"\udc00"`, false},
		{"high text", `"\ud800A"`, false},
		{"high non-low", `"\ud800\u0041"`, false},
		{"high high", `"\ud800\ud800"`, false},
		{"low high", `"\udc00\ud800"`, false},
		{"escaped low", `"\ud800\\udc00"`, false},
		{"pair then lone", `"\ud83d\ude00\udc00"`, false},
		{"raw ff", "\"a\xffb\"", false},
		{"raw surrogate", "\"\xed\xa0\x80\"", false},
		{"overlong utf8", "\"\xc0\x80\"", false},
		{"truncated utf8", "\"\xe2\x82\"", false},
		{"not string", `null`, false},
		{"number", `42`, false},
		{"array", `["a"]`, false},
		{"extra value", `"a" "b"`, false},
		{"incomplete escape", `"\uD800`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := draft.ValidJSONString([]byte(tc.raw)); got != tc.valid {
				t.Fatalf("got %v want %v", got, tc.valid)
			}
		})
	}
}
