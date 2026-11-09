package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const recipeTestID = "10000000-0000-4000-8000-000000000001"
const recipeTestKey = "10000000-0000-4000-8000-000000000002"

func syntheticRecipeContent() RecipeContent {
	return RecipeContent{Title: "Synthetic test dish", Icon: "🍽️", ActiveMinutes: 10, TotalMinutes: 20, Attention: "low", Tags: []string{}, DefaultServings: 4, Ingredients: []RecipeIngredient{{ID: "i1", Name: "Test ingredient", Aisle: "Other"}}, PrepAhead: []RecipePrepAhead{}, PrepTonight: []RecipePrepTonight{}, CookTonight: []RecipeCookStep{{ID: "c1", Text: "Synthetic instruction"}}}
}

type recipeFake struct {
	calls          int
	owner          RecipeOwner
	content        RecipeContent
	key, after, id string
	limit          int
	revision       int64
	err            error
	replay         bool
}

func (s *recipeFake) envelope() RecipeEnvelope {
	return RecipeEnvelope{ID: recipeTestID, Revision: 1, Status: "approved", Origin: "manual", CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC(), Content: s.content}
}
func (s *recipeFake) Create(_ context.Context, o RecipeOwner, key string, c RecipeContent) (RecipeEnvelope, bool, error) {
	s.calls++
	s.owner = o
	s.key = key
	s.content = c
	return s.envelope(), s.replay, s.err
}
func (s *recipeFake) Get(_ context.Context, o RecipeOwner, id string) (RecipeEnvelope, error) {
	s.calls++
	s.owner = o
	s.id = id
	return s.envelope(), s.err
}
func (s *recipeFake) List(_ context.Context, o RecipeOwner, after string, limit int) ([]RecipeEnvelope, string, error) {
	s.calls++
	s.owner = o
	s.after = after
	s.limit = limit
	return nil, "", s.err
}
func (s *recipeFake) Update(_ context.Context, o RecipeOwner, id string, revision int64, c RecipeContent) (RecipeEnvelope, error) {
	s.calls++
	s.owner = o
	s.id = id
	s.revision = revision
	s.content = c
	return s.envelope(), s.err
}
func recipeRequest(t *testing.T, store RecipeRepository, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", recipeTestKey)
	req = req.WithContext(context.WithValue(req.Context(), bearerContextKey{}, UserIdentity{Issuer: testIssuer, Subject: "account-a", Email: "unused@example.invalid"}))
	rec := httptest.NewRecorder()
	RecipesHandler(store).ServeHTTP(rec, req)
	return rec
}
func recipeBody(t *testing.T, c RecipeContent) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"content": c})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func TestRecipeAPICreatesVerifiedOwnerAndServerEnvelope(t *testing.T) {
	store := &recipeFake{}
	rec := recipeRequest(t, store, "POST", "/api/recipes", recipeBody(t, syntheticRecipeContent()))
	if rec.Code != 201 || store.calls != 1 || store.owner != (RecipeOwner{Issuer: testIssuer, Subject: "account-a"}) || store.key != recipeTestKey {
		t.Fatalf("status=%d owner=%+v calls=%d body=%s", rec.Code, store.owner, store.calls, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || strings.Contains(rec.Body.String(), "account-a") {
		t.Fatal("unsafe response")
	}
	store.replay = true
	if got := recipeRequest(t, store, "POST", "/api/recipes", recipeBody(t, syntheticRecipeContent())).Code; got != 200 {
		t.Fatalf("replay=%d", got)
	}
}
func TestRecipeAPIRejectsServerFieldsAndMalformedContent(t *testing.T) {
	valid := recipeBody(t, syntheticRecipeContent())
	cases := map[string]string{"owner": strings.Replace(valid, `{"content":`, `{"owner":"victim","content":`, 1), "status": strings.Replace(valid, `"title":`, `"status":"approved","title":`, 1), "id": strings.Replace(valid, `"title":`, `"id":"client-id","title":`, 1), "missing number": strings.Replace(valid, `"activeMinutes":10,`, "", 1), "null number": strings.Replace(valid, `"activeMinutes":10`, `"activeMinutes":null`, 1), "null array": strings.Replace(valid, `"tags":[]`, `"tags":null`, 1), "null content": `{"content":null}`, "unknown nested": strings.Replace(valid, `"name":`, `"owner":"victim","name":`, 1), "trailing json": valid + `{}`, "negative amount": strings.Replace(valid, `"quantity":null`, `"quantity":-1`, 1), "missing ingredient boolean": strings.Replace(valid, `,"isPantry":false`, "", 1), "null ingredient boolean": strings.Replace(valid, `"isPantry":false`, `"isPantry":null`, 1)}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			store := &recipeFake{}
			rec := recipeRequest(t, store, "POST", "/api/recipes", body)
			if rec.Code != 400 || store.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, store.calls, rec.Body)
			}
		})
	}
}
func TestRecipeAPIRequiresAuthEvenWithoutWrapper(t *testing.T) {
	store := &recipeFake{}
	rec := httptest.NewRecorder()
	RecipesHandler(store).ServeHTTP(rec, httptest.NewRequest("GET", "/api/recipes", nil))
	if rec.Code != 401 || store.calls != 0 {
		t.Fatal(rec.Code)
	}
}
func TestRecipeAPIRejectsInvalidBearerBeforeStorage(t *testing.T) {
	f := newBearerFixture(t)
	for _, change := range []string{"missing", "expired", "issuer", "audience", "unverified", "subject", "signature"} {
		t.Run(change, func(t *testing.T) {
			claims := f.claims()
			switch change {
			case "expired":
				claims["exp"] = f.now.Add(-time.Second).Unix()
			case "issuer":
				claims["iss"] = "https://other.invalid"
			case "audience":
				claims["aud"] = "other"
			case "unverified":
				claims["email_verified"] = false
			case "subject":
				claims["sub"] = ""
			}
			token := f.token(t, claims)
			if change == "signature" {
				token = token[:len(token)-5] + "aaaaa"
			}
			store := &recipeFake{}
			req := httptest.NewRequest("GET", "/api/recipes", nil)
			if change != "missing" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			RequireUserWithConfig(f.config(), RecipesHandler(store)).ServeHTTP(rec, req)
			if rec.Code != 401 || store.calls != 0 {
				t.Fatalf("%d calls %d", rec.Code, store.calls)
			}
		})
	}
}
func TestRecipeAPIVerifiedIssuerIsPassedToStore(t *testing.T) {
	f := newBearerFixture(t)
	req := httptest.NewRequest("GET", "/api/recipes", nil)
	req.Header.Set("Authorization", "Bearer "+f.token(t, f.claims()))
	rec := httptest.NewRecorder()
	store := &recipeFake{}
	RequireUserWithConfig(f.config(), RecipesHandler(store)).ServeHTTP(rec, req)
	if rec.Code != 200 || store.owner.Issuer != testIssuer || store.owner.Subject != "user-test" {
		t.Fatal(rec.Code, store.owner)
	}
}
func TestRecipeAPIQueriesAndErrors(t *testing.T) {
	for _, path := range []string{"/api/recipes?limit=0", "/api/recipes?limit=101", "/api/recipes?limit=x", "/api/recipes?after=bad", "/api/recipes?owner=bad", "/api/recipes?id=bad", "/api/recipes?limit=1&limit=2", "/api/recipes?id=" + recipeTestID + "&after=" + recipeTestID} {
		store := &recipeFake{}
		if rec := recipeRequest(t, store, "GET", path, ""); rec.Code != 400 || store.calls != 0 {
			t.Fatal(path, rec.Code, store.calls)
		}
	}
	store := &recipeFake{}
	rec := recipeRequest(t, store, "GET", "/api/recipes?limit=2&after="+recipeTestID, "")
	if rec.Code != 200 || store.limit != 2 || store.after != recipeTestID || rec.Body.String() != "{\"recipes\":[],\"nextCursor\":null}\n" {
		t.Fatal(rec.Code, store, rec.Body)
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{ErrRecipeNotFound, 404, "not_found"}, {ErrRecipeConflict, 409, "conflict"}, {ErrRecipeIdempotencyConflict, 409, "idempotency_conflict"}, {errors.New("private DSN password and recipe payload"), 503, "unavailable"}} {
		rec := recipeRequest(t, &recipeFake{err: tc.err}, "GET", "/api/recipes?id="+recipeTestID, "")
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) || strings.Contains(rec.Body.String(), "private DSN") {
			t.Fatal(rec.Code, rec.Body)
		}
	}
}
func TestRecipeAPIUpdateRequiresRevision(t *testing.T) {
	content, _ := json.Marshal(syntheticRecipeContent())
	for _, rev := range []string{"null", "0", "-1", "1.5", "9007199254740992"} {
		store := &recipeFake{}
		rec := recipeRequest(t, store, "PUT", "/api/recipes?id="+recipeTestID, fmt.Sprintf(`{"content":%s,"expectedRevision":%s}`, content, rev))
		if rec.Code != 400 || store.calls != 0 {
			t.Fatal(rev, rec.Code)
		}
	}
	store := &recipeFake{}
	rec := recipeRequest(t, store, "PUT", "/api/recipes?id="+recipeTestID, fmt.Sprintf(`{"content":%s,"expectedRevision":3}`, content))
	if rec.Code != 200 || store.revision != 3 || store.id != recipeTestID {
		t.Fatal(rec.Code, store)
	}
}
func TestRecipeAPIRequestBoundsAndUnavailable(t *testing.T) {
	store := &recipeFake{}
	rec := recipeRequest(t, store, "POST", "/api/recipes", `{"content":{"title":"`+strings.Repeat("x", maxRecipeRequestBytes)+`"}}`)
	if rec.Code != 413 || store.calls != 0 {
		t.Fatal(rec.Code)
	}
	rec = recipeRequest(t, nil, "GET", "/api/recipes", "")
	if rec.Code != 503 {
		t.Fatal(rec.Code)
	}
	rec = recipeRequest(t, store, "DELETE", "/api/recipes?id="+recipeTestID, "")
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET, POST, PUT" {
		t.Fatal(rec.Code)
	}
	req := httptest.NewRequest("POST", "/api/recipes", bytes.NewBufferString(recipeBody(t, syntheticRecipeContent())))
	req = req.WithContext(context.WithValue(req.Context(), bearerContextKey{}, UserIdentity{Issuer: testIssuer, Subject: "a"}))
	out := httptest.NewRecorder()
	RecipesHandler(store).ServeHTTP(out, req)
	if out.Code != 415 {
		t.Fatal(out.Code)
	}
}
func TestRecipeContentBounds(t *testing.T) {
	for _, mutate := range []func(*RecipeContent){func(c *RecipeContent) { c.Title = strings.Repeat("x", 121) }, func(c *RecipeContent) { c.TotalMinutes = 1 }, func(c *RecipeContent) { c.DefaultServings = 0 }, func(c *RecipeContent) { c.Attention = "unknown" }, func(c *RecipeContent) { c.Ingredients = append(c.Ingredients, c.Ingredients[0]) }, func(c *RecipeContent) { c.Ingredients[0].Aisle = "unknown" }, func(c *RecipeContent) { c.CookTonight = nil }, func(c *RecipeContent) { n := 0; c.CookTonight[0].TimerSeconds = &n }} {
		c := syntheticRecipeContent()
		mutate(&c)
		if ValidateRecipeContent(c) == nil {
			t.Fatal("invalid recipe accepted")
		}
	}
}

func TestRecipeAPIRejectsAmbiguousJSONBeforeStorage(t *testing.T) {
	content := syntheticRecipeContent()
	content.PrepAhead = []RecipePrepAhead{{ID: "p1", Text: "Prep", Minutes: 1, StorageNote: "Refrigerate"}}
	content.PrepTonight = []RecipePrepTonight{{ID: "p2", Text: "Prep now", Minutes: 1}}
	valid := recipeBody(t, content)
	cases := map[string]string{
		"wrapper case alias":           strings.Replace(valid, `"content":`, `"Content":`, 1),
		"duplicate wrapper":            strings.Replace(valid, `{"content":`, `{"content":{},"content":`, 1),
		"content case alias":           strings.Replace(valid, `"title":`, `"Title":"different","title":`, 1),
		"case alias replaces required": strings.Replace(valid, `"title":`, `"Title":`, 1),
		"duplicate title":              strings.Replace(valid, `"title":`, `"title":"different","title":`, 1),
		"escaped duplicate title":      strings.Replace(valid, `"title":`, `"\u0074itle":"different","title":`, 1),
		"duplicate ingredient":         strings.Replace(valid, `"name":`, `"name":"different","name":`, 1),
		"ingredient case alias":        strings.Replace(valid, `"isPantry":`, `"IsPantry":true,"isPantry":`, 1),
		"prep-ahead case alias":        strings.Replace(valid, `"storageNote":`, `"StorageNote":"different","storageNote":`, 1),
		"prep-tonight duplicate":       strings.Replace(valid, `"text":"Prep now"`, `"text":"different","text":"Prep now"`, 1),
		"cook duplicate":               strings.Replace(valid, `"timerSeconds":`, `"timerSeconds":10,"timerSeconds":`, 1),
		"create null revision":         valid[:len(valid)-1] + `,"expectedRevision":null}`,
		"create revision":              valid[:len(valid)-1] + `,"expectedRevision":1}`,
		"invalid UTF-8":                strings.Replace(valid, "Synthetic test dish", "Synthetic\xffdish", 1),
		"two documents":                valid + `{}`,
		"trailing garbage":             valid + `!`,
		"null":                         `null`,
		"array":                        `[]`,
		"missing content":              `{}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			store := &recipeFake{}
			rec := recipeRequest(t, store, "POST", "/api/recipes", body)
			if rec.Code != 400 || store.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", rec.Code, store.calls, rec.Body)
			}
		})
	}
	for _, revisionFields := range []string{`"expectedRevision":null`, `"expectedRevision":1,"expectedRevision":2`, `"expectedRevision":1,"ExpectedRevision":2`, `"expectedRevision":"1"`, `"expectedRevision":true`} {
		store := &recipeFake{}
		body := valid[:len(valid)-1] + `,` + revisionFields + `}`
		rec := recipeRequest(t, store, "PUT", "/api/recipes?id="+recipeTestID, body)
		if rec.Code != 400 || store.calls != 0 {
			t.Fatalf("%s status=%d calls=%d", revisionFields, rec.Code, store.calls)
		}
	}
}

func TestRecipeAPIRejectsMalformedQueriesBeforeStorage(t *testing.T) {
	valid := recipeBody(t, syntheticRecipeContent())
	for _, method := range []string{"GET", "POST", "PUT"} {
		for _, query := range []string{"unexpected=%zz", "id=%zz", "id=", "after=", "limit=", "id=" + recipeTestID + "&unexpected=%zz", "id=" + recipeTestID + ";owner=other", "%zz=ignored", "id=" + recipeTestID + "&id="} {
			store := &recipeFake{}
			rec := recipeRequest(t, store, method, "/api/recipes?"+query, valid)
			if rec.Code != 400 || store.calls != 0 {
				t.Fatalf("%s ?%s status=%d calls=%d", method, query, rec.Code, store.calls)
			}
		}
	}
}

func TestRecipeAPIWholeBodySizeLimit(t *testing.T) {
	valid := recipeBody(t, syntheticRecipeContent())
	for _, chunked := range []bool{false, true} {
		for _, suffix := range []string{" ", "x", "{}"} {
			store := &recipeFake{}
			body := valid + strings.Repeat(suffix, (maxRecipeRequestBytes-len(valid))/len(suffix)+1)
			req := httptest.NewRequest("POST", "/api/recipes", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", recipeTestKey)
			req = req.WithContext(context.WithValue(req.Context(), bearerContextKey{}, UserIdentity{Issuer: testIssuer, Subject: "account-a"}))
			if chunked {
				req.ContentLength = -1
			}
			rec := httptest.NewRecorder()
			RecipesHandler(store).ServeHTTP(rec, req)
			if rec.Code != 413 || store.calls != 0 {
				t.Fatalf("chunked=%v suffix=%q status=%d calls=%d", chunked, suffix, rec.Code, store.calls)
			}
		}
	}
	for _, size := range []int{maxRecipeRequestBytes - 1, maxRecipeRequestBytes} {
		store := &recipeFake{}
		rec := recipeRequest(t, store, "POST", "/api/recipes", valid+strings.Repeat(" ", size-len(valid)))
		if rec.Code != 201 || store.calls != 1 {
			t.Fatalf("valid boundary size=%d status=%d calls=%d", size, rec.Code, store.calls)
		}
	}
}
