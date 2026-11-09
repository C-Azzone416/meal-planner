package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// RecipeOwner is derived only from the verified token. The issuer namespace
// prevents equal subjects in different auth environments from sharing data.
type RecipeOwner struct{ Issuer, Subject string }

type RecipeContent struct {
	Title           string              `json:"title"`
	ShortName       string              `json:"shortName,omitempty"`
	Icon            string              `json:"icon"`
	ActiveMinutes   int                 `json:"activeMinutes"`
	TotalMinutes    int                 `json:"totalMinutes"`
	Attention       string              `json:"attention"`
	Tags            []string            `json:"tags"`
	DefaultServings int                 `json:"defaultServings"`
	Ingredients     []RecipeIngredient  `json:"ingredients"`
	PrepAhead       []RecipePrepAhead   `json:"prepAhead"`
	PrepTonight     []RecipePrepTonight `json:"prepTonight"`
	CookTonight     []RecipeCookStep    `json:"cookTonight"`
}
type RecipeIngredient struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Quantity *float64 `json:"quantity"`
	Unit     string   `json:"unit"`
	Aisle    string   `json:"aisle"`
	IsPantry bool     `json:"isPantry"`
}
type RecipePrepAhead struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	Label       string `json:"label,omitempty"`
	Minutes     int    `json:"minutes"`
	KeepsDays   *int   `json:"keepsDays"`
	StorageNote string `json:"storageNote"`
}
type RecipePrepTonight struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Minutes int    `json:"minutes"`
}
type RecipeCookStep struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	TimerSeconds *int   `json:"timerSeconds"`
}

type RecipeEnvelope struct {
	ID        string        `json:"id"`
	Revision  int64         `json:"revision"`
	Status    string        `json:"status"`
	Origin    string        `json:"origin"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Content   RecipeContent `json:"content"`
}

var (
	ErrRecipeNotFound            = errors.New("recipe not found")
	ErrRecipeConflict            = errors.New("recipe revision conflict")
	ErrRecipeIdempotencyConflict = errors.New("recipe idempotency conflict")
)

type RecipeRepository interface {
	Create(context.Context, RecipeOwner, string, RecipeContent) (RecipeEnvelope, bool, error)
	List(context.Context, RecipeOwner, string, int) ([]RecipeEnvelope, string, error)
	Get(context.Context, RecipeOwner, string) (RecipeEnvelope, error)
	Update(context.Context, RecipeOwner, string, int64, RecipeContent) (RecipeEnvelope, error)
}

const maxRecipeRequestBytes = 256 * 1024

var recipeUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// RecipesHandler must be wrapped in RequireUser. It also fails closed if called
// without the verified context, so accidental wiring cannot expose recipes.
func RecipesHandler(repository RecipeRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		identity, ok := UserFromContext(r.Context())
		if !ok || identity.Issuer == "" || identity.Subject == "" {
			recipeError(w, 401, "unauthorized", "Sign in to access recipes.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodPut {
			w.Header().Set("Allow", "GET, POST, PUT")
			recipeError(w, 405, "method_not_allowed", "Method not allowed.")
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			recipeError(w, 400, "invalid_request", "Invalid recipe query.")
			return
		}
		for key, values := range query {
			if len(values) != 1 || values[0] == "" || (key != "id" && key != "limit" && key != "after") {
				recipeError(w, 400, "invalid_request", "Invalid recipe query.")
				return
			}
		}
		id := query.Get("id")
		if id != "" && !recipeUUID.MatchString(id) {
			recipeError(w, 400, "invalid_request", "Invalid recipe ID.")
			return
		}
		if (r.Method == http.MethodPost && len(query) != 0) || (r.Method == http.MethodPut && (id == "" || len(query) != 1)) || (id != "" && len(query) != 1) {
			recipeError(w, 400, "invalid_request", "Invalid recipe query.")
			return
		}
		if repository == nil {
			recipeError(w, 503, "unavailable", "Recipe storage is unavailable. Your recipe has not been confirmed saved.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		owner := RecipeOwner{Issuer: identity.Issuer, Subject: identity.Subject}
		switch r.Method {
		case http.MethodGet:
			if id != "" {
				recipe, err := repository.Get(ctx, owner, id)
				if err != nil {
					recipeRepositoryError(w, err)
					return
				}
				recipeJSON(w, 200, recipe)
				return
			}
			after := query.Get("after")
			if after != "" && !recipeUUID.MatchString(after) {
				recipeError(w, 400, "invalid_request", "Invalid cursor.")
				return
			}
			limit := 100
			if raw, ok := query["limit"]; ok {
				value, err := strconv.Atoi(raw[0])
				if err != nil || value < 1 || value > 100 {
					recipeError(w, 400, "invalid_request", "Limit must be from 1 to 100.")
					return
				}
				limit = value
			}
			recipes, next, err := repository.List(ctx, owner, after, limit)
			if err != nil {
				recipeRepositoryError(w, err)
				return
			}
			body, err := recipePageJSON(recipes, next, maxRecipeListResponseBytes)
			if err != nil {
				recipeError(w, 503, "unavailable", "Recipe storage is unavailable.")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write(body)
		default:
			media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "application/json" {
				recipeError(w, 415, "invalid_request", "Use application/json.")
				return
			}
			// Read the entire bounded body before decoding. This also detects an
			// oversized whitespace suffix or second document as 413, not 400.
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRecipeRequestBytes))
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					recipeError(w, 413, "invalid_request", "Recipe is too large.")
				} else {
					recipeError(w, 400, "invalid_request", "Invalid recipe request.")
				}
				return
			}
			fields, err := recipeObjectFields(raw, map[string]bool{"content": true, "expectedRevision": true})
			if err != nil {
				recipeError(w, 400, "invalid_request", "Invalid recipe request.")
				return
			}
			revisionJSON, hasRevision := fields["expectedRevision"]

			content, err := decodeRecipeContent(fields["content"])
			if err != nil {
				recipeError(w, 400, "invalid_request", "Recipe fields are missing or invalid.")
				return
			}
			if r.Method == http.MethodPost {
				// Requiring a UUID prevents unbounded keys; the key is never a recipe ID.
				key := r.Header.Get("Idempotency-Key")
				if !recipeUUID.MatchString(key) || hasRevision {
					recipeError(w, 400, "invalid_request", "A valid Idempotency-Key is required for a new recipe.")
					return
				}
				recipe, replayed, err := repository.Create(ctx, owner, key, content)
				if err != nil {
					recipeRepositoryError(w, err)
					return
				}
				status := http.StatusCreated
				if replayed {
					status = http.StatusOK
				}
				recipeJSON(w, status, recipe)
			} else {
				var revision int64
				if !hasRevision || json.Unmarshal(revisionJSON, &revision) != nil || revision < 1 || revision > 9007199254740991 {
					recipeError(w, 400, "invalid_request", "A valid expectedRevision is required.")
					return
				}
				recipe, err := repository.Update(ctx, owner, id, revision, content)
				if err != nil {
					recipeRepositoryError(w, err)
					return
				}
				recipeJSON(w, 200, recipe)
			}
		}
	})
}
func recipeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// maxRecipeListResponseBytes bounds one list response, measured on the exact bytes written (recipes, commas,
// envelope, nextCursor and the encoder's trailing newline). Vercel Functions fail responses over 4.5 MB
// (FUNCTION_PAYLOAD_TOO_LARGE), and the 256 KiB request cap does NOT bound the encoded size: encoding/json
// writes <, > and & as 6-byte escapes, so one valid recipe can encode to about 6x its request size (~1.5 MB).
// 3.5 MB leaves about 1 MB of headroom, and a single worst-case recipe fits many times over (see tests).
const maxRecipeListResponseBytes = 3_500_000

// recipeEncode is the one encoder used for every recipe response, so sizes are measured with the settings
// that are actually written (json.Encoder defaults: HTML escaping on, trailing newline).
func recipeEncode(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// recipePageJSON builds the list response: whole recipes only, in the order given, as many as fit in budget
// bytes, but always at least one so a client can make progress. When it has to stop early (or the repository
// reports more rows), nextCursor is the ID of the last INCLUDED recipe; the store's cursor means "after this
// ID" in ID order, so the next request continues with no loss and no duplicates. A page may therefore hold
// fewer than `limit` items while nextCursor is non-null.
func recipePageJSON(recipes []RecipeEnvelope, next string, budget int) ([]byte, error) {
	const head = `{"recipes":[`
	tail := func(cursor string) (string, error) {
		if cursor == "" {
			return "],\"nextCursor\":null}\n", nil
		}
		encoded, err := recipeEncode(cursor)
		if err != nil {
			return "", err
		}
		return "],\"nextCursor\":" + string(encoded[:len(encoded)-1]) + "}\n", nil
	}
	items := make([][]byte, 0, len(recipes))
	used := len(head)
	finalTail := ""
	for i, recipe := range recipes {
		encoded, err := recipeEncode(recipe)
		if err != nil {
			return nil, err
		}
		encoded = encoded[:len(encoded)-1] // the encoder's newline is not part of the array element
		cursor := next
		if i < len(recipes)-1 {
			cursor = recipe.ID // stopping here would continue right after this recipe
		}
		t, err := tail(cursor)
		if err != nil {
			return nil, err
		}
		size := used + len(encoded) + len(t)
		if i > 0 {
			size++ // comma
		}
		if i > 0 && size > budget {
			// Recipe i does not fit: stop after the previous one and continue from it.
			prev := recipes[i-1].ID
			if finalTail, err = tail(prev); err != nil {
				return nil, err
			}
			break
		}
		used += len(encoded)
		if i > 0 {
			used++
		}
		items = append(items, encoded)
		finalTail = t
	}
	if len(items) == 0 {
		finalTail, _ = tail(next)
	}
	out := make([]byte, 0, used+len(finalTail))
	out = append(out, head...)
	for i, item := range items {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, item...)
	}
	out = append(out, finalTail...)
	return out, nil
}
func recipeError(w http.ResponseWriter, status int, code, message string) {
	recipeJSON(w, status, struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{code, message}})
}
func recipeRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrRecipeNotFound):
		recipeError(w, 404, "not_found", "Recipe not found.")
	case errors.Is(err, ErrRecipeConflict):
		recipeError(w, 409, "conflict", "This recipe changed elsewhere. Reload before saving again.")
	case errors.Is(err, ErrRecipeIdempotencyConflict):
		recipeError(w, 409, "idempotency_conflict", "This save key was already used for different content.")
	default:
		recipeError(w, 503, "unavailable", "Recipe storage is unavailable. Your recipe has not been confirmed saved.")
	}
}
func decodeRecipeContent(raw json.RawMessage) (RecipeContent, error) {
	var content RecipeContent
	if err := requiredRecipeFields(raw, reflect.TypeFor[RecipeContent]()); err != nil {
		return content, err
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		return content, err
	}
	if err := ValidateRecipeContent(content); err != nil {
		return content, err
	}
	return content, nil
}

// recipeObjectFields rejects duplicate and unknown keys before Go's permissive
// struct decoder can fold key case or replace earlier values. JSON-escaped keys
// are compared after decoding, so escaped duplicates are rejected too.
func recipeObjectFields(raw json.RawMessage, allowed map[string]bool) (map[string]json.RawMessage, error) {
	bad := errors.New("invalid recipe object")
	if !utf8.Valid(raw) {
		return nil, bad
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, bad
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, bad
		}
		key, ok := token.(string)
		if !ok || !allowed[key] {
			return nil, bad
		}
		if _, exists := fields[key]; exists {
			return nil, bad
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, bad
		}
		fields[key] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, bad
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, bad
	}
	return fields, nil
}

// encoding/json accepts null for scalar fields and missing fields as zero. The
// API requires the complete content object and only permits documented nulls.
func requiredRecipeFields(raw json.RawMessage, t reflect.Type) error {
	bad := errors.New("missing or null recipe field")
	if t.Kind() == reflect.Pointer {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || len(raw) == 0 {
		return bad
	}
	if t.Kind() == reflect.Struct {
		allowed := make(map[string]bool, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			allowed[strings.Split(t.Field(i).Tag.Get("json"), ",")[0]] = true
		}
		fields, err := recipeObjectFields(raw, allowed)
		if err != nil {
			return err
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")
			value, ok := fields[tag[0]]
			optional := len(tag) > 1 && tag[1] == "omitempty"
			if !ok && optional {
				continue
			}
			if !ok {
				return bad
			}
			if err := requiredRecipeFields(value, f.Type); err != nil {
				return err
			}
		}
	} else if t.Kind() == reflect.Slice {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return bad
		}
		for _, item := range items {
			if err := requiredRecipeFields(item, t.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
func ValidateRecipeContent(c RecipeContent) error {
	bad := errors.New("invalid recipe content")
	text := func(s string, max int, required bool) bool {
		return utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && (!required || strings.TrimSpace(s) != "") && !strings.ContainsRune(s, 0)
	}
	bounded := func(n, min, max int) bool { return n >= min && n <= max }
	if !text(c.Title, 120, true) || !text(c.ShortName, 120, false) || !text(c.Icon, 16, true) || !bounded(c.ActiveMinutes, 0, 600) || !bounded(c.TotalMinutes, c.ActiveMinutes, 10080) || !bounded(c.DefaultServings, 1, 12) || (c.Attention != "low" && c.Attention != "medium" && c.Attention != "high") {
		return bad
	}
	if len(c.Tags) > 32 || len(c.Ingredients) < 1 || len(c.Ingredients) > 200 || len(c.CookTonight) < 1 || len(c.CookTonight) > 200 || len(c.PrepAhead) > 200 || len(c.PrepTonight) > 200 {
		return bad
	}
	for _, tag := range c.Tags {
		if !text(tag, 80, true) {
			return bad
		}
	}
	ids := map[string]bool{}
	unique := func(id string) bool {
		if !text(id, 160, true) || ids[id] {
			return false
		}
		ids[id] = true
		return true
	}
	aisles := map[string]bool{"Produce": true, "Meat & fish": true, "Dairy": true, "Bakery": true, "Pantry aisle": true, "Frozen": true, "Other": true}
	for _, i := range c.Ingredients {
		if !unique(i.ID) || !text(i.Name, 500, true) || !text(i.Unit, 80, false) || !aisles[i.Aisle] || (i.Quantity != nil && (math.IsNaN(*i.Quantity) || math.IsInf(*i.Quantity, 0) || *i.Quantity <= 0 || *i.Quantity > 1000000)) {
			return bad
		}
	}
	// IDs need only be unique within each section; prefixes in the UI keep them separate.
	ids = map[string]bool{}
	for _, s := range c.PrepAhead {
		if !unique(s.ID) || !text(s.Text, 4000, true) || !text(s.Label, 500, false) || !text(s.StorageNote, 2000, false) || !bounded(s.Minutes, 0, 10080) || (s.KeepsDays != nil && !bounded(*s.KeepsDays, 1, 3650)) {
			return bad
		}
	}
	ids = map[string]bool{}
	for _, s := range c.PrepTonight {
		if !unique(s.ID) || !text(s.Text, 4000, true) || !bounded(s.Minutes, 0, 10080) {
			return bad
		}
	}
	ids = map[string]bool{}
	for _, s := range c.CookTonight {
		if !unique(s.ID) || !text(s.Text, 4000, true) || (s.TimerSeconds != nil && !bounded(*s.TimerSeconds, 1, 604800)) {
			return bad
		}
	}
	return nil
}
