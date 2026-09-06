package config

import (
	"strings"
	"testing"
)

func TestPrivacyJobConfigReadsOnlyItsDependencies(t *testing.T) {
	t.Parallel()
	for _, media := range []bool{false, true} {
		t.Run(map[bool]string{false: "database", true: "media"}[media], func(t *testing.T) {
			getenv := func(name string) string {
				if name != "DATABASE_URL" && !media {
					t.Fatalf("database job read unrelated setting: %s", name)
				}
				switch name {
				case "DATABASE_URL":
					return "postgres://localhost/example"
				case "MEDIA_BACKEND":
					return "fs"
				case "MEDIA_FS_DIR":
					return t.TempDir()
				default:
					if strings.HasPrefix(name, "MEDIA_") && media {
						return ""
					}
					t.Fatalf("unexpected configuration read: %s", name)
					return ""
				}
			}
			cfg, err := LoadPrivacyJob(getenv, media)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.DatabaseURL != "postgres://localhost/example" || cfg.PublicOrigin != "" || cfg.ChromiumPath != "" {
				t.Fatal("job config loaded unrelated runtime dependencies")
			}
		})
	}
}

func TestPrivacyJobConfigFailsClosedWithoutValuesInErrors(t *testing.T) {
	t.Parallel()
	for _, values := range []map[string]string{
		{},
		{"DATABASE_URL": "postgres://localhost/example", "MEDIA_BACKEND": "invalid-private-value"},
	} {
		_, err := LoadPrivacyJob(func(name string) string { return values[name] }, true)
		if err == nil || strings.Contains(err.Error(), "invalid-private-value") {
			t.Fatal("invalid job configuration did not fail with a fixed error")
		}
	}
}
