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
		{"canonical IPv6 loopback", "http://[::1]:20080", "development", "http://[::1]:20080", true},
		{"uppercase IPv6", "https://[2001:DB8::1]", "production", "https://[2001:db8::1]", true},
		{"uncompressed IPv6", "https://[2001:0db8::0001]", "production", "", false},
		{"unabbreviated IPv6", "https://[2001:db8:0:0:0:0:0:1]", "production", "", false},
		{"long-form IPv6 loopback", "http://[0:0::1]:20080", "development", "", false},
		{"IPv4-mapped IPv6", "https://[::ffff:1.2.3.4]", "production", "", false},
		{"IPv6 zone", "http://[::1%25lo0]:20080", "development", "", false},
		{"short IPv4", "https://127.1", "production", "", false},
		{"hex IPv4", "https://0x7f.0.0.1", "production", "", false},
		{"three-part IPv4", "https://1.2.3", "production", "", false},
		{"leading-zero IPv4", "https://010.0.0.1", "production", "", false},
		{"numeric last label", "https://example.123", "production", "", false},
		{"canonical IPv4", "https://192.0.2.1", "production", "https://192.0.2.1", true},
		{"leading-zero default port", "https://resume.example:0443", "production", "", false},
		{"leading-zero port", "https://resume.example:08443", "production", "", false},
		{"port out of range", "https://resume.example:65536", "production", "", false},
		{"port zero", "https://resume.example:0", "production", "https://resume.example:0", true},
		{"highest port", "https://resume.example:65535", "production", "https://resume.example:65535", true},
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
