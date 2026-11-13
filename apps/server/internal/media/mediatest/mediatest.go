package mediatest

import (
	"os"
	"testing"

	"danny.vn/aboutme/apps/server/internal/media"
)

func RequireTestS3(t testing.TB) media.S3Config {
	t.Helper()
	return requireTestS3(t, os.Getenv)
}

func requireTestS3(t testing.TB, getenv func(string) string) media.S3Config {
	t.Helper()
	endpoint := getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		if getenv("REQUIRE_TEST_S3") == "1" {
			t.Fatal("REQUIRE_TEST_S3=1 is set but TEST_S3_ENDPOINT is unset; refusing to silently skip this live test-S3 test (run 'make test-media-s3-up' and use 'make server-test-media-s3')")
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
			t.Fatalf("mediatest: TEST_S3_ENDPOINT is set but %s is missing; run 'make test-media-s3-up' and load .dev/test-s3.env", v.name)
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
