package publicresume

import "testing"

func TestPublicOriginEnvironmentRules(t *testing.T) {
	for _, test := range []struct {
		name, raw, environment string
		want                   string
		valid                  bool
	}{
		{"production HTTPS", "https://resume.example", "production", "https://resume.example", true},
		{"normalizes default port", "HTTPS://RESUME.EXAMPLE:443", "production", "https://resume.example", true},
		{"staging HTTPS", "https://staging.example:8443", "staging", "https://staging.example:8443", true},
		{"development loopback HTTP", "http://127.0.0.1:20080", "development", "http://127.0.0.1:20080", true},
		{"credentials", "https://u:p@example.test", "production", "", false},
		{"path", "https://example.test/x", "production", "", false},
		{"query", "https://example.test?q=x", "production", "", false},
		{"fragment", "https://example.test#x", "production", "", false},
		{"trailing slash", "https://example.test/", "production", "", false},
		{"production HTTP", "http://example.test", "production", "", false},
		{"development non-loopback HTTP", "http://example.test", "development", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			origin, err := ParsePublicOrigin(test.raw, test.environment)
			if test.valid != (err == nil) {
				t.Fatalf("ParsePublicOrigin() error = %v, valid = %v", err, test.valid)
			}
			if err == nil && origin.String() != test.want {
				t.Fatalf("String() = %q, want %q", origin.String(), test.want)
			}
		})
	}
}

func TestPublicOriginResolve(t *testing.T) {
	origin, err := ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := origin.Resolve("/api/v1/public/resumes/ada/photo"), "https://resume.example/api/v1/public/resumes/ada/photo"; got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}
