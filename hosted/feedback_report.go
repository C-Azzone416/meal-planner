package hosted

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"time"
	"unicode/utf8"
)

const feedbackMaxMedia = 3 * 1024 * 1024
const feedbackMaxJSON = 16 * 1024
const feedbackMaxBody = feedbackMaxMedia + feedbackMaxJSON + 4096

var feedbackUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

type feedbackReport struct {
	V           int               `json:"v"`
	ID          string            `json:"id"`
	GameID      string            `json:"gameId"`
	BuildSHA    string            `json:"buildSha"`
	Scene       string            `json:"scene"`
	SessionID   string            `json:"sessionId"`
	Seed        json.RawMessage   `json:"seed"`
	Platform    string            `json:"platform"`
	Input       string            `json:"input"`
	CapturedAt  string            `json:"capturedAt"`
	Notes       string            `json:"notes"`
	Breadcrumbs []json.RawMessage `json:"breadcrumbs"`
	Media       *struct {
		Type  string `json:"type"`
		Bytes int    `json:"bytes"`
		SHA   string `json:"sha256"`
	} `json:"media"`
	AppVersion string `json:"appVersion,omitempty"`
}

func (s feedbackService) parseReport(w http.ResponseWriter, r *http.Request) (report feedbackReport, raw, media []byte, err error) {
	invalid := feedbackError("INVALID_REQUEST", 400)
	large := feedbackError("TOO_LARGE", 413)
	typ, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || typ != "multipart/form-data" || len(params) != 1 || len(params["boundary"]) == 0 || len(params["boundary"]) > 200 {
		return report, nil, nil, feedbackError("UNSUPPORTED_MEDIA_TYPE", 415)
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, feedbackMaxBody))
	if e != nil {
		return report, nil, nil, large
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	seen := map[string]bool{}
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return report, nil, nil, invalid
		}
		name := part.FormName()
		if seen[name] || (name != "report" && name != "media") {
			return report, nil, nil, invalid
		}
		seen[name] = true
		want, limit := "application/json", feedbackMaxJSON
		if name == "media" {
			want = "image/png"
			limit = feedbackMaxMedia
		}
		if part.Header.Get("Content-Type") != want {
			return report, nil, nil, feedbackError("UNSUPPORTED_MEDIA_TYPE", 415)
		}
		b, e := io.ReadAll(io.LimitReader(part, int64(limit+1)))
		if e != nil {
			return report, nil, nil, invalid
		}
		if len(b) > limit {
			return report, nil, nil, large
		}
		if name == "report" {
			raw = b
		} else {
			media = b
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&report) != nil || d.Decode(&struct{}{}) != io.EOF {
		return report, nil, nil, invalid
	}
	allowed := func(v string, list ...string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	if report.V != 1 || report.GameID != s.config.App || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(report.BuildSHA) || !feedbackUUID.MatchString(report.ID) || !feedbackUUID.MatchString(report.SessionID) || !allowed(report.Scene, "week", "recipes", "prep", "shopping", "drafts", "cook", "house-rules", "other") || !allowed(report.Platform, "mobile", "tablet", "desktop") || !allowed(report.Input, "mouse", "keyboard", "touch", "mixed", "unknown") || string(report.Seed) != "null" || report.Breadcrumbs == nil || len(report.Breadcrumbs) != 0 || !utf8.ValidString(report.Notes) || utf8.RuneCountInString(report.Notes) > 2000 {
		return report, nil, nil, invalid
	}
	if report.AppVersion != "" && !regexp.MustCompile(`^[A-Za-z0-9._+-]{1,40}$`).MatchString(report.AppVersion) {
		return report, nil, nil, invalid
	}
	if _, e := time.Parse(time.RFC3339Nano, report.CapturedAt); e != nil || len(report.CapturedAt) > 30 {
		return report, nil, nil, invalid
	}
	if !seen["media"] {
		if report.Media != nil || len(bytes.TrimSpace([]byte(report.Notes))) == 0 {
			return report, nil, nil, invalid
		}
	} else {
		if !s.config.Screenshots {
			return report, nil, nil, feedbackError("SCREENSHOTS_DISABLED", 403)
		}
		hash := sha256.Sum256(media)
		if report.Media == nil || report.Media.Type != "image/png" || report.Media.Bytes != len(media) || report.Media.SHA != hex.EncodeToString(hash[:]) {
			return report, nil, nil, invalid
		}
		cfg, e := png.DecodeConfig(bytes.NewReader(media))
		if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || cfg.Width*cfg.Height > 12000000 {
			return report, nil, nil, invalid
		}
	}
	// Re-encode the validated narrow contract so no extra/duplicate fields travel upstream.
	raw, e = json.Marshal(report)
	if e != nil || len(raw) > feedbackMaxJSON {
		return report, nil, nil, large
	}
	return report, raw, media, nil
}
