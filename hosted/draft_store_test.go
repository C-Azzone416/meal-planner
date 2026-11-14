package hosted

import (
	"crypto/sha256"
	"database/sql"
	"testing"
	"time"
)

func TestDraftGrantPolicyFailClosed(t *testing.T) {
	now := time.Now().UTC()
	a := DraftAccess{TokenHash: sha256.Sum256([]byte("synthetic")), Environment: "preview", Issuer: "https://synthetic.invalid", AllowedEmails: []string{"owner@example.invalid"}}
	g := draftGrant{Owner: RecipeOwner{a.Issuer, "subject"}, Email: a.AllowedEmails[0], Scope: "draft:create", Environment: "preview", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if !validDraftAccess(a) || !draftGrantAllowed(g, a, now) {
		t.Fatal("valid synthetic grant rejected")
	}
	for _, mutate := range []func(*draftGrant){func(g *draftGrant) { g.Owner.Issuer += "/other" }, func(g *draftGrant) { g.Owner.Subject = "" }, func(g *draftGrant) { g.Environment = "production" }, func(g *draftGrant) { g.Scope = "recipes:approve" }, func(g *draftGrant) { g.Email = "other@example.invalid" }, func(g *draftGrant) { g.CreatedAt = now.Add(time.Hour) }, func(g *draftGrant) { g.ExpiresAt = now.Add(8 * 24 * time.Hour) }, func(g *draftGrant) { g.ExpiresAt = now }, func(g *draftGrant) { g.RevokedAt = sql.NullTime{Time: now, Valid: true} }} {
		bad := g
		mutate(&bad)
		if draftGrantAllowed(bad, a, now) {
			t.Fatal("bad grant accepted")
		}
	}
	a.TokenHash = [32]byte{}
	if validDraftAccess(a) {
		t.Fatal("zero token hash accepted")
	}
}
func TestDraftQuotaCountsReceiptsAndKeys(t *testing.T) {
	a := DraftAccess{Environment: "preview"}
	o := RecipeOwner{"issuer", "subject"}
	initial := draftRetainedCharge(a, o, "key", []byte("request"), []byte("response"))
	if initial != int64(len("previewissuersubjectkeyrequestresponse"))+64+4096 {
		t.Fatal("incomplete retained-byte accounting")
	}
	if draftRetainedCharge(a, o, "key2", []byte("request"), []byte("response-with-receipt")) <= initial {
		t.Fatal("receipt/key growth uncharged")
	}
}
