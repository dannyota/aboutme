// Package mediatest provides the standalone test-S3 harness gate for suites
// that run against the local S3-compatible service (D10). It mirrors
// testutil's skip-or-fail-closed shape and, like testutil, is imported only
// by _test.go files, never by production code.
package mediatest

import (
	"os"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/media"
)

// RequireTestS3 returns the media.S3Config described by the TEST_S3_*
// variables that `make test-s3-up` writes to .dev/test-s3.env. It skips the
// calling test when TEST_S3_ENDPOINT is unset — UNLESS REQUIRE_TEST_S3=1 is
// set, in which case a missing TEST_S3_ENDPOINT is a hard Fatal instead, so
// a gate run can never pass vacuously with every S3-backed test silently
// skipped. Tests never fall back to deployment MEDIA_* credentials.
func RequireTestS3(t testing.TB) media.S3Config {
	t.Helper()
	return requireTestS3(t, os.Getenv)
}

// requireTestS3 is RequireTestS3 with an injectable environment lookup so
// the fail-closed arm is provable without mutating process state. Every
// Skip/Fatal call is immediately followed by a return: the real testing.T
// never comes back from them, and the recording fake used to prove the
// fail-closed arm does.
func requireTestS3(t testing.TB, getenv func(string) string) media.S3Config {
	t.Helper()
	endpoint := getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		if getenv("REQUIRE_TEST_S3") == "1" {
			t.Fatal("REQUIRE_TEST_S3=1 is set but TEST_S3_ENDPOINT is unset; refusing to silently skip this live test-S3 test (run 'make test-s3-up' and use 'make server-test-s3')")
			return media.S3Config{}
		}
		t.Skip("TEST_S3_ENDPOINT not set; skipping live test-S3 conformance test")
		return media.S3Config{}
	}

	cfg := media.S3Config{Endpoint: endpoint}
	required := []struct {
		name  string
		field *string
	}{
		{"TEST_S3_REGION", &cfg.Region},
		{"TEST_S3_BUCKET", &cfg.Bucket},
		{"TEST_S3_ACCESS_KEY_ID", &cfg.AccessKeyID},
		{"TEST_S3_SECRET_ACCESS_KEY", &cfg.SecretAccessKey},
	}
	for _, v := range required {
		*v.field = getenv(v.name)
		if *v.field == "" {
			// Names only, never values: a partially configured harness is
			// broken, and it must fail rather than skip.
			t.Fatalf("mediatest: TEST_S3_ENDPOINT is set but %s is missing; run 'make test-s3-up' and load .dev/test-s3.env", v.name)
			return media.S3Config{}
		}
	}
	switch getenv("TEST_S3_FORCE_PATH_STYLE") {
	case "true":
		cfg.ForcePathStyle = true
	case "false", "":
		cfg.ForcePathStyle = false
	default:
		t.Fatalf("mediatest: TEST_S3_FORCE_PATH_STYLE must be true or false")
		return media.S3Config{}
	}
	return cfg
}
