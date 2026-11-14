package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

const storedEnvelopeID = "drf_00000000000000000001"

func storedEnvelopeFile(t *testing.T, rev, created, updated string) (dir, path, raw string) {
	t.Helper()
	dir = t.TempDir()
	var b strings.Builder
	fmt.Fprintf(&b, `{"schema_version":1,"id":%q,"revision":%s,"status":"draft",`, storedEnvelopeID, rev)
	if created != "" {
		fmt.Fprintf(&b, `"created_at":%s,`, created)
	}
	if updated != "" {
		fmt.Fprintf(&b, `"updated_at":%s,`, updated)
	}
	b.WriteString(`"title":"T"}`)
	raw = b.String()
	if err := os.Mkdir(filepath.Join(dir, "drafts"), 0o700); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "drafts", storedEnvelopeID+".json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, path, raw
}

func envelopeRequests(rev int) string {
	return `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"draft_get","arguments":{"id":"` + storedEnvelopeID + `"}}}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"draft_list","arguments":{}}}` + "\n" +
		fmt.Sprintf(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"draft_update","arguments":{"id":%q,"expected_revision":%d,"draft":{"title":"replacement"}}}}`, storedEnvelopeID, rev) + "\n"
}

func toolContent(frame map[string]any) (isError bool, content map[string]any) {
	result, _ := frame["result"].(map[string]any)
	content, _ = result["structuredContent"].(map[string]any)
	return result["isError"] == true, content
}

// TestSubprocessStoredEnvelope: a stored file with a bad revision or timestamp is corrupt/invalid_content
// through MCP get and update, skipped by list, and left byte for byte.
func TestSubprocessStoredEnvelope(t *testing.T) {
	const ok = `"2026-11-04T09:30:00Z"`
	future := `"` + time.Now().UTC().Add(2*draft.MaxFutureSkew).Format("2006-01-02T15:04:05Z") + `"`
	for _, tc := range []struct{ name, rev, created, updated string }{
		{"created_at missing", "1", "", ok},
		{"created_at null", "1", "null", ok},
		{"created_at zero", "1", `"0001-01-01T00:00:00Z"`, ok},
		{"created_at offset", "1", `"2026-11-04T11:30:00+02:00"`, ok},
		{"created_at nanoseconds", "1", `"2026-11-04T09:30:00.5Z"`, ok},
		{"created_at far future", "1", future, future},
		{"updated_at zero", "1", ok, `"0001-01-01T00:00:00Z"`},
		{"updated_at before created_at", "1", ok, `"2026-11-04T09:29:59Z"`},
		{"revision 0", "0", ok, ok},
		{"revision 2^53", "9007199254740992", ok, ok},
		{"revision int64 max", "9223372036854775807", ok, ok},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, path, raw := storedEnvelopeFile(t, tc.rev, tc.created, tc.updated)
			code, frames, stderr := runRemediationFrames(t, binPath, dir, envelopeRequests(1))
			if code != 0 || len(frames) != 4 {
				t.Fatalf("exit=%d replies=%v stderr=%s", code, frames, stderr)
			}
			for _, frame := range frames[1:] {
				isError, content := toolContent(frame)
				switch frame["id"] {
				case float64(3):
					skipped, _ := content["skipped"].([]any)
					if isError || content["total"] != float64(0) || content["skipped_total"] != float64(1) || len(skipped) != 1 {
						t.Fatalf("list = %v", frame)
					}
					if entry, _ := skipped[0].(map[string]any); entry["id"] != storedEnvelopeID || entry["category"] != "corrupt" {
						t.Fatalf("skipped entry = %v", entry)
					}
				case float64(2), float64(4):
					failure, _ := content["error"].(map[string]any)
					if !isError || failure["category"] != "corrupt" || failure["code"] != "invalid_content" {
						t.Fatalf("frame %v = %v, want corrupt/invalid_content", frame["id"], frame)
					}
				default:
					t.Fatalf("unexpected frame %v", frame)
				}
			}
			if after, err := os.ReadFile(path); err != nil || string(after) != raw {
				t.Fatalf("file changed: %q (%v)", after, err)
			}
		})
	}
}

// TestSubprocessRevisionCap: through MCP, an update from revision 2^53-1 is refused with no write
// and an update from 2^53-2 succeeds.
func TestSubprocessRevisionCap(t *testing.T) {
	const ok = `"2026-11-04T09:30:00Z"`
	t.Run("update at 2^53-1 is refused with no write", func(t *testing.T) {
		dir, path, raw := storedEnvelopeFile(t, "9007199254740991", ok, ok)
		code, frames, stderr := runRemediationFrames(t, binPath, dir, envelopeRequests(9007199254740991))
		if code != 0 || len(frames) != 4 {
			t.Fatalf("exit=%d replies=%v stderr=%s", code, frames, stderr)
		}
		for _, frame := range frames[1:] {
			isError, content := toolContent(frame)
			switch frame["id"] {
			case float64(2):
				d, _ := content["draft"].(map[string]any)
				if isError || d["revision"] != float64(9007199254740991) {
					t.Fatalf("get = %v", frame)
				}
			case float64(3):
				if isError || content["total"] != float64(1) || content["skipped_total"] != float64(0) {
					t.Fatalf("list = %v", frame)
				}
			case float64(4):
				failure, _ := content["error"].(map[string]any)
				msg, _ := failure["message"].(string)
				if !isError || failure["category"] != "limit_exceeded" || failure["code"] != "revision_limit" || !strings.Contains(msg, "9007199254740991") {
					t.Fatalf("update = %v, want limit_exceeded/revision_limit", frame)
				}
			}
		}
		if after, err := os.ReadFile(path); err != nil || string(after) != raw {
			t.Fatalf("file changed: %q (%v)", after, err)
		}
	})
	t.Run("update beyond 2^53-1 is an invalid revision", func(t *testing.T) {
		dir, path, raw := storedEnvelopeFile(t, "9007199254740991", ok, ok)
		_, frames, _ := runRemediationFrames(t, binPath, dir, envelopeRequests(9007199254740992))
		update := frameByID(t, frames, 4)
		_, content := toolContent(update)
		failure, _ := content["error"].(map[string]any)
		if failure["category"] != "invalid_argument" || failure["code"] != "invalid_revision" {
			t.Fatalf("update = %v", update)
		}
		if after, err := os.ReadFile(path); err != nil || string(after) != raw {
			t.Fatalf("file changed: %q (%v)", after, err)
		}
	})
	t.Run("update at 2^53-2 succeeds", func(t *testing.T) {
		dir, path, _ := storedEnvelopeFile(t, "9007199254740990", ok, ok)
		code, frames, stderr := runRemediationFrames(t, binPath, dir, envelopeRequests(9007199254740990))
		if code != 0 || len(frames) != 4 {
			t.Fatalf("exit=%d replies=%v stderr=%s", code, frames, stderr)
		}
		update := frameByID(t, frames, 4)
		isError, content := toolContent(update)
		d, _ := content["draft"].(map[string]any)
		if isError || d["revision"] != float64(9007199254740991) || d["title"] != "replacement" {
			t.Fatalf("update = %v", update)
		}
		after, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(after), `"revision": 9007199254740991`) {
			t.Fatalf("file = %q (%v)", after, err)
		}
		// The new file is valid: a second session reads it.
		_, frames, _ = runRemediationFrames(t, binPath, dir, envelopeRequests(9007199254740991))
		if get := frameByID(t, frames, 2); func() bool { isError, c := toolContent(get); return isError || c["draft"] == nil }() {
			t.Fatalf("get after update = %v", get)
		}
	})
}

// frameByID returns the response with the given numeric id; responses may arrive in any order.
func frameByID(t *testing.T, frames []map[string]any, id float64) map[string]any {
	t.Helper()
	for _, f := range frames {
		if f["id"] == id {
			return f
		}
	}
	t.Fatalf("no response with id %v in %v", id, frames)
	return nil
}
