package hosted

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestRecipeDatabaseURL(t *testing.T) {
	good := "postgres://runtime:synthetic@database.example.test/recipes?sslmode=verify-full"
	got, err := recipeDatabaseURL(good)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got)
	for k, v := range map[string]string{"sslmode": "verify-full", "connect_timeout": "5", "statement_timeout": "10000", "lock_timeout": "5000"} {
		if u.Query().Get(k) != v {
			t.Errorf("%s not bounded", k)
		}
	}
	for _, bad := range []string{"", "host=localhost user=test", "https://user@host/db?sslmode=verify-full", "postgres://host/db?sslmode=verify-full", "postgres://user@host/?sslmode=verify-full", strings.Replace(good, "verify-full", "require", 1), strings.Replace(good, "verify-full", "disable", 1), good + "&sslmode=disable", good + "&host=localhost", good + "&channel_binding=require", good + "&password=other", good + "#fragment", good + "&connect_timeout=0", good + "&sslrootcert=%zz"} {
		t.Run(bad, func(t *testing.T) {
			if _, e := recipeDatabaseURL(bad); e == nil {
				t.Fatal("accepted unsafe connection settings")
			}
		})
	}
	// Construction is lazy: a transient network outage cannot poison a cached store.
	store, err := OpenRecipeStore(context.Background(), good)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.db.Stats().MaxOpenConnections != 4 {
		t.Fatal("unbounded pool")
	}
}
func TestRecipeUUIDAndPayload(t *testing.T) {
	a, e := newRecipeUUID()
	if e != nil {
		t.Fatal(e)
	}
	b, e := newRecipeUUID()
	if e != nil {
		t.Fatal(e)
	}
	if a == b || !validRecipeUUID(a) || a[14] != '4' {
		t.Fatal("invalid UUID")
	}
	for _, bad := range []string{"", "not-an-id", strings.Replace(a, "-", "", 1), strings.Replace(a, a[:1], "g", 1)} {
		if validRecipeUUID(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
	_, h, e := recipePayload(RecipeContent{Title: "Synthetic"})
	if e != nil {
		t.Fatal(e)
	}
	_, h2, _ := recipePayload(RecipeContent{Title: "Synthetic"})
	_, h3, _ := recipePayload(RecipeContent{Title: "Changed"})
	if string(h) != string(h2) || string(h) == string(h3) {
		t.Fatal("unstable payload hashing")
	}
}
