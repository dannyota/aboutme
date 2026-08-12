package auth_test

// reason_test.go proves rejection logs use a closed token vocabulary while
// clients receive one opaque error.

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
)

// reasonTokenPattern permits stable snake_case log grouping keys only.
var reasonTokenPattern = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

// TestRejectReason_VocabularyIsClosedAndStable proves the vocabulary is
// exhaustive and machine-usable: every value in the declared range has a
// token, every token is unique, and every token is a stable grouping key.
// A new constant added without a token would surface here rather than as
// an unlabeled log line in production.
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

// TestRejectReason_UnnamedValueIsNeverSilentlyEmpty proves a rejectReason
// outside the declared range (the zero value, or a hand-cast integer)
// still renders as something an operator can see and alert on, rather than
// an empty attribute that looks like a missing field.
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

// TestCallbackRejection_LogsTypedReasonToken proves distinct internal failures
// retain stable operator signals behind one client-facing error.
func TestCallbackRejection_LogsTypedReasonToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wantToken string
		// cookies returns the cookies to send with the callback, given a
		// handler that can begin a real transaction; state is the query
		// value to send.
		exercise func(t *testing.T, handler http.Handler)
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

// assertLoggedReason asserts logged carries exactly the reason attribute
// wanted, and that whatever reason it carries is a token rather than
// prose.
func assertLoggedReason(t *testing.T, logged, want string) {
	t.Helper()

	if !strings.Contains(logged, `"reason":"`+want+`"`) {
		t.Errorf("log record = %q, want a %q reason attribute (typed, closed-vocabulary token)", logged, want)
	}

	// Check the reason attribute itself for prose rather than scanning the
	// whole record: the message field legitimately contains spaces.
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
