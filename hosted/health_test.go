package hosted

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/C-Azzone416/meal-planner/internal/core/version"
)

func TestHealthGet(t *testing.T) {
	rec := httptest.NewRecorder()
	Health(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	var got HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := HealthResponse{Status: "ok", Service: "meal-planner-api", Version: version.Version}
	if got != want {
		t.Fatalf("body = %+v, want %+v", got, want)
	}
}

func TestHealthRejectsOtherMethods(t *testing.T) {
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		Health(rec, httptest.NewRequest(m, "/api/health", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", m, rec.Code)
		}
	}
}
