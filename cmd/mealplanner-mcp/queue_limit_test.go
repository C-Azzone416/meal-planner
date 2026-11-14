package main

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestQueueLimitRejectsBeforeRegistration(t *testing.T) {
	g := newFrameGate(time.Millisecond)
	g.mu.Lock()
	g.queue.Write(bytes.Repeat([]byte{' '}, maxQueuedBytes-1))
	accepted := g.deliver([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if accepted || !errors.Is(g.inputErr, errQueueFull) || len(g.seen) != 0 || len(g.pending) != 0 || g.queue.Len() != maxQueuedBytes-1 {
		t.Fatal("overload changed admission state")
	}
	g.mu.Unlock()
	g.Stop()
}

func TestNotificationFloodQueueIsBounded(t *testing.T) {
	g := newFrameGate(time.Millisecond)
	frame := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{"padding":"` + string(bytes.Repeat([]byte{'x'}, 1<<20)) + `"}}`)
	g.mu.Lock()
	accepted := 0
	for g.deliver(frame) {
		accepted++
		if accepted > 10 {
			t.Fatal("unbounded queue")
		}
	}
	if accepted == 0 || g.queue.Len() > maxQueuedBytes || !errors.Is(g.inputErr, errQueueFull) {
		t.Fatal("queue bound not enforced")
	}
	g.mu.Unlock()
	g.Stop()
}
