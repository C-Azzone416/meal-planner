package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubprocessUpdateLeavesCorruptDraftIntact(t *testing.T) {
	const id = "drf_00000000000000000001"
	for _, tc := range []struct{ name, field string }{
		{"ambiguous", `"title":"first","Title":"second"`},
		{"invalid characters", `"title":"first\u001b\u0085\u202e"`},
		{"blank title", `"title":"   "`},
		{"long title", `"title":"` + strings.Repeat("a", 201) + `"`},
		{"invalid phase", `"title":"T","instructions":[{"text":"step","phase":"invalid"}]`},
		{"javascript URL", `"title":"T","sources":[{"id":"s","kind":"url","url":"javascript:alert(1)"}]`},
		{"non URL text", `"title":"T","sources":[{"id":"s","kind":"url","url":"not a URL"}]`},
		{"negative minutes", `"title":"T","estimates":{"active_minutes":{"value":-1,"basis":{"kind":"agent_estimate"}}}`},
		{"line separator", `"title":"T\u2028"`},
		{"paragraph separator", `"title":"T\u2029"`},
		{"BOM", `"title":"T\ufeff"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			raw := `{"schema_version":1,"id":"` + id + `","revision":1,"status":"draft",` + tc.field + "}"
			if err := os.Mkdir(filepath.Join(dir, "drafts"), 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "drafts", id+".json")
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			const request = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"draft_get","arguments":{"id":"` + id + `"}}}` + "\n" +
				`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"draft_list","arguments":{}}}` + "\n" +
				`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"draft_update","arguments":{"id":"` + id + `","expected_revision":1,"draft":{"title":"replacement"}}}}` + "\n"
			code, frames, stderr := runRemediationFrames(t, binPath, dir, request)
			if code != 0 || len(frames) != 4 {
				t.Fatalf("exit=%d replies=%v stderr=%s", code, frames, stderr)
			}
			seen := map[float64]bool{}
			for _, frame := range frames[1:] {
				frameID, _ := frame["id"].(float64)
				if frameID < 2 || frameID > 4 || seen[frameID] {
					t.Fatalf("unexpected response ID: %v", frame)
				}
				seen[frameID] = true
				result, _ := frame["result"].(map[string]any)
				content, _ := result["structuredContent"].(map[string]any)
				if frameID == 3 {
					skipped, _ := content["skipped"].([]any)
					if result["isError"] == true || content["total"] != float64(0) || content["skipped_total"] != float64(1) || len(skipped) != 1 {
						t.Fatalf("list result = %v", result)
					}
					entry, _ := skipped[0].(map[string]any)
					if entry["id"] != id || entry["category"] != "corrupt" {
						t.Fatalf("skipped entry = %v", entry)
					}
				} else {
					failure, _ := content["error"].(map[string]any)
					if result["isError"] != true || failure["category"] != "corrupt" || failure["code"] != "invalid_content" {
						t.Fatalf("get/update result = %v, want corrupt/invalid_content", result)
					}
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != raw {
				t.Fatalf("corrupt draft changed: %q (%v)", after, err)
			}
		})
	}
}
