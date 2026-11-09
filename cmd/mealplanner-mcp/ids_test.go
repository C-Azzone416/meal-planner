package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Run the real pinned SDK in the built binary. Decode response IDs as raw JSON,
// not float64: doing the latter would hide the very rounding bug under test.
func TestSubprocessRequestIDBoundary(t *testing.T) {
	for _, tc := range []struct {
		id       string
		accepted bool
	}{
		{`0`, true}, {`1`, true}, {`-1`, true},
		{`9007199254740991`, true}, {`-9007199254740991`, true},
		{`"9007199254740993"`, true}, {`"-0"`, true},
		{`-0`, false}, {`0.0`, false}, {`-0.0`, false}, {`2.0`, false},
		{`2e0`, false}, {`1E+0`, false}, {`1.5`, false},
		{`9007199254740992`, false}, {`9007199254740993`, false},
		{`-9007199254740992`, false}, {`9223372036854775807`, false},
		{`1e309`, false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binPath, "--data-dir", t.TempDir())
			// The final frame has no newline, exercising EOF validation as well.
			cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":` + tc.id + `,"method":"ping"}`)
			var out, stderr bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &stderr
			err := cmd.Run()
			if ctx.Err() != nil {
				t.Fatalf("ID triggered shutdown timeout: %s", stderr.String())
			}
			if !tc.accepted {
				if err == nil || out.Len() != 0 || !strings.Contains(stderr.String(), "unsupported request id") {
					t.Fatalf("expected explicit rejection without response, err=%v stdout=%s stderr=%s", err, &out, &stderr)
				}
				return
			}
			if err != nil {
				t.Fatalf("accepted ID failed: %v %s", err, &stderr)
			}
			var f struct {
				ID     json.RawMessage `json:"id"`
				Result json.RawMessage `json:"result"`
			}
			if err := json.Unmarshal(out.Bytes(), &f); err != nil {
				t.Fatal(err)
			}
			if string(f.ID) != tc.id || len(f.Result) == 0 {
				t.Fatalf("ID was not echoed exactly: %s", &out)
			}
		})
	}
}

func TestIDValidationRejectsBeforeAnyMutationFrame(t *testing.T) {
	// Both the rejected request and frames after it must never reach the SDK.
	for _, frame := range []string{
		`{"jsonrpc":"2.0","id":9007199254740993,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":9007199254740993,"method":"ping","params":"invalid"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":9007199254740993}}`,
	} {
		var out, stderr bytes.Buffer
		input := `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n" + frame + "\n" + `{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\n"
		code := run(context.Background(), []string{"--data-dir", t.TempDir()}, noEnv, strings.NewReader(input), &out, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "unsupported request id") {
			t.Fatalf("code=%d stderr=%s", code, &stderr)
		}
		if strings.Contains(out.String(), `"id":2`) {
			t.Fatalf("processed frame after rejection: %s", &out)
		}
	}
}

func TestProductionGateRejectsOversizedFrameBeforeDelivery(t *testing.T) {
	g := newFrameGate(time.Second)
	g.start(strings.NewReader(`{"jsonrpc":"2.0","id":9007199254740993,"method":"ping","pad":"` + strings.Repeat("x", maxTrackedLine) + `"}`))
	got, err := io.ReadAll(g)
	if !errors.Is(err, errFrameTooLarge) || len(got) != 0 || g.Expired() {
		t.Fatalf("len=%d err=%v expired=%v", len(got), err, g.Expired())
	}
}

// Reject ambiguous envelope members before either the gate or SDK can select
// one of the values. Exercise reversed order, escaped keys, case variants,
// cancellation params, and an invalid params type that once bypassed validation.
func TestSubprocessRejectsAmbiguousEnvelope(t *testing.T) {
	cases := []string{
		`{"jsonrpc":"2.0","id":9007199254740993,"method":"ping"}{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":9007199254740993,"id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"id":9007199254740993,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"ID":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","ID":2,"id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","Id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"\u0069d":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"Method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","METHOD":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":{},"Params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":{},"params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"requestId":2}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"RequestID":2}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"RequestID":2,"requestId":1}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"REQUESTID":1}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"request\u0049d":2}}`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binPath, "--data-dir", t.TempDir())
			cmd.Stdin = strings.NewReader(input + "\n" + `{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n")
			var out, stderr bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &stderr
			err := cmd.Run()
			if ctx.Err() != nil || err == nil || out.Len() != 0 || !strings.Contains(stderr.String(), "invalid protocol envelope") {
				t.Fatalf("err=%v timeout=%v stdout=%s stderr=%s", err, ctx.Err(), &out, &stderr)
			}
		})
	}
}

func TestGateTracksRequestWithMalformedParams(t *testing.T) {
	g := newFrameGate(time.Second)
	g.start(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping","params":"invalid"}` + "\n"))
	waitPending(t, g, 1)
	done := readAllAsync(g)
	io.WriteString(g.wrapWriter(io.Discard), `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"invalid"}}`+"\n")
	<-done
	if g.Expired() {
		t.Fatal("accepted malformed-params request was not drained")
	}
}

func TestRejectedFramesDrainEarlierAcceptedRequests(t *testing.T) {
	for _, frame := range []string{`oops`, `[]`, `[ {"jsonrpc":"2.0","id":2,"method":"ping"} ]`, `null`, `42`, `"value"`} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, binPath, "--data-dir", t.TempDir())
		cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n" + frame + "\n" + `{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n")
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		cancel()
		if err == nil || !strings.Contains(stderr.String(), "invalid protocol envelope") {
			t.Fatalf("frame=%s err=%v stderr=%s", frame, err, &stderr)
		}
		var response struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.ID != 1 || len(response.Result) == 0 {
			t.Fatalf("frame=%s prior request not drained exactly once: %s err=%v", frame, &out, err)
		}
	}
}
