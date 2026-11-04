package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// binPath is the built mealplanner-mcp binary, shared by the subprocess tests.
var binPath, stallBinPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mealplanner-mcp-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "mealplanner-mcp")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	stallBinPath = filepath.Join(dir, "mealplanner-mcp-stall")
	build = exec.Command("go", "build", "-tags", "teststall", "-o", stallBinPath, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "stall build failed: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// proc is a running server driven over raw newline-delimited JSON-RPC, so the test sees exactly
// what is on stdout.
type proc struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *bytes.Buffer
	mu     sync.Mutex
	frames []map[string]any // every stdout line, decoded
	bad    []string         // stdout lines that are not JSON-RPC frames
	wake   chan struct{}
	nextID int
	modern string // protocol version >= 2026-07-28: stateless requests carry a _meta triple
}

func start(t *testing.T, dataDir string) *proc { return startBin(t, binPath, dataDir) }

// startBin starts bin with extra environment entries (KEY=VALUE).
func startBin(t *testing.T, bin, dataDir string, env ...string) *proc {
	t.Helper()
	cmd := exec.Command(bin, "--data-dir", dataDir)
	cmd.Env = append(os.Environ(), env...)
	p := &proc{t: t, cmd: cmd, stderr: &bytes.Buffer{}, wake: make(chan struct{}, 1024)}
	cmd.Stderr = p.stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	p.stdin = in
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			line := sc.Text()
			var f map[string]any
			p.mu.Lock()
			if json.Unmarshal([]byte(line), &f) != nil || f["jsonrpc"] != "2.0" {
				p.bad = append(p.bad, line)
			} else {
				p.frames = append(p.frames, f)
			}
			p.mu.Unlock()
			p.wake <- struct{}{}
		}
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return p
}

func (p *proc) sendRaw(line string) {
	p.t.Helper()
	if _, err := io.WriteString(p.stdin, line+"\n"); err != nil {
		p.t.Fatal(err)
	}
}

// rpc sends a request and waits for the response with the same id.
func (p *proc) rpc(method string, params any) map[string]any {
	p.t.Helper()
	p.nextID++
	id := p.nextID
	if p.modern != "" {
		m, _ := params.(map[string]any)
		if m == nil {
			m = map[string]any{}
		}
		m["_meta"] = map[string]any{
			mcp.MetaKeyProtocolVersion:    p.modern,
			mcp.MetaKeyClientInfo:         map[string]any{"name": "subprocess-test", "version": "0"},
			mcp.MetaKeyClientCapabilities: map[string]any{},
		}
		params = m
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	p.sendRaw(string(b))
	return p.await(func(f map[string]any) bool { return f["id"] == float64(id) })
}

func (p *proc) notify(method string) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	p.sendRaw(string(b))
}

func (p *proc) await(match func(map[string]any) bool) map[string]any {
	p.t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		p.mu.Lock()
		for _, f := range p.frames {
			if match(f) {
				p.mu.Unlock()
				return f
			}
		}
		p.mu.Unlock()
		select {
		case <-p.wake:
		case <-deadline:
			p.t.Fatalf("timed out waiting for a frame; stderr:\n%s", p.stderr.String())
		}
	}
}

func (p *proc) initialize(protocol string) map[string]any {
	p.t.Helper()
	if protocol >= "2026-07-28" {
		// Stateless protocol: no initialize handshake; the client probes with server/discover.
		p.modern = protocol
		r := p.rpc("server/discover", map[string]any{})
		if r["error"] != nil {
			p.t.Fatalf("server/discover(%s): %v", protocol, r["error"])
		}
		res := r["result"].(map[string]any)
		ok := false
		for _, v := range res["supportedVersions"].([]any) {
			ok = ok || v == protocol
		}
		if !ok {
			p.t.Fatalf("server does not list %s in supportedVersions: %v", protocol, res["supportedVersions"])
		}
		res["protocolVersion"] = protocol
		return res
	}
	r := p.rpc("initialize", map[string]any{
		"protocolVersion": protocol,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "subprocess-test", "version": "0"},
	})
	if r["error"] != nil {
		p.t.Fatalf("initialize(%s): %v", protocol, r["error"])
	}
	p.notify("notifications/initialized")
	return r["result"].(map[string]any)
}

// tool calls a tool and returns (isError, structuredContent).
func (p *proc) tool(name string, args any) (bool, map[string]any) {
	p.t.Helper()
	r := p.rpc("tools/call", map[string]any{"name": name, "arguments": args})
	if r["error"] != nil {
		p.t.Fatalf("%s: protocol error %v", name, r["error"])
	}
	res := r["result"].(map[string]any)
	sc, _ := res["structuredContent"].(map[string]any)
	content := res["content"].([]any)
	var fromText map[string]any
	if err := json.Unmarshal([]byte(content[0].(map[string]any)["text"].(string)), &fromText); err != nil {
		p.t.Fatalf("%s: text block is not JSON: %v", name, err)
	}
	a, _ := json.Marshal(sc)
	b, _ := json.Marshal(fromText)
	if !bytes.Equal(a, b) {
		p.t.Fatalf("%s: structuredContent and text differ", name)
	}
	isErr, _ := res["isError"].(bool)
	return isErr, sc
}

func (p *proc) shutdown() int {
	p.t.Helper()
	_ = p.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return 0
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		p.t.Fatal(err)
	case <-time.After(10 * time.Second):
		p.t.Fatal("server did not exit after stdin EOF")
	}
	return -1
}

func (p *proc) assertStdoutClean() {
	p.t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.bad) > 0 {
		p.t.Fatalf("stdout contains non-JSON-RPC lines: %q", p.bad)
	}
	if len(p.frames) == 0 {
		p.t.Fatal("no frames on stdout")
	}
}

func TestSubprocessEndToEnd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	const title = "Synthetic Subprocess Stew"

	p := start(t, dir)
	init := p.initialize(mcp.SupportedProtocolVersions()[0])
	if init["protocolVersion"] != mcp.SupportedProtocolVersions()[0] {
		t.Fatalf("protocolVersion = %v", init["protocolVersion"])
	}
	caps := init["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok || len(caps) != 1 {
		t.Fatalf("capabilities = %v, want tools only", caps)
	}

	lt := p.rpc("tools/list", map[string]any{})["result"].(map[string]any)["tools"].([]any)
	var names []string
	for _, x := range lt {
		names = append(names, x.(map[string]any)["name"].(string))
	}
	if got, want := strings.Join(names, ","), "draft_create,draft_get,draft_list,draft_update,draft_validate,server_status"; sortedJoin(names) != sortedJoin(strings.Split(want, ",")) {
		t.Fatalf("tools = %s", got)
	}

	isErr, created := p.tool("draft_create", map[string]any{"draft": map[string]any{"title": title}})
	if isErr {
		t.Fatalf("create: %v", created)
	}
	id := created["draft"].(map[string]any)["id"].(string)
	if _, got := p.tool("draft_get", map[string]any{"id": id}); got["draft"].(map[string]any)["title"] != title {
		t.Fatalf("get = %v", got)
	}
	if _, page := p.tool("draft_list", map[string]any{}); page["total"].(float64) != 1 {
		t.Fatalf("list = %v", page)
	}
	if isErr, upd := p.tool("draft_update", map[string]any{"id": id, "expected_revision": 1, "draft": map[string]any{"title": title, "notes": "second"}}); isErr || upd["draft"].(map[string]any)["revision"].(float64) != 2 {
		t.Fatalf("update = %v", upd)
	}
	// Stale update: conflict, nothing written.
	isErr, conflict := p.tool("draft_update", map[string]any{"id": id, "expected_revision": 1, "draft": map[string]any{"title": "Stale"}})
	if er := conflict["error"].(map[string]any); !isErr || er["code"] != "revision_mismatch" || er["current_revision"].(float64) != 2 {
		t.Fatalf("conflict = %v", conflict)
	}
	// Invalid input: tool error with issues. Invalid candidate to validate: success.
	if isErr, bad := p.tool("draft_create", map[string]any{"draft": map[string]any{"title": " "}}); !isErr || bad["error"].(map[string]any)["code"] != "validation_failed" {
		t.Fatalf("invalid create = %v", bad)
	}
	if isErr, v := p.tool("draft_validate", map[string]any{"draft": map[string]any{"title": " "}}); isErr || v["validation"].(map[string]any)["structurally_valid"] != false {
		t.Fatalf("invalid validate = %v", v)
	}
	_, st := p.tool("server_status", map[string]any{})
	if st["data_dir"].(map[string]any)["source"] != "flag" || st["store"].(map[string]any)["draft_count"].(float64) != 1 {
		t.Fatalf("status = %v", st)
	}
	if strings.Contains(fmt.Sprint(st), dir) {
		t.Fatal("status exposes the data directory path")
	}

	// Unknown method and unknown tool are protocol errors and change nothing.
	if r := p.rpc("no/such/method", map[string]any{}); r["error"] == nil {
		t.Fatalf("unknown method accepted: %v", r)
	}
	if r := p.rpc("tools/call", map[string]any{"name": "draft_delete", "arguments": map[string]any{"id": id}}); r["error"] == nil {
		t.Fatalf("unknown tool accepted: %v", r)
	}

	if code := p.shutdown(); code != 0 {
		t.Fatalf("exit code = %d, stderr:\n%s", code, p.stderr.String())
	}
	p.assertStdoutClean()
	if strings.Contains(p.stderr.String(), title) || strings.Contains(p.stderr.String(), dir) {
		t.Fatalf("stderr leaks a payload or path:\n%s", p.stderr.String())
	}
	if !strings.Contains(p.stderr.String(), "tool=draft_create") {
		t.Fatalf("stderr has no call log:\n%s", p.stderr.String())
	}

	// Restart on the same data directory: the draft persisted, the malformed input created nothing.
	q := start(t, dir)
	q.initialize(mcp.SupportedProtocolVersions()[0])
	_, got := q.tool("draft_get", map[string]any{"id": id})
	d := got["draft"].(map[string]any)
	if d["title"] != title || d["revision"].(float64) != 2 || d["notes"] != "second" {
		t.Fatalf("after restart: %v", d)
	}
	if _, page := q.tool("draft_list", map[string]any{}); page["total"].(float64) != 1 {
		t.Fatalf("list after restart = %v", page)
	}
	if code := q.shutdown(); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	q.assertStdoutClean()
}

// With the pinned SDK (v1.8.0) a line that is not valid JSON ends the session: Run returns the
// decode error, the server exits 1 and writes nothing malformed to stdout. This pins that behavior
// and that no handler ran and nothing was stored.
func TestSubprocessMalformedFrameEndsSessionWithoutMutation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	p := start(t, dir)
	p.initialize(mcp.SupportedProtocolVersions()[1])
	if isErr, _ := p.tool("draft_create", map[string]any{"draft": map[string]any{"title": "Before"}}); isErr {
		t.Fatal("create failed")
	}
	p.sendRaw(`{"jsonrpc":"2.0","id":99,"method":"tools/call","params":{"name":"draft_create","arguments":`)
	p.sendRaw(`this is not json`)
	if code := p.shutdown(); code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", code, p.stderr.String())
	}
	p.assertStdoutClean()
	if strings.Contains(p.stderr.String(), "this is not json") || strings.Contains(p.stderr.String(), dir) {
		t.Fatalf("stderr echoes input or a path:\n%s", p.stderr.String())
	}
	q := start(t, dir)
	q.initialize(mcp.SupportedProtocolVersions()[1])
	if _, page := q.tool("draft_list", map[string]any{}); page["total"].(float64) != 1 {
		t.Fatalf("list = %v, want only the first draft", page)
	}
	if code := q.shutdown(); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
}

func sortedJoin(s []string) string {
	c := append([]string(nil), s...)
	for i := range c {
		for j := i + 1; j < len(c); j++ {
			if c[j] < c[i] {
				c[i], c[j] = c[j], c[i]
			}
		}
	}
	return strings.Join(c, ",")
}

// Every protocol version the pinned SDK supports must initialize and serve the tools.
func TestSubprocessProtocolVersions(t *testing.T) {
	versions := mcp.SupportedProtocolVersions()
	if len(versions) < 2 {
		t.Fatalf("SDK supports %v; the test needs a current and a legacy version", versions)
	}
	for _, v := range versions {
		t.Run(v, func(t *testing.T) {
			p := start(t, filepath.Join(t.TempDir(), "data"))
			res := p.initialize(v)
			if res["protocolVersion"] != v {
				t.Fatalf("negotiated %v, want %s", res["protocolVersion"], v)
			}
			tools := p.rpc("tools/list", map[string]any{})["result"].(map[string]any)["tools"].([]any)
			if len(tools) != 6 {
				t.Fatalf("got %d tools", len(tools))
			}
			if isErr, st := p.tool("server_status", map[string]any{}); isErr || st["mode"] != "local" {
				t.Fatalf("status = %v", st)
			}
			if code := p.shutdown(); code != 0 {
				t.Fatalf("exit code = %d", code)
			}
			p.assertStdoutClean()
		})
	}
}

func TestSubprocessUnknownProtocolVersionNegotiatesDown(t *testing.T) {
	p := start(t, filepath.Join(t.TempDir(), "data"))
	res := p.initialize("1999-01-01")
	supported := mcp.SupportedProtocolVersions()
	found := false
	for _, v := range supported {
		found = found || res["protocolVersion"] == v
	}
	if !found {
		t.Fatalf("negotiated %v, want one of %v", res["protocolVersion"], supported)
	}
	if code := p.shutdown(); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestSubprocessBadDataDirExitsBeforeProtocol(t *testing.T) {
	cmd := exec.Command(binPath, "--data-dir", "relative")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 2 {
		t.Fatalf("err = %v, want exit 2", err)
	}
	if out.Len() != 0 || !strings.Contains(errb.String(), "absolute") {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errb.String())
	}
}

func TestSubprocessVersionFlag(t *testing.T) {
	out, err := exec.Command(binPath, "--version").Output()
	if err != nil || !strings.HasPrefix(string(out), "mealplanner-mcp ") || strings.Count(string(out), "\n") != 1 {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

// The SDK's own client (current protocol, stateless discover) and its legacy initialize path both
// work against the built binary.
func TestSubprocessWithSDKClient(t *testing.T) {
	for name, opts := range map[string]*mcp.ClientSessionOptions{
		"current": nil,
		"legacy":  {ProtocolVersion: mcp.SupportedProtocolVersions()[1]},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.Command(binPath, "--data-dir", filepath.Join(t.TempDir(), "data"))
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "sdk-client", Version: "0"}, nil).
				Connect(ctx, &mcp.CommandTransport{Command: cmd}, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			tools, err := cs.ListTools(ctx, nil)
			if err != nil || len(tools.Tools) != 6 {
				t.Fatalf("tools=%v err=%v", tools, err)
			}
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "draft_create", Arguments: map[string]any{"draft": map[string]any{"title": "Via SDK client"}}})
			if err != nil || res.IsError {
				t.Fatalf("create: %v %+v", err, res)
			}
		})
	}
}

func (p *proc) frameLine(id int, method string, params any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return string(b)
}

// waitExit waits for the process to exit by itself and returns its exit code.
func (p *proc) waitExit(d time.Duration) int {
	p.t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return 0
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		p.t.Fatal(err)
	case <-time.After(d):
		p.t.Fatalf("process did not exit within %v; stderr:\n%s", d, p.stderr.String())
	}
	return -1
}

func createLine(p *proc, id int, title string) string {
	return p.frameLine(id, "tools/call", map[string]any{"name": "draft_create", "arguments": map[string]any{"draft": map[string]any{"title": title}}})
}

func (p *proc) snapshot() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any(nil), p.frames...)
}

func (p *proc) hasResponse(id int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, f := range p.frames {
		if f["id"] == float64(id) && f["method"] == nil {
			return true
		}
	}
	return false
}

func countDrafts(t *testing.T, dir string) int {
	q := start(t, dir)
	q.initialize(mcp.SupportedProtocolVersions()[1])
	_, page := q.tool("draft_list", map[string]any{})
	if code := q.shutdown(); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	return int(page["total"].(float64))
}

// printf 'initialize\ninitialized\ncreate\n' | mealplanner-mcp: both requests are answered and the
// draft is stored, even though stdin hits EOF immediately.
func TestSubprocessPipedRequestsAreAnsweredBeforeEOF(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	p := start(t, dir)
	legacy := mcp.SupportedProtocolVersions()[1]
	p.sendRaw(p.frameLine(1, "initialize", map[string]any{"protocolVersion": legacy, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "pipe", "version": "0"}}))
	p.sendRaw(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	p.sendRaw(createLine(p, 2, "Piped Draft"))
	_ = p.stdin.Close()
	if code := p.waitExit(10 * time.Second); code != 0 {
		t.Fatalf("exit code = %d, stderr:\n%s", code, p.stderr.String())
	}
	if !p.hasResponse(1) || !p.hasResponse(2) {
		t.Fatalf("missing responses; frames: %v", p.snapshot())
	}
	p.assertStdoutClean()
	if n := countDrafts(t, dir); n != 1 {
		t.Fatalf("stored drafts = %d, want 1", n)
	}
}

// A request still running when stdin reaches EOF completes and is answered.
func TestSubprocessInFlightRequestCompletesAtEOF(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	p := startBin(t, stallBinPath, dir, "MEALPLANNER_TEST_STALL_MS=800")
	p.initialize(mcp.SupportedProtocolVersions()[1])
	p.sendRaw(createLine(p, 50, "Slow Draft"))
	_ = p.stdin.Close()
	if code := p.waitExit(10 * time.Second); code != 0 {
		t.Fatalf("exit code = %d, stderr:\n%s", code, p.stderr.String())
	}
	if !p.hasResponse(50) {
		t.Fatalf("the in-flight request was not answered; frames: %v", p.snapshot())
	}
	p.assertStdoutClean()
	if n := countDrafts(t, dir); n != 1 {
		t.Fatalf("stored drafts = %d, want 1", n)
	}
}

func TestSubprocessSIGTERMWhileIdleExitsZero(t *testing.T) {
	p := start(t, filepath.Join(t.TempDir(), "data"))
	p.initialize(mcp.SupportedProtocolVersions()[1])
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := p.waitExit(5 * time.Second); code != 0 {
		t.Fatalf("exit code = %d, stderr:\n%s", code, p.stderr.String())
	}
	p.assertStdoutClean()
}

func TestSubprocessSIGTERMWhileBusyAnswersThenExitsZero(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	p := startBin(t, stallBinPath, dir, "MEALPLANNER_TEST_STALL_MS=800")
	p.initialize(mcp.SupportedProtocolVersions()[1])
	p.sendRaw(createLine(p, 60, "Busy Draft"))
	time.Sleep(200 * time.Millisecond) // the call is now inside the stalled Create
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := p.waitExit(10 * time.Second); code != 0 {
		t.Fatalf("exit code = %d, stderr:\n%s", code, p.stderr.String())
	}
	if !p.hasResponse(60) {
		t.Fatal("the busy request was not answered")
	}
	p.assertStdoutClean()
	if n := countDrafts(t, dir); n != 1 {
		t.Fatalf("stored drafts = %d, want 1", n)
	}
}

// If a request cannot finish within the grace period the server gives up on it and exits 1.
func TestSubprocessGraceExpiryExitsOne(t *testing.T) {
	p := startBin(t, stallBinPath, filepath.Join(t.TempDir(), "data"),
		"MEALPLANNER_TEST_STALL_MS=4000", "MEALPLANNER_TEST_GRACE_MS=300")
	p.initialize(mcp.SupportedProtocolVersions()[1])
	p.sendRaw(createLine(p, 70, "Stuck Draft"))
	_ = p.stdin.Close()
	start := time.Now()
	if code := p.waitExit(3500 * time.Millisecond); code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", code, p.stderr.String())
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v, want about the grace period", time.Since(start))
	}
	if !strings.Contains(p.stderr.String(), "grace period") {
		t.Fatalf("stderr lacks the grace message:\n%s", p.stderr.String())
	}
	p.assertStdoutClean()
}

func TestSubprocessHelpExitsZero(t *testing.T) {
	out, err := exec.Command(binPath, "--help").Output()
	if err != nil || !strings.Contains(string(out), "usage: mealplanner-mcp") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
