package localclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

const (
	testBinding = "binding_1234567890abcdef"
	testGrant   = "mp_lc1_pv_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testIntent  = "11111111-1111-4111-8111-111111111111"
	testReceipt = "22222222-2222-4222-8222-222222222222"
	testHosted  = "33333333-3333-4333-8333-333333333333"
)

type recordedRequest struct {
	URL, Authorization, OperationKey string
	Body                             []byte
}

type queueDoer struct {
	mu        sync.Mutex
	responses []*http.Response
	requests  []recordedRequest
}

func (d *queueDoer) Do(request *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(request.Body)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.requests = append(d.requests, recordedRequest{
		URL: request.URL.String(), Authorization: request.Header.Get("Authorization"),
		OperationKey: request.Header.Get("Idempotency-Key"), Body: raw,
	})
	if len(d.responses) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	response := d.responses[0]
	d.responses = d.responses[1:]
	response.Request = request
	return response, nil
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func testProfile(binding string) Profile {
	return Profile{Environment: PreviewEnvironment, APIOrigin: PreviewOrigin, WebOrigin: PreviewOrigin, BindingID: binding}
}

func testDraft(revision int64) draft.Draft {
	return draft.Draft{
		SchemaVersion: 1, ID: "drf_0123456789abcdefabcd", Revision: revision, Status: draft.StatusDraft,
		CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now(),
		Content: draft.Content{Title: "Synthetic soup", Ingredients: []draft.Ingredient{}, Instructions: []draft.Step{}, Sources: []draft.Source{}, Guidance: []draft.Guidance{}},
	}
}

func newTestClient(t *testing.T, doer httpDoer, binding string) (*Client, *MemoryCredentialStore, *Ledger) {
	t.Helper()
	credentials := NewMemoryCredentialStore()
	profile := testProfile(binding)
	credential, err := newCredential([]byte(testGrant))
	if err != nil {
		t.Fatal(err)
	}
	if err := credentials.Put(context.Background(), credentialKey(profile), credential); err != nil {
		t.Fatal(err)
	}
	ledger, err := OpenLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client, err := newWithDoer(profile, credentials, ledger, doer)
	if err != nil {
		t.Fatal(err)
	}
	return client, credentials, ledger
}

func TestProfileIsExactPreviewOnly(t *testing.T) {
	valid := testProfile(testBinding)
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Profile){
		func(p *Profile) { p.Environment = "production" },
		func(p *Profile) { p.APIOrigin = "https://example.invalid" },
		func(p *Profile) { p.WebOrigin += "/" },
		func(p *Profile) { p.BindingID = "short" },
	} {
		candidate := valid
		mutate(&candidate)
		if candidate.Validate() == nil {
			t.Fatalf("accepted unsafe profile: %+v", candidate)
		}
	}
}

func TestUnavailableCredentialStoreHasNoFallback(t *testing.T) {
	store := UnavailableCredentialStore{}
	key := credentialKey(testProfile(testBinding))
	if _, err := store.Get(context.Background(), key); err != ErrCredentialMissing {
		t.Fatalf("Get error = %v", err)
	}
	credential, _ := newCredential([]byte(testGrant))
	if err := store.Put(context.Background(), key, credential); err != ErrUnavailable {
		t.Fatalf("Put error = %v", err)
	}
}

func TestCredentialRepresentationsAreAlwaysRedacted(t *testing.T) {
	credential, err := newCredential([]byte(testGrant))
	if err != nil {
		t.Fatal(err)
	}
	defer credential.clear()
	values := []string{
		fmt.Sprint(credential), fmt.Sprintf("%v", credential), fmt.Sprintf("%+v", credential),
		fmt.Sprintf("%#v", credential), fmt.Sprintf("%s", credential), fmt.Sprintf("%q", credential),
	}
	text, _ := credential.MarshalText()
	encoded, _ := json.Marshal(credential)
	values = append(values, string(text), string(encoded))
	for _, value := range values {
		if strings.Contains(value, testGrant) || !strings.Contains(value, "REDACTED") {
			t.Fatalf("unsafe credential representation %q", value)
		}
	}
}

func TestSendCreatesIntentThenPollsAndPersistsReceipt(t *testing.T) {
	expires := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
	submitted := time.Now().UTC().Format(time.RFC3339)
	doer := &queueDoer{}
	client, _, _ := newTestClient(t, doer, testBinding)
	local := testDraft(7)
	submissionBytes, _ := json.Marshal(submission{ContentSchemaVersion: 1, Content: local.Content})
	hash := sha256Hex(submissionBytes)
	const goldenHash = "224729ef543ecf9777695795d924be7378c0953cf9070736001c682cc3599ac0"
	if hash != goldenHash {
		t.Fatalf("canonical submission hash = %s, want %s", hash, goldenHash)
	}
	doer.responses = []*http.Response{
		jsonResponse(201, `{"api_version":"draft-intent-v1","intent":{"id":"`+testIntent+`","state":"pending","revision":1,"content_hash":"`+hash+`","expires_at":"`+expires+`"},"approval":{"path":"/incoming-drafts/`+testIntent+`"}}`),
		jsonResponse(200, `{"api_version":"draft-intent-v1","state":"approved","receipt":{"receipt_id":"`+testReceipt+`","draft_id":"`+testHosted+`","accepted_revision":1,"submitted_at":"`+submitted+`"}}`),
	}

	first, err := client.Send(context.Background(), local)
	if err != nil || first.State != SendPending || first.IntentID != testIntent || !strings.HasSuffix(first.ApprovalURL, "/incoming-drafts/"+testIntent) {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := client.Send(context.Background(), local)
	if err != nil || second.State != SendSaved || second.ReceiptID != testReceipt || second.HostedDraftID != testHosted {
		t.Fatalf("second = %+v, %v", second, err)
	}
	third, err := client.Send(context.Background(), local)
	if err != nil || third != second {
		t.Fatalf("durable replay = %+v, %v", third, err)
	}
	if len(doer.requests) != 2 {
		t.Fatalf("network calls = %d", len(doer.requests))
	}
	create := doer.requests[0]
	if create.URL != PreviewOrigin+"/api/draft-send-intents" || create.Authorization != "Bearer "+testGrant ||
		!strings.HasPrefix(create.OperationKey, "snd_") || !bytes.Equal(create.Body, submissionBytes) {
		t.Fatalf("unexpected create request: %+v", create)
	}
	if bytes.Contains(create.Body, []byte(local.ID)) || bytes.Contains(create.Body, []byte(testGrant)) {
		t.Fatal("submission leaked a local identifier or credential")
	}
	poll := doer.requests[1]
	if poll.URL != PreviewOrigin+"/api/draft-send-intents/poll" || poll.OperationKey != "" || bytes.Contains(poll.Body, []byte(testGrant)) {
		t.Fatalf("unexpected poll request: %+v", poll)
	}
}

func TestCreateReplayRecoversTerminalAcceptedReceipt(t *testing.T) {
	local := testDraft(7)
	submissionBytes, _ := json.Marshal(submission{ContentSchemaVersion: 1, Content: local.Content})
	hash := sha256Hex(submissionBytes)
	expires := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	submitted := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	doer := &queueDoer{responses: []*http.Response{jsonResponse(200,
		`{"api_version":"draft-intent-v1","intent":{"id":"`+testIntent+`","state":"approved","revision":2,"content_hash":"`+hash+`","expires_at":"`+expires+`","draft_id":"`+testHosted+`","receipt_id":"`+testReceipt+`","accepted_revision":1,"submitted_at":"`+submitted+`"},"approval":{"path":"/incoming-drafts/`+testIntent+`"}}`)}}
	client, _, _ := newTestClient(t, doer, testBinding)

	result, err := client.Send(context.Background(), local)
	if err != nil || result.State != SendSaved || result.ReceiptID != testReceipt || result.HostedDraftID != testHosted {
		t.Fatalf("terminal replay = %+v, %v", result, err)
	}
	again, err := client.Send(context.Background(), local)
	if err != nil || again != result || len(doer.requests) != 1 {
		t.Fatalf("durable terminal replay = %+v, %v; requests=%d", again, err, len(doer.requests))
	}
}

func TestCreateReplaySurfacesDeniedAndExpired(t *testing.T) {
	local := testDraft(7)
	submissionBytes, _ := json.Marshal(submission{ContentSchemaVersion: 1, Content: local.Content})
	hash := sha256Hex(submissionBytes)
	expires := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)

	for _, test := range []struct {
		name, state string
		want        error
	}{
		{name: "denied", state: "denied", want: ErrDenied},
		{name: "expired pending", state: "pending", want: ErrExpired},
	} {
		t.Run(test.name, func(t *testing.T) {
			doer := &queueDoer{responses: []*http.Response{jsonResponse(200,
				`{"api_version":"draft-intent-v1","intent":{"id":"`+testIntent+`","state":"`+test.state+`","revision":2,"content_hash":"`+hash+`","expires_at":"`+expires+`"},"approval":{"path":"/incoming-drafts/`+testIntent+`"}}`)}}
			client, _, _ := newTestClient(t, doer, testBinding)
			if _, err := client.Send(context.Background(), local); err != test.want {
				t.Fatalf("terminal replay error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestHostedErrorEnvelopeMapsLifecycleErrors(t *testing.T) {
	for _, test := range []struct {
		name, code string
		status     int
		want       error
	}{
		{name: "denied", code: "intent_denied", status: http.StatusGone, want: ErrDenied},
		{name: "expired", code: "intent_expired", status: http.StatusGone, want: ErrExpired},
		{name: "revoked", code: "unauthorized", status: http.StatusUnauthorized, want: ErrCredentialMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			doer := &queueDoer{responses: []*http.Response{jsonResponse(test.status,
				`{"error":{"code":"`+test.code+`","message":"Safe hosted message."}}`)}}
			client, _, _ := newTestClient(t, doer, testBinding)
			if _, err := client.Send(context.Background(), testDraft(1)); err != test.want {
				t.Fatalf("hosted error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPollMapsHostedLifecycleErrorEnvelope(t *testing.T) {
	for _, test := range []struct {
		name, code string
		status     int
		want       error
	}{
		{name: "denied", code: "intent_denied", status: http.StatusGone, want: ErrDenied},
		{name: "expired", code: "intent_expired", status: http.StatusGone, want: ErrExpired},
		{name: "revoked", code: "unauthorized", status: http.StatusUnauthorized, want: ErrCredentialMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			local := testDraft(1)
			submissionBytes, _ := json.Marshal(submission{ContentSchemaVersion: 1, Content: local.Content})
			hash := sha256Hex(submissionBytes)
			expires := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
			doer := &queueDoer{responses: []*http.Response{
				jsonResponse(http.StatusCreated, `{"api_version":"draft-intent-v1","intent":{"id":"`+testIntent+`","state":"pending","revision":1,"content_hash":"`+hash+`","expires_at":"`+expires+`"},"approval":{"path":"/incoming-drafts/`+testIntent+`"}}`),
				jsonResponse(test.status, `{"error":{"code":"`+test.code+`","message":"Safe hosted message."}}`),
			}}
			client, _, _ := newTestClient(t, doer, testBinding)
			if _, err := client.Send(context.Background(), local); err != nil {
				t.Fatal(err)
			}
			if _, err := client.Send(context.Background(), local); err != test.want {
				t.Fatalf("poll error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestRedirectAndHostileResponsesFailClosed(t *testing.T) {
	redirect := jsonResponse(307, `{}`)
	redirect.Header.Set("Location", "https://evil.invalid/steal")
	client, _, _ := newTestClient(t, &queueDoer{responses: []*http.Response{redirect}}, testBinding)
	if _, err := client.Send(context.Background(), testDraft(1)); err != ErrInvalidResponse {
		t.Fatalf("redirect error = %v", err)
	}

	hostile := `{"api_version":"draft-intent-v1","intent":{"id":"` + testIntent + `","state":"pending","revision":1,"content_hash":"wrong","expires_at":"2030-01-01T00:00:00Z"},"approval":{"path":"https://evil.invalid/"}}`
	client, _, _ = newTestClient(t, &queueDoer{responses: []*http.Response{jsonResponse(201, hostile)}}, testBinding)
	if _, err := client.Send(context.Background(), testDraft(1)); err != ErrInvalidResponse {
		t.Fatalf("hostile response error = %v", err)
	}
}

func TestLedgerConcurrentRetriesAndAccountRevisionIsolation(t *testing.T) {
	ledger, err := OpenLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := ledgerScope{Environment: PreviewEnvironment, APIOrigin: PreviewOrigin, BindingID: testBinding, LocalID: "drf_0123456789abcdefabcd", Revision: 1, ContentHash: strings.Repeat("a", 64)}
	const count = 20
	keys := make(chan string, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record, beginErr := ledger.Begin(context.Background(), base)
			if beginErr != nil {
				keys <- "error:" + beginErr.Error()
				return
			}
			keys <- record.OperationKey
		}()
	}
	wg.Wait()
	close(keys)
	var first string
	for key := range keys {
		if first == "" {
			first = key
		}
		if key != first {
			t.Fatalf("concurrent Begin returned %q and %q", first, key)
		}
	}
	otherAccount := base
	otherAccount.BindingID = "binding_abcdef1234567890"
	accountRecord, _ := ledger.Begin(context.Background(), otherAccount)
	otherRevision := base
	otherRevision.Revision = 2
	revisionRecord, _ := ledger.Begin(context.Background(), otherRevision)
	if accountRecord.OperationKey == first || revisionRecord.OperationKey == first || accountRecord.OperationKey == revisionRecord.OperationKey {
		t.Fatal("account or revision reused an operation key")
	}

	leftover := filepath.Join(ledger.root, ".tmp-ledger-crash")
	if err := os.WriteFile(leftover, []byte("recipe content must not matter"), 0600); err != nil {
		t.Fatal(err)
	}
	replay, err := ledger.Begin(context.Background(), base)
	if err != nil || replay.OperationKey != first {
		t.Fatalf("crash replay = %+v, %v", replay, err)
	}
	files, _ := os.ReadDir(ledger.root)
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".json") {
			raw, _ := os.ReadFile(filepath.Join(ledger.root, file.Name()))
			if bytes.Contains(raw, []byte("Synthetic soup")) || bytes.Contains(raw, []byte(testGrant)) {
				t.Fatal("ledger persisted recipe content or a grant")
			}
		}
	}
}

func TestPackageContainsNoListener(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		raw, _ := os.ReadFile(entry.Name())
		for _, forbidden := range []string{"net.Listen(", "ListenAndServe(", "ListenPacket(", "localhost", "127.0.0.1"} {
			if bytes.Contains(raw, []byte(forbidden)) {
				t.Fatalf("%s contains forbidden listener marker %q", entry.Name(), forbidden)
			}
		}
	}
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
