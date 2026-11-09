package main

import (
	"bytes"
	"encoding/json"
	"errors"
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

func TestGateMatchesSupportedNumericIDs(t *testing.T) {
	for _, c := range []struct{ req, resp string }{{"2", "2"}, {"-1", "-1"}, {"0", "0"}, {`"7"`, `"7"`}} {
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

func TestGateRejectsUnsupportedIDsWithoutAwaiting(t *testing.T) {
	for _, id := range []string{"1.5", "true", "[1]", `{"a":1}`, "null", "-0", "2.0", "1e0", "9007199254740993"} {
		g := newFrameGate(5 * time.Second)
		g.start(strings.NewReader(`{"jsonrpc":"2.0","id":` + id + `,"method":"x"}` + "\n"))
		b, err := io.ReadAll(g)
		if !errors.Is(err, errUnsupportedID) || len(b) != 0 || g.Expired() {
			t.Fatalf("id=%s bytes=%q err=%v expired=%v", id, b, err, g.Expired())
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

// stepReader yields scripted chunks. Between chunks it can wait for a condition, which lets a test
// control exactly when bytes (such as a trailing newline) reach the gate.
type stepReader struct {
	steps []func() []byte // each returns the next chunk; nil with done set ends the stream
	i     int
}

func (r *stepReader) Read(p []byte) (int, error) {
	if r.i >= len(r.steps) {
		return 0, io.EOF
	}
	b := r.steps[r.i]()
	r.i++
	return copy(p, b), nil
}

// fakeSDK reads from g like the SDK's JSON decoder: it answers a request as soon as the bytes it
// has read form one complete JSON object, without waiting for the newline.
func fakeSDK(g *frameGate, w io.Writer, answered chan<- struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		var acc []byte
		replied := false
		buf := make([]byte, 4096)
		for {
			n, err := g.Read(buf)
			acc = append(acc, buf[:n]...)
			if !replied && json.Valid(bytes.TrimSpace(acc)) {
				replied = true
				io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
				close(answered)
			}
			if err != nil {
				return
			}
		}
	}()
	return done
}

// Regression: the request object is complete, its newline arrives only after the response was
// written (or would have been). The gate must register the id before the SDK can consume the
// request, otherwise the response is released before the request is counted and EOF waits out the
// whole grace period.
func TestGateRegistersBeforeTheSDKCanConsumeARequest(t *testing.T) {
	g := newFrameGate(2 * time.Second)
	answered := make(chan struct{})
	src := &stepReader{steps: []func() []byte{
		func() []byte { return []byte(`{"jsonrpc":"2.0","id":1,"method":"x"}`) },
		func() []byte {
			select {
			case <-answered: // before the fix: the SDK answered from the unterminated object
			case <-time.After(300 * time.Millisecond): // after the fix: it cannot until the newline
			}
			return []byte("\n")
		},
	}}
	g.start(src)
	start := time.Now()
	done := fakeSDK(g, g.wrapWriter(io.Discard), answered)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("gate never delivered EOF")
	}
	if g.Expired() || time.Since(start) > time.Second {
		t.Fatalf("EOF waited for the grace period (expired=%v, %v)", g.Expired(), time.Since(start))
	}
}

func TestGateUnterminatedFinalFrameIsRegisteredBeforeDelivery(t *testing.T) {
	g := newFrameGate(2 * time.Second)
	answered := make(chan struct{})
	g.start(&stepReader{steps: []func() []byte{
		func() []byte { return []byte(`{"jsonrpc":"2.0","id":1,"method":"x"}`) }, // then EOF, no newline
	}})
	start := time.Now()
	done := fakeSDK(g, g.wrapWriter(io.Discard), answered)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("gate never delivered EOF")
	}
	select {
	case <-answered:
	default:
		t.Fatal("the unterminated final frame was never delivered")
	}
	if g.Expired() || time.Since(start) > time.Second {
		t.Fatalf("expired=%v after %v", g.Expired(), time.Since(start))
	}
}

func TestGatePreservesObjectBytesAndNormalizesDelimiters(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"x"}` + "\r\n" + `{"jsonrpc":"2.0","method":"n"}` + "\n" + `{"jsonrpc":"2.0","method":"tail"}`
	g := newFrameGate(100 * time.Millisecond)
	g.start(strings.NewReader(in))
	go func() {
		time.Sleep(50 * time.Millisecond)
		io.WriteString(g.wrapWriter(io.Discard), `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
	}()
	b, _ := io.ReadAll(g)
	want := strings.ReplaceAll(in, "\r\n", "\n") + "\n"
	if string(b) != want {
		t.Fatalf("got %q want %q", b, want)
	}
}

func TestGateLongLinesAreRejectedBeforeDelivery(t *testing.T) {
	long := `{"jsonrpc":"2.0","id":1,"method":"x","pad":"` + strings.Repeat("a", maxTrackedLine) + `"}` + "\n"
	g := newFrameGate(time.Second)
	g.start(strings.NewReader(long + `{"jsonrpc":"2.0","id":2,"method":"y"}` + "\n"))
	b, err := io.ReadAll(g)
	if !errors.Is(err, errFrameTooLarge) || len(b) != 0 || g.Expired() {
		t.Fatalf("bytes=%d err=%v expired=%v", len(b), err, g.Expired())
	}
}

func TestGateStringIDsMatchAcrossEscapeSpellings(t *testing.T) {
	cases := []struct{ name, req, resp string }{
		{"escaped request, plain response", `"\u0061bc"`, `"abc"`},
		{"plain request, escaped response", `"abc"`, `"\u0061\u0062c"`},
		{"slash escape", `"a\/b"`, `"a/b"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(c.req+c.resp, `\`) {
				t.Fatal("fixture has no backslash escape")
			}
			g := newFrameGate(5 * time.Second)
			pr, pw := io.Pipe()
			g.start(pr)
			io.WriteString(pw, `{"jsonrpc":"2.0","id":`+c.req+`,"method":"x"}`+"\n")
			waitPending(t, g, 1)
			pw.Close()
			done := readAllAsync(g)
			io.WriteString(g.wrapWriter(io.Discard), `{"jsonrpc":"2.0","id":`+c.resp+`,"result":{}}`+"\n")
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("response id did not clear the request")
			}
			if g.Expired() {
				t.Fatal("Expired")
			}
		})
	}
}

func TestGateCancelledEscapedRequestID(t *testing.T) {
	g := newFrameGate(5 * time.Second)
	in := `{"jsonrpc":"2.0","id":"\u0061","method":"x"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"a"}}` + "\n"
	if !strings.Contains(in, `\u0061`) {
		t.Fatal("fixture has no backslash escape")
	}
	g.start(strings.NewReader(in))
	select {
	case <-readAllAsync(g):
	case <-time.After(2 * time.Second):
		t.Fatal("an equivalent cancellation id did not release the request")
	}
	if g.Expired() {
		t.Fatal("Expired")
	}
}

func TestGateStringAndNumberIDsStayDistinct(t *testing.T) {
	for _, c := range []struct{ req, resp string }{{`"1"`, `1`}, {`1`, `"1"`}} {
		g := newFrameGate(150 * time.Millisecond)
		g.start(strings.NewReader(`{"jsonrpc":"2.0","id":` + c.req + `,"method":"x"}` + "\n"))
		waitPending(t, g, 1)
		done := readAllAsync(g)
		io.WriteString(g.wrapWriter(io.Discard), `{"jsonrpc":"2.0","id":`+c.resp+`,"result":{}}`+"\n")
		<-done
		if !g.Expired() {
			t.Fatalf("response id %s answered request id %s", c.resp, c.req)
		}
	}
}

// Regression: a complete request read without a trailing newline, stdin still open, then a signal
// (Stop). The request must be registered, delivered to the SDK and answered before shutdown.
func TestGateStopRegistersAndDeliversAnUnterminatedRequest(t *testing.T) {
	g := newFrameGate(2 * time.Second)
	pr, pw := io.Pipe()
	g.start(pr)
	go io.WriteString(pw, `{"jsonrpc":"2.0","id":1,"method":"x"}`) // no newline; the pipe stays open
	time.Sleep(150 * time.Millisecond)                             // the pump has read it
	g.Stop()
	answered := make(chan struct{})
	done := fakeSDK(g, g.wrapWriter(io.Discard), answered)
	select {
	case <-answered:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("the unterminated request was never delivered to the SDK after Stop")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("no EOF after the request was answered")
	}
	if g.Expired() {
		t.Fatal("Expired")
	}
	pw.Close()
}

// A partial line that is not a complete JSON value is neither registered nor delivered on Stop, so
// the SDK is not handed a truncated frame.
func TestGateStopDropsAnIncompletePartialLine(t *testing.T) {
	g := newFrameGate(time.Second)
	pr, pw := io.Pipe()
	g.start(pr)
	go io.WriteString(pw, `{"jsonrpc":"2.0","id":1,"meth`)
	time.Sleep(150 * time.Millisecond)
	g.Stop()
	select {
	case b := <-readAllAsync(g):
		if b != "" || g.Expired() {
			t.Fatalf("got %q expired=%v", b, g.Expired())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop with an incomplete partial line did not reach EOF")
	}
	pw.Close()
}
