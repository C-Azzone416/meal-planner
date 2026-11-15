package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// These model the expected provider contract, not live service verification.
func TestFeedbackMediaRetryContract(t *testing.T) {
	for _, outcome := range []string{"ready", "wrong-id", "wrong-app", "pending", "already-ready", "expired-ticket"} {
		t.Run(outcome, func(t *testing.T) {
			posts, puts := 0, 0
			report, media := []byte(`{"id":"stable-report"}`), []byte("synthetic PNG bytes")
			c := feedbackClient{"meal-planner-preview", &http.Client{Transport: feedbackRoundTrip(func(r *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				reply := map[string]any{"id": "stable-report", "gameId": "meal-planner-preview", "status": "ready"}
				status := 200
				switch r.Method {
				case "POST":
					posts++
					if r.URL.Path != "/api/v1/reports" || !bytes.Equal(body, report) {
						t.Fatal("metadata identity changed")
					}
					if posts == 1 || outcome != "already-ready" {
						reply["status"] = "pending"
						reply["ticket"] = strings.Repeat(string(rune('a'+posts)), 43)
						reply["expiresAt"] = time.Now().UTC().Add(time.Minute)
						if posts > 1 && outcome == "expired-ticket" {
							reply["expiresAt"] = time.Now().UTC().Add(-time.Minute)
						}
					}
				case "PUT":
					puts++
					if r.Header.Get("X-Upload-Ticket") != strings.Repeat(string(rune('a'+posts)), 43) {
						t.Fatal("retry did not use fresh upload ticket")
					}
					if r.URL.Path != "/api/v1/reports/stable-report/media" || !bytes.Equal(body, media) {
						t.Fatal("media changed")
					}
					if puts == 1 {
						status = 502
					} else {
						switch outcome {
						case "wrong-id":
							reply["id"] = "other"
						case "wrong-app":
							reply["gameId"] = "other"
						case "pending":
							reply["status"] = "pending"
						}
					}
				default:
					t.Fatalf("unexpected method %s", r.Method)
				}
				b, _ := json.Marshal(reply)
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(b))}, nil
			})}}
			if err := c.Report(context.Background(), "synthetic-token", "stable-report", report, media); err == nil {
				t.Fatal("media502 accepted")
			}
			err := c.Report(context.Background(), "synthetic-token", "stable-report", report, media)
			success := outcome == "ready" || outcome == "already-ready"
			if (err == nil) != success {
				t.Fatalf("retry outcome %v", err)
			}
			expectedPuts := 2
			if outcome == "already-ready" || outcome == "expired-ticket" {
				expectedPuts = 1
			}
			if posts != 2 || puts != expectedPuts {
				t.Fatalf("calls posts=%d puts=%d", posts, puts)
			}
		})
	}
}
