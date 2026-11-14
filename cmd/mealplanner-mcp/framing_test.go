package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Exercise the actual pinned SDK after a completed initialize handshake, not
// merely the gate decoder. The SDK rejects a space immediately after a JSON
// value even though that is legal JSON whitespace.
func TestSubprocessPaddedFramesAfterInitialize(t *testing.T) {
	for _, prefix := range []string{"", " ", "\t", "\r", " \t\r"} {
		for _, suffix := range []string{"", " ", "\t", "\r", " \t\r"} {
			for _, end := range []string{"", "\n", "\r\n"} {
				t.Run(fmt.Sprintf("%q_%q_%q", prefix, suffix, end), func(t *testing.T) {
					p := start(t, t.TempDir())
					p.initialize("2025-11-25")
					frame := `{"jsonrpc":"2.0","id":55,"method":"ping"}`
					if _, err := io.WriteString(p.stdin, prefix+frame+suffix+end); err != nil {
						t.Fatal(err)
					}
					if code := p.shutdown(); code != 0 {
						t.Fatalf("exit=%d stderr=%s", code, p.stderr.String())
					}
					got := p.await(func(f map[string]any) bool { return f["id"] == float64(55) })
					if got["error"] != nil || got["result"] == nil {
						t.Fatalf("ping failed: %v", got)
					}
				})
			}
		}
	}
}

func TestGateFramePaddingPreservesObjectAndWaitsForDelimiter(t *testing.T) {
	const object = `{ "jsonrpc" : "2.0", "id" : " a\t\n\r\u0020 ", "method" : "ping", "params" : {"value":"  ", "n":1e0} }`
	g := newFrameGate(time.Second)
	pr, pw := io.Pipe()
	g.start(pr)
	done := make(chan struct{})
	go func() { defer close(done); io.WriteString(pw, " \t"+object+" \t") }()
	<-done // pump consumed the partial line, but may not admit it yet
	g.mu.Lock()
	if g.queue.Len() != 0 || len(g.pending) != 0 {
		t.Error("frame delivered before LF/EOF")
	}
	g.mu.Unlock()
	go func() { io.WriteString(pw, "\r\n"); pw.Close() }()
	waitPending(t, g, 1)
	io.WriteString(g.wrapWriter(io.Discard), `{"jsonrpc":"2.0","id":" a\t\n\r\u0020 ","result":{}}`+"\n")
	got, err := io.ReadAll(g)
	if err != nil || string(got) != object+"\n" || g.Expired() {
		t.Fatalf("got=%q err=%v expired=%v", got, err, g.Expired())
	}
}

func TestFrameWhitespaceDoesNotWeakenRejection(t *testing.T) {
	for _, bad := range []string{
		"\v" + `{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\f",
		"\u00a0" + `{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		"\ufeff" + `{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"} {"id":4}`,
		" \t\r", "",
	} {
		var out, stderr strings.Builder
		input := " \t" + `{"jsonrpc":"2.0","id":1,"method":"ping"}` + " \r\n" + bad + "\n" + `{"jsonrpc":"2.0","id":3,"method":"ping"}` + " \n"
		code := run(context.Background(), []string{"--data-dir", t.TempDir()}, noEnv, strings.NewReader(input), &out, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "invalid protocol envelope") || !strings.Contains(out.String(), `"id":1`) || strings.Contains(out.String(), `"id":3`) {
			t.Fatalf("bad=%q code=%d stdout=%s stderr=%s", bad, code, out.String(), stderr.String())
		}
	}
}

func TestFramePaddingCountsTowardSizeLimit(t *testing.T) {
	const frame = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	for _, tc := range []struct {
		name, ending string
		extra        int
		wantErr      bool
	}{
		{"LF exact", "\n", 0, false},
		{"LF oversize", "\n", 1, true},
		{"CRLF exact", "\r\n", 0, false},
		{"CRLF oversize", "\r\n", 1, true},
		{"extra CR before CRLF", "\r\r\n", 0, true},
		{"EOF exact with CR padding", "\r", -1, false},
		{"EOF oversize with CR padding", "\r", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newFrameGate(time.Second)
			g.start(strings.NewReader(frame + strings.Repeat(" ", maxTrackedLine-len(frame)+tc.extra) + tc.ending))
			got, err := io.ReadAll(g)
			if tc.wantErr {
				if !errors.Is(err, errFrameTooLarge) || len(got) != 0 {
					t.Fatalf("oversize len=%d err=%v", len(got), err)
				}
			} else if err != nil || string(got) != frame+"\n" {
				t.Fatalf("limit frame len=%d err=%v", len(got), err)
			}
		})
	}
}

func TestFrameLimitWithSplitCR(t *testing.T) {
	const frame = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	for _, tc := range []struct {
		name, next string
		wantErr    bool
	}{
		{"LF completes CRLF", "\n", false},
		{"space leaves CR in frame", " \n", true},
		{"stop leaves CR in frame", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newFrameGate(time.Second)
			g.mu.Lock()
			g.admit([]byte(frame + strings.Repeat(" ", maxTrackedLine-len(frame)) + "\r"))
			firstErr, firstQueued := g.inputErr, g.queue.Len()
			g.admit([]byte(tc.next))
			g.mu.Unlock()
			g.Stop()
			got, err := io.ReadAll(g)
			if firstErr != nil || firstQueued != 0 {
				t.Fatalf("incomplete CRLF had error=%v queued=%d", firstErr, firstQueued)
			}
			if tc.wantErr {
				if !errors.Is(err, errFrameTooLarge) || len(got) != 0 {
					t.Fatalf("oversize len=%d err=%v", len(got), err)
				}
			} else if err != nil || string(got) != frame+"\n" {
				t.Fatalf("limit frame len=%d err=%v", len(got), err)
			}
		})
	}
}

func TestGateRejectsAmbiguousOrMalformedRequestParams(t *testing.T) {
	for _, tc := range []struct{ name, frame string }{
		{"initialize duplicate", `"id":2,"method":"initialize","params":{"protocolVersion":"2024-11-05","protocolV\u0065rsion":"2025-11-25"}`},
		{"logging case alias", `"id":2,"method":"logging/setLevel","params":{"level":"info","LEVEL":"debug"}`},
		{"ping nested case alias", `"id":2,"method":"ping","params":{"_meta":{"items":[{"a":0,"A":1}]}}`},
		{"unknown method duplicate", `"id":2,"method":"example/test","params":{"name":"a","name":"b"}`},
		{"notification reversed surrogate", `"method":"notifications/initialized","params":{"_meta":{"text":"\udc00\ud800"}}`},
		{"notification surrogate in key", `"method":"notifications/progress","params":{"_meta":{"\ud800":1}}`},
		{"cancellation duplicate", `"method":"notifications/cancelled","params":{"requestId":1,"_meta":{"value":0,"VALUE":1}}`},
		{"cancellation surrogate", `"method":"notifications/cancelled","params":{"requestId":1,"reason":"\ud800"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newFrameGate(time.Second)
			const prior = `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"
			g.mu.Lock()
			defer g.mu.Unlock()
			g.admit([]byte(prior + `{"jsonrpc":"2.0",` + tc.frame + "}\n"))
			if !errors.Is(g.inputErr, errAmbiguousEnvelope) || g.queue.String() != prior || len(g.pending) != 1 || len(g.seen) != 1 {
				t.Fatalf("input error=%v queued=%q pending=%v seen=%v", g.inputErr, g.queue.String(), g.pending, g.seen)
			}
		})
	}
}

func TestGateAcceptsValidUnicodeRequestParams(t *testing.T) {
	const input = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolV\u0065rsion":"2025-11-25","_meta":{"\ud83d\ude00":["\uFFFD","\ud83d\ude00","\\ud800"]}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{"_meta":{"items":[{"a":0},{"A":1}]}}}` + "\n"
	g := newFrameGate(time.Second)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.admit([]byte(input))
	if g.inputErr != nil || g.queue.String() != input || len(g.pending) != 1 {
		t.Fatalf("input error=%v queued=%q pending=%v", g.inputErr, g.queue.String(), g.pending)
	}
}

func TestSubprocessMalformedFramePreservesPriorCreate(t *testing.T) {
	base := `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":50}}
`
	cases := map[string]string{
		"bad reason":       strings.Replace(base, `"requestId":50`, `"requestId":50,"reason":3`, 1),
		"bad meta":         strings.Replace(base, `"requestId":50`, `"requestId":50,"_meta":3`, 1),
		"bad version":      strings.Replace(base, `"2.0"`, `"1.0"`, 1),
		"null version":     strings.Replace(base, `"2.0"`, `null`, 1),
		"missing version":  strings.Replace(base, `"jsonrpc":"2.0",`, "", 1),
		"missing target":   strings.Replace(base, `"requestId":50`, `"reason":"bye"`, 1),
		"not notification": strings.Replace(base, `"jsonrpc":"2.0"`, `"jsonrpc":"2.0","id":99`, 1),
		"deep meta":        strings.Replace(base, `"requestId":50`, `"requestId":50,"_meta":{"nested":`+strings.Repeat("[", 1001)+"0"+strings.Repeat("]", 1001)+"}", 1),
		"duplicate level":  `{"jsonrpc":"2.0","id":99,"method":"logging/setLevel","params":{"level":"info","LEVEL":"debug"}}`,
		"ping surrogate":   `{"jsonrpc":"2.0","id":99,"method":"ping","params":{"_meta":{"text":"\udc00"}}}`,
	}
	for _, method := range []string{"null", "true", "false", "42", "[]", "{}", `"\ud800"`, "\"a\xffb\""} {
		cases["invalid method "+method] = `{"jsonrpc":"2.0","id":99,"method":` + method + `}`
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := start(t, dir)
			p.initialize("2025-11-25")
			p.sendRaw(createLine(p, 50, "Prior accepted draft") + "\n" + strings.TrimSpace(bad) + "\n" + createLine(p, 51, "Must not execute"))
			if code := p.shutdown(); code != 1 || !strings.Contains(p.stderr.String(), "invalid protocol envelope") {
				t.Fatalf("code=%d stderr=%s", code, p.stderr.String())
			}
			if !p.hasResponse(50) || p.hasResponse(51) {
				t.Fatalf("prior/later response mismatch: %v", p.snapshot())
			}
			if n := countDrafts(t, dir); n != 1 {
				t.Fatalf("stored drafts=%d want exactly prior draft", n)
			}
		})
	}
}

func TestSubprocessCancelledStallStillHasSessionDeadline(t *testing.T) {
	for _, signal := range []bool{false, true} {
		t.Run(fmt.Sprintf("signal=%v", signal), func(t *testing.T) {
			dir := t.TempDir()
			p := startBin(t, stallBinPath, dir, "MEALPLANNER_TEST_STALL_MS=1800", "MEALPLANNER_TEST_GRACE_MS=250")
			p.initialize("2025-11-25")
			p.sendRaw(createLine(p, 50, "Cancelled stalled draft"))
			time.Sleep(150 * time.Millisecond) // ensure the handler is inside the test stall
			p.sendRaw(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":50,"reason":"test","_meta":{}}}`)
			started := time.Now()
			if signal {
				// Let the complete cancellation reach the gate before Stop closes input.
				time.Sleep(30 * time.Millisecond)
				if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			} else {
				_ = p.stdin.Close()
			}
			if code := p.waitExit(1200 * time.Millisecond); code != 1 || !strings.Contains(p.stderr.String(), "grace period") {
				t.Fatalf("code=%d stderr=%s", code, p.stderr.String())
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("session exceeded deadline: %v", elapsed)
			}
			if n := countDrafts(t, dir); n != 0 {
				t.Fatalf("cancelled write persisted %d drafts", n)
			}
		})
	}
}

func TestSubprocessCancelledHandlerFinishesWithinGrace(t *testing.T) {
	dir := t.TempDir()
	p := startBin(t, stallBinPath, dir, "MEALPLANNER_TEST_STALL_MS=400", "MEALPLANNER_TEST_GRACE_MS=1500")
	p.initialize("2025-11-25")
	p.sendRaw(createLine(p, 50, "Cancelled draft"))
	time.Sleep(100 * time.Millisecond)
	p.sendRaw(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":50,"reason":"test","_meta":{"extra":null}}}`)
	if code := p.shutdown(); code != 0 {
		t.Fatalf("valid cancellation code=%d stderr=%s", code, p.stderr.String())
	}
	if n := countDrafts(t, dir); n != 0 {
		t.Fatalf("cancelled write persisted %d drafts", n)
	}
}

func TestGateSessionDeadlineRemainsAfterPendingIsEmpty(t *testing.T) {
	g := newFrameGate(30 * time.Millisecond)
	g.start(strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"))
	if _, err := io.ReadAll(g); err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.DeadlineC():
	case <-time.After(time.Second):
		t.Fatal("SDK shutdown has no deadline after pending IDs drained")
	}
	if g.Expired() {
		t.Fatal("no unanswered requests; session deadline is separate")
	}
}

func TestGateDeadlineStartsBeforeSDKReads(t *testing.T) {
	g := newFrameGate(30 * time.Millisecond)
	g.start(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"))
	select {
	case <-g.DeadlineC():
	case <-time.After(time.Second):
		t.Fatal("EOF deadline incorrectly waits for the SDK to read")
	}
}

func TestSubprocessEmptyStringMethodIsTracked(t *testing.T) {
	p := start(t, t.TempDir())
	p.initialize("2025-11-25")
	p.sendRaw(`{"jsonrpc":"2.0","id":51,"method":""}`)
	if code := p.shutdown(); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, p.stderr.String())
	}
	response := p.await(func(f map[string]any) bool { return f["id"] == float64(51) })
	if response["error"] == nil {
		t.Fatalf("empty method should receive an error response: %v", response)
	}
}
