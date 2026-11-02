package contracts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/C-Azzone416/meal-planner/hosted"
)

type spec struct {
	Paths map[string]map[string]struct {
		Responses map[string]struct {
			Content map[string]struct {
				Schema struct {
					Ref string `yaml:"$ref"`
				} `yaml:"schema"`
			} `yaml:"content"`
		} `yaml:"responses"`
	} `yaml:"paths"`
	Components struct {
		Schemas map[string]schema `yaml:"schemas"`
	} `yaml:"components"`
}

type schema struct {
	Type                 string            `yaml:"type"`
	Required             []string          `yaml:"required"`
	AdditionalProperties *bool             `yaml:"additionalProperties"`
	Enum                 []string          `yaml:"enum"`
	Properties           map[string]schema `yaml:"properties"`
}

func TestHealthMatchesOpenAPI(t *testing.T) {
	raw, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var s spec
	if err := yaml.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	resp := s.Paths["/api/health"]["get"].Responses["200"]
	if _, ok := resp.Content["application/json"]; !ok {
		t.Fatal("spec lacks 200 application/json response for GET /api/health")
	}
	const prefix = "#/components/schemas/"
	ref := resp.Content["application/json"].Schema.Ref
	if len(ref) <= len(prefix) || ref[:len(prefix)] != prefix {
		t.Fatalf("unexpected schema ref %q", ref)
	}
	sch := s.Components.Schemas[ref[len(prefix):]]

	rec := httptest.NewRecorder()
	hosted.Health(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if sch.Type != "object" {
		t.Fatalf("schema type = %q", sch.Type)
	}
	for _, k := range sch.Required {
		if _, ok := body[k]; !ok {
			t.Errorf("missing required field %q", k)
		}
	}
	for k, v := range body {
		p, ok := sch.Properties[k]
		if !ok {
			if sch.AdditionalProperties != nil && !*sch.AdditionalProperties {
				t.Errorf("undeclared field %q", k)
			}
			continue
		}
		str, isStr := v.(string)
		if p.Type == "string" && !isStr {
			t.Errorf("field %q: want string, got %T", k, v)
			continue
		}
		if len(p.Enum) > 0 {
			found := false
			for _, e := range p.Enum {
				found = found || e == str
			}
			if !found {
				t.Errorf("field %q = %q not in enum %v", k, str, p.Enum)
			}
		}
	}
}
