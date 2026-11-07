//go:build integration

package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestFeedbackBrowserServer is an opt-in, loopback-only browser integration fixture.
// It runs the real Go service, SQL store, auth verifier and wire client. Only the
// provider JWKS and Framebriar network responses are synthetic. It is never built
// into a production Go binary. Stop it by POST /__fixture/shutdown after the run.
func TestFeedbackBrowserServer(t *testing.T) {
	if os.Getenv("FEEDBACK_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in browser fixture")
	}
	root, err := os.MkdirTemp("/tmp", "feedback-browser-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	binary := func(name string) string {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		if config, err := exec.LookPath("pg_config"); err == nil {
			if out, err := exec.Command(config, "--bindir").Output(); err == nil {
				candidate := filepath.Join(strings.TrimSpace(string(out)), name)
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					return candidate
				}
			}
		}
		return "/opt/homebrew/bin/" + name
	}
	run := func(name string, args ...string) {
		t.Helper()
		if output, err := exec.Command(binary(name), args...).CombinedOutput(); err != nil {
			t.Fatalf("temporary %s: %v: %s", name, err, output)
		}
	}
	data := filepath.Join(root, "data")
	run("initdb", "-D", data, "-A", "trust", "-U", "feedback_test", "--no-locale")
	run("pg_ctl", "-D", data, "-l", filepath.Join(root, "server.log"), "-o", "-k "+root+" -p 55484 -h '' -c unix_socket_permissions=0700", "-w", "start")
	defer func() {
		run("pg_ctl", "-D", data, "-m", "fast", "-w", "stop")
		t.Log("Temporary fixture PostgreSQL stopped")
	}()
	store, err := newFeedbackSQLStore("postgres:///postgres?host=" + root + "&port=55484&user=feedback_test&sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	sqlStore := store.(*feedbackSQLStore)
	defer sqlStore.db.Close()
	migration, err := os.ReadFile("../migrations/001_framebriar.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sqlStore.db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	service, _, _ := feedbackFixture()
	service.store = store
	service.config.Origin = "http://localhost:4181"
	service.config.SHA = "1234567890123456789012345678901234567890"
	service.config.Screenshots = true
	auth := newBearerFixture(t)
	claims := auth.claims()
	claims["sub"] = "synthetic-account-one"
	claims["email"] = "synthetic@example.test"
	policy := auth.config()
	policy.AllowedEmail = "synthetic@example.test"
	token := auth.token(t, claims)
	claims["sub"] = "synthetic-account-two"
	tokenTwo := auth.token(t, claims)
	var mu sync.Mutex
	failReports := false
	reports := []json.RawMessage{}
	mediaCount := 0
	familyExpiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	transport := feedbackRoundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if !strings.HasPrefix(r.URL.String(), feedbackEndpoint+"/") {
			return nil, errors.New("fixture unexpected target")
		}
		respond := func(v any) (*http.Response, error) {
			b, _ := json.Marshal(v)
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(b))}, nil
		}
		path := strings.TrimPrefix(r.URL.Path, "/api")
		switch path {
		case "/v1/access/redeem", "/v1/access/refresh":
			return respond(feedbackGrant{V: 1, GameID: service.config.App, Scope: 2, Token: strings.Repeat("a", 32), Refresh: strings.Repeat("b", 32), Expires: time.Now().UTC().Add(time.Hour).Truncate(time.Second), RefreshExpires: familyExpiry})
		case "/v1/access/disconnect":
			return respond(map[string]int{"v": 1})
		case "/v1/reports":
			b, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			var report feedbackReport
			if json.Unmarshal(b, &report) != nil {
				return nil, errors.New("fixture invalid report")
			}
			reports = append(reports, json.RawMessage(b))
			if failReports {
				return nil, errors.New("fixture delivery unavailable")
			}
			if report.Media != nil {
				return respond(map[string]any{"id": report.ID, "gameId": service.config.App, "status": "pending", "ticket": strings.Repeat("t", 43), "expiresAt": time.Now().UTC().Add(time.Minute)})
			}
			return respond(map[string]string{"id": report.ID, "gameId": service.config.App, "status": "ready"})
		default:
			if strings.HasPrefix(path, "/v1/reports/") && strings.HasSuffix(path, "/media") && r.Method == "PUT" {
				id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/reports/"), "/media")
				mediaCount++
				return respond(map[string]string{"id": id, "gameId": service.config.App, "status": "ready"})
			}
			return nil, errors.New("fixture unsupported operation")
		}
	})
	service.upstream = feedbackClient{service.config.App, &http.Client{Transport: transport, Timeout: time.Second}}
	mux := http.NewServeMux()
	for _, action := range []string{"status", "connect", "disconnect", "reports"} {
		mux.Handle("/api/feedback/"+action, RequireUserWithConfig(policy, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { service.serve(action, w, r) })))
	}
	done := make(chan struct{})
	var stop sync.Once
	mux.HandleFunc("/__fixture/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		feedbackJSON(w, 200, map[string]bool{"ready": true})
	})
	mux.HandleFunc("/__fixture/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		selectedToken, subject := token, "synthetic-account-one"
		if r.URL.Query().Get("account") == "two" {
			selectedToken, subject = tokenTwo, "synthetic-account-two"
		}
		feedbackJSON(w, 200, map[string]any{"token": selectedToken, "user": map[string]any{"id": subject, "email": "synthetic@example.test", "emailVerified": true}})
	})
	mux.HandleFunc("/__fixture/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var input struct {
			Connected bool `json:"connected"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		if _, err := sqlStore.db.Exec("TRUNCATE framebriar_enrollments"); err != nil {
			feedbackWriteError(w, err)
			return
		}
		mu.Lock()
		failReports = false
		reports = []json.RawMessage{}
		mediaCount = 0
		mu.Unlock()
		if input.Connected {
			if err := service.connect(r.Context(), service.owner("synthetic-account-one"), "synthetic-invitation-fixture"); err != nil {
				feedbackWriteError(w, err)
				return
			}
		}
		feedbackJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/__fixture/failure", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var input struct {
			FailReports bool `json:"failReports"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		failReports = input.FailReports
		mu.Unlock()
		feedbackJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/__fixture/evidence", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		feedbackJSON(w, 200, map[string]any{"reports": reports, "mediaCount": mediaCount})
	})
	mux.HandleFunc("/__fixture/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		feedbackJSON(w, 200, map[string]bool{"ok": true})
		stop.Do(func() { close(done) })
	})
	listener, err := net.Listen("tcp", "127.0.0.1:4182")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	t.Log("Synthetic browser Go fixture ready at http://127.0.0.1:4182; PostgreSQL isolated on a private Unix socket")
	signalCtx, signalStop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer signalStop()
	select {
	case <-signalCtx.Done():
	case <-done:
	case err := <-serverErr:
		if err != http.ErrServerClosed {
			t.Error(err)
		}
	case <-time.After(15 * time.Minute):
		t.Error("fixture timed out waiting for shutdown")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Error(err)
	}
}
