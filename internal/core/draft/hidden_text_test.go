package draft

import "testing"

func TestUnicodeTagBlockRejectedInAllTextModes(t *testing.T) {
	for r := rune(0xe0000); r <= 0xe007f; r++ {
		for _, multiline := range []bool{false, true} {
			if validChars("safe"+string(r)+"text", multiline) {
				t.Fatalf("accepted U+%X multiline=%v", r, multiline)
			}
		}
	}
	for _, s := range []string{"café", "مرحبا", "a\u200cb", "a\u200db", "a\u200eb"} {
		if !validChars(s, false) {
			t.Fatalf("legitimate text rejected: %q", s)
		}
	}
	for _, c := range []Content{{Title: "a\U000e0041b"}, {Title: "safe", Notes: "a\U000e0041b"}, {Title: "safe", Instructions: []Step{{Text: "a\U000e0041b"}}}} {
		if Validate(c).StructurallyValid {
			t.Fatal("hidden tag content accepted")
		}
	}
}
