package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubprocessUpdateLeavesCorruptDraftIntact(t *testing.T) {
	const id = "drf_00000000000000000001"
	for _, tc := range []struct{ name, field string }{
		{"ambiguous", `"title":"first","Title":"second"`},
		{"invalid characters", `"title":"first\u001b\u0085\u202e"`},
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
			const request = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"draft_update","arguments":{"id":"` + id + `","expected_revision":1,"draft":{"title":"replacement"}}}}` + "\n"
			code, frames, stderr := runRemediationFrames(t, binPath, dir, request)
			if code != 0 || len(frames) != 2 || frames[1]["id"] != float64(2) {
				t.Fatalf("exit=%d replies=%v stderr=%s", code, frames, stderr)
			}
			result, _ := frames[1]["result"].(map[string]any)
			content, _ := result["structuredContent"].(map[string]any)
			failure, _ := content["error"].(map[string]any)
			if result["isError"] != true || failure["category"] != "corrupt" || failure["code"] != "invalid_content" {
				t.Fatalf("update result = %v, want corrupt/invalid_content", result)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != raw {
				t.Fatalf("corrupt draft changed: %q (%v)", after, err)
			}
		})
	}
}
