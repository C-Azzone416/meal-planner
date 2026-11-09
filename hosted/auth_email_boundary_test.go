package hosted

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"
	"unicode/utf16"
)

type emailBoundaryCase struct {
	name      string
	allowed   string
	emailJSON string
	identity  string
	accepted  bool
	malformed bool
}

// Keep the raw UTF-8 and JSON-escaped spellings distinct, including U+2028 and
// U+2029, which encoding/json normally escapes even in otherwise raw strings.
func emailBoundaryJSON(value string, escaped bool) string {
	var result strings.Builder
	result.WriteByte('"')
	for _, r := range value {
		if escaped {
			if r > 0xffff {
				high, low := utf16.EncodeRune(r)
				fmt.Fprintf(&result, `\u%04x\u%04x`, high, low)
			} else {
				fmt.Fprintf(&result, `\u%04x`, r)
			}
		} else if r < 0x80 {
			encoded, _ := json.Marshal(string(r))
			result.Write(encoded[1 : len(encoded)-1])
		} else {
			result.WriteRune(r)
		}
	}
	result.WriteByte('"')
	return result.String()
}

func TestAuthEmailBoundaryAcrossEntryPoints(t *testing.T) {
	var cases []emailBoundaryCase
	add := func(name, allowed, email string, accepted bool) {
		for _, escaped := range []bool{false, true} {
			encoding := "raw"
			if escaped {
				encoding = "escaped"
			}
			cases = append(cases, emailBoundaryCase{
				name: name + "/" + encoding, allowed: allowed,
				emailJSON: emailBoundaryJSON(email, escaped),
				identity:  strings.Trim(email, " \t\r\n"), accepted: accepted,
			})
		}
	}
	add("exact", testAllowedEmail, testAllowedEmail, true)
	add("ascii_case", testAllowedEmail, strings.ToUpper(testAllowedEmail), true)
	add("mixed_ascii_padding", testAllowedEmail, " \t\r\nALLOWED@Example.Invalid\n\r\t ", true)
	add("different_address", testAllowedEmail, testOtherEmail, false)

	addPadding := func(name string, r rune, accepted bool) {
		padding := string(r)
		add(name+"/prefix", testAllowedEmail, padding+testAllowedEmail, accepted)
		add(name+"/suffix", testAllowedEmail, testAllowedEmail+padding, accepted)
		add(name+"/both", testAllowedEmail, padding+testAllowedEmail+padding, accepted)
		add(name+"/interior", testAllowedEmail, "allowed"+padding+"@example.invalid", false)
	}
	// Enumerate the complete Unicode White_Space property, not just the four
	// reported bypasses. Only ASCII SP, HT, CR and LF are supported padding.
	for _, span := range unicode.White_Space.R16 {
		for r := uint32(span.Lo); r <= uint32(span.Hi); r += uint32(span.Stride) {
			accepted := r == ' ' || r == '\t' || r == '\r' || r == '\n'
			addPadding(fmt.Sprintf("whitespace/U+%04X", r), rune(r), accepted)
		}
	}
	for _, span := range unicode.White_Space.R32 {
		for r := span.Lo; r <= span.Hi; r += span.Stride {
			addPadding(fmt.Sprintf("whitespace/U+%04X", r), rune(r), false)
		}
	}
	// Non-whitespace invisibles, bidi controls, replacement characters and
	// ASCII controls must not disappear during identity normalization either.
	for _, r := range []rune{
		0, 7, 8, 0x1f, 0x7f, 0x00ad, 0x034f, 0x061c, 0x180e,
		0x200b, 0x200c, 0x200d, 0x200e, 0x200f, 0x202a, 0x202e,
		0x2060, 0x2066, 0x2069, 0xfeff, 0xfffd, 0x1f37d,
	} {
		addPadding(fmt.Sprintf("invisible_or_control/U+%04X", r), r, false)
	}
	for _, tc := range []struct{ name, allowed, email string }{
		{"kelvin_simple_fold", "kelvin@example.invalid", "Kelvin@example.invalid"},
		{"long_s_simple_fold", "sam@example.invalid", "ſam@example.invalid"},
		{"cyrillic_a", testAllowedEmail, "аllowed@example.invalid"},
		{"roman_numeral_l", testAllowedEmail, "allowed@exampⅼe.invalid"},
		{"fullwidth_a", testAllowedEmail, "ａllowed@example.invalid"},
		{"fullwidth_at", testAllowedEmail, "allowed＠example.invalid"},
		{"composed_accent", "cafe@example.invalid", "café@example.invalid"},
		{"combining_accent", "cafe@example.invalid", "cafe\u0301@example.invalid"},
	} {
		add("homoglyph/"+tc.name, tc.allowed, tc.email, false)
	}
	// Construct malformed bytes directly. json.Marshal would replace invalid
	// UTF-8 with U+FFFD and therefore would not test the original signed bytes.
	for _, tc := range []struct{ name, bytes string }{
		{"invalid_byte", "\xff"}, {"isolated_continuation", "\x80"},
		{"truncated_utf8", "\xe2\x80"}, {"overlong_ascii_space", "\xc0\xa0"},
		{"utf8_surrogate", "\xed\xa0\x80"}, {"out_of_range_utf8", "\xf4\x90\x80\x80"},
		{"unpaired_high_surrogate", `\ud800`}, {"unpaired_low_surrogate", `\udfff`},
		{"unescaped_tab", "\t"}, {"unescaped_newline", "\n"},
	} {
		for _, prefix := range []bool{true, false} {
			position, value := "suffix", testAllowedEmail+tc.bytes
			if prefix {
				position, value = "prefix", tc.bytes+testAllowedEmail
			}
			cases = append(cases, emailBoundaryCase{
				name:    "malformed/" + tc.name + "/" + position,
				allowed: testAllowedEmail, emailJSON: `"` + value + `"`, malformed: true,
			})
		}
	}

	bearer, webhook := newBearerFixture(t), newWebhookFixture(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("bearer", func(t *testing.T) { checkEmailBoundaryBearer(t, bearer, tc) })
			t.Run("webhook", func(t *testing.T) { checkEmailBoundaryWebhook(t, webhook, tc) })
			for route := range authProxyEmailRoutes {
				t.Run("proxy/"+route, func(t *testing.T) { checkEmailBoundaryProxy(t, route, tc) })
			}
		})
	}
}

func checkEmailBoundaryBearer(t *testing.T, f *bearerFixture, tc emailBoundaryCase) {
	t.Helper()
	claims, err := json.Marshal(f.claims())
	if err != nil {
		t.Fatal(err)
	}
	claims = bytes.Replace(claims, []byte(`"`+testAllowedEmail+`"`), []byte(tc.emailJSON), 1)
	token := f.rawToken([]byte(`{"alg":"EdDSA","kid":"test-key"}`), claims)
	config := f.config()
	config.AllowedEmail = tc.allowed
	identity, err := VerifyBearer(t.Context(), "Bearer "+token, config)
	want := UserIdentity{Issuer: testIssuer, Subject: "user-test", Email: tc.identity}
	if tc.accepted {
		if err != nil || identity != want {
			t.Errorf("valid identity = %+v, error = %v; want %+v", identity, err, want)
		}
	} else if !errors.Is(err, ErrInvalidBearer) || identity != (UserIdentity{}) {
		t.Errorf("invalid email returned identity %+v, error = %v", identity, err)
	}

	called := false
	handler := RequireUserWithConfig(config, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		got, ok := UserFromContext(r.Context())
		if !ok || got != want {
			t.Errorf("context identity = %+v, present = %v; want %+v", got, ok, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	wantStatus := http.StatusUnauthorized
	if tc.accepted {
		wantStatus = http.StatusNoContent
	}
	if rec.Code != wantStatus || called != tc.accepted {
		t.Errorf("protected handler status = %d, called = %v; want %d, %v", rec.Code, called, wantStatus, tc.accepted)
	}
}

func checkEmailBoundaryWebhook(t *testing.T, f *webhookFixture, tc emailBoundaryCase) {
	t.Helper()
	body := bytes.Replace(f.payload(testAllowedEmail), []byte(`"`+testAllowedEmail+`"`), []byte(tc.emailJSON), 1)
	config := f.config()
	config.AllowedEmail = tc.allowed
	rec := httptest.NewRecorder()
	AuthBeforeCreate(config).ServeHTTP(rec, f.request(body))
	if tc.malformed {
		if rec.Code != http.StatusBadRequest {
			t.Errorf("malformed signed email status = %d, body = %s", rec.Code, rec.Body.String())
		}
		return
	}
	var response beforeCreateResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Allowed != tc.accepted {
		t.Fatalf("signed email status = %d, body = %s; want allowed = %v", rec.Code, rec.Body.String(), tc.accepted)
	}
	if !tc.accepted && (response.ErrorCode != "SIGNUP_NOT_ALLOWED" || response.ErrorMessage == "") {
		t.Errorf("blocked signup lacks policy error: %+v", response)
	}
}

func checkEmailBoundaryProxy(t *testing.T, route string, tc emailBoundaryCase) {
	t.Helper()
	// The email is canonicalized, but provider extension numbers must retain
	// their bytes and text must retain its Unicode runes (including combining
	// marks and a surrogate pair). Test a canonical lowercase email key too.
	const fields = `,"password":"synthetic\t🍽e\u0301","extension":{"number":9007199254740993,"exponent":1.20e+30,"label":"\ud83c\udf7d","nullable":null}`
	body := `{"email":` + tc.emailJSON + fields + `}`
	calls := 0
	config := AuthProxyConfig{
		BaseURL: testIssuer + "/neondb/auth", ExpectedEndpoint: testEndpointID, AllowedEmail: tc.allowed,
		Client: authProxyDoer(func(r *http.Request) (*http.Response, error) {
			calls++
			if !tc.accepted {
				t.Error("invalid email reached the upstream provider")
			}
			forwarded, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			var got, original map[string]json.RawMessage
			if err := json.Unmarshal(forwarded, &got); err != nil {
				t.Fatalf("invalid forwarded JSON: %v", err)
			}
			if tc.accepted {
				if err := json.Unmarshal([]byte(body), &original); err != nil {
					t.Fatal(err)
				}
				canonical, _ := json.Marshal(tc.allowed)
				if !bytes.Equal(got["email"], canonical) {
					t.Errorf("forwarded email bytes = %s; want configured email %s", got["email"], canonical)
				}
				if len(got) != len(original) {
					t.Errorf("forwarded field count = %d; want %d", len(got), len(original))
				}
				for _, key := range []string{"password", "extension"} {
					if !bytes.Equal(got[key], original[key]) {
						t.Errorf("forwarded %s bytes changed: %s; want %s", key, got[key], original[key])
					}
				}
				var password string
				if json.Unmarshal(got["password"], &password) != nil || password != "synthetic\t🍽e\u0301" {
					t.Errorf("forwarded password runes changed: %q", password)
				}
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"upstream":true}`)), Header: make(http.Header)}, nil
		}),
	}
	req := httptest.NewRequest(http.MethodPost, "https://meal.invalid/api/auth-proxy/"+route, strings.NewReader(body))
	req.Header.Set("Origin", "https://meal.invalid")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	AuthProxy(config).ServeHTTP(rec, req)
	if tc.accepted {
		if calls != 1 || rec.Code != http.StatusOK || rec.Body.String() != `{"upstream":true}` {
			t.Errorf("accepted proxy calls/status/body = %d/%d/%s", calls, rec.Code, rec.Body.String())
		}
		return
	}
	if calls != 0 {
		t.Errorf("invalid email made %d upstream calls", calls)
	}
	wantStatus, wantBody := http.StatusForbidden, "authentication request is not allowed\n"
	switch route {
	case "email-otp/request-password-reset":
		wantStatus, wantBody = http.StatusOK, `{"success":true}`
	case "email-otp/reset-password":
		wantStatus, wantBody = http.StatusBadRequest, `{"code":"INVALID_OTP","message":"Invalid OTP"}`
	case "sign-in/email":
		wantStatus, wantBody = http.StatusUnauthorized, `{"code":"INVALID_EMAIL_OR_PASSWORD","message":"Invalid email or password"}`
	}
	if rec.Code != wantStatus || rec.Body.String() != wantBody {
		t.Errorf("blocked proxy status/body = %d/%s; want %d/%s", rec.Code, rec.Body.String(), wantStatus, wantBody)
	}
}
