package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const remediationInit = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"remediation-test","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
`

func createFrame(id, title string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"tools/call","params":{"name":"draft_create","arguments":{"draft":{"title":%q}}}}`+"\n", id, title)
}

func runRemediationFrames(t *testing.T, binary, dir, input string) (int, []map[string]any, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--data-dir", dir)
	cmd.Env = append(os.Environ(), "MEALPLANNER_TEST_STALL_MS=300", "MEALPLANNER_TEST_GRACE_MS=1800")
	cmd.Stdin = strings.NewReader(remediationInit + input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	code := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok || ctx.Err() != nil {
			t.Fatalf("run: %v, context: %v, stderr: %s", err, ctx.Err(), &stderr)
		}
		code = exit.ExitCode()
	}
	var frames []map[string]any
	for line := range strings.SplitSeq(strings.TrimSuffix(string(out), "\n"), "\n") {
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil || frame["jsonrpc"] != "2.0" {
			t.Fatalf("invalid stdout frame: %q (%v)", line, err)
		}
		frames = append(frames, frame)
	}
	return code, frames, stderr.String()
}

func assertSavedTitle(t *testing.T, dir, title string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "drafts", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected exactly one saved draft, got %v (%v)", files, err)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var saved struct{ ID, Title string }
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Title != title || saved.ID == "" {
		t.Fatalf("saved = %+v (%v), want title %q and an id", saved, err, title)
	}
	return saved.ID
}

func assertRemediationReplies(t *testing.T, frames []map[string]any, wantID any, draftID string) {
	t.Helper()
	if len(frames) != 2 || frames[0]["id"] != float64(1) || frames[1]["id"] != wantID {
		t.Fatalf("expected initialize and one response for %v, got %v", wantID, frames)
	}
	result, _ := frames[1]["result"].(map[string]any)
	content, _ := result["structuredContent"].(map[string]any)
	saved, _ := content["draft"].(map[string]any)
	if saved["id"] != draftID {
		t.Fatalf("response for %v did not name the only saved draft %q: %v", wantID, draftID, frames[1])
	}
}

func TestSubprocessRejectsAmbiguousToolParams(t *testing.T) {
	for name, params := range map[string]string{
		"literal name":       `{"name":"server_status","name":"draft_create","arguments":{"draft":{"title":"unexpected"}}}`,
		"escaped name":       `{"name":"server_status","n\u0061me":"draft_create","arguments":{"draft":{"title":"unexpected"}}}`,
		"case name":          `{"name":"server_status","NAME":"draft_create","arguments":{"draft":{"title":"unexpected"}}}`,
		"literal arguments":  `{"name":"draft_create","arguments":{"draft":{"title":"first argument"}},"arguments":{"draft":{"title":"unexpected"}}}`,
		"escaped arguments":  `{"name":"draft_create","arguments":{"draft":{"title":"first argument"}},"argum\u0065nts":{"draft":{"title":"unexpected"}}}`,
		"case arguments":     `{"name":"draft_create","arguments":{"draft":{"title":"first argument"}},"Arguments":{"draft":{"title":"unexpected"}}}`,
		"case alias alone":   `{"Name":"draft_create","arguments":{"draft":{"title":"unexpected"}}}`,
		"nested draft array": `{"name":"draft_create","arguments":{"draft":{"title":"unexpected","ingredients":[{"text":"a","t\u0065xt":"b"}]}}}`,
		"duplicate meta":     `{"name":"draft_create","arguments":{"draft":{"title":"unexpected"}},"_meta":{},"_m\u0065ta":{}}`,
		"nested meta":        `{"name":"draft_create","arguments":{"draft":{"title":"unexpected"}},"_meta":{"example.test/data":[{"token":1,"TOKEN":2}]}}`,
		"unicode case fold":  `{"name":"draft_create","arguments":{"draft":{"title":"unexpected"}},"_meta":{"example.test/data":{"k":1,"\u212a":2}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			bad := `{"jsonrpc":"2.0","id":73,"method":"tools/call","params":` + params + "}\n"
			code, frames, stderr := runRemediationFrames(t, binPath, dir, createFrame("72", "accepted")+bad+createFrame("74", "later"))
			id := assertSavedTitle(t, dir, "accepted")
			assertRemediationReplies(t, frames, float64(72), id)
			if code != 1 || !strings.Contains(stderr, "invalid tools/call params") || strings.Contains(stderr, "unexpected") {
				t.Fatalf("exit=%d stderr=%s", code, stderr)
			}
		})
	}
}

func TestSubprocessAcceptsUniqueEscapedToolParams(t *testing.T) {
	dir := t.TempDir()
	input := `{"jsonrpc":"2.0","id":73,"method":"tools/call","params":{"n\u0061me":"draft_create","arguments":{"draft":{"t\u0069tle":"accepted"}},"_meta":{"example.test/data":{"items":[{"k":1},{"k":2}]}}}}` + "\n"
	code, frames, stderr := runRemediationFrames(t, binPath, dir, input)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	assertRemediationReplies(t, frames, float64(73), assertSavedTitle(t, dir, "accepted"))
}

func TestSubprocessRejectsInFlightIDReuse(t *testing.T) {
	for _, ids := range []struct {
		first, duplicate string
		reply            any
	}{{"50", "50", float64(50)}, {`"a"`, `"\u0061"`, "a"}} {
		t.Run(ids.first, func(t *testing.T) {
			dir := t.TempDir()
			input := createFrame(ids.first, "first") + createFrame(ids.duplicate, "duplicate") + createFrame("51", "later")
			code, frames, stderr := runRemediationFrames(t, stallBinPath, dir, input)
			id := assertSavedTitle(t, dir, "first")
			assertRemediationReplies(t, frames, ids.reply, id)
			if code != 1 || !strings.Contains(stderr, "request id already used in this session") || strings.Contains(stderr, "grace period elapsed") {
				t.Fatalf("exit=%d stderr=%s", code, stderr)
			}

			fresh := t.TempDir()
			code, frames, stderr = runRemediationFrames(t, binPath, fresh, createFrame(ids.duplicate, "fresh session"))
			if code != 0 {
				t.Fatalf("fresh session exit=%d stderr=%s", code, stderr)
			}
			assertRemediationReplies(t, frames, ids.reply, assertSavedTitle(t, fresh, "fresh session"))
		})
	}
}

func TestSubprocessRejectsIDReuseAfterReply(t *testing.T) {
	dir := t.TempDir()
	p := start(t, dir)
	p.initialize("2025-11-25")
	p.sendRaw(strings.TrimSuffix(createFrame("50", "first"), "\n"))
	p.await(func(f map[string]any) bool { return f["id"] == float64(50) })
	p.sendRaw(strings.TrimSuffix(createFrame("50", "duplicate"), "\n"))
	if code := p.waitExit(4 * time.Second); code != 1 || !strings.Contains(p.stderr.String(), "request id already used in this session") {
		t.Fatalf("exit=%d stderr=%s", code, p.stderr)
	}
	p.assertStdoutClean()
	assertRemediationReplies(t, p.snapshot(), float64(50), assertSavedTitle(t, dir, "first"))
}

func TestSubprocessCancelledIDStaysReserved(t *testing.T) {
	dir := t.TempDir()
	const cancel = `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"\u00350"}}` + "\n"
	// Same-looking numeric ID is unrelated to the cancelled string ID.
	input := createFrame("50", "unrelated") + createFrame(`"50"`, "cancelled") + cancel +
		createFrame(`"\u00350"`, "duplicate") + createFrame("51", "later")
	code, frames, stderr := runRemediationFrames(t, stallBinPath, dir, input)
	if code != 1 || !strings.Contains(stderr, "request id already used in this session") || strings.Contains(stderr, "grace period elapsed") {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	var unrelated []map[string]any
	seenCancelled := false
	for _, frame := range frames {
		if frame["id"] == "50" {
			if seenCancelled {
				t.Fatalf("multiple replies for cancelled ID: %v", frames)
			}
			seenCancelled = true
			continue
		}
		unrelated = append(unrelated, frame)
	}
	assertRemediationReplies(t, unrelated, float64(50), assertSavedTitle(t, dir, "unrelated"))
}
