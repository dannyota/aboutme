package publicformat

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// robotsAllowed evaluates path against rules the way RFC 9309 does: the
// longest matching rule wins, Allow wins a tie, "*" matches any run, and a
// trailing "$" anchors the end.
func robotsAllowed(t *testing.T, robots, path string) bool {
	t.Helper()
	best, allowed := -1, true
	for _, line := range strings.Split(robots, "\n") {
		kind, pattern, ok := strings.Cut(line, ": ")
		if !ok || (kind != "Allow" && kind != "Disallow") {
			continue
		}
		expression := "^" + strings.ReplaceAll(regexp.QuoteMeta(strings.TrimSuffix(pattern, "$")), `\*`, ".*")
		if strings.HasSuffix(pattern, "$") {
			expression += "$"
		}
		if !regexp.MustCompile(expression).MatchString(path) {
			continue
		}
		if len(pattern) > best || (len(pattern) == best && kind == "Allow") {
			best, allowed = len(pattern), kind == "Allow"
		}
	}
	return allowed
}

func TestRobotsKeepsPrivatePathsOutAndResumesIn(t *testing.T) {
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	robots := string(Robots(origin))
	for _, test := range []struct {
		path    string
		allowed bool
	}{
		{"/", true},
		{"/privacy", true},
		{"/terms", true},
		{"/ada", true},
		{"/ada.md", true},
		// A resume slug that only starts like a sign-in page stays crawlable.
		{"/login-expert", true},
		{"/registered-nurse", true},
		// Social cards fetch the share image and the preview cards; nothing
		// else under /api.
		{"/api/v1/public/resumes/ada/og.png", true},
		{"/api/v1/public/resumes/ada/pdf", false},
		{"/api/v1/public/resumes/ada", false},
		{"/api/v1/public/resumes/ada/og.png.bak", false},
		{"/api/v1/public/resumes/ada/og/0123456789abcdef.png", true},
		{"/api/v1/public/resumes/ada/ogx/0123456789abcdef.png", false},
		{"/api/v1/me", false},
		{"/app/settings/sessions", false},
		{"/login", false},
		{"/login?next=/app", false},
		{"/register", false},
		{"/forgot-password", false},
		{"/reset-password?token=x", false},
		{"/verify-email?token=x", false},
		{"/authorize?client_id=x", false},
	} {
		if got := robotsAllowed(t, robots, test.path); got != test.allowed {
			t.Errorf("robots allows %s = %t, want %t", test.path, got, test.allowed)
		}
	}
}
