package mcpadapter_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
	"github.com/C-Azzone416/meal-planner/internal/localclient"
	"github.com/C-Azzone416/meal-planner/internal/mcpadapter"
)

type fakeSender struct {
	called int
	got    draft.Draft
	result localclient.SendResult
	err    error
}

func (s *fakeSender) Send(_ context.Context, value draft.Draft) (localclient.SendResult, error) {
	s.called++
	s.got = value
	return s.result, s.err
}

func TestDraftSendIsInjectionGatedAndRevisionBound(t *testing.T) {
	sender := &fakeSender{result: localclient.SendResult{
		State: localclient.SendPending, IntentID: "11111111-1111-4111-8111-111111111111",
		ApprovalURL: "https://athome-meals-dev.vercel.app/incoming-drafts/11111111-1111-4111-8111-111111111111",
		Message:     "Waiting for owner approval.",
	}}
	e := setup(t, mcpadapter.WithDraftSender(sender))
	id := e.create("Outbound synthetic draft")
	tools, err := e.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name != mcpadapter.ToolDraftSend {
			continue
		}
		found = true
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint ||
			tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Fatalf("send annotations = %+v", tool.Annotations)
		}
	}
	if !found || len(tools.Tools) != 7 {
		t.Fatalf("tools = %+v", tools.Tools)
	}
	result := e.ok(mcpadapter.ToolDraftSend, map[string]any{"id": id, "expected_revision": 1})
	if sender.called != 1 || sender.got.ID != id || sender.got.Revision != 1 {
		t.Fatalf("sender = calls %d, draft %+v", sender.called, sender.got)
	}
	encoded := text(mustCallWith(t, e, mcpadapter.ToolDraftSend, map[string]any{"id": id, "expected_revision": 1}))
	if strings.Contains(encoded, "mp_lc1") || strings.Contains(encoded, "snd_") {
		t.Fatalf("send result leaked credential material: %s", encoded)
	}
	if send := result["send"].(map[string]any); send["state"] != string(localclient.SendPending) {
		t.Fatalf("send result = %v", send)
	}

	stale := e.fail(mcpadapter.ToolDraftSend, map[string]any{"id": id, "expected_revision": 2})
	if stale["code"] != draft.CodeRevisionMismatch || sender.called != 2 { // second call above was a valid retry
		t.Fatalf("stale = %v, calls = %d", stale, sender.called)
	}
	status := e.ok(mcpadapter.ToolServerStatus, map[string]any{})
	if ops := status["operations"].([]any); len(ops) != 7 || ops[6] != mcpadapter.ToolDraftSend {
		t.Fatalf("operations = %v", ops)
	}
}

func mustCallWith(t *testing.T, e *env, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := e.call(name, args)
	if err != nil || res.IsError {
		t.Fatalf("%s: %v %s", name, err, text(res))
	}
	return res
}
