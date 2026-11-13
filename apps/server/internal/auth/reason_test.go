package auth_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/auth/oidctest"
)

var reasonTokenPattern = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

func TestRejectReason_VocabularyIsClosedAndStable(t *testing.T) {
	t.Parallel()

	tokens := auth.RejectReasonTokensForTest()
	if len(tokens) < 10 {
		t.Fatalf("declared reason vocabulary has %d entries, want the full auth funnel's worth (>=10) -- the seam is probably not reading the real enum", len(tokens))
	}

	seen := make(map[string]int, len(tokens))
	for i, token := range tokens {
		if token == "" {
			t.Errorf("reason #%d has an empty token -- every declared rejectReason needs one", i+1)
			continue
		}
		if !reasonTokenPattern.MatchString(token) {
			t.Errorf("reason #%d token = %q, want a snake_case grouping key (free text is exactly what this item replaces)", i+1, token)
		}
		if prev, dup := seen[token]; dup {
			t.Errorf("reason #%d token = %q, already used by reason #%d -- two distinct causes sharing one token are indistinguishable to an operator", i+1, token, prev)
		}
		seen[token] = i + 1
	}
}

func TestRejectReason_UnnamedValueIsNeverSilentlyEmpty(t *testing.T) {
	t.Parallel()

	placeholder := auth.ZeroRejectReasonTokenForTest()
	if placeholder == "" {
		t.Fatal("the zero rejectReason renders as an empty token; want a visible placeholder so an unmapped value cannot look like a missing attribute")
	}
	for _, declared := range auth.RejectReasonTokensForTest() {
		if declared == placeholder {
			t.Errorf("the zero-value placeholder %q collides with a real declared reason", declared)
		}
	}
}

func TestCallbackRejection_LogsTypedReasonToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wantToken string
		exercise  func(t *testing.T, handler http.Handler)
	}{
		{
			name:      "no transaction cookie at all",
			wantToken: "tx_cookie_missing",
			exercise: func(t *testing.T, handler http.Handler) {
				t.Helper()
				doCallback(t, handler, "code", "state") //nolint:bodyclose // doCallback -> doGet closes the body itself.
			},
		},
		{
			name:      "transaction cookie present but never issued",
			wantToken: "tx_invalid",
			exercise: func(t *testing.T, handler http.Handler) {
				t.Helper()
				stale := &http.Cookie{Name: auth.OAuthTxCookieName, Value: randomHandle(t)}
				doCallback(t, handler, "code", "state", stale) //nolint:bodyclose // doCallback -> doGet closes the body itself.
			},
		},
		{
			name:      "state parameter does not match the transaction",
			wantToken: "state_mismatch",
			exercise: func(t *testing.T, handler http.Handler) {
				t.Helper()
				txCookie, _, _ := beginGoogle(t, handler)
				doCallback(t, handler, "code", "not-the-real-state", txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself.
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			logger, logBuf := newCapturingLogger()
			handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLogger(logger))

			tc.exercise(t, handler)
			assertLoggedReason(t, logBuf.String(), tc.wantToken)
		})
	}
}

func assertLoggedReason(t *testing.T, logged, want string) {
	t.Helper()

	if !strings.Contains(logged, `"reason":"`+want+`"`) {
		t.Errorf("log record = %q, want a %q reason attribute (typed, closed-vocabulary token)", logged, want)
	}

	const marker = `"reason":"`
	i := strings.Index(logged, marker)
	if i < 0 {
		return
	}
	rest := logged[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j >= 0 && strings.ContainsAny(rest[:j], " (),") {
		t.Errorf("logged reason = %q, want a snake_case token with no prose", rest[:j])
	}
}
