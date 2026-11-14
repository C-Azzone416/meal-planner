package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestGateRetainedIDBudgetDrainsEarlierRequests(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		budget                   idBudget
		completed, pending, over string // raw JSON IDs
		completedReply           string // may use a different escape spelling
	}{
		{"count", idBudget{bytes: 64, count: 2}, `"a"`, "1", "2", `"a"`},
		{"decoded bytes", idBudget{bytes: 3, count: 10}, `"\u00e9"`, `"b"`, `"c"`, `"é"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newFrameGate(2 * time.Second)
			g.idBudget = tc.budget
			out := g.wrapWriter(io.Discard)
			request := func(id string) string {
				return fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"method\":\"ping\"}\n", id)
			}
			admit := func(input string) {
				g.mu.Lock()
				defer g.mu.Unlock()
				g.admit([]byte(input))
			}
			reply := func(id string) {
				t.Helper()
				if _, err := fmt.Fprintf(out, "{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{}}\n", id); err != nil {
					t.Fatal(err)
				}
			}
			readFrame := func(want string) {
				t.Helper()
				got := make([]byte, len(want))
				if _, err := io.ReadFull(g, got); err != nil || string(got) != want {
					t.Fatalf("read = %q (%v), want %q", got, err, want)
				}
			}

			first := request(tc.completed)
			admit(first)
			readFrame(first)
			reply(tc.completedReply)
			pending := request(tc.pending)
			admit(pending + request(tc.over) + request("9"))
			if err := g.InputError(); err == nil || !strings.Contains(err.Error(), "reconnect") {
				t.Fatalf("input error = %v, want a reconnect diagnostic", err)
			}
			var stderr bytes.Buffer
			if !reportInputError(g, slog.New(slog.NewTextHandler(&stderr, nil))) ||
				!strings.Contains(stderr.String(), "reconnect") {
				t.Fatalf("stderr = %s, want reconnect diagnostic", &stderr)
			}
			if len(g.seen) != 2 || len(g.pending) != 1 {
				t.Fatalf("seen=%d pending=%d, want 2 and 1", len(g.seen), len(g.pending))
			}

			readFrame(pending)
			reply(tc.pending)
			got, err := io.ReadAll(g)
			if err != g.InputError() || len(got) != 0 || g.Expired() {
				t.Fatalf("shutdown bytes=%q err=%v expired=%t", got, err, g.Expired())
			}
		})
	}
}
