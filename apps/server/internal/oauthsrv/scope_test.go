package oauthsrv

import (
	"errors"
	"strings"
	"testing"
)

func TestParseScopesAcceptsTheClosedSet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want Scopes
	}{
		{"read only", "resumes:read", Scopes{ScopeResumesRead}},
		{"write only", "resumes:write", Scopes{ScopeResumesWrite}},
		{"both in canonical order", "resumes:read resumes:write", Scopes{ScopeResumesRead, ScopeResumesWrite}},
		{"both reversed", "resumes:write resumes:read", Scopes{ScopeResumesRead, ScopeResumesWrite}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseScopes(tc.raw)
			if err != nil {
				t.Fatalf("ParseScopes(%q) error = %v, want nil", tc.raw, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("ParseScopes(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ParseScopes(%q) = %v, want %v (canonical order)", tc.raw, got, tc.want)
				}
			}
		})
	}
}

func TestParseScopesRejects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"space only", " "},
		{"duplicate read", "resumes:read resumes:read"},
		{"duplicate write", "resumes:write resumes:write"},
		{"unknown scope", "resumes:admin"},
		{"unknown alongside known", "resumes:read openid"},
		{"uppercase", "RESUMES:READ"},
		{"mixed case", "Resumes:Read"},
		{"leading space", " resumes:read"},
		{"trailing space", "resumes:read "},
		{"double space", "resumes:read  resumes:write"},
		{"tab separated", "resumes:read\tresumes:write"},
		{"newline separated", "resumes:read\nresumes:write"},
		{"comma separated", "resumes:read,resumes:write"},
		{"plus separated", "resumes:read+resumes:write"},
		{"prefix of a known scope", "resumes:rea"},
		{"suffix on a known scope", "resumes:reads"},
		{"null byte", "resumes:read\x00"},
		{"over the byte cap", strings.Repeat("resumes:read ", 8)},
		{"long hostile input", strings.Repeat("a", 100_000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseScopes(tc.raw)
			if !errors.Is(err, ErrScopeInvalid) {
				t.Errorf("ParseScopes(%s) error = %v, want ErrScopeInvalid", tc.name, err)
			}
			if got != nil {
				t.Errorf("ParseScopes(%s) returned scopes, want nil", tc.name)
			}
		})
	}
}

func TestScopesStringIsCanonicalAndRoundTrips(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"read only", "resumes:read", "resumes:read"},
		{"write only", "resumes:write", "resumes:write"},
		{"both reversed", "resumes:write resumes:read", "resumes:read resumes:write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scopes, err := ParseScopes(tc.raw)
			if err != nil {
				t.Fatalf("ParseScopes(%q) error = %v, want nil", tc.raw, err)
			}
			if got := scopes.String(); got != tc.want {
				t.Fatalf("Scopes.String() = %q, want %q", got, tc.want)
			}
			again, err := ParseScopes(scopes.String())
			if err != nil {
				t.Fatalf("ParseScopes(canonical string) error = %v, want nil", err)
			}
			if again.String() != tc.want {
				t.Errorf("round trip = %q, want %q", again.String(), tc.want)
			}
		})
	}
	if got := (Scopes(nil)).String(); got != "" {
		t.Errorf("Scopes(nil).String() = %q, want the empty string", got)
	}
}

func TestScopesHas(t *testing.T) {
	t.Parallel()

	readOnly, err := ParseScopes("resumes:read")
	if err != nil {
		t.Fatalf("ParseScopes error = %v, want nil", err)
	}
	both, err := ParseScopes("resumes:read resumes:write")
	if err != nil {
		t.Fatalf("ParseScopes error = %v, want nil", err)
	}
	cases := []struct {
		name   string
		scopes Scopes
		scope  Scope
		want   bool
	}{
		{"read in read-only", readOnly, ScopeResumesRead, true},
		{"write in read-only", readOnly, ScopeResumesWrite, false},
		{"read in both", both, ScopeResumesRead, true},
		{"write in both", both, ScopeResumesWrite, true},
		{"unknown scope", both, Scope("resumes:admin"), false},
		{"empty scope", both, Scope(""), false},
		{"nil scopes", nil, ScopeResumesRead, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.scopes.Has(tc.scope); got != tc.want {
				t.Errorf("Scopes.Has = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScopeConstantsMatchTheDecision(t *testing.T) {
	t.Parallel()

	// M3 freezes the scope spellings; a rename here would silently widen or
	// break every stored grant.
	if got := string(ScopeResumesRead); got != "resumes:read" {
		t.Errorf("ScopeResumesRead = %q, want %q", got, "resumes:read")
	}
	if got := string(ScopeResumesWrite); got != "resumes:write" {
		t.Errorf("ScopeResumesWrite = %q, want %q", got, "resumes:write")
	}
}

func TestScopeErrorCarriesNoInput(t *testing.T) {
	t.Parallel()

	_, err := ParseScopes(oauthPrimSentinel)
	if !errors.Is(err, ErrScopeInvalid) {
		t.Fatalf("ParseScopes error = %v, want ErrScopeInvalid", err)
	}
	if strings.Contains(err.Error(), oauthPrimSentinel) {
		t.Error("ParseScopes error text echoes the presented scope string")
	}
	if got := ErrScopeInvalid.Error(); got != "oauth scope invalid" {
		t.Errorf("ErrScopeInvalid.Error() = %q, want %q", got, "oauth scope invalid")
	}
}
