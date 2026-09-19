package auth

import (
	"strings"
	"testing"
)

func TestValidatedLoginReturnPath(t *testing.T) {
	const fallback = "/app/resumes"
	for _, test := range []struct {
		name, raw, want string
	}{
		{"absent", "", fallback},
		{"resume list", "/app/resumes", "/app/resumes"},
		{"deep link", "/app/resumes/0190f0e2-0000-7000-8000-000000000001", "/app/resumes/0190f0e2-0000-7000-8000-000000000001"},
		{"MCP authorize keeps its query", "/authorize?client_id=c&redirect_uri=https%3A%2F%2Fclient.example%2Fcb&state=a%5Cb",
			"/authorize?client_id=c&redirect_uri=https%3A%2F%2Fclient.example%2Fcb&state=a%5Cb"},
		{"network path", "//evil.example", fallback},
		{"absolute URL", "https://evil.example", fallback},
		{"javascript scheme", "javascript:alert(1)", fallback},
		{"no leading slash", "app/resumes", fallback},
		{"backslash", `/\evil.example`, fallback},
		{"encoded double slash", "/%2F%2Fevil.example", fallback},
		{"encoded slash after the first", "/%2Fevil.example", fallback},
		{"encoded backslash", "/%5Cevil.example", fallback},
		{"encoded backslash later in the path", "/app/%5C%5Cevil", fallback},
		{"tab", "/\t/evil.example", fallback},
		{"newline", "/app\n/x", fallback},
		{"carriage return", "/app\r/x", fallback},
		{"nul", "/app\x00", fallback},
		{"delete", "/app\x7f", fallback},
		{"encoded tab", "/%09/evil.example", fallback},
		{"encoded newline", "/app%0A", fallback},
		{"bad escape", "/app/%zz", fallback},
		{"dot segment", "/./x", fallback},
		{"dot-dot segment", "/../x", fallback},
		{"dot segment before a network path", "/.//evil.example", fallback},
		{"encoded dot segment before a network path", "/%2e//evil.example", fallback},
		{"trailing dot-dot segment", "/app/new/..", fallback},
		{"encoded dot-dot segment", "/a/%2E%2E/b", fallback},
		{"dots inside a segment stay", "/app/resumes/a..b/.well", "/app/resumes/a..b/.well"},
		{"dot segment in the query stays", "/authorize?path=/../x", "/authorize?path=/../x"},
		{"2048 bytes", "/" + strings.Repeat("a", 2047), "/" + strings.Repeat("a", 2047)},
		{"2049 bytes", "/" + strings.Repeat("a", 2048), fallback},

		{"new resume", "/app/new", "/app/new"},
		{"new resume with every parameter", "/app/new?sample=ats-plain&template=ats-plain&lng=vi",
			"/app/new?lng=vi&sample=ats-plain&template=ats-plain"},
		{"only en and vi are languages", "/app/new?lng=en-US", "/app/new"},
		{"upper-case language dropped", "/app/new?lng=VI&template=ats-plain", "/app/new?template=ats-plain"},
		{"unknown parameter dropped", "/app/new?sample=ats-plain&next=//evil.example", "/app/new?sample=ats-plain"},
		{"repeated parameter dropped", "/app/new?sample=a&sample=b&lng=en", "/app/new?lng=en"},
		{"malformed id dropped", "/app/new?sample=../x&template=Ats_Plain", "/app/new"},
		{"overlong id dropped", "/app/new?template=" + strings.Repeat("a", 65), "/app/new"},
		{"malformed language dropped", "/app/new?lng=english!", "/app/new"},
		{"fragment dropped", "/app/new?lng=en#x", "/app/new?lng=en"},
		{"malformed query dropped", "/app/new?sample=a;b", "/app/new"},
		{"router-equivalent path is checked", "/APP/New/?sample=a&x=1", "/app/new?sample=a"},
		{"encoded path is checked", "/app/%6Eew?x=1", "/app/new"},
		{"encoded network path in query of a new resume", "/app/new?sample=%2F%2Fevil", "/app/new"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validatedLoginReturnPath(test.raw); got != test.want {
				t.Fatalf("validatedLoginReturnPath(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}
