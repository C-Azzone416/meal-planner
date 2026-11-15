package hosted

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

type feedbackConfig struct {
	Enabled, Screenshots                                       bool
	Environment, Origin, App, SHA, Provider, Database, Current string
	Keys                                                       map[string][]byte
}

func feedbackConfiguration() (feedbackConfig, error) {
	c := feedbackConfig{Enabled: os.Getenv("FRAMEBRIAR_ENABLED") == "1", Screenshots: os.Getenv("FRAMEBRIAR_SCREENSHOTS") == "1", Environment: os.Getenv("FRAMEBRIAR_ENVIRONMENT"), Origin: os.Getenv("FRAMEBRIAR_ALLOWED_ORIGIN"), App: os.Getenv("FRAMEBRIAR_APP_ID"), Database: os.Getenv("FRAMEBRIAR_DATABASE_URL")}
	if !c.Enabled {
		return c, nil
	}
	var buildErr error
	c.SHA, buildErr = feedbackRuntimeBuildSHA(os.Getenv("FRAMEBRIAR_BUILD_SHA"), c.Environment, c.App, feedbackBuildSHA, feedbackBuildEnvironment, feedbackBuildApp)
	if buildErr != nil {
		return c, buildErr
	}
	expected := map[string]string{"preview": "meal-planner-preview", "production": "meal-planner"}[c.Environment]
	u, e := url.Parse(c.Origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || strings.Contains(u.Host, "*") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.ForceQuery || expected == "" || c.App != expected || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(c.SHA) || c.Database == "" {
		return c, feedbackUnavailable
	}
	shot := os.Getenv("FRAMEBRIAR_SCREENSHOTS")
	if shot != "0" && shot != "1" {
		return c, feedbackUnavailable
	}
	c.Provider, e = normalizeManagedAuthBaseURL(os.Getenv("NEON_AUTH_BASE_URL"), os.Getenv("NEON_AUTH_ENDPOINT_ID"))
	if e != nil {
		return c, feedbackUnavailable
	}
	var ring struct {
		Current string            `json:"current"`
		Keys    map[string]string `json:"keys"`
	}
	if json.Unmarshal([]byte(os.Getenv("FRAMEBRIAR_CREDENTIAL_KEYS")), &ring) != nil || ring.Current == "" || len(ring.Keys) == 0 {
		return c, feedbackUnavailable
	}
	c.Current = ring.Current
	c.Keys = map[string][]byte{}
	for id, key := range ring.Keys {
		b, e := base64.StdEncoding.DecodeString(key)
		if e != nil || len(b) != 32 || len(id) > 64 {
			return c, feedbackUnavailable
		}
		c.Keys[id] = b
	}
	if c.Keys[c.Current] == nil {
		return c, feedbackUnavailable
	}
	return c, nil
}

// Runtime provenance comes from the immutable generated build, never a mutable
// deployment setting. A legacy runtime SHA, when present, must still agree.
func feedbackRuntimeBuildSHA(runtimeSHA, environment, app, buildSHA, buildEnvironment, buildApp string) (string, error) {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(buildSHA) || environment != buildEnvironment || app != buildApp || (runtimeSHA != "" && runtimeSHA != buildSHA) {
		return "", feedbackUnavailable
	}
	return buildSHA, nil
}

type feedbackService struct {
	config   feedbackConfig
	store    feedbackStore
	upstream feedbackUpstream
}

var feedbackRuntime struct {
	sync.Mutex
	database string
	store    feedbackStore
}

// FeedbackHandler always uses the existing verified authentication policy.
func FeedbackHandler(action string) http.Handler {
	return RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := feedbackConfiguration()
		if err != nil {
			feedbackJSON(w, 503, map[string]string{"error": "UNAVAILABLE"})
			return
		}
		var store feedbackStore
		if c.Enabled {
			feedbackRuntime.Lock()
			if feedbackRuntime.store == nil || feedbackRuntime.database != c.Database {
				feedbackRuntime.store, err = newFeedbackSQLStore(c.Database)
				feedbackRuntime.database = c.Database
			}
			store = feedbackRuntime.store
			feedbackRuntime.Unlock()
			if err != nil || store == nil {
				feedbackJSON(w, 503, map[string]string{"error": "UNAVAILABLE"})
				return
			}
		}
		s := feedbackService{c, store, feedbackClient{c.App, &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}}
		s.serve(action, w, r)
	}))
}
func feedbackJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func feedbackWriteError(w http.ResponseWriter, err error) {
	var f feedbackFailure
	if errors.As(err, &f) {
		feedbackJSON(w, f.Status, map[string]string{"error": f.Code})
		return
	}
	feedbackJSON(w, 503, map[string]string{"error": "UNAVAILABLE"})
}
func feedbackRandom() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic("secure random unavailable")
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	v := hex.EncodeToString(b)
	return v[:8] + "-" + v[8:12] + "-" + v[12:16] + "-" + v[16:20] + "-" + v[20:]
}
func (s feedbackService) owner(subject string) string {
	b := sha256.Sum256([]byte(s.config.Environment + "\x00" + s.config.App + "\x00" + s.config.Provider + "\x00" + subject))
	return hex.EncodeToString(b[:])
}
func (s feedbackService) seal(row *feedbackRecord, g feedbackGrant) error {
	b, e := aes.NewCipher(s.config.Keys[s.config.Current])
	if e != nil {
		return feedbackUnavailable
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return feedbackUnavailable
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return feedbackUnavailable
	}
	plain, e := json.Marshal(g)
	if e != nil {
		return feedbackUnavailable
	}
	row.Sealed = a.Seal(nonce, nonce, plain, []byte(row.Owner+"|"+row.Generation+"|"+s.config.App))
	row.KeyID = s.config.Current
	row.Expires = g.RefreshExpires
	row.State = "connected"
	return nil
}
func (s feedbackService) open(row *feedbackRecord) (feedbackGrant, error) {
	var g feedbackGrant
	b, e := aes.NewCipher(s.config.Keys[row.KeyID])
	if e != nil {
		return g, feedbackError("NEEDS_RECONNECT", 409)
	}
	a, e := cipher.NewGCM(b)
	if e != nil || len(row.Sealed) < a.NonceSize() {
		return g, feedbackError("NEEDS_RECONNECT", 409)
	}
	plain, e := a.Open(nil, row.Sealed[:a.NonceSize()], row.Sealed[a.NonceSize():], []byte(row.Owner+"|"+row.Generation+"|"+s.config.App))
	if e != nil || json.Unmarshal(plain, &g) != nil {
		return g, feedbackError("NEEDS_RECONNECT", 409)
	}
	return g, nil
}
func (s feedbackService) view(row *feedbackRecord) map[string]any {
	status := row.State
	if status == "" {
		status = "not-connected"
	}
	if status == "connected" {
		if !row.Expires.After(time.Now()) {
			status = "expired"
		} else if _, err := s.open(row); err != nil {
			status = "needs-reconnect"
		} else if row.Cooldown.After(time.Now()) {
			status = "rate-limited"
		}
	}
	b := sha256.Sum256([]byte("meal-feedback-outbox|" + row.Owner + "|" + row.Generation))
	v := map[string]any{"status": status, "outboxKey": base64.RawURLEncoding.EncodeToString(b[:]), "environment": s.config.Environment, "appId": s.config.App, "buildSha": s.config.SHA, "screenshots": s.config.Screenshots}
	if !row.Expires.IsZero() {
		v["expiresAt"] = row.Expires.UTC().Format(time.RFC3339)
	}
	if row.Attempt != "" {
		v["redemption"] = "pending"
	}
	return v
}
func feedbackDecode(r *http.Request, out any) error {
	typ, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || typ != "application/json" {
		return feedbackError("UNSUPPORTED_MEDIA_TYPE", 415)
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 8193))
	if e != nil {
		return feedbackError("INVALID_REQUEST", 400)
	}
	if len(b) > 8192 {
		return feedbackError("TOO_LARGE", 413)
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(&struct{}{}) != io.EOF {
		return feedbackError("INVALID_REQUEST", 400)
	}
	return nil
}
func (s feedbackService) serve(action string, w http.ResponseWriter, r *http.Request) {
	method := "POST"
	if action == "status" {
		method = "GET"
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		feedbackWriteError(w, feedbackError("METHOD_NOT_ALLOWED", 405))
		return
	}
	if !s.config.Enabled {
		feedbackJSON(w, 200, map[string]any{"status": "disabled", "screenshots": false})
		return
	}
	origin := r.Header.Get("Origin")
	// Browsers omit Origin on same-origin GET. Accept only an exact referrer
	// origin with browser same-origin evidence, never Host or forwarded headers.
	if origin == "" && r.Method == http.MethodGet && r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		if ref, err := url.Parse(r.Referer()); err == nil && ref.User == nil && ref.Scheme != "" && ref.Host != "" {
			origin = ref.Scheme + "://" + ref.Host
		}
	}
	if origin != s.config.Origin {
		feedbackWriteError(w, feedbackError("ORIGIN_REJECTED", 403))
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok || user.Subject == "" {
		feedbackWriteError(w, feedbackError("UNAUTHORIZED", 401))
		return
	}
	owner := s.owner(user.Subject)
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	var result any
	if action == "status" {
		err := s.store.WithRecord(ctx, owner, func(row *feedbackRecord) error { result = s.view(row); return nil })
		if err != nil {
			feedbackWriteError(w, err)
			return
		}
		feedbackJSON(w, 200, result)
		return
	}
	// Durable per-account limit is shared by every serverless instance.
	err := s.store.WithRecord(ctx, owner, func(row *feedbackRecord) error {
		now := time.Now()
		if now.Sub(row.RateWindow) >= time.Minute {
			row.RateWindow = now
			row.RateCount = 0
		}
		if row.RateCount >= 12 {
			if end := row.RateWindow.Add(time.Minute); end.After(row.Cooldown) {
				row.Cooldown = end
			}
			return feedbackError("RATE_LIMITED", 429)
		}
		row.RateCount++
		return nil
	})
	if err != nil {
		feedbackWriteError(w, err)
		return
	}
	switch action {
	case "connect":
		var input struct {
			Invitation string `json:"invitation"`
		}
		if err = feedbackDecode(r, &input); err == nil {
			if len(input.Invitation) < 16 || len(input.Invitation) > 4096 {
				err = feedbackError("INVALID_REQUEST", 400)
			} else {
				err = s.connect(ctx, owner, input.Invitation)
			}
		}
	case "disconnect":
		var input struct{}
		if err = feedbackDecode(r, &input); err == nil {
			err = s.RevokeAndErase(ctx, owner, false)
		}
	case "reports":
		var report feedbackReport
		var raw, media []byte
		report, raw, media, err = s.parseReport(w, r)
		if err == nil {
			err = s.send(ctx, owner, report.ID, raw, media)
			if err == nil {
				result = map[string]string{"id": report.ID, "gameId": s.config.App, "status": "received"}
			}
		}
	default:
		err = feedbackError("NOT_FOUND", 404)
	}
	if err != nil {
		feedbackWriteError(w, err)
		return
	}
	if result == nil {
		err = s.store.WithRecord(ctx, owner, func(row *feedbackRecord) error { result = s.view(row); return nil })
		if err != nil {
			feedbackWriteError(w, err)
			return
		}
	}
	feedbackJSON(w, 200, result)
}
func (s feedbackService) connect(ctx context.Context, owner, invitation string) error {
	digestBytes := sha256.Sum256([]byte(invitation))
	digest := hex.EncodeToString(digestBytes[:])
	// Commit an idempotency ID before sending the invitation. Never persist the invitation.
	err := s.store.WithRecord(ctx, owner, func(row *feedbackRecord) error {
		if s.view(row)["status"] == "rate-limited" {
			return feedbackError("RATE_LIMITED", 429)
		}
		if s.view(row)["status"] == "connected" {
			return feedbackError("ALREADY_CONNECTED", 409)
		}
		if row.Attempt == "" {
			row.Attempt = feedbackRandom()
			row.AttemptAt = time.Now()
			row.AttemptDigest = digest
		}
		if row.AttemptDigest != digest || time.Since(row.AttemptAt) > 15*time.Minute {
			return feedbackError("PENDING_REDEMPTION", 409)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.store.WithRecord(ctx, owner, func(row *feedbackRecord) error {
		if s.view(row)["status"] == "rate-limited" {
			return feedbackError("RATE_LIMITED", 429)
		}
		if s.view(row)["status"] == "connected" {
			return nil
		}
		if row.Attempt == "" || row.AttemptDigest != digest || time.Since(row.AttemptAt) > 15*time.Minute {
			return feedbackError("PENDING_REDEMPTION", 409)
		}
		g, e := s.upstream.Grant(ctx, "redeem", map[string]any{"invitation": invitation, "attemptId": row.Attempt})
		if e != nil {
			var f feedbackFailure
			if errors.As(e, &f) && (f.Status == 400 || f.Status == 401 || f.Status == 403) {
				row.Attempt, row.AttemptDigest = "", ""
				row.AttemptAt = time.Time{}
				return feedbackError("INVITATION_REJECTED", 400)
			}
			return e
		}
		if e = s.seal(row, g); e != nil {
			return e
		}
		row.Attempt, row.AttemptDigest = "", ""
		row.AttemptAt = time.Time{}
		return nil
	})
}

// RevokeAndErase is the future account-deletion hook. Call with the scope-hashed owner,
// and rotate=true BEFORE deleting the provider identity. Never delete on an error.
// Lost redemption answers and lost encryption keys deliberately block success rather
// than falsely claiming upstream revocation. Delivered Framebriar reports are unchanged.
func (s feedbackService) RevokeAndErase(ctx context.Context, owner string, rotate bool) error {
	return s.store.WithRecord(ctx, owner, func(row *feedbackRecord) error {
		if row.Attempt != "" {
			return feedbackError("PENDING_REDEMPTION", 409)
		}
		if len(row.Sealed) > 0 && row.Expires.After(time.Now()) {
			g, e := s.open(row)
			if e != nil {
				return e
			}
			e = s.upstream.Disconnect(ctx, g.Refresh)
			var f feedbackFailure
			if e != nil && !(errors.As(e, &f) && f.Code == "REVOKED") {
				return e
			}
		}
		row.Sealed = nil
		row.KeyID = ""
		row.Expires = time.Time{}
		row.State = "not-connected"
		row.Cooldown = time.Time{}
		if rotate {
			row.Generation = feedbackRandom()
		}
		return nil
	})
}
func (s feedbackService) send(ctx context.Context, owner, id string, report, media []byte) error {
	return s.store.WithRecord(ctx, owner, func(row *feedbackRecord) error {
		status := s.view(row)["status"].(string)
		if status == "rate-limited" {
			return feedbackError("RATE_LIMITED", 429)
		}
		if status != "connected" {
			return feedbackError(strings.ToUpper(strings.ReplaceAll(status, "-", "_")), 409)
		}
		if row.Cooldown.After(time.Now()) {
			return feedbackError("RATE_LIMITED", 429)
		}
		g, e := s.open(row)
		if e != nil {
			return e
		}
		refresh := func() error {
			next, err := s.upstream.Grant(ctx, "refresh", map[string]any{"refreshToken": g.Refresh})
			if err != nil {
				var f feedbackFailure
				if errors.As(err, &f) {
					if f.Code == "REVOKED" {
						row.State = "revoked"
					}
					if f.Code == "DISABLED" {
						row.State = "disabled"
					}
				}
				row.Cooldown = time.Now().Add(time.Minute)
				return err
			}
			if !next.RefreshExpires.Equal(g.RefreshExpires) {
				return feedbackError("UNCERTAIN", 503)
			}
			g = next
			return s.seal(row, g)
		}
		refreshed := false
		if time.Until(g.Expires) < 2*time.Minute {
			if e = refresh(); e != nil {
				return e
			}
			refreshed = true
		} else if row.KeyID != s.config.Current {
			if e = s.seal(row, g); e != nil {
				return e
			}
		}
		e = s.upstream.Report(ctx, g.Token, id, report, media)
		var f feedbackFailure
		if errors.As(e, &f) && f.Code == "REVOKED" && !refreshed {
			if e = refresh(); e != nil {
				return e
			}
			e = s.upstream.Report(ctx, g.Token, id, report, media)
		}
		if errors.As(e, &f) {
			switch f.Code {
			case "REVOKED":
				row.State = "revoked"
			case "DISABLED":
				row.State = "disabled"
			case "RATE_LIMITED":
				row.Cooldown = time.Now().Add(time.Minute)
			}
		}
		return e
	})
}

// RevokeAndEraseFeedback is the account-deletion integration hook. The caller must
// obtain identity from RequireUser and invoke this BEFORE deleting the provider
// account. It is not an HTTP endpoint and does not authorize account deletion.
// On success credentials are erased and the durable generation is rotated. On
// error deletion must stop: pending redemption or missing keys need operator
// reconciliation. This does not erase reports already delivered to Framebriar.
// Disabled or incomplete configuration fails closed and blocks deletion.
func RevokeAndEraseFeedback(ctx context.Context, identity UserIdentity) error {
	if identity.Subject == "" {
		return feedbackError("UNAUTHORIZED", 401)
	}
	// Disabling the feature must not bypass revocation. A missing configuration
	// cannot prove absence of enrollment, so the deletion coordinator must stop.
	c, err := feedbackConfiguration()
	if err != nil || !c.Enabled {
		return feedbackUnavailable
	}
	store, err := newFeedbackSQLStore(c.Database)
	if err != nil {
		return feedbackUnavailable
	}
	sqlStore := store.(*feedbackSQLStore)
	defer sqlStore.db.Close()
	service := feedbackService{c, store, feedbackClient{c.App, &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}}
	return service.RevokeAndErase(ctx, service.owner(identity.Subject), true)
}
