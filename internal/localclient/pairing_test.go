package localclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPairingContractAndPrivateCredentialDelivery(t *testing.T) {
	store := NewMemoryCredentialStore()
	client, err := NewPairingClient(store)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	doer := &queueDoer{responses: []*http.Response{
		jsonResponse(201, `{"api_version":"local-client-v1","pairing_id":"`+testIntent+`","poll_token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","user_code":"2345ABCD","verification_uri":"https://athome-meals-dev.vercel.app/connect","expires_in":600,"interval":5,"challenge_fingerprint":"0123abcd"}`),
		jsonResponse(202, `{"api_version":"local-client-v1","state":"pending"}`),
		jsonResponse(200, `{"api_version":"local-client-v1","state":"approved","scope":"draft:propose","environment":"preview","api_origin":"https://athome-meals-dev.vercel.app","web_origin":"https://athome-meals-dev.vercel.app","grant":"`+testGrant+`","expires_at":"`+expires+`","owner_label":"Synthetic owner","binding_id":"`+testBinding+`"}`),
	}}
	client.http = doer
	session, err := client.BeginPairing(context.Background(), "Meal Planner on Mac")
	if err != nil {
		t.Fatal(err)
	}
	if session.UserCode != "2345ABCD" || session.VerificationURI != PreviewOrigin+"/connect" || session.pollToken == "" || session.codeVerifier == "" {
		t.Fatalf("session = %+v", session)
	}
	for _, representation := range []string{fmt.Sprint(session), fmt.Sprintf("%+v", session), fmt.Sprintf("%#v", session), string(mustJSON(t, session))} {
		if strings.Contains(representation, session.pollToken) || strings.Contains(representation, session.codeVerifier) ||
			strings.Contains(representation, "poll_token") || strings.Contains(representation, "code_verifier") {
			t.Fatalf("pairing session leaked a secret: %s", representation)
		}
	}
	var create map[string]any
	if err := json.Unmarshal(doer.requests[0].Body, &create); err != nil {
		t.Fatal(err)
	}
	if create["code_challenge_method"] != "S256" || create["client_label"] != "Meal Planner on Mac" {
		t.Fatalf("create body = %#v", create)
	}
	if doer.requests[0].Authorization != "" {
		t.Fatal("pairing create unexpectedly sent authorization")
	}
	pending, err := client.PollPairing(context.Background(), session)
	if err != nil || !pending.Pending {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	approved, err := client.PollPairing(context.Background(), session)
	if err != nil || approved.Pending || approved.BindingID != testBinding || approved.OwnerLabel != "Synthetic owner" {
		t.Fatalf("approved = %+v, %v", approved, err)
	}
	if session.pollToken != "" || session.codeVerifier != "" {
		t.Fatal("pairing secrets remained after delivery")
	}
	credential, err := store.Get(context.Background(), credentialKey(testProfile(testBinding)))
	if err != nil || !credential.valid() || strings.Contains(credential.String(), testGrant) {
		t.Fatalf("stored credential = %v, %v", credential, err)
	}
	credential.clear()
	for _, request := range doer.requests {
		if strings.Contains(string(request.Body), testGrant) || request.Authorization != "" {
			t.Fatal("a pairing request exposed a grant")
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPairingRejectsVerificationURIWithQuery(t *testing.T) {
	client, _ := NewPairingClient(NewMemoryCredentialStore())
	client.http = &queueDoer{responses: []*http.Response{jsonResponse(201,
		`{"api_version":"local-client-v1","pairing_id":"`+testIntent+`","poll_token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","user_code":"2345ABCD","verification_uri":"https://athome-meals-dev.vercel.app/connect?code=2345ABCD","expires_in":600,"interval":5,"challenge_fingerprint":"0123abcd"}`)}}
	if _, err := client.BeginPairing(context.Background(), "Mac"); err != ErrInvalidResponse {
		t.Fatalf("error = %v", err)
	}
}

func TestPairingRateLimitIsTerminalAndIgnoresRetryAfter(t *testing.T) {
	for _, retryAfter := range []string{"", "not-a-duration", "999999999999", "https://attacker.invalid/retry"} {
		t.Run(retryAfter, func(t *testing.T) {
			store := NewMemoryCredentialStore()
			client, _ := NewPairingClient(store)
			response := jsonResponse(http.StatusTooManyRequests, `{"error":{"code":"rate_limited"}}`)
			if retryAfter != "" {
				response.Header.Set("Retry-After", retryAfter)
			}
			doer := &queueDoer{responses: []*http.Response{response}}
			client.http = doer
			session := &PairingSession{
				PairingID: testIntent, ExpiresAt: time.Now().Add(10 * time.Minute), Interval: 5 * time.Second,
				pollToken: strings.Repeat("A", 43), codeVerifier: strings.Repeat("A", 43),
			}
			if _, err := client.PollPairing(context.Background(), session); !errors.Is(err, ErrRateLimited) {
				t.Fatalf("error = %v", err)
			}
			if session.pollToken != "" || session.codeVerifier != "" {
				t.Fatal("pairing secrets remained retryable")
			}
			if _, err := client.PollPairing(context.Background(), session); !errors.Is(err, ErrExpired) || len(doer.requests) != 1 {
				t.Fatalf("second poll error=%v requests=%d", err, len(doer.requests))
			}
			if _, err := store.Get(context.Background(), credentialKey(testProfile(testBinding))); !errors.Is(err, ErrCredentialMissing) {
				t.Fatalf("credential stored after 429: %v", err)
			}
		})
	}
}

func TestPairingCreateRateLimitIsSpecific(t *testing.T) {
	client, _ := NewPairingClient(NewMemoryCredentialStore())
	client.http = &queueDoer{responses: []*http.Response{jsonResponse(http.StatusTooManyRequests, "rate limited")}}
	if _, err := client.BeginPairing(context.Background(), "Mac"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("error = %v", err)
	}
}
