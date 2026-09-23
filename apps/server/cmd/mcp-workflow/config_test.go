package main

import "testing"

func TestConfigOrigin(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"https://aboutme.vn", "https://localhost:20443"} {
		valid, err := parseOrigin(raw)
		if err != nil {
			t.Fatalf("parseOrigin(%q) error = %v", raw, err)
		}
		if got := valid.String(); got != raw {
			t.Fatalf("parseOrigin(%q) = %q", raw, got)
		}
	}

	for _, raw := range []string{
		"https://ABOUTME.vn",
		"https://aboutme.vn/",
		"http://aboutme.vn",
		"https://aboutme.vn/mcp",
		"https://aboutme.vn?resource=x",
		"https://user@aboutme.vn",
		"https://aboutme.vn:443",
		"https://evil.example",
		"https://localhost",
		"https://localhost:20444",
		"http://localhost:20443",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			if _, err := parseOrigin(raw); err == nil {
				t.Fatalf("parseOrigin(%q) error = nil, want rejection", raw)
			}
		})
	}
}
