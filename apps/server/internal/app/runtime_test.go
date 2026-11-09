package app

import "testing"

func TestPublicRenderConfigRejectsSwappedOriginsAndUnsafeDigests(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, environment, publicOrigin, renderOrigin, appDigest, rendererDigest string
	}{
		{"development config value", "dev", "https://aboutme.example", "http://127.0.0.1:20030", "sha256:app", "sha256:renderer"},
		{"development parser value", "development", "https://aboutme.example", "http://127.0.0.1:20030", "sha256:app", "sha256:renderer"},
		{"production config value", "prod", "https://aboutme.example", "http://127.0.0.1:3000", "sha256:app", "sha256:renderer"},
		{"staging", "staging", "https://aboutme.example", "http://127.0.0.1:3000", "sha256:app", "sha256:renderer"},
		{"swapped", "dev", "http://127.0.0.1:20030", "https://aboutme.example", "sha256:app", "sha256:renderer"},
		{"blank app digest", "dev", "https://aboutme.example", "http://127.0.0.1:20030", "", "sha256:renderer"},
		{"control renderer digest", "dev", "https://aboutme.example", "http://127.0.0.1:20030", "sha256:app", "sha256:renderer\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParsePublicRuntime(test.publicOrigin, test.renderOrigin, test.environment, test.appDigest, test.rendererDigest)
			if test.name == "development config value" || test.name == "development parser value" || test.name == "production config value" || test.name == "staging" {
				if err != nil {
					t.Fatalf("ParsePublicRuntime() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ParsePublicRuntime() error = nil, want rejection")
			}
		})
	}
}
