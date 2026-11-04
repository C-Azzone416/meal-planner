package main

import (
	"io"
	"strings"
	"testing"
	"time"
)

func readAllAsync(g *frameGate) chan string {
	ch := make(chan string, 1)
	go func() { b, _ := io.ReadAll(g); ch <- string(b) }()
	return ch
}

func TestGateWithholdsEOFUntilRequestsAreAnswered(t *testing.T) {
	g := newFrameGate(5 * time.Second)
	pr, pw := io.Pipe()
	g.start(pr)
	out := g.wrapWriter(io.Discard)
	done := readAllAsync(g)

	io.WriteString(pw, `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`+"\n")
	io.WriteString(pw, `{"jsonrpc":"2.0","id":"a","method":"x"}`+"\n")
	io.WriteString(pw, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	pw.Close()
	select {
	case <-done:
		t.Fatal("EOF released with requests unanswered")
	case <-time.After(150 * time.Millisecond):
	}
	io.WriteString(out, `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
	select {
	case <-done:
		t.Fatal("EOF released with one request unanswered")
	case <-time.After(100 * time.Millisecond):
	}
	// The response may arrive split across writes.
	io.WriteString(out, `{"jsonrpc":"2.0","id":"a",`)
	io.WriteString(out, `"error":{"code":-1,"message":"m"}}`+"\n")
	got := <-done
	if !strings.Contains(got, `"id":1`) || g.Expired() {
		t.Fatalf("got %q expired=%v", got, g.Expired())
	}
}

func TestGateIgnoresNotificationsBatchesAndGarbage(t *testing.T) {
	g := newFrameGate(5 * time.Second)
	g.start(strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`[{"jsonrpc":"2.0","id":1,"method":"x"}]` + "\n" +
		"not json\n" +
		`{"jsonrpc":"2.0","id":null,"method":"x"}` + "\n" +
		`{"jsonrpc":"2.0","id":7,"result":{}}` + "\n"))
	select {
	case <-readAllAsync(g):
	case <-time.After(2 * time.Second):
		t.Fatal("gate waited on frames that are not tracked requests")
	}
}

func TestGateCancelledNotificationReleasesTheWait(t *testing.T) {
	g := newFrameGate(5 * time.Second)
	g.start(strings.NewReader(`{"jsonrpc":"2.0","id":5,"method":"tools/call"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":5}}` + "\n"))
	select {
	case <-readAllAsync(g):
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled request still blocks EOF")
	}
}

func TestGateGraceExpiry(t *testing.T) {
	g := newFrameGate(100 * time.Millisecond)
	g.start(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"x"}` + "\n"))
	select {
	case <-readAllAsync(g):
	case <-time.After(2 * time.Second):
		t.Fatal("grace period did not release EOF")
	}
	if !g.Expired() {
		t.Fatal("Expired = false")
	}
}

func TestGateStopDeliversWhatWasReadThenEOF(t *testing.T) {
	g := newFrameGate(5 * time.Second)
	pr, pw := io.Pipe()
	g.start(pr)
	io.WriteString(pw, `{"jsonrpc":"2.0","id":1,"method":"x"}`+"\n")
	time.Sleep(100 * time.Millisecond)
	g.Stop()
	done := readAllAsync(g)
	select {
	case <-done:
		t.Fatal("released with a request unanswered")
	case <-time.After(100 * time.Millisecond):
	}
	io.WriteString(g.wrapWriter(io.Discard), `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
	if got := <-done; !strings.Contains(got, `"id":1`) {
		t.Fatalf("got %q", got)
	}
	pw.Close()
}

func TestGateTracksAnUnterminatedFinalFrame(t *testing.T) {
	g := newFrameGate(5 * time.Second)
	g.start(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"x"}`)) // no trailing newline
	done := readAllAsync(g)
	select {
	case <-done:
		t.Fatal("released with the final, unterminated request unanswered")
	case <-time.After(150 * time.Millisecond):
	}
	waitPending(t, g, 1)
	io.WriteString(g.wrapWriter(io.Discard), `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
	<-done
}

func TestGateCanonicalizesNumericIDs(t *testing.T) {
	for _, c := range []struct{ req, resp string }{{"2.0", "2"}, {"1e0", "1"}, {"3", "3.0"}, {`"7"`, `"7"`}} {
		g := newFrameGate(5 * time.Second)
		g.start(strings.NewReader(`{"jsonrpc":"2.0","id":` + c.req + `,"method":"x"}` + "\n"))
		done := readAllAsync(g)
		select {
		case <-done:
			t.Fatalf("%s: released early", c.req)
		case <-time.After(100 * time.Millisecond):
		}
		io.WriteString(g.wrapWriter(io.Discard), `{"jsonrpc":"2.0","id":`+c.resp+`,"result":{}}`+"\n")
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("request id %s was not matched by response id %s", c.req, c.resp)
		}
	}
	// A string id and a number id are different ids.
	g := newFrameGate(100 * time.Millisecond)
	g.start(strings.NewReader(`{"jsonrpc":"2.0","id":"1","method":"x"}` + "\n"))
	done := readAllAsync(g)
	waitPending(t, g, 1)
	io.WriteString(g.wrapWriter(io.Discard), `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
	<-done
	if !g.Expired() {
		t.Fatal(`response id 1 must not answer request id "1"`)
	}
}

func TestGateDuplicateClientIDsNeedEveryResponse(t *testing.T) {
	g := newFrameGate(5 * time.Second)
	g.start(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"x"}` + "\n" + `{"jsonrpc":"2.0","id":1,"method":"y"}` + "\n"))
	done := readAllAsync(g)
	w := g.wrapWriter(io.Discard)
	waitPending(t, g, 2)
	io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
	select {
	case <-done:
		t.Fatal("released after one of two responses")
	case <-time.After(150 * time.Millisecond):
	}
	io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("not released after both responses")
	}
}

func TestGateOnlyAwaitsStringOrIntegerIDs(t *testing.T) {
	for _, id := range []string{"1.5", "true", "[1]", `{"a":1}`, "null"} {
		g := newFrameGate(5 * time.Second)
		g.start(strings.NewReader(`{"jsonrpc":"2.0","id":` + id + `,"method":"x"}` + "\n"))
		select {
		case <-readAllAsync(g):
		case <-time.After(2 * time.Second):
			t.Fatalf("id %s: the gate waited for an invalid id", id)
		}
		if g.Expired() {
			t.Fatalf("id %s: Expired", id)
		}
	}
}

// waitPending waits until the pump has counted n awaited responses, so a test never writes a
// response before the gate has read its request.
func waitPending(t *testing.T, g *frameGate, n int) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		g.mu.Lock()
		total := 0
		for _, c := range g.pending {
			total += c
		}
		g.mu.Unlock()
		if total == n {
			return
		}
	}
	t.Fatalf("gate never counted %d pending responses", n)
}
