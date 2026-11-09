package main

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"sync"
	"time"
)

// maxTrackedLine caps a complete protocol frame before any bytes reach the SDK.
const maxTrackedLine = 4 << 20

// frameGate sits between stdin/stdout and the SDK transport. The SDK treats stdin EOF as the end
// of the session and may drop requests that were read but not yet answered (for example when a
// client pipes `initialize`, `initialized` and a tool call and closes stdin). The gate withholds
// the EOF until every request frame read so far has had a response written, bounded by a grace
// period, so those requests are answered.
//
// A request frame is one JSON object with a string "method" and an "id" that is a string or an
// integer. Notifications, batches and lines that are not JSON objects are not tracked: nothing
// waits on them. A notifications/cancelled for a tracked request stops the wait for it.
//
// Registration happens before delivery. The pump holds input until a line is complete, registers
// the request id, and only then makes the line available to the SDK reader, so a response can
// never be written for a request that is not yet counted (the SDK's JSON decoder may consume a
// complete object before its newline arrives). At EOF an unterminated final frame is registered and
// then delivered, followed by EOF. The JSON object bytes reach the SDK unchanged;
// outer JSON whitespace is removed and exactly one LF delimiter is appended.
// Unsupported IDs, ambiguous envelope keys and overlong frames terminate input
// before SDK delivery. Earlier accepted requests still get their shutdown grace.
type frameGate struct {
	grace time.Duration

	mu        sync.Mutex
	cond      *sync.Cond
	queue     bytes.Buffer   // registered bytes the SDK may read
	lineBuf   []byte         // incomplete line held back until its newline (or EOF) arrives
	pending   map[string]int // request id -> responses still owed (a client may reuse an id)
	inputErr  error          // non-EOF input failure, reported after accepted requests drain
	inEOF     bool           // underlying reader ended
	stopped   bool           // Stop was called (signal)
	graceOn   bool           // the grace period started
	timedOut  bool           // the grace period elapsed
	expired   bool           // input was released with requests still unanswered
	deadlineC chan struct{}  // closes at the session deadline, even after cancellation removes pending IDs
}

func newFrameGate(grace time.Duration) *frameGate {
	g := &frameGate{grace: grace, pending: map[string]int{}, deadlineC: make(chan struct{})}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// start reads src in the background. Call once. The pump has no backpressure: it queues everything
// it reads, however slowly the SDK consumes it. That is acceptable for a local stdio server whose
// client is trusted to be the user's own agent host (the same trust model as the rest of this
// slice); a hostile peer on stdin could already run anything the user can.
func (g *frameGate) start(src io.Reader) {
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				g.mu.Lock()
				if g.stopped {
					g.mu.Unlock()
					return // after Stop nothing new is accepted
				}
				g.admit(buf[:n])
				if g.inputErr != nil {
					g.cond.Broadcast()
					g.mu.Unlock()
					return
				}
				g.cond.Broadcast()
				g.mu.Unlock()
			}
			if err != nil {
				g.mu.Lock()
				if !g.stopped && len(g.lineBuf) > 0 {
					g.deliver(g.lineBuf) // validate and register before delivery
					g.lineBuf = nil      // ... then deliver the unterminated final frame
				}
				if err != io.EOF {
					g.inputErr = err
				}
				g.inEOF = true
				g.startGrace()
				g.cond.Broadcast()
				g.mu.Unlock()
				return
			}
		}
	}()
}

// admit registers complete lines in b and queues them; an incomplete tail is held back. g.mu must
// be held.
func (g *frameGate) admit(b []byte) {
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			if len(g.lineBuf)+len(b) > maxTrackedLine {
				g.rejectInput(errFrameTooLarge)
			} else {
				g.lineBuf = append(g.lineBuf, b...)
			}
			return
		}
		if len(g.lineBuf)+i > maxTrackedLine {
			g.rejectInput(errFrameTooLarge)
			return
		}
		g.lineBuf = append(g.lineBuf, b[:i+1]...)
		if !g.deliver(g.lineBuf) {
			return
		}
		g.lineBuf = g.lineBuf[:0]
		b = b[i+1:]
	}
}

type frame struct {
	Method *string         `json:"method"`
	ID     json.RawMessage `json:"id"`
	Params json.RawMessage `json:"params"`
}

// idKey returns the canonical form of a JSON-RPC id, so a request id and its response id match even
// if the spelling differs (2.0, 2, 2e0 or "\u0061", "a"). ok is false for anything that is not a valid
// MCP id: only a string or an integer is, so null, booleans, arrays, objects and fractions (1.5) are
// never awaited.
func idKey(raw json.RawMessage) (key string, ok bool) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return "", false
	}
	switch t[0] {
	case '"':
		// Decode and re-encode, so "\u0061" and "a" are the same id. Strings and numbers live in
		// separate key spaces.
		var str string
		if json.Unmarshal(t, &str) != nil {
			return "", false
		}
		enc, err := json.Marshal(str)
		if err != nil {
			return "", false
		}
		return "s:" + string(enc), true
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil || math.IsInf(f, 0) || f != math.Trunc(f) {
			return "", false
		}
		return "n:" + strconv.FormatFloat(f, 'f', -1, 64), true
	}
	return "", false
}

func (g *frameGate) line(l []byte) {
	var f frame
	if json.Unmarshal(l, &f) != nil || f.Method == nil {
		return
	}
	if key, ok := idKey(f.ID); ok {
		g.pending[key]++
	} else if *f.Method == "notifications/cancelled" {
		var params struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(f.Params, &params) != nil {
			return
		}
		if key, ok := idKey(params.RequestID); ok {
			g.release(key)
		}
	}
}

// release records one response (or cancellation) for id. g.mu must be held.
func (g *frameGate) release(id string) {
	if g.pending[id] <= 1 {
		delete(g.pending, id)
	} else {
		g.pending[id]--
	}
	g.cond.Broadcast()
}

// startGrace starts the session deadline when input ends, not only when the
// SDK next asks for bytes. Even a stalled transport/handler must remain bounded.
// g.mu must be held. Repeated EOF, rejection and Stop signals share one timer.
func (g *frameGate) startGrace() {
	if g.graceOn {
		return
	}
	g.graceOn = true
	time.AfterFunc(g.grace, func() {
		g.mu.Lock()
		g.timedOut = true
		close(g.deadlineC)
		g.cond.Broadcast()
		g.mu.Unlock()
	})
}

// Read implements io.Reader for the SDK transport.
func (g *frameGate) Read(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for {
		if g.queue.Len() > 0 {
			return g.queue.Read(p)
		}
		if g.inEOF || g.stopped {
			g.startGrace()
			if len(g.pending) == 0 || g.timedOut {
				if len(g.pending) > 0 && !g.expired {
					g.expired = true
				}
				if g.inputErr != nil {
					return 0, g.inputErr
				}
				return 0, io.EOF
			}
		}
		g.cond.Wait()
	}
}

// Stop ends input as if stdin had reached EOF: bytes already read, including a final unterminated
// frame, are still registered, delivered and answered (within the grace period); nothing further is
// read.
func (g *frameGate) Stop() {
	g.mu.Lock()
	if !g.stopped && len(g.lineBuf) > 0 {
		// Same as EOF: a partial line that is already a complete JSON value is a frame whose newline
		// has not arrived. Register it, then deliver it with the newline added so the SDK can
		// process it now. A partial line that is not valid JSON is dropped (a truncated frame would
		// only make the SDK fail), and nothing waits on it.
		if b := g.lineBuf; json.Valid(b) {
			g.deliver(b)
		}
		g.lineBuf = nil
	}
	g.stopped = true
	g.startGrace()
	g.cond.Broadcast()
	g.mu.Unlock()
}

// Expired reports whether the grace period ran out with requests still unanswered.
func (g *frameGate) Expired() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.expired
}

// DeadlineC bounds the whole SDK session shutdown, not just response tracking.
// A valid cancellation can remove the last pending ID while its handler is
// still unwinding; that must not make an uncooperative handler unbounded.
func (g *frameGate) DeadlineC() <-chan struct{} { return g.deadlineC }

func (g *frameGate) Close() error { return nil }

// responseWriter passes the SDK's output to w and marks a request answered once its response has
// been written. A response is a frame with an id and no method.
type responseWriter struct {
	g   *frameGate
	w   io.Writer
	mu  sync.Mutex
	buf []byte
}

func (g *frameGate) wrapWriter(w io.Writer) io.WriteCloser { return &responseWriter{g: g, w: w} }

func (r *responseWriter) Write(p []byte) (int, error) {
	n, err := r.w.Write(p)
	if n > 0 {
		r.mu.Lock()
		r.buf = append(r.buf, p[:n]...)
		for {
			i := bytes.IndexByte(r.buf, '\n')
			if i < 0 {
				if len(r.buf) > maxTrackedLine {
					r.buf = nil
				}
				break
			}
			r.answered(r.buf[:i])
			r.buf = r.buf[i+1:]
		}
		r.mu.Unlock()
	}
	return n, err
}

func (r *responseWriter) answered(l []byte) {
	var f frame
	if json.Unmarshal(l, &f) != nil || f.Method != nil {
		return
	}
	key, ok := idKey(f.ID)
	if !ok {
		return
	}
	r.g.mu.Lock()
	r.g.release(key)
	r.g.mu.Unlock()
}

func (*responseWriter) Close() error { return nil }

// InputError reports a rejected input frame after the server has stopped.
func (g *frameGate) InputError() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inputErr
}
