package hosted

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakePairingRepo struct {
	created     DraftPairingCreated
	createErr   error
	poll        DraftPairingPollResult
	pollErr     error
	approveErr  error
	grants      []DraftGrantSummary
	revokeErr   error
	createIn    DraftPairingCreateRequest
	pollIn      DraftPairingPollRequest
	approveIn   DraftPairingApproveRequest
	owner       RecipeOwner
	revoked     string
	deadline    time.Time
	hasDeadline bool
}

func (f *fakePairingRepo) observeDeadline(ctx context.Context) {
	f.deadline, f.hasDeadline = ctx.Deadline()
}
func (f *fakePairingRepo) CreatePairing(ctx context.Context, _ DraftPairingConfig, in DraftPairingCreateRequest) (DraftPairingCreated, error) {
	f.observeDeadline(ctx)
	f.createIn = in
	return f.created, f.createErr
}
func (f *fakePairingRepo) PollPairing(ctx context.Context, _ DraftPairingConfig, in DraftPairingPollRequest) (DraftPairingPollResult, error) {
	f.observeDeadline(ctx)
	f.pollIn = in
	return f.poll, f.pollErr
}
func (f *fakePairingRepo) ApprovePairing(ctx context.Context, _ DraftPairingConfig, _ UserIdentity, in DraftPairingApproveRequest) error {
	f.observeDeadline(ctx)
	f.approveIn = in
	return f.approveErr
}
func (f *fakePairingRepo) ListDraftGrants(ctx context.Context, _ DraftPairingConfig, owner RecipeOwner) ([]DraftGrantSummary, error) {
	f.observeDeadline(ctx)
	f.owner = owner
	return f.grants, nil
}
func (f *fakePairingRepo) RevokeDraftGrant(ctx context.Context, _ DraftPairingConfig, owner RecipeOwner, id string) error {
	f.observeDeadline(ctx)
	f.owner, f.revoked = owner, id
	return f.revokeErr
}

func testPairingConfig() DraftPairingConfig {
	return DraftPairingConfig{Enabled: true, Environment: "preview", Issuer: "https://ep-auth.example.test", APIOrigin: draftPreviewOrigin, WebOrigin: draftPreviewOrigin, MembershipEpoch: "epoch_000000000001"}
}

func TestDraftGrantOwnerLockKeyIsTextSafeAndUnambiguous(t *testing.T) {
	keys := make(map[string]bool)
	for _, parts := range [][3]string{
		{"preview", "ab", "c"},
		{"preview", "a", "bc"},
		{"preview", `quote"and\\slash`, "subject"},
		{"preview", "quote", `and"\\slashsubject`},
		{"preview", "issuer", "subject\x00with-null"},
	} {
		key, err := draftGrantOwnerLockKey(parts[0], parts[1], parts[2])
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsRune(key, '\x00') || !json.Valid([]byte(key)) {
			t.Fatalf("unsafe lock key %q", key)
		}
		if keys[key] {
			t.Fatalf("ambiguous lock key %q", key)
		}
		keys[key] = true
	}
}

func TestDraftPairingCreateLockKeyAndCapacityAreFailClosed(t *testing.T) {
	a, err := draftPairingCreateLockKey("preview", "ab:c")
	if err != nil || !json.Valid([]byte(a)) || strings.ContainsRune(a, '\x00') {
		t.Fatalf("unsafe lock key %q: %v", a, err)
	}
	b, err := draftPairingCreateLockKey("preview:ab", "c")
	if err != nil || a == b {
		t.Fatalf("ambiguous lock keys %q %q: %v", a, b, err)
	}
	for _, tc := range []struct {
		active, hourly, retained int
		want                     bool
	}{
		{0, 0, 0, true},
		{maxActiveDraftPairings - 1, maxHourlyDraftPairings - 1, maxRetainedPairings - 1, true},
		{maxActiveDraftPairings, 0, 0, false},
		{0, maxHourlyDraftPairings, 0, false},
		{0, 0, maxRetainedPairings, false},
	} {
		if got := draftPairingCapacityAvailable(tc.active, tc.hourly, tc.retained); got != tc.want {
			t.Fatalf("capacity(%d,%d,%d)=%v want %v", tc.active, tc.hourly, tc.retained, got, tc.want)
		}
	}
}

func testVerifier() (string, string) {
	verifier := strings.Repeat("a", 43)
	h := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(h[:])
}

func jsonRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = draftPreviewHost
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestPairingPublicEndpointsRejectAlternateHostsAndPathsBeforeRepository(t *testing.T) {
	verifier, challenge := testVerifier()
	createBody := `{"code_challenge":"` + challenge + `","code_challenge_method":"S256","client_label":"Synthetic Mac"}`
	pollBody := `{"pairing_id":"11111111-1111-4111-8111-111111111111","poll_token":"` + strings.Repeat("b", 43) + `","code_verifier":"` + verifier + `"}`
	for _, tc := range []struct {
		name, path, host, body string
		handler                func(DraftPairingRepository, DraftPairingConfig) http.Handler
	}{
		{"create generated host", "/api/draft-pairings", "meal-planner-git-test-owner.vercel.app", createBody, DraftPairingCreateHandler},
		{"create Production-like host", "/api/draft-pairings", "meal-planner.vercel.app", createBody, DraftPairingCreateHandler},
		{"create trailing slash", "/api/draft-pairings/", draftPreviewHost, createBody, DraftPairingCreateHandler},
		{"create encoded slash", "/api/draft-pairings%2f", draftPreviewHost, createBody, DraftPairingCreateHandler},
		{"poll generated host", "/api/draft-pairings/poll", "meal-planner-git-test-owner.vercel.app", pollBody, DraftPairingPollHandler},
		{"poll trailing slash", "/api/draft-pairings/poll/", draftPreviewHost, pollBody, DraftPairingPollHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakePairingRepo{}
			r := jsonRequest("POST", tc.path, tc.body)
			r.Host = tc.host
			w := httptest.NewRecorder()
			tc.handler(repo, testPairingConfig()).ServeHTTP(w, r)
			if w.Code != http.StatusNotFound || repo.hasDeadline || strings.Contains(w.Body.String(), draftPreviewHost) {
				t.Fatalf("status=%d repository=%v body=%s", w.Code, repo.hasDeadline, w.Body.String())
			}
		})
	}
}

func TestPairingCreateContractAndClosedGates(t *testing.T) {
	verifier, challenge := testVerifier()
	_ = verifier
	repo := &fakePairingRepo{created: DraftPairingCreated{PairingID: "11111111-1111-4111-8111-111111111111", PollToken: strings.Repeat("b", 43), UserCode: "23456789", VerificationURI: draftPreviewOrigin + "/connect", ExpiresIn: 600, Interval: 5, ChallengeFingerprint: "1234abcd"}}
	body := `{"code_challenge":"` + challenge + `","code_challenge_method":"S256","client_label":"Caroline's Mac"}`

	for name, cfg := range map[string]DraftPairingConfig{
		"disabled":     {},
		"production":   {Enabled: true, Environment: "production", Issuer: "x", APIOrigin: draftPreviewOrigin, WebOrigin: draftPreviewOrigin, MembershipEpoch: "epoch_000000000001"},
		"wrong origin": {Enabled: true, Environment: "preview", Issuer: "x", APIOrigin: "https://evil.invalid", WebOrigin: "https://evil.invalid", MembershipEpoch: "epoch_000000000001"},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			DraftPairingCreateHandler(repo, cfg).ServeHTTP(w, jsonRequest("POST", "/api/draft-pairings", body))
			if w.Code != 404 {
				t.Fatalf("got %d", w.Code)
			}
		})
	}

	w := httptest.NewRecorder()
	DraftPairingCreateHandler(repo, testPairingConfig()).ServeHTTP(w, jsonRequest("POST", "/api/draft-pairings", body))
	if w.Code != 201 {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if repo.createIn.CodeChallenge != challenge || repo.createIn.CodeChallengeMethod != "S256" || repo.createIn.ClientLabel != "Caroline's Mac" {
		t.Fatalf("wrong input: %#v", repo.createIn)
	}
	if !repo.hasDeadline || time.Until(repo.deadline) <= 0 || time.Until(repo.deadline) > draftPairingStoreLimit {
		t.Fatalf("repository call deadline: present=%v remaining=%v", repo.hasDeadline, time.Until(repo.deadline))
	}
	var got DraftPairingCreated
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.APIVersion != "local-client-v1" || got.VerificationURI != draftPreviewOrigin+"/connect" || strings.Contains(got.VerificationURI, "?") {
		t.Fatalf("bad response: %s", w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
}

func TestPairingCreateRejectsAliasDuplicateAndBrowserCrossOrigin(t *testing.T) {
	_, challenge := testVerifier()
	repo := &fakePairingRepo{}
	cases := []struct {
		name, body string
		origin     string
	}{
		{"missing method", `{"code_challenge":"` + challenge + `","client_label":"Mac"}`, ""},
		{"wrong method", `{"code_challenge":"` + challenge + `","code_challenge_method":"plain","client_label":"Mac"}`, ""},
		{"duplicate", `{"code_challenge":"` + challenge + `","code_challenge":"` + challenge + `","code_challenge_method":"S256","client_label":"Mac"}`, ""},
		{"cross origin", `{"code_challenge":"` + challenge + `","code_challenge_method":"S256","client_label":"Mac"}`, "https://evil.invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := jsonRequest("POST", "/api/draft-pairings", tc.body)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			DraftPairingCreateHandler(repo, testPairingConfig()).ServeHTTP(w, r)
			if w.Code < 400 {
				t.Fatalf("got %d", w.Code)
			}
		})
	}
}

func TestPairingCreateCapacityReturnsBounded429(t *testing.T) {
	_, challenge := testVerifier()
	repo := &fakePairingRepo{createErr: ErrDraftPairingLimit}
	body := `{"code_challenge":"` + challenge + `","code_challenge_method":"S256","client_label":"Synthetic Mac"}`
	w := httptest.NewRecorder()
	DraftPairingCreateHandler(repo, testPairingConfig()).ServeHTTP(w, jsonRequest("POST", "/api/draft-pairings", body))
	if w.Code != 429 || !strings.Contains(w.Body.String(), `"code":"pairing_limit"`) || strings.Contains(w.Body.String(), "20") || strings.Contains(w.Body.String(), "500") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestPairingPollStatesAndOneTimeSecretShape(t *testing.T) {
	verifier, _ := testVerifier()
	id := "11111111-1111-4111-8111-111111111111"
	poll := strings.Repeat("b", 43)
	body := `{"pairing_id":"` + id + `","poll_token":"` + poll + `","code_verifier":"` + verifier + `"}`
	for _, tc := range []struct {
		name   string
		err    error
		status int
		state  string
	}{
		{"pending", ErrDraftPairingPending, 202, "pending"}, {"slow", ErrDraftPairingSlowDown, 202, "slow_down"}, {"gone", ErrDraftPairingGone, 410, "expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakePairingRepo{pollErr: tc.err}
			w := httptest.NewRecorder()
			DraftPairingPollHandler(repo, testPairingConfig()).ServeHTTP(w, jsonRequest("POST", "/api/draft-pairings/poll", body))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"state":"`+tc.state+`"`) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "mp_lc1") {
				t.Fatal("secret leaked before approval")
			}
		})
	}
	repo := &fakePairingRepo{poll: DraftPairingPollResult{State: "approved", Environment: "preview", APIOrigin: draftPreviewOrigin, WebOrigin: draftPreviewOrigin, BindingID: "22222222-2222-4222-8222-222222222222", Grant: "mp_lc1_pv_" + strings.Repeat("c", 43), ExpiresAt: "2030-01-01T00:00:00Z", OwnerLabel: "owner@example.test", Scope: "draft:propose"}}
	w := httptest.NewRecorder()
	DraftPairingPollHandler(repo, testPairingConfig()).ServeHTTP(w, jsonRequest("POST", "/api/draft-pairings/poll", body))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"scope":"draft:propose"`) || !strings.Contains(w.Body.String(), `"grant":"mp_lc1_pv_`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func ownerRequest(method, path, body string) *http.Request {
	r := jsonRequest(method, path, body)
	r.Header.Set("Origin", draftPreviewOrigin)
	identity := UserIdentity{Issuer: testPairingConfig().Issuer, Subject: "owner-subject", Email: "owner@example.test"}
	return r.WithContext(context.WithValue(r.Context(), bearerContextKey{}, identity))
}

func TestOwnerApprovalRequiresIdentityAndSameOrigin(t *testing.T) {
	repo := &fakePairingRepo{}
	body := `{"user_code":"23456789","challenge_fingerprint":"1234abcd"}`
	for name, mutate := range map[string]func(*http.Request){
		"no identity": func(r *http.Request) {
			*r = *jsonRequest("POST", "/api/draft-pairings/approve", body)
			r.Header.Set("Origin", draftPreviewOrigin)
		},
		"no origin":    func(r *http.Request) { r.Header.Del("Origin") },
		"wrong origin": func(r *http.Request) { r.Header.Set("Origin", "https://evil.invalid") },
	} {
		t.Run(name, func(t *testing.T) {
			r := ownerRequest("POST", "/api/draft-pairings/approve", body)
			mutate(r)
			w := httptest.NewRecorder()
			DraftPairingApproveHandler(repo, testPairingConfig()).ServeHTTP(w, r)
			if w.Code != 401 && w.Code != 403 {
				t.Fatalf("got %d", w.Code)
			}
		})
	}
	r := ownerRequest("POST", "/api/draft-pairings/approve", body)
	w := httptest.NewRecorder()
	DraftPairingApproveHandler(repo, testPairingConfig()).ServeHTTP(w, r)
	if w.Code != 200 || repo.approveIn.UserCode != "23456789" {
		t.Fatalf("%d %#v", w.Code, repo.approveIn)
	}
}

func TestGrantListAndIdempotentOwnerScopedRevoke(t *testing.T) {
	repo := &fakePairingRepo{grants: []DraftGrantSummary{{ID: "22222222-2222-4222-8222-222222222222", Scope: "draft:propose", Active: true}}}
	w := httptest.NewRecorder()
	DraftGrantsHandler(repo, testPairingConfig()).ServeHTTP(w, ownerRequest("GET", "/api/draft-grants", ""))
	if w.Code != 200 || repo.owner.Subject != "owner-subject" || strings.Contains(w.Body.String(), "token") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	DraftGrantsHandler(repo, testPairingConfig()).ServeHTTP(w, ownerRequest("POST", "/api/draft-grants", `{"grant_id":"22222222-2222-4222-8222-222222222222"}`))
	if w.Code != 204 || repo.revoked == "" || w.Body.Len() != 0 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestPairingConfigRequiresPreviewOptInPinnedOriginAndEpoch(t *testing.T) {
	for _, key := range []string{"VERCEL_ENV", "MEAL_DRAFT_PAIRING_ENABLED", "MEAL_DRAFT_PAIRING_ORIGIN", "MEAL_DRAFT_MEMBERSHIP_EPOCH", "NEON_AUTH_BASE_URL", "NEON_AUTH_ENDPOINT_ID", "AUTH_ALLOWED_EMAIL", "AUTH_ALLOWED_EMAILS_JSON"} {
		t.Setenv(key, "")
	}
	if DraftPairingConfigFromEnvironment().Enabled {
		t.Fatal("enabled by default")
	}
	t.Setenv("VERCEL_ENV", "preview")
	t.Setenv("MEAL_DRAFT_PAIRING_ENABLED", "true")
	t.Setenv("MEAL_DRAFT_PAIRING_ORIGIN", draftPreviewOrigin)
	t.Setenv("MEAL_DRAFT_MEMBERSHIP_EPOCH", "epoch_000000000001")
	t.Setenv("NEON_AUTH_BASE_URL", "https://ep-synthetic.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth")
	t.Setenv("NEON_AUTH_ENDPOINT_ID", "ep-synthetic")
	t.Setenv("AUTH_ALLOWED_EMAIL", "owner@example.test")
	config := DraftPairingConfigFromEnvironment()
	if config.Enabled {
		t.Fatal("environment settings bypassed the closed source gate")
	}
	_, challenge := testVerifier()
	body := `{"code_challenge":"` + challenge + `","code_challenge_method":"S256","client_label":"Mac"}`
	w := httptest.NewRecorder()
	DraftPairingCreateHandler(&fakePairingRepo{}, config).ServeHTTP(w, jsonRequest("POST", "/api/draft-pairings", body))
	if w.Code != 404 {
		t.Fatalf("env-only activation returned %d", w.Code)
	}
}
