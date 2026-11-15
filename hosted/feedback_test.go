package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

type feedbackMemoryStore struct {
	mu   sync.Mutex
	rows map[string]*feedbackRecord
}

func (m *feedbackMemoryStore) WithRecord(_ context.Context, owner string, fn func(*feedbackRecord) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = map[string]*feedbackRecord{}
	}
	if m.rows[owner] == nil {
		m.rows[owner] = &feedbackRecord{Owner: owner, Generation: feedbackRandom()}
	}
	return fn(m.rows[owner])
}

type feedbackFakeUpstream struct {
	attempts                        []string
	failRedeem                      bool
	refreshes, reports, disconnects int
	failDisconnect                  bool
}

func (f *feedbackFakeUpstream) Grant(_ context.Context, action string, p map[string]any) (feedbackGrant, error) {
	if action == "redeem" {
		f.attempts = append(f.attempts, p["attemptId"].(string))
		if f.failRedeem {
			f.failRedeem = false
			return feedbackGrant{}, feedbackError("UNCERTAIN", 503)
		}
	} else {
		f.refreshes++
	}
	return feedbackGrant{V: 1, GameID: "meal-planner-preview", Scope: 2, Token: strings.Repeat("a", 20), Refresh: strings.Repeat("b", 20), Expires: time.Now().Add(time.Hour), RefreshExpires: time.Now().Add(24 * time.Hour).Truncate(time.Hour)}, nil
}
func (f *feedbackFakeUpstream) Disconnect(context.Context, string) error {
	f.disconnects++
	if f.failDisconnect {
		return feedbackUnavailable
	}
	return nil
}
func (f *feedbackFakeUpstream) Report(context.Context, string, string, []byte, []byte) error {
	f.reports++
	return nil
}
func feedbackFixture() (feedbackService, *feedbackMemoryStore, *feedbackFakeUpstream) {
	m := &feedbackMemoryStore{}
	u := &feedbackFakeUpstream{}
	s := feedbackService{feedbackConfig{Enabled: true, Environment: "preview", App: "meal-planner-preview", SHA: strings.Repeat("a", 40), Origin: "https://preview.example.com", Provider: "provider", Current: "k1", Keys: map[string][]byte{"k1": bytes.Repeat([]byte{1}, 32)}}, m, u}
	return s, m, u
}
func TestFeedbackRedemptionRetryAndIsolation(t *testing.T) {
	s, m, u := feedbackFixture()
	ctx := context.Background()
	u.failRedeem = true
	owner := s.owner("user-a")
	if s.connect(ctx, owner, "invitation-secret") == nil {
		t.Fatal("expected uncertainty")
	}
	if m.rows[owner].Attempt == "" {
		t.Fatal("attempt not durable")
	}
	if err := s.connect(ctx, owner, "invitation-secret"); err != nil {
		t.Fatal(err)
	}
	if len(u.attempts) != 2 || u.attempts[0] != u.attempts[1] {
		t.Fatal("redemption changed attempt")
	}
	r := m.rows[owner]
	if bytes.Contains(r.Sealed, []byte(strings.Repeat("b", 20))) {
		t.Fatal("plaintext credentials")
	}
	other := *r
	other.Owner = s.owner("user-b")
	if _, err := s.open(&other); err == nil {
		t.Fatal("cross-account decrypt")
	}
	other = *r
	other.Generation = feedbackRandom()
	if _, err := s.open(&other); err == nil {
		t.Fatal("cross-generation decrypt")
	}
	prod := s
	prod.config.Environment = "production"
	if prod.owner("user-a") == owner {
		t.Fatal("cross-env owner collision")
	}
	provider := s
	provider.config.Provider = "other"
	if provider.owner("user-a") == owner {
		t.Fatal("cross-provider owner collision")
	}
}
func TestFeedbackDisconnectTruthAndGeneration(t *testing.T) {
	s, m, u := feedbackFixture()
	ctx := context.Background()
	owner := s.owner("user")
	if err := s.connect(ctx, owner, "invitation-secret"); err != nil {
		t.Fatal(err)
	}
	generation := m.rows[owner].Generation
	u.failDisconnect = true
	if s.RevokeAndErase(ctx, owner, true) == nil || len(m.rows[owner].Sealed) == 0 {
		t.Fatal("must preserve on revoke failure")
	}
	u.failDisconnect = false
	if err := s.RevokeAndErase(ctx, owner, true); err != nil {
		t.Fatal(err)
	}
	if len(m.rows[owner].Sealed) != 0 || m.rows[owner].Generation == generation {
		t.Fatal("erase/rotation failed")
	}
}
func TestFeedbackPendingBlocksDeletion(t *testing.T) {
	s, m, _ := feedbackFixture()
	owner := s.owner("user")
	_ = m.WithRecord(context.Background(), owner, func(r *feedbackRecord) error {
		r.Attempt = "pending"
		r.AttemptAt = time.Now().Add(-time.Hour)
		return nil
	})
	if s.RevokeAndErase(context.Background(), owner, true) == nil {
		t.Fatal("must not claim unknown family revoked")
	}
}
func TestFeedbackConcurrentRefreshBounded(t *testing.T) {
	s, m, u := feedbackFixture()
	ctx := context.Background()
	owner := s.owner("user")
	if err := s.connect(ctx, owner, "invitation-secret"); err != nil {
		t.Fatal(err)
	}
	r := m.rows[owner]
	g, _ := s.open(r)
	g.Expires = time.Now()
	_ = s.seal(r, g)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.send(ctx, owner, feedbackRandom(), []byte("{}"), nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if u.refreshes != 1 || u.reports != 8 {
		t.Fatalf("refresh=%d reports=%d", u.refreshes, u.reports)
	}
}
func TestFeedbackHTTPGuardsAndDurableRateLimit(t *testing.T) {
	s, _, _ := feedbackFixture()
	call := func(method, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/feedback/disconnect", strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(context.WithValue(r.Context(), bearerContextKey{}, UserIdentity{Subject: "a"}))
		w := httptest.NewRecorder()
		s.serve("disconnect", w, r)
		return w
	}
	if got := call("GET", s.config.Origin, "{}").Code; got != 405 {
		t.Fatal(got)
	}
	if got := call("POST", "https://evil.example", "{}").Code; got != 403 {
		t.Fatal(got)
	}
	for i := 0; i < 12; i++ {
		if got := call("POST", s.config.Origin, "{}").Code; got != 200 {
			t.Fatal(got)
		}
	}
	if got := call("POST", s.config.Origin, "{}").Code; got != 429 {
		t.Fatal(got)
	}
}
func feedbackReportRequest(t *testing.T, report map[string]any, media []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{"Content-Disposition": {`form-data; name="report"; filename="report.json"`}, "Content-Type": {"application/json"}}
	part, _ := mw.CreatePart(h)
	_ = json.NewEncoder(part).Encode(report)
	if media != nil {
		h.Set("Content-Disposition", `form-data; name="media"; filename="capture.png"`)
		h.Set("Content-Type", "image/png")
		part, _ = mw.CreatePart(h)
		_, _ = part.Write(media)
	}
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/feedback/reports", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}
func feedbackValidReport(s feedbackService) map[string]any {
	return map[string]any{"v": 1, "id": feedbackRandom(), "gameId": s.config.App, "buildSha": s.config.SHA, "scene": "week", "sessionId": feedbackRandom(), "seed": nil, "platform": "mobile", "input": "touch", "capturedAt": time.Now().UTC().Format(time.RFC3339Nano), "notes": "Useful report", "breadcrumbs": []any{}, "media": nil}
}
func TestFeedbackReportStrictContext(t *testing.T) {
	s, _, _ := feedbackFixture()
	r := feedbackValidReport(s)
	if _, _, _, err := s.parseReport(httptest.NewRecorder(), feedbackReportRequest(t, r, nil)); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{"gameId": "travel-packing", "buildSha": "dev", "scene": "https://private.example/secret", "sessionId": "account-id", "breadcrumbs": []any{"email"}, "email": "canary@example.com", "seed": "private recipe"} {
		t.Run(key, func(t *testing.T) {
			r := feedbackValidReport(s)
			r[key] = value
			if _, _, _, err := s.parseReport(httptest.NewRecorder(), feedbackReportRequest(t, r, nil)); err == nil {
				t.Fatal("accepted private or wrong context")
			}
		})
	}
}
func TestFeedbackReportMediaKillSwitch(t *testing.T) {
	s, _, _ := feedbackFixture()
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	r := feedbackValidReport(s)
	r["media"] = map[string]any{"type": "image/png", "bytes": b.Len(), "sha256": "bad"}
	if _, _, _, err := s.parseReport(httptest.NewRecorder(), feedbackReportRequest(t, r, b.Bytes())); err == nil {
		t.Fatal("capture kill switch")
	}
	s.config.Screenshots = true
	if _, _, _, err := s.parseReport(httptest.NewRecorder(), feedbackReportRequest(t, r, b.Bytes())); err == nil {
		t.Fatal("hash mismatch accepted")
	}
}

type feedbackRoundTrip func(*http.Request) (*http.Response, error)

func (f feedbackRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestFeedbackUpstreamReceiptIntegrity(t *testing.T) {
	for _, body := range []string{`{"id":"wrong","gameId":"meal-planner-preview","status":"ready"}`, `{"id":"report","gameId":"travel-packing","status":"ready"}`, `{"id":"report","gameId":"meal-planner-preview","status":"pending","ticket":"bad"}`} {
		c := feedbackClient{"meal-planner-preview", &http.Client{Transport: feedbackRoundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != feedbackEndpoint+"/v1/reports" {
				t.Fatal("unfixed endpoint")
			}
			w := httptest.NewRecorder()
			_, _ = w.WriteString(body)
			return w.Result(), nil
		})}}
		if err := c.Report(context.Background(), "secret", "report", []byte("{}"), nil); err == nil {
			t.Fatal("invalid receipt accepted")
		}
	}
}
func TestFeedbackSafeFailure(t *testing.T) {
	w := httptest.NewRecorder()
	feedbackWriteError(w, fmt.Errorf("secret identity invitation"))
	if strings.Contains(w.Body.String(), "secret") || w.Code != 503 {
		t.Fatal(w.Body.String())
	}
	if !errors.Is(feedbackUnavailable, feedbackUnavailable) {
		t.Fatal("unreachable")
	}
}

func TestFeedbackPendingInvitationCannotChange(t *testing.T) {
	s, m, u := feedbackFixture()
	owner := s.owner("a")
	u.failRedeem = true
	_ = s.connect(context.Background(), owner, "first invitation")
	attempt := m.rows[owner].Attempt
	if err := s.connect(context.Background(), owner, "different invitation"); err == nil {
		t.Fatal("changed pending invitation")
	}
	if len(u.attempts) != 1 || m.rows[owner].Attempt != attempt {
		t.Fatal("unsafe retry")
	}
}
func TestFeedbackStatusBrowserOriginEvidence(t *testing.T) {
	s, _, _ := feedbackFixture()
	for _, test := range []struct {
		referer, site string
		want          int
	}{{s.config.Origin + "/week", "same-origin", 200}, {"https://evil.example/week", "same-origin", 403}, {s.config.Origin + "/week", "", 403}, {"", "same-origin", 403}} {
		r := httptest.NewRequest("GET", "/api/feedback/status", nil)
		r.Header.Set("Referer", test.referer)
		r.Header.Set("Sec-Fetch-Site", test.site)
		r = r.WithContext(context.WithValue(r.Context(), bearerContextKey{}, UserIdentity{Subject: "a"}))
		w := httptest.NewRecorder()
		s.serve("status", w, r)
		if w.Code != test.want {
			t.Fatalf("got %d want %d", w.Code, test.want)
		}
	}
}
func TestFeedbackConfigurationFailsClosed(t *testing.T) {
	t.Setenv("FRAMEBRIAR_ENABLED", "")
	c, err := feedbackConfiguration()
	if err != nil || c.Enabled {
		t.Fatal("default not off")
	}
	t.Setenv("FRAMEBRIAR_ENABLED", "1")
	t.Setenv("FRAMEBRIAR_BUILD_SHA", strings.Repeat("a", 40))
	t.Setenv("FRAMEBRIAR_ENVIRONMENT", "preview")
	t.Setenv("FRAMEBRIAR_APP_ID", "meal-planner-preview")
	t.Setenv("FRAMEBRIAR_ALLOWED_ORIGIN", "https://*.example.com")
	if _, err := feedbackConfiguration(); err == nil {
		t.Fatal("incomplete configured deployment accepted")
	}
}
func TestFeedbackKeyRotationAndLoss(t *testing.T) {
	s, m, _ := feedbackFixture()
	owner := s.owner("a")
	if err := s.connect(context.Background(), owner, "invitation-secret"); err != nil {
		t.Fatal(err)
	}
	r := m.rows[owner]
	s.config.Keys["k2"] = bytes.Repeat([]byte{2}, 32)
	s.config.Current = "k2"
	if err := s.send(context.Background(), owner, feedbackRandom(), []byte("{}"), nil); err != nil {
		t.Fatal(err)
	}
	if r.KeyID != "k2" {
		t.Fatal("not resealed")
	}
	delete(s.config.Keys, "k2")
	if s.view(r)["status"] != "needs-reconnect" {
		t.Fatal("lost key hidden")
	}
	if s.RevokeAndErase(context.Background(), owner, false) == nil || len(r.Sealed) == 0 {
		t.Fatal("false revoke on key loss")
	}
}

func TestFeedbackQueuedPriorBuildPreserved(t *testing.T) {
	s, _, _ := feedbackFixture()
	r := feedbackValidReport(s)
	r["buildSha"] = strings.Repeat("b", 40)
	parsed, raw, _, err := s.parseReport(httptest.NewRecorder(), feedbackReportRequest(t, r, nil))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.BuildSHA != r["buildSha"] || !bytes.Contains(raw, []byte(strings.Repeat("b", 40))) {
		t.Fatal("captured provenance rewritten")
	}
}

type feedbackRejectOnce struct {
	feedbackFakeUpstream
	rejected bool
}

func (f *feedbackRejectOnce) Grant(ctx context.Context, action string, p map[string]any) (feedbackGrant, error) {
	if !f.rejected {
		f.rejected = true
		return feedbackGrant{}, feedbackError("REVOKED", 401)
	}
	return f.feedbackFakeUpstream.Grant(ctx, action, p)
}
func TestFeedbackDefinitiveInvitationRefusalAllowsNewAttempt(t *testing.T) {
	s, m, _ := feedbackFixture()
	u := &feedbackRejectOnce{}
	s.upstream = u
	owner := s.owner("a")
	if err := s.connect(context.Background(), owner, "wrong invitation"); err == nil {
		t.Fatal("expected rejection")
	}
	if m.rows[owner].Attempt != "" || m.rows[owner].AttemptDigest != "" {
		t.Fatal("definitive failure retained pending")
	}
	m.rows[owner].AttemptAt = time.Now().Add(-time.Hour)
	if err := s.connect(context.Background(), owner, "correct invitation"); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackCooldownStatusAndRetry(t *testing.T) {
	s, m, u := feedbackFixture()
	ctx := context.Background()
	owner := s.owner("cooldown-account")
	if err := s.connect(ctx, owner, "synthetic-invitation"); err != nil {
		t.Fatal(err)
	}
	row := m.rows[owner]
	row.Cooldown = time.Now().Add(time.Minute)
	if got := s.view(row)["status"]; got != "rate-limited" {
		t.Fatalf("status=%v", got)
	}
	var f feedbackFailure
	if err := s.send(ctx, owner, feedbackRandom(), []byte("{}"), nil); !errors.As(err, &f) || f.Status != 429 {
		t.Fatalf("send cooldown=%v", err)
	}
	if err := s.connect(ctx, owner, "another-invitation"); !errors.As(err, &f) || f.Status != 429 || row.Attempt != "" {
		t.Fatalf("connect changed enrollment during cooldown: %v", err)
	}
	if u.reports != 0 {
		t.Fatal("sent during cooldown")
	}
	row.Cooldown = time.Now().Add(-time.Second)
	if got := s.view(row)["status"]; got != "connected" {
		t.Fatalf("status after cooldown=%v", got)
	}
	if err := s.send(ctx, owner, feedbackRandom(), []byte("{}"), nil); err != nil {
		t.Fatal(err)
	}
	if u.reports != 1 {
		t.Fatal("retry did not resume")
	}
	row.Cooldown = time.Now().Add(time.Minute)
	row.Expires = time.Now().Add(-time.Second)
	if got := s.view(row)["status"]; got != "expired" {
		t.Fatalf("cooldown hid expiry=%v", got)
	}
	row.State = "revoked"
	if got := s.view(row)["status"]; got != "revoked" {
		t.Fatalf("cooldown hid revocation=%v", got)
	}
}
func TestFeedbackLocalRateLimitPersistsWindowCooldown(t *testing.T) {
	s, m, _ := feedbackFixture()
	ctx := context.Background()
	subject := "local-limit"
	owner := s.owner(subject)
	if err := s.connect(ctx, owner, "synthetic-invitation"); err != nil {
		t.Fatal(err)
	}
	row := m.rows[owner]
	row.RateWindow = time.Now().Add(-30 * time.Second)
	row.RateCount = 12
	r := httptest.NewRequest("POST", "/api/feedback/connect", strings.NewReader(`{"invitation":"synthetic-invitation"}`))
	r.Header.Set("Origin", s.config.Origin)
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(context.WithValue(ctx, bearerContextKey{}, UserIdentity{Subject: subject}))
	w := httptest.NewRecorder()
	s.serve("connect", w, r)
	if w.Code != 429 || !row.Cooldown.Equal(row.RateWindow.Add(time.Minute)) || s.view(row)["status"] != "rate-limited" {
		t.Fatal("rate-limit window not reflected in durable status")
	}
}
