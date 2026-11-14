package main

import (
	"strings"
	"testing"
	"time"
)

func TestSubprocessInvalidFrameUTF8(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"invalid byte", "\xff", false},
		{"valid replacement character", "\uFFFD", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			call := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"draft_create","arguments":{"draft":{"title":"Metadata test"}},"_meta":{"note":"` +
				tc.value + `"}}}` + "\n"
			if tc.valid {
				code, frames, stderr := runRemediationFrames(t, binPath, dir, call)
				if code != 0 {
					t.Fatalf("exit=%d stderr=%s", code, stderr)
				}
				assertRemediationReplies(t, frames, float64(3), assertSavedTitle(t, dir, "Metadata test"))
				return
			}

			input := createFrame("2", "Accepted first") + call + createFrame("4", "Later")
			code, frames, stderr := runRemediationFrames(t, binPath, dir, input)
			if code != 1 || !strings.Contains(stderr, "invalid protocol envelope") {
				t.Fatalf("exit=%d stderr=%s", code, stderr)
			}
			assertRemediationReplies(t, frames, float64(2), assertSavedTitle(t, dir, "Accepted first"))
		})
	}
}

func TestSubprocessInvalidUTF8CancellationDoesNotReleasePriorCall(t *testing.T) {
	dir := t.TempDir()
	cancel := `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2,"reason":"` + "\xff" + `"}}` + "\n"
	code, frames, stderr := runRemediationFrames(t, stallBinPath, dir, createFrame("2", "Accepted first")+cancel)
	if code != 1 || !strings.Contains(stderr, "invalid protocol envelope") {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	assertRemediationReplies(t, frames, float64(2), assertSavedTitle(t, dir, "Accepted first"))
}

func TestGateInvalidUTF8OutsideRequests(t *testing.T) {
	for _, input := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{"note":"` + "\xff" + `"}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"note":"` + "\xff" + `"}}`,
	} {
		g := newFrameGate(time.Second)
		g.mu.Lock()
		delivered := g.deliver([]byte(input))
		g.mu.Unlock()
		if delivered || g.InputError() != errAmbiguousEnvelope || g.queue.Len() != 0 || len(g.pending) != 0 || len(g.seen) != 0 {
			t.Fatalf("malformed frame reached the SDK or changed request accounting: delivered=%t error=%v", delivered, g.InputError())
		}
	}
}
