package oauthsrv

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// redirectOfBytes builds a syntactically ordinary https redirect URI of
// exactly n bytes.
func redirectOfBytes(t *testing.T, n int) string {
	t.Helper()
	const prefix = "https://example.com/"
	if n < len(prefix) {
		t.Fatalf("redirectOfBytes(%d): shorter than the fixed prefix", n)
	}
	raw := prefix + strings.Repeat("a", n-len(prefix))
	if len(raw) != n {
		t.Fatalf("redirectOfBytes built %d bytes, want %d", len(raw), n)
	}
	return raw
}

func TestValidateRedirectURIAccepts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{"https host only", "https://example.com"},
		{"https with path", "https://example.com/callback"},
		{"https with port", "https://example.com:8443/callback"},
		{"https with query", "https://example.com/callback?flow=agent"},
		{"https with percent-encoded path", "https://example.com/call%20back"},
		{"https with trailing slash", "https://example.com/"},
		{"uppercase scheme and host", "HTTPS://Example.COM/callback"},
		{"http loopback ipv4", "http://127.0.0.1/callback"},
		{"http loopback ipv4 with port", "http://127.0.0.1:20080/callback"},
		{"http loopback name", "http://localhost/callback"},
		{"http loopback name uppercase", "http://LOCALHOST:3000/callback"},
		{"http loopback ipv6", "http://[::1]/callback"},
		{"http loopback ipv6 with port", "http://[::1]:8080/callback"},
		{"http loopback uppercase scheme", "HTTP://127.0.0.1/callback"},
		{"subdomain", "https://agent.example.co.uk/oauth/cb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateRedirectURI(tc.raw); err != nil {
				t.Errorf("ValidateRedirectURI(%s) error = %v, want nil", tc.name, err)
			}
		})
	}
}

func TestValidateRedirectURIRejects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"space only", " "},
		{"relative path", "/callback"},
		{"scheme relative", "//example.com/callback"},
		{"no scheme", "example.com/callback"},
		{"opaque", "https:example.com"},
		{"empty host", "https://"},
		{"empty host with path", "https:///callback"},
		{"http non-loopback", "http://example.com/callback"},
		{"http uppercase non-loopback", "HTTP://Example.com/callback"},
		{"http loopback-looking suffix", "http://127.0.0.1.evil.example/callback"},
		{"http loopback-looking prefix", "http://localhost.evil.example/callback"},
		{"http other private address", "http://127.0.0.2/callback"},
		{"http ipv6 expanded loopback", "http://[0:0:0:0:0:0:0:1]/callback"},
		{"http ipv4-mapped loopback", "http://[::ffff:127.0.0.1]/callback"},
		{"http ipv6 non-loopback", "http://[2001:db8::1]/callback"},
		{"fragment", "https://example.com/callback#section"},
		{"empty fragment", "https://example.com/callback#"},
		{"fragment in query", "https://example.com/callback?a=1#b"},
		{"userinfo", "https://user@example.com/callback"},
		{"userinfo with password", "https://user:secret@example.com/callback"},
		{"custom scheme", "myapp://callback"},
		{"ftp scheme", "ftp://example.com/callback"},
		{"javascript scheme", "javascript:alert(1)"},
		{"data scheme", "data:text/html,hello"},
		{"space in host", "https://exa mple.com/callback"},
		{"space in path", "https://example.com/call back"},
		{"tab", "https://example.com/call\tback"},
		{"newline", "https://example.com/callback\n"},
		{"null byte", "https://example.com/call\x00back"},
		{"delete character", "https://example.com/call\x7fback"},
		{"non-ascii host", "https://ex\u00e4mple.com/callback"},
		{"non-ascii path", "https://example.com/c\u00e4llback"},
		{"bare host", "example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateRedirectURI(tc.raw); !errors.Is(err, ErrRedirectInvalid) {
				t.Errorf("ValidateRedirectURI(%s) error = %v, want ErrRedirectInvalid", tc.name, err)
			}
		})
	}
}

func TestValidateRedirectURIByteBound(t *testing.T) {
	t.Parallel()

	if err := ValidateRedirectURI(redirectOfBytes(t, 512)); err != nil {
		t.Errorf("ValidateRedirectURI(512 bytes) error = %v, want nil", err)
	}
	if err := ValidateRedirectURI(redirectOfBytes(t, 513)); !errors.Is(err, ErrRedirectInvalid) {
		t.Errorf("ValidateRedirectURI(513 bytes) error = %v, want ErrRedirectInvalid", err)
	}
	// The bound is on bytes, not characters: an oversize input must be
	// rejected before any parsing work happens.
	if err := ValidateRedirectURI(redirectOfBytes(t, 100_000)); !errors.Is(err, ErrRedirectInvalid) {
		t.Errorf("ValidateRedirectURI(100000 bytes) error = %v, want ErrRedirectInvalid", err)
	}
}

func TestValidateRedirectURIDoesNotBoundListLength(t *testing.T) {
	t.Parallel()

	// M1 caps a registration at five redirect URIs. That count is the
	// registration caller's rule (T04): each of these six URIs is
	// individually valid, so a validator that silently enforced the count
	// here would hide the caller's check.
	list := []string{
		"https://a.example/cb",
		"https://b.example/cb",
		"https://c.example/cb",
		"https://d.example/cb",
		"https://e.example/cb",
		"https://f.example/cb",
	}
	if len(list) != 6 {
		t.Fatalf("case list holds %d URIs, want 6", len(list))
	}
	for _, raw := range list {
		if err := ValidateRedirectURI(raw); err != nil {
			t.Errorf("ValidateRedirectURI error = %v, want nil for every entry", err)
		}
	}
}

func TestValidateRedirectURIErrorCarriesNoInput(t *testing.T) {
	t.Parallel()

	err := ValidateRedirectURI("ftp://" + oauthPrimSentinel + ".example/cb")
	if !errors.Is(err, ErrRedirectInvalid) {
		t.Fatalf("ValidateRedirectURI error = %v, want ErrRedirectInvalid", err)
	}
	if strings.Contains(err.Error(), oauthPrimSentinel) {
		t.Error("ValidateRedirectURI error text echoes the presented URI")
	}
	if got := ErrRedirectInvalid.Error(); got != "oauth redirect uri invalid" {
		t.Errorf("ErrRedirectInvalid.Error() = %q, want %q", got, "oauth redirect uri invalid")
	}
}

func TestValidateClientNameAccepts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"single character", "a", "a"},
		{"ascii name", "Claude Desktop", "Claude Desktop"},
		{"punctuation", "Agent (v1) \u2014 build #3", "Agent (v1) \u2014 build #3"},
		{"not trimmed", "  spaced  ", "  spaced  "},
		{"emoji is one code point", "\U0001F916 agent", "\U0001F916 agent"},
		{"already composed", "caf\u00e9", "caf\u00e9"},
		{"64 code points", strings.Repeat("a", 64), strings.Repeat("a", 64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateClientName(tc.raw)
			if err != nil {
				t.Fatalf("ValidateClientName(%s) error = %v, want nil", tc.name, err)
			}
			if got != tc.want {
				t.Errorf("ValidateClientName(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestValidateClientNameNormalizesToNFC(t *testing.T) {
	t.Parallel()

	// "e" + U+0301 COMBINING ACUTE ACCENT is two code points that NFC
	// composes into one.
	decomposed := "cafe\u0301"
	if utf8.RuneCountInString(decomposed) != 5 {
		t.Fatalf("decomposed fixture holds %d code points, want 5", utf8.RuneCountInString(decomposed))
	}
	got, err := ValidateClientName(decomposed)
	if err != nil {
		t.Fatalf("ValidateClientName error = %v, want nil", err)
	}
	if got != "caf\u00e9" {
		t.Errorf("ValidateClientName returned %q, want the NFC composition %q", got, "caf\u00e9")
	}
}

func TestValidateClientNameCodePointBounds(t *testing.T) {
	t.Parallel()

	// The bound is on code points after NFC, not bytes before it: 64
	// decomposed pairs compose to exactly 64 code points and must pass,
	// while 65 must not.
	at := strings.Repeat("e\u0301", 64)
	over := strings.Repeat("e\u0301", 65)
	if utf8.RuneCountInString(at) != 128 || len(at) != 192 {
		t.Fatalf("fixture holds %d code points in %d bytes, want 128 in 192", utf8.RuneCountInString(at), len(at))
	}
	got, err := ValidateClientName(at)
	if err != nil {
		t.Fatalf("ValidateClientName(64 code points after NFC) error = %v, want nil", err)
	}
	if utf8.RuneCountInString(got) != 64 {
		t.Errorf("canonical name holds %d code points, want 64", utf8.RuneCountInString(got))
	}
	if _, err := ValidateClientName(over); !errors.Is(err, ErrClientNameInvalid) {
		t.Errorf("ValidateClientName(65 code points after NFC) error = %v, want ErrClientNameInvalid", err)
	}
	if _, err := ValidateClientName(strings.Repeat("a", 65)); !errors.Is(err, ErrClientNameInvalid) {
		t.Errorf("ValidateClientName(65 ascii) error = %v, want ErrClientNameInvalid", err)
	}
}

func TestValidateClientNameRawByteCap(t *testing.T) {
	t.Parallel()

	// The raw byte cap is the O(1) gate in front of normalization: 4 bytes
	// per allowed code point. A decomposed Hangul name normalizes to 64 code
	// points but arrives as 576 bytes, so it is rejected before NFC runs;
	// the same name in composed form (the usual wire form) is accepted.
	decomposed := strings.Repeat("\u1100\u1161\u11a8", 64)
	composed := strings.Repeat("\uac01", 64)
	if len(decomposed) != 576 || len(composed) != 192 {
		t.Fatalf("fixtures hold %d and %d bytes, want 576 and 192", len(decomposed), len(composed))
	}
	if _, err := ValidateClientName(decomposed); !errors.Is(err, ErrClientNameInvalid) {
		t.Errorf("ValidateClientName(576 raw bytes) error = %v, want ErrClientNameInvalid", err)
	}
	if got, err := ValidateClientName(composed); err != nil || got != composed {
		t.Errorf("ValidateClientName(composed Hangul) = %q, %v; want the input unchanged and nil", got, err)
	}
	if _, err := ValidateClientName(strings.Repeat("a", 257)); !errors.Is(err, ErrClientNameInvalid) {
		t.Errorf("ValidateClientName(257 raw bytes) error = %v, want ErrClientNameInvalid", err)
	}
}

func TestValidateClientNameRejects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"null byte", "agent\x00"},
		{"newline", "agent\nname"},
		{"carriage return", "agent\rname"},
		{"tab", "agent\tname"},
		{"escape", "agent\x1bname"},
		{"delete", "agent\x7f"},
		{"c1 control", "agent\u0085name"},
		{"leading control", "\x01agent"},
		{"invalid utf-8", "agent\xff"},
		{"lone surrogate bytes", "agent\xed\xa0\x80"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateClientName(tc.raw)
			if !errors.Is(err, ErrClientNameInvalid) {
				t.Errorf("ValidateClientName(%s) error = %v, want ErrClientNameInvalid", tc.name, err)
			}
			if got != "" {
				t.Errorf("ValidateClientName(%s) returned a canonical name, want empty", tc.name)
			}
		})
	}
}

func TestValidateClientNameErrorCarriesNoInput(t *testing.T) {
	t.Parallel()

	_, err := ValidateClientName(oauthPrimSentinel + "\x00")
	if !errors.Is(err, ErrClientNameInvalid) {
		t.Fatalf("ValidateClientName error = %v, want ErrClientNameInvalid", err)
	}
	if strings.Contains(err.Error(), oauthPrimSentinel) {
		t.Error("ValidateClientName error text echoes the presented name")
	}
	if got := ErrClientNameInvalid.Error(); got != "oauth client name invalid" {
		t.Errorf("ErrClientNameInvalid.Error() = %q, want %q", got, "oauth client name invalid")
	}
}
