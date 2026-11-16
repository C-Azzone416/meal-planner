package hosted

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

const diagnosticCanary = "SYNTHETIC_SECRET_INVITATION_ACCOUNT_SQL_CANARY"

func checkFeedbackDiagnostic(t *testing.T, err error, code string) {
	t.Helper()
	var buf bytes.Buffer
	oldWriter, oldFlags, oldPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() { log.SetOutput(oldWriter); log.SetFlags(oldFlags); log.SetPrefix(oldPrefix) }()
	w := httptest.NewRecorder()
	feedbackWriteError(w, err)
	if w.Code != 503 || w.Body.String() != "{\"error\":\"UNAVAILABLE\"}\n" {
		t.Fatalf("client changed: %d %s", w.Code, w.Body.String())
	}
	if buf.String() != code+"\n" {
		t.Fatalf("unexpected diagnostic: %q", buf.String())
	}
	if strings.Contains(buf.String()+w.Body.String()+err.Error(), diagnosticCanary) {
		t.Fatal("sensitive error leaked")
	}
}
func TestFeedbackDiagnosticsStaticOnly(t *testing.T) {
	if got := (feedbackDiagnostic{fbStoreBegin, errFeedbackStorage}).Error(); got != "feedback storage unavailable" {
		t.Fatal("safe storage error contract changed")
	}
	if got := (feedbackDiagnostic{fbConfigPublic, feedbackUnavailable}).Error(); got != "UNAVAILABLE" {
		t.Fatal("safe config error contract changed")
	}
	for i, code := range []string{"FB_CONFIG_PROVENANCE", "FB_CONFIG_PUBLIC", "FB_CONFIG_AUTH_PROVIDER", "FB_CONFIG_KEY_RING", "FB_STORE_DSN", "FB_STORE_INPUT", "FB_STORE_BEGIN", "FB_STORE_TIMEOUTS", "FB_STORE_RANDOM", "FB_STORE_INSERT", "FB_STORE_SELECT", "FB_STORE_UPDATE", "FB_STORE_COMMIT"} {
		checkFeedbackDiagnostic(t, feedbackDiagnostic{feedbackDiagnosticPhase(i), errors.New(diagnosticCanary)}, code)
	}
	checkFeedbackDiagnostic(t, feedbackDiagnostic{255, errors.New(diagnosticCanary)}, "FB_DIAGNOSTIC_UNKNOWN")
}
func TestFeedbackConfigurationDiagnostics(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	valid := map[string]string{"FRAMEBRIAR_ENABLED": "1", "FRAMEBRIAR_ENVIRONMENT": "preview", "FRAMEBRIAR_APP_ID": "meal-planner-preview", "FRAMEBRIAR_ALLOWED_ORIGIN": "https://preview.example.test", "FRAMEBRIAR_SCREENSHOTS": "1", "FRAMEBRIAR_DATABASE_URL": "postgresql://test:" + diagnosticCanary + "@example.test/db?sslmode=verify-full", "NEON_AUTH_BASE_URL": "https://ep-synthetic.neonauth.c-4.us-west-2.aws.neon.tech/neondb/auth", "NEON_AUTH_ENDPOINT_ID": "ep-synthetic", "FRAMEBRIAR_BUILD_SHA": "", "FRAMEBRIAR_CREDENTIAL_KEYS": "{\"current\":\"a\",\"keys\":{\"a\":\"" + base64.StdEncoding.EncodeToString(make([]byte, 32)) + "\"}}"}
	for k, v := range valid {
		t.Setenv(k, v)
	}
	if _, err := feedbackConfigurationForBuild(sha, "preview", "meal-planner-preview"); err != nil {
		t.Fatalf("valid configuration: %v", err)
	}
	for _, tc := range []struct{ name, key, value, code string }{
		{"provenance", "FRAMEBRIAR_BUILD_SHA", diagnosticCanary, "FB_CONFIG_PROVENANCE"},
		{"origin", "FRAMEBRIAR_ALLOWED_ORIGIN", diagnosticCanary, "FB_CONFIG_PUBLIC"},
		{"screenshots", "FRAMEBRIAR_SCREENSHOTS", diagnosticCanary, "FB_CONFIG_PUBLIC"},
		{"provider", "NEON_AUTH_BASE_URL", diagnosticCanary, "FB_CONFIG_AUTH_PROVIDER"},
		{"key json", "FRAMEBRIAR_CREDENTIAL_KEYS", diagnosticCanary, "FB_CONFIG_KEY_RING"},
		{"key bytes", "FRAMEBRIAR_CREDENTIAL_KEYS", "{\"current\":\"a\",\"keys\":{\"a\":\"" + diagnosticCanary + "\"}}", "FB_CONFIG_KEY_RING"},
		{"key missing", "FRAMEBRIAR_CREDENTIAL_KEYS", "{\"current\":\"b\",\"keys\":{\"a\":\"" + base64.StdEncoding.EncodeToString(make([]byte, 32)) + "\"}}", "FB_CONFIG_KEY_RING"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			_, err := feedbackConfigurationForBuild(sha, "preview", "meal-planner-preview")
			if !errors.Is(err, feedbackUnavailable) {
				t.Fatal("sentinel changed")
			}
			checkFeedbackDiagnostic(t, err, tc.code)
		})
	}
	_, err := newFeedbackSQLStore(diagnosticCanary)
	if !errors.Is(err, errFeedbackStorage) {
		t.Fatal("storage sentinel changed")
	}
	checkFeedbackDiagnostic(t, err, "FB_STORE_DSN")
}

// Test-only driver injects errors without opening a network connection or adding
// a runtime injection seam. No production driver behavior is replaced.
type feedbackFaultConnector struct {
	phase      string
	rolledBack bool
}

func (c *feedbackFaultConnector) Connect(context.Context) (driver.Conn, error) {
	return &feedbackFaultConn{c}, nil
}
func (c *feedbackFaultConnector) Driver() driver.Driver { return feedbackFaultDriver{} }

type feedbackFaultDriver struct{}

func (feedbackFaultDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unused") }

type feedbackFaultConn struct{ c *feedbackFaultConnector }

func (c *feedbackFaultConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *feedbackFaultConn) Close() error                        { return nil }
func (c *feedbackFaultConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *feedbackFaultConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if c.c.phase == "begin" {
		return nil, errors.New(diagnosticCanary)
	}
	return &feedbackFaultTx{c.c}, nil
}
func (c *feedbackFaultConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	fail := c.c.phase == "lock" && strings.HasPrefix(q, "SET LOCAL lock") || c.c.phase == "statement" && strings.HasPrefix(q, "SET LOCAL statement") || c.c.phase == "insert" && strings.HasPrefix(q, "INSERT") || c.c.phase == "update" && strings.HasPrefix(q, "UPDATE")
	if fail {
		return nil, errors.New(diagnosticCanary)
	}
	return driver.RowsAffected(1), nil
}
func (c *feedbackFaultConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if c.c.phase == "select" {
		return nil, errors.New(diagnosticCanary)
	}
	return &feedbackFaultRows{}, nil
}

type feedbackFaultTx struct{ c *feedbackFaultConnector }

func (t *feedbackFaultTx) Commit() error {
	if t.c.phase == "commit" {
		return errors.New(diagnosticCanary)
	}
	return nil
}
func (t *feedbackFaultTx) Rollback() error { t.c.rolledBack = true; return nil }

type feedbackFaultRows struct{ done bool }

func (*feedbackFaultRows) Columns() []string {
	return []string{"generation", "state", "key_id", "sealed", "expires_at", "attempt", "attempt_at", "cooldown_until", "rate_window", "rate_count", "attempt_digest"}
}
func (*feedbackFaultRows) Close() error { return nil }
func (r *feedbackFaultRows) Next(v []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(v, []driver.Value{"synthetic-generation", "", "", []byte(nil), nil, "", nil, nil, nil, int64(0), ""})
	return nil
}
func TestFeedbackSQLPhaseDiagnostics(t *testing.T) {
	for _, tc := range []struct{ phase, code string }{{"begin", "FB_STORE_BEGIN"}, {"lock", "FB_STORE_TIMEOUTS"}, {"statement", "FB_STORE_TIMEOUTS"}, {"insert", "FB_STORE_INSERT"}, {"select", "FB_STORE_SELECT"}, {"update", "FB_STORE_UPDATE"}, {"commit", "FB_STORE_COMMIT"}} {
		t.Run(tc.phase, func(t *testing.T) {
			c := &feedbackFaultConnector{phase: tc.phase}
			db := sql.OpenDB(c)
			defer db.Close()
			s := &feedbackSQLStore{db}
			err := s.WithRecord(context.Background(), diagnosticCanary, func(*feedbackRecord) error { return nil })
			if !errors.Is(err, errFeedbackStorage) {
				t.Fatal("storage sentinel changed")
			}
			checkFeedbackDiagnostic(t, err, tc.code)
			if tc.phase != "begin" && tc.phase != "commit" && !c.rolledBack {
				t.Fatal("transaction not rolled back")
			}
		})
	}
	c := &feedbackFaultConnector{}
	db := sql.OpenDB(c)
	defer db.Close()
	s := &feedbackSQLStore{db}
	called := false
	if err := s.WithRecord(context.Background(), "synthetic", func(*feedbackRecord) error { called = true; return nil }); err != nil || !called {
		t.Fatal("success path changed")
	}
	business := feedbackError("UNCERTAIN", 503)
	if err := s.WithRecord(context.Background(), "synthetic", func(*feedbackRecord) error { return business }); err != business {
		t.Fatal("business error changed")
	}
}
