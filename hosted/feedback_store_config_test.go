package hosted

import (
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Explicit test-only pool injection keeps local disposable socket fixtures out of
// the production URL validator. No runtime flag can disable verified TLS.
func feedbackTestSQLStore(dsn string) (feedbackStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	return &feedbackSQLStore{db: db}, nil
}

func TestFeedbackDatabaseURL(t *testing.T) {
	const base = "postgres://feedback:synthetic-only@db.example.test/preview?sslmode=verify-full"
	for _, raw := range []string{base, strings.Replace(base, "postgres:", "postgresql:", 1), base + "&sslrootcert=/approved/ca.pem"} {
		normalized, err := feedbackDatabaseURL(raw)
		if err != nil {
			t.Fatalf("valid server-held URL rejected: %v", err)
		}
		u, err := url.Parse(normalized)
		if err != nil || u.Query().Get("sslmode") != "verify-full" || u.Query().Get("connect_timeout") != "5" || u.Query().Get("application_name") != "meal-planner-feedback" {
			t.Fatal("missing verified TLS or bounded connection settings")
		}
		if u.Query().Has("statement_timeout") || u.Query().Has("lock_timeout") {
			t.Fatal("transaction settings leaked into startup packet")
		}
	}
	bad := []string{"", "host=db.example.test password=synthetic-only", "postgres:///preview?sslmode=verify-full", "postgres://db.example.test/preview?sslmode=verify-full", "postgres://feedback@db.example.test/?sslmode=verify-full", "https://feedback@db.example.test/preview?sslmode=verify-full", strings.Replace(base, "verify-full", "require", 1), strings.Replace(base, "verify-full", "disable", 1), strings.Split(base, "?")[0], base + "#fragment", base + "&sslmode=disable", base + "&host=elsewhere.test", base + "&user=other", base + "&password=other", base + "&search_path=other", base + "&connect_timeout=0", base + "&options=-c%20statement_timeout%3D0", base + "&channel_binding=require", base + "&sslmode%ZZ=verify-full"}
	for _, raw := range bad {
		if _, err := feedbackDatabaseURL(raw); !errors.Is(err, errFeedbackStorage) {
			t.Fatal("unsafe database URL accepted or error exposed configuration")
		}
		if store, err := newFeedbackSQLStore(raw); store != nil || !errors.Is(err, errFeedbackStorage) {
			t.Fatal("unsafe database opened")
		}
	}
}

func TestFeedbackRuntimeBuildSHA(t *testing.T) {
	const sha = "1234567890123456789012345678901234567890"
	for _, legacy := range []string{"", sha} {
		got, err := feedbackRuntimeBuildSHA(legacy, "preview", "meal-planner-preview", sha, "preview", "meal-planner-preview")
		if err != nil || got != sha {
			t.Fatal("immutable provenance not used")
		}
	}
	cases := [][6]string{
		{"", "preview", "meal-planner-preview", "", "preview", "meal-planner-preview"},
		{"", "preview", "meal-planner-preview", "dev", "preview", "meal-planner-preview"},
		{"", "preview", "meal-planner-preview", strings.Repeat("A", 40), "preview", "meal-planner-preview"},
		{"", "production", "meal-planner-preview", sha, "preview", "meal-planner-preview"},
		{"", "preview", "meal-planner", sha, "preview", "meal-planner-preview"},
		{strings.Repeat("a", 40), "preview", "meal-planner-preview", sha, "preview", "meal-planner-preview"},
	}
	for _, c := range cases {
		if got, err := feedbackRuntimeBuildSHA(c[0], c[1], c[2], c[3], c[4], c[5]); err == nil || got != "" {
			t.Fatal("untrusted or mismatched provenance accepted")
		}
	}
}
