package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The local workflow, end to end and offline, with no agent host: the built MCP server creates and
// revises a draft, is stopped and started again, and the built CLI then inspects the same draft in
// the same data directory. This is the repeatable CI counterpart of the manual host checks in
// docs/local-workflow.md.

// buildCLI builds cmd/mealplanner into a per-test temporary directory.
func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "mealplanner")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/C-Azzone416/meal-planner/cmd/mealplanner")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cannot build the CLI: %v\n%s", err, out)
	}
	return bin
}

func runCLI(t *testing.T, bin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "MEALPLANNER_DATA_DIR=") // flags only; never an inherited directory
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

func dirSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		out[rel] = string(b)
		return nil
	})
	return out
}

func TestWorkflowDirectionalMarksRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const rtl = "שלום\u200e עולם\u200f مرحبا\u061c"
	p := start(t, dir)
	p.initialize("2025-11-25")
	content := map[string]any{"title": rtl, "notes": rtl + "\n" + rtl}
	isErr, created := p.tool("draft_create", map[string]any{"draft": content})
	if isErr {
		t.Fatalf("create: %v", created)
	}
	id := created["draft"].(map[string]any)["id"].(string)
	isErr, updated := p.tool("draft_update", map[string]any{"id": id, "expected_revision": 1, "draft": content})
	if isErr || updated["draft"].(map[string]any)["title"] != rtl {
		t.Fatalf("update: %v", updated)
	}
	if code := p.shutdown(); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, p.stderr)
	}

	p = start(t, dir)
	p.initialize("2025-11-25")
	isErr, got := p.tool("draft_get", map[string]any{"id": id})
	if isErr {
		t.Fatalf("get: %v", got)
	}
	if saved := got["draft"].(map[string]any); saved["title"] != rtl || saved["notes"] != content["notes"] || saved["revision"] != float64(2) {
		t.Fatalf("stored round trip: %v", saved)
	}
	isErr, list := p.tool("draft_list", map[string]any{})
	if isErr || list["total"] != float64(1) || list["drafts"].([]any)[0].(map[string]any)["title"] != rtl {
		t.Fatalf("list: %v", list)
	}
	if code := p.shutdown(); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, p.stderr)
	}

	cli := buildCLI(t)
	out, stderr, code := runCLI(t, cli, "draft", "show", id, "--data-dir", dir, "--json")
	var shown struct {
		Draft map[string]any `json:"draft"`
	}
	if code != 0 || stderr != "" || json.Unmarshal([]byte(out), &shown) != nil || !reflect.DeepEqual(shown.Draft, got["draft"]) {
		t.Fatalf("CLI round trip: exit=%d stderr=%s stdout=%s", code, stderr, out)
	}
}

func TestWorkflowMCPCreateRestartCLIInspect(t *testing.T) {
	cli := buildCLI(t)
	dir := filepath.Join(t.TempDir(), "data")
	const title = "Synthetic Workflow Toast"
	proto := mcp.SupportedProtocolVersions()[0]

	// Session 1: create and revise.
	p1 := start(t, dir)
	p1.initialize(proto)
	content := map[string]any{
		"title":        title,
		"ingredients":  []any{map[string]any{"text": "2 slices bread"}},
		"instructions": []any{map[string]any{"text": "Toast the bread.", "phase": "cook_tonight"}},
		"estimates":    map[string]any{"active_minutes": map[string]any{"value": 5, "basis": map[string]any{"kind": "agent_estimate"}}},
	}
	isErr, created := p1.tool("draft_create", map[string]any{"draft": content})
	if isErr {
		t.Fatalf("create: %v", created)
	}
	id := created["draft"].(map[string]any)["id"].(string)
	content["notes"] = "Added in the second revision."
	if isErr, upd := p1.tool("draft_update", map[string]any{"id": id, "expected_revision": 1, "draft": content}); isErr || upd["draft"].(map[string]any)["revision"].(float64) != 2 {
		t.Fatalf("update: %v", upd)
	}
	p1.assertStdoutClean()
	if code := p1.shutdown(); code != 0 {
		t.Fatalf("first server exit = %d\n%s", code, p1.stderr)
	}

	// Session 2: a new process on the same directory sees the persisted result, and a stale update
	// from before the restart still conflicts without changing it.
	p2 := start(t, dir)
	p2.initialize(proto)
	_, got := p2.tool("draft_get", map[string]any{"id": id})
	mcpDraft := got["draft"].(map[string]any)
	if mcpDraft["title"] != title || mcpDraft["revision"].(float64) != 2 || mcpDraft["notes"] != "Added in the second revision." {
		t.Fatalf("after restart: %v", mcpDraft)
	}
	if isErr, c := p2.tool("draft_update", map[string]any{"id": id, "expected_revision": 1, "draft": map[string]any{"title": "Stale"}}); !isErr || c["error"].(map[string]any)["code"] != "revision_mismatch" {
		t.Fatalf("stale update after restart: %v", c)
	}
	p2.assertStdoutClean()
	if code := p2.shutdown(); code != 0 {
		t.Fatalf("second server exit = %d\n%s", code, p2.stderr)
	}

	// The CLI reads the same store. Inspection never modifies it.
	before := dirSnapshot(t, dir)
	out, errs, code := runCLI(t, cli, "draft", "show", id, "--data-dir", dir, "--json")
	if code != 0 || errs != "" {
		t.Fatalf("show: exit %d, stderr %q", code, errs)
	}
	var shown struct {
		Draft map[string]any `json:"draft"`
	}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("show output is not JSON: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(shown.Draft, mcpDraft) {
		t.Fatalf("CLI and MCP disagree about the same draft:\n cli %v\n mcp %v", shown.Draft, mcpDraft)
	}

	out, errs, code = runCLI(t, cli, "draft", "list", "--data-dir", dir, "--json")
	var page struct {
		Total  int              `json:"total"`
		Drafts []map[string]any `json:"drafts"`
	}
	if code != 0 || errs != "" || json.Unmarshal([]byte(out), &page) != nil || page.Total != 1 || page.Drafts[0]["id"] != id || page.Drafts[0]["revision"].(float64) != 2 {
		t.Fatalf("list: exit %d stderr %q out %s", code, errs, out)
	}

	out, _, code = runCLI(t, cli, "draft", "show", id, "--data-dir", dir)
	if code != 0 || !strings.Contains(out, title) || !strings.Contains(out, "status: draft (not reviewed or verified)") ||
		!strings.Contains(out, "active minutes: 5 [agent estimate, unverified]") {
		t.Fatalf("human show (exit %d):\n%s", code, out)
	}
	if out, _, code = runCLI(t, cli, "draft", "list", "--data-dir", dir); code != 0 || !strings.Contains(out, id) {
		t.Fatalf("human list (exit %d):\n%s", code, out)
	}

	// The same content validates identically through the CLI.
	raw, _ := json.Marshal(content)
	vf := filepath.Join(t.TempDir(), "content.json")
	if err := os.WriteFile(vf, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, code = runCLI(t, cli, "draft", "validate", vf); code != 0 {
		t.Fatalf("validate exit = %d", code)
	}

	if !reflect.DeepEqual(before, dirSnapshot(t, dir)) {
		t.Fatal("CLI inspection modified the data directory")
	}

	// A missing id is a rejection, not a failure, and still changes nothing.
	if _, _, code = runCLI(t, cli, "draft", "show", "drf_ffffffffffffffffffff", "--data-dir", dir); code != 1 {
		t.Fatalf("missing id exit = %d, want 1", code)
	}
	if !reflect.DeepEqual(before, dirSnapshot(t, dir)) {
		t.Fatal("a failed show modified the data directory")
	}
}
