package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGateRejectsAmbiguityThroughoutFrame(t *testing.T) {
	for _, envelope := range []struct{ name, format string }{
		{"response result", `{"jsonrpc":"2.0","id":7,"result":%s}`},
		{"response error", `{"jsonrpc":"2.0","id":7,"error":{"code":-32000,"message":"error","data":%s}}`},
		{"request extra", `{"jsonrpc":"2.0","id":2,"method":"ping","extra":%s}`},
		{"notification extra", `{"jsonrpc":"2.0","method":"notifications/initialized","extra":%s}`},
		{"cancellation extra", `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1},"extra":%s}`},
	} {
		for name, value := range map[string]string{
			"duplicate":         `{"key":1,"key":2}`,
			"escaped duplicate": `{"key":1,"k\u0065y":2}`,
			"case fold":         `{"key":1,"KEY":2}`,
			"Unicode fold":      `{"k":1,"\u212a":2}`,
			"nested array":      `{"items":[{"key":1,"KEY":2}]}`,
			"surrogate value":   `{"items":["\ud800"]}`,
			"surrogate key":     `{"\udc00":1}`,
			"reversed pair":     `{"text":"\udc00\ud800"}`,
		} {
			t.Run(envelope.name+"/"+name, func(t *testing.T) {
				assertGateRejectsWholeFrame(t, fmt.Sprintf(envelope.format, value))
			})
		}
	}
	for name, frame := range map[string]string{
		"root unknown case fold": `{"jsonrpc":"2.0","id":2,"method":"ping","extra":1,"EXTRA":2}`,
		"root Unicode case fold": `{"jsonrpc":"2.0","id":2,"method":"ping","k":1,"\u212a":2}`,
		"root unknown key":       `{"jsonrpc":"2.0","id":2,"method":"ping","\ud800":1}`,
		"root unknown value":     `{"jsonrpc":"2.0","id":2,"method":"ping","extra":"\udc00"}`,
		"error message":          `{"jsonrpc":"2.0","id":7,"error":{"code":-32000,"message":"\ud800"}}`,
		"error member fold":      `{"jsonrpc":"2.0","id":7,"error":{"code":-32000,"Code":-32001,"message":"error"}}`,
	} {
		t.Run(name, func(t *testing.T) { assertGateRejectsWholeFrame(t, frame) })
	}
}

func assertGateRejectsWholeFrame(t *testing.T, frame string) {
	t.Helper()
	g := newFrameGate(time.Second)
	g.mu.Lock()
	defer g.mu.Unlock()
	const prior = `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"
	g.admit([]byte(prior + frame + "\n" + `{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n"))
	if !errors.Is(g.inputErr, errAmbiguousEnvelope) || !g.stopped || g.queue.String() != prior || len(g.seen) != 1 || len(g.pending) != 1 {
		t.Fatalf("error=%v stopped=%v queued=%q seen=%v pending=%v", g.inputErr, g.stopped, g.queue.String(), g.seen, g.pending)
	}
}

func TestGateAcceptsUnambiguousWholeFrames(t *testing.T) {
	const value = `{"items":[{"key":"\ud83d\ude00"},{"KEY":"\ufffd"}],"\ud83d\ude00":"\\ud800"}`
	for _, frame := range []string{
		`{"jsonrpc":"2.0","id":7,"result":` + value + `,"extra":"שלום\u200e"}`,
		`{"jsonrpc":"2.0","id":7,"error":{"code":-32000,"message":"مرحبا\u200f","data":` + value + `}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","ex\u0074ra":` + value + `}`,
	} {
		g := newFrameGate(time.Second)
		g.mu.Lock()
		accepted := g.deliver([]byte(frame))
		g.mu.Unlock()
		if !accepted || g.InputError() != nil || g.queue.String() != frame+"\n" || len(g.pending) != 0 || len(g.seen) != 0 {
			t.Fatalf("accepted=%v error=%v queued=%q pending=%v seen=%v", accepted, g.InputError(), g.queue.String(), g.pending, g.seen)
		}
	}
}

func TestGateWholeFrameDepthBudget(t *testing.T) {
	for _, member := range []string{"params", "result", "extra"} {
		for _, depth := range []int{64, 65} {
			t.Run(fmt.Sprintf("%s/%d", member, depth), func(t *testing.T) {
				prefix := `{"jsonrpc":"2.0","method":"notifications/initialized",`
				if member == "result" {
					prefix = `{"jsonrpc":"2.0","id":7,`
				}
				frame := prefix + `"` + member + `":` + strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth) + "}"
				g := newFrameGate(time.Second)
				g.mu.Lock()
				accepted := g.deliver([]byte(frame))
				g.mu.Unlock()
				if accepted != (depth == 64) {
					t.Fatalf("depth=%d accepted=%v error=%v", depth, accepted, g.InputError())
				}
			})
		}
	}
}

func TestSubprocessWholeFrameAmbiguityPreservesPriorCreate(t *testing.T) {
	for name, bad := range map[string]string{
		"response result": `{"jsonrpc":"2.0","id":90,"result":{"items":[{"a":1,"A":2}]}}`,
		"response error":  `{"jsonrpc":"2.0","id":90,"error":{"code":-32000,"message":"error","data":{"text":"\ud800"}}}`,
		"request extra":   `{"jsonrpc":"2.0","id":90,"method":"ping","extra":{"k":1,"\u212a":2}}`,
		"root extra":      `{"jsonrpc":"2.0","id":90,"method":"ping","extra":1,"EXTRA":2}`,
		"cancel extra":    `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":72},"extra":{"a":1,"a":2}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			code, frames, stderr := runRemediationFrames(t, binPath, dir, createFrame("72", "accepted")+bad+"\n"+createFrame("74", "later"))
			assertRemediationReplies(t, frames, float64(72), assertSavedTitle(t, dir, "accepted"))
			if code != 1 || !strings.Contains(stderr, "invalid protocol envelope") {
				t.Fatalf("exit=%d stderr=%s", code, stderr)
			}
		})
	}
}
