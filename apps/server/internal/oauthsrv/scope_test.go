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

func TestParseScopesAcceptsProjectScopesInAnyOrder(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"projects:read":              "projects:read",
		"projects:write":             "projects:write",
		"projects:read resumes:read": "resumes:read projects:read",
		"projects:write projects:read resumes:write resumes:read": "resumes:read resumes:write projects:read projects:write",
	} {
		got, err := ParseScopes(raw)
		if err != nil || got.String() != want {
			t.Errorf("ParseScopes(%q) = %q, %v; want %q", raw, got.String(), err, want)
		}
		if canonical := canonicalScope(raw); canonical != want {
			t.Errorf("canonicalScope(%q) = %q, want %q", raw, canonical, want)
		}
	}
	for _, raw := range []string{"projects:read projects:read", "projects:admin", "resumes:read  projects:read"} {
		if got, err := ParseScopes(raw); !errors.Is(err, ErrScopeInvalid) || got != nil {
			t.Errorf("ParseScopes(%q) = %v, %v; want ErrScopeInvalid", raw, got, err)
		}
		if canonical := canonicalScope(raw); canonical != raw {
			t.Errorf("canonicalScope(%q) = %q, want the input unchanged", raw, canonical)
		}
	}
}

func TestParseStoredScopesAcceptsCanonicalSubsetsOnly(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"resumes:read",
		"resumes:read resumes:write",
		"projects:read",
		"resumes:write projects:write",
		"resumes:read resumes:write projects:read projects:write",
	} {
		got, err := ParseStoredScopes(raw)
		if err != nil || got.String() != raw {
			t.Errorf("ParseStoredScopes(%q) = %q, %v; want the same canonical set", raw, got.String(), err)
		}
	}
	for _, raw := range []string{
		"",
		"resumes:write resumes:read",
		"projects:read resumes:read",
		"projects:write projects:read",
		"projects:read projects:read",
		"projects:admin",
		"resumes:read  projects:read",
		strings.Repeat("projects:read ", 8),
	} {
		if got, err := ParseStoredScopes(raw); !errors.Is(err, ErrScopeInvalid) || got != nil {
			t.Errorf("ParseStoredScopes(%q) = %v, %v; want ErrScopeInvalid", raw, got, err)
		}
	}
	both, err := ParseStoredScopes("resumes:read resumes:write projects:read projects:write")
	if err != nil {
		t.Fatalf("ParseStoredScopes: %v", err)
	}
	for _, scope := range []Scope{ScopeResumesRead, ScopeResumesWrite, ScopeProjectsRead, ScopeProjectsWrite} {
		if !both.Has(scope) {
			t.Errorf("four-scope set lacks %s", scope)
		}
	}
	if got := string(ScopeProjectsRead) + " " + string(ScopeProjectsWrite); got != "projects:read projects:write" {
		t.Errorf("project scope constants = %q", got)
	}
}
