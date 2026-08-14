package directrender

import "testing"

func TestRenderOriginEnvironmentAllowlist(t *testing.T) {
	// This fails if a viewer-reachable or non-direct origin is accepted.
	for _, test := range []struct {
		name, raw, environment string
		want                   string
		valid                  bool
	}{
		{"production direct listener", "http://127.0.0.1:3000", "production", "http://127.0.0.1:3000", true},
		{"staging direct listener", "http://127.0.0.1:3000", "staging", "http://127.0.0.1:3000", true},
		{"development Nuxt", "http://127.0.0.1:20030", "development", "http://127.0.0.1:20030", true},
		{"development fixture", "http://127.0.0.1:20440", "development", "http://127.0.0.1:20440", true},
		{"development service", "http://web:3000", "development", "http://web:3000", true},
		{"HTTPS", "https://127.0.0.1:3000", "production", "", false},
		{"wrong production port", "http://127.0.0.1:20030", "production", "", false},
		{"wrong development host", "http://localhost:20030", "development", "", false},
		{"credentials", "http://user:pass@127.0.0.1:3000", "production", "", false},
		{"path", "http://127.0.0.1:3000/internal-render", "production", "", false},
		{"query", "http://127.0.0.1:3000?x=1", "production", "", false},
		{"fragment", "http://127.0.0.1:3000#x", "production", "", false},
		{"trailing slash", "http://127.0.0.1:3000/", "production", "", false},
		{"control", "http://127.0.0.1:3000\n", "production", "", false},
		{"non ASCII", "http://127.0.0.1:3000é", "production", "", false},
		{"too long", "http://" + string(make([]byte, 506)), "production", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseRenderOrigin(test.raw, test.environment)
			if test.valid != (err == nil) {
				t.Fatalf("ParseRenderOrigin() error = %v, valid = %v", err, test.valid)
			}
			if err == nil && got.String() != test.want {
				t.Fatalf("String() = %q, want %q", got.String(), test.want)
			}
		})
	}
}
