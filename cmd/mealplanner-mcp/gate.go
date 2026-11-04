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

// maxTrackedLine bounds the partial-line buffer used to spot request frames. A longer line (4 MiB,
// far above MaxInputBytes, so no legitimate frame) is still passed through to the SDK untouched; it
// is just not tracked, so the gate never waits for it.
const maxTrackedLine = 4 << 20

// frameGate sits between stdin/stdout and the SDK transport. The SDK treats stdin EOF as the end
// of the session and may drop requests that were read but not yet answered (for example when a
// client pipes `initialize`, `initialized` and a tool call and closes stdin). The gate withholds
// the EOF until every request frame read so far has had a response written, bounded by a grace
// period, so those requests are answered.
//
// A request frame is one JSON object with a string "method" and an "id" that is a string or an integer. Notifications,
// batches and lines that are not JSON objects are not tracked: nothing waits on them. A
// notifications/cancelled for a tracked request stops the wait for it. Frames are counted when the
// bytes are read, before the SDK sees them, so there is no window in which a frame has been read
// but is not counted.
type frameGate struct {
	grace time.Duration

	mu       sync.Mutex
	cond     *sync.Cond
	queue    bytes.Buffer   // bytes read from stdin, not yet handed to the SDK
	lineBuf  []byte         // partial line seen by the reader pump
	skipLine bool           // the current line exceeded maxTrackedLine
	pending  map[string]int // request id -> responses still owed (a client may reuse an id)
	inEOF    bool           // underlying reader ended
	stopped  bool           // Stop was called (signal)
	graceOn  bool           // the grace period started
	timedOut bool           // the grace period elapsed
	expired  bool           // input was released with requests still unanswered
	expiredC chan struct{}  // closed when expired becomes true
}

func newFrameGate(grace time.Duration) *frameGate {
	g := &frameGate{grace: grace, pending: map[string]int{}, expiredC: make(chan struct{})}
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
				g.track(buf[:n])
				g.queue.Write(buf[:n])
				g.cond.Broadcast()
				g.mu.Unlock()
			}
			if err != nil {
				g.mu.Lock()
				if !g.skipLine && len(g.lineBuf) > 0 {
					g.line(g.lineBuf) // an unterminated final frame is still a frame
					g.lineBuf = nil
				}
				g.inEOF = true
				g.cond.Broadcast()
				g.mu.Unlock()
				return
			}
		}
	}()
}

// track scans b for complete lines and records request ids. g.mu must be held.
func (g *frameGate) track(b []byte) {
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			if !g.skipLine {
				if len(g.lineBuf)+len(b) > maxTrackedLine {
					g.skipLine, g.lineBuf = true, nil
				} else {
					g.lineBuf = append(g.lineBuf, b...)
				}
			}
			return
		}
		if !g.skipLine {
			g.line(append(g.lineBuf, b[:i]...))
		}
		g.lineBuf, g.skipLine = g.lineBuf[:0], false
		b = b[i+1:]
	}
}

type frame struct {
	Method *string         `json:"method"`
	ID     json.RawMessage `json:"id"`
	Params struct {
		RequestID json.RawMessage `json:"requestId"`
	} `json:"params"`
}

// idKey returns the canonical form of a JSON-RPC id, so a request id and its response id match even
// if the number is spelled differently (2.0, 2, 2e0). ok is false for anything that is not a valid
// MCP id: only a string or an integer is, so null, booleans, arrays, objects and fractions (1.5) are
// never awaited.
func idKey(raw json.RawMessage) (key string, ok bool) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return "", false
	}
	switch t[0] {
	case '"':
		var out bytes.Buffer
		if json.Compact(&out, t) != nil {
			return "", false
		}
		return out.String(), true
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil || math.IsInf(f, 0) || f != math.Trunc(f) {
			return "", false
		}
		return strconv.FormatFloat(f, 'f', -1, 64), true
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
		if key, ok := idKey(f.Params.RequestID); ok {
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

// Read implements io.Reader for the SDK transport.
func (g *frameGate) Read(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for {
		if g.queue.Len() > 0 {
			return g.queue.Read(p)
		}
		if g.inEOF || g.stopped {
			if !g.graceOn {
				g.graceOn = true
				time.AfterFunc(g.grace, func() {
					g.mu.Lock()
					g.timedOut = true
					g.cond.Broadcast()
					g.mu.Unlock()
				})
			}
			if len(g.pending) == 0 || g.timedOut {
				if len(g.pending) > 0 && !g.expired {
					g.expired = true
					close(g.expiredC)
				}
				return 0, io.EOF
			}
		}
		g.cond.Wait()
	}
}

// Stop ends input as if stdin had reached EOF: bytes already read are still delivered and answered
// (within the grace period), nothing further is read.
func (g *frameGate) Stop() {
	g.mu.Lock()
	g.stopped = true
	g.cond.Broadcast()
	g.mu.Unlock()
}

// Expired reports whether the grace period ran out with requests still unanswered.
func (g *frameGate) Expired() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.expired
}

// ExpiredC is closed when the grace period ran out with requests unanswered.
func (g *frameGate) ExpiredC() <-chan struct{} { return g.expiredC }

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
