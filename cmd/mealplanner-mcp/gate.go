package main

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// maxTrackedLine caps the frame bytes before any bytes reach the SDK. The LF
// or CRLF terminator is excluded, and all other padding counts.
const maxTrackedLine = 4 << 20

// The byte limit bounds retained decoded strings without shrinking the existing
// per-frame limit. The count limit also bounds the map cost of short, empty and
// numeric IDs. Both leave room for thousands of ordinary host calls.
const (
	maxSessionIDBytes = 8 << 20
	maxSessionIDs     = 32 << 10
)

type idBudget struct {
	bytes int
	count int
}

// frameGate sits between stdin/stdout and the SDK transport. The SDK treats stdin EOF as the end
// of the session and may drop requests that were read but not yet answered (for example when a
// client pipes `initialize`, `initialized` and a tool call and closes stdin). The gate withholds
// the EOF until every request frame read so far has had a response written, bounded by a grace
// period, so those requests are answered.
//
// A request frame is one JSON object with a string "method" and an "id" that is a string or an
// integer. A notifications/cancelled for a tracked request stops the wait for it.
// Both answered and cancelled IDs remain reserved for the session, so a late
// response or cancellation cannot match another call.
//
// Registration happens before delivery. The pump holds input until a line is complete, registers
// the request id, and only then makes the line available to the SDK reader, so a response can
// never be written for a request that is not yet counted (the SDK's JSON decoder may consume a
// complete object before its newline arrives). At EOF an unterminated final frame is registered and
// then delivered, followed by EOF. The JSON object bytes reach the SDK unchanged;
// outer JSON whitespace is removed and exactly one LF delimiter is appended.
// Invalid frames terminate input before SDK delivery. Earlier accepted
// requests still get their shutdown grace.
type frameGate struct {
	grace    time.Duration
	idBudget idBudget // set before the gate starts

	mu        sync.Mutex
	cond      *sync.Cond
	queue     bytes.Buffer            // registered bytes the SDK may read
	lineBuf   []byte                  // incomplete line held back until its newline (or EOF) arrives
	seen      map[jsonrpc.ID]struct{} // request IDs accepted in this session
	seenBytes int                     // decoded string bytes retained in seen
	pending   map[jsonrpc.ID]struct{} // accepted IDs still awaiting a response
	inputErr  error                   // non-EOF input failure, reported after accepted requests drain
	inEOF     bool                    // underlying reader ended
	stopped   bool                    // Stop was called (signal)
	graceOn   bool                    // the grace period started
	timedOut  bool                    // the grace period elapsed
	expired   bool                    // input was released with requests still unanswered
	deadlineC chan struct{}           // closes at the session deadline, even after cancellation removes pending IDs
}

func newFrameGate(grace time.Duration) *frameGate {
	g := &frameGate{
		grace:     grace,
		idBudget:  idBudget{bytes: maxSessionIDBytes, count: maxSessionIDs},
		seen:      make(map[jsonrpc.ID]struct{}),
		pending:   make(map[jsonrpc.ID]struct{}),
		deadlineC: make(chan struct{}),
	}
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
				if err != io.EOF && g.inputErr == nil {
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
		n := i
		if i < 0 {
			n = len(b)
		}
		if size := len(g.lineBuf) + n; size > maxTrackedLine {
			// Hold one extra trailing CR until we know whether an LF follows.
			// The CR and LF can land in different reads.
			trailingCR := (n > 0 && b[n-1] == '\r') || (n == 0 && g.lineBuf[len(g.lineBuf)-1] == '\r')
			if size != maxTrackedLine+1 || !trailingCR {
				g.rejectInput(errFrameTooLarge)
				return
			}
		}
		g.lineBuf = append(g.lineBuf, b[:n]...)
		if i < 0 {
			return
		}
		if !g.deliver(bytes.TrimSuffix(g.lineBuf, []byte{'\r'})) {
			return
		}
		g.lineBuf = g.lineBuf[:0]
		b = b[i+1:]
	}
}

// release records one response (or cancellation) for id. g.mu must be held.
func (g *frameGate) release(id jsonrpc.ID) {
	delete(g.pending, id)
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
		if b := g.lineBuf; len(b) > maxTrackedLine || json.Valid(b) {
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
	msg, err := jsonrpc.DecodeMessage(l)
	resp, ok := msg.(*jsonrpc.Response)
	if err != nil || !ok {
		return
	}
	r.g.mu.Lock()
	r.g.release(resp.ID)
	r.g.mu.Unlock()
}

func (*responseWriter) Close() error { return nil }

// InputError reports a rejected input frame after the server has stopped.
func (g *frameGate) InputError() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inputErr
}
