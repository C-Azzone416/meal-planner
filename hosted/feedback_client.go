package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"
)

const feedbackEndpoint = "https://framebriar-masra91-poc.azurewebsites.net/api"

var feedbackUnavailable = errors.New("UNAVAILABLE")

type feedbackFailure struct {
	Code   string
	Status int
}

func (e feedbackFailure) Error() string           { return e.Code }
func feedbackError(code string, status int) error { return feedbackFailure{code, status} }

type feedbackGrant struct {
	V              int       `json:"v"`
	GameID         string    `json:"gameId"`
	Scope          int       `json:"scopeVersion"`
	Token          string    `json:"token"`
	Refresh        string    `json:"refreshToken"`
	Expires        time.Time `json:"expiresAt"`
	RefreshExpires time.Time `json:"refreshExpiresAt"`
}
type feedbackReceipt struct {
	ID      string    `json:"id"`
	GameID  string    `json:"gameId"`
	Status  string    `json:"status"`
	Ticket  string    `json:"ticket"`
	Expires time.Time `json:"expiresAt"`
}
type feedbackUpstream interface {
	Grant(context.Context, string, map[string]any) (feedbackGrant, error)
	Disconnect(context.Context, string) error
	Report(context.Context, string, string, []byte, []byte) error
}
type feedbackClient struct {
	app    string
	client *http.Client
}

func (c feedbackClient) call(ctx context.Context, method, path, typ, token, ticket string, body []byte, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, feedbackEndpoint+path, bytes.NewReader(body))
	if err != nil {
		return feedbackUnavailable
	}
	req.Header.Set("Content-Type", typ)
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ticket != "" {
		req.Header.Set("X-Upload-Ticket", ticket)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return feedbackError("UNCERTAIN", 503)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		switch resp.StatusCode {
		case 401:
			return feedbackError("REVOKED", 401)
		case 403:
			return feedbackError("DISABLED", 403)
		case 429:
			return feedbackError("RATE_LIMITED", 429)
		case 400:
			return feedbackError("INVALID_REQUEST", 400)
		}
		return feedbackError("UNCERTAIN", 503)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if err != nil || len(data) > 16384 || json.Unmarshal(data, out) != nil {
		return feedbackError("UNCERTAIN", 503)
	}
	return nil
}
func (c feedbackClient) Grant(ctx context.Context, action string, p map[string]any) (feedbackGrant, error) {
	p["v"] = 1
	p["gameId"] = c.app
	p["scopeVersion"] = 2
	b, _ := json.Marshal(p)
	var g feedbackGrant
	err := c.call(ctx, "POST", "/v1/access/"+action, "application/json", "", "", b, &g)
	if err != nil {
		return g, err
	}
	now := time.Now()
	if g.V != 1 || g.GameID != c.app || g.Scope != 2 || len(g.Token) < 16 || len(g.Token) > 4096 || len(g.Refresh) < 16 || len(g.Refresh) > 4096 || !g.Expires.After(now) || g.Expires.After(now.Add(2*time.Hour)) || g.Expires.After(g.RefreshExpires) || !g.RefreshExpires.After(now) {
		return feedbackGrant{}, feedbackError("UNCERTAIN", 503)
	}
	if old, ok := p["refreshToken"]; ok && old != g.Refresh {
		return feedbackGrant{}, feedbackError("UNCERTAIN", 503)
	}
	return g, nil
}
func (c feedbackClient) Disconnect(ctx context.Context, refresh string) error {
	b, _ := json.Marshal(map[string]any{"v": 1, "gameId": c.app, "refreshToken": refresh})
	var out struct {
		V int `json:"v"`
	}
	err := c.call(ctx, "POST", "/v1/access/disconnect", "application/json", "", "", b, &out)
	if err == nil && out.V != 1 {
		return feedbackError("UNCERTAIN", 503)
	}
	return err
}
func (c feedbackClient) Report(ctx context.Context, token, id string, report, media []byte) error {
	var receipt feedbackReceipt
	err := c.call(ctx, "POST", "/v1/reports", "application/json", token, "", report, &receipt)
	if err != nil {
		return err
	}
	ready := func() bool { return receipt.ID == id && receipt.GameID == c.app && receipt.Status == "ready" }
	if ready() {
		return nil
	}
	now := time.Now()
	if len(media) == 0 || receipt.ID != id || receipt.Status != "pending" || (receipt.GameID != "" && receipt.GameID != c.app) || !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(receipt.Ticket) || !receipt.Expires.After(now) || receipt.Expires.After(now.Add(10*time.Minute)) {
		return feedbackError("UNCERTAIN", 503)
	}
	ticket := receipt.Ticket
	receipt = feedbackReceipt{}
	err = c.call(ctx, "PUT", "/v1/reports/"+id+"/media", "image/png", token, ticket, media, &receipt)
	if err != nil {
		return err
	}
	if !ready() {
		return feedbackError("UNCERTAIN", 503)
	}
	return nil
}
