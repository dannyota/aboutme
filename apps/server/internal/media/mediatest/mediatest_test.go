package mediatest

import (
	"fmt"
	"strings"
	"testing"
)

// fakeTB records Skip/Fatal calls instead of stopping the goroutine, so the
// fail-closed arm is provable. Unlike the real testing.T, its methods
// return; requireTestS3 is written to return immediately after each
// Skip/Fatal call, so the difference is invisible to production callers.
type fakeTB struct {
	testing.TB
	fatals []string
	skips  []string
}

func (f *fakeTB) Helper() {}
func (f *fakeTB) Fatal(args ...any) {
	f.fatals = append(f.fatals, fmt.Sprint(args...))
}
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatals = append(f.fatals, fmt.Sprintf(format, args...))
}
func (f *fakeTB) Skip(args ...any) {
	f.skips = append(f.skips, fmt.Sprint(args...))
}
func (f *fakeTB) Skipf(format string, args ...any) {
	f.skips = append(f.skips, fmt.Sprintf(format, args...))
}

func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

const fakeSecret = "MEDIATEST-SECRET-SENTINEL-77aa"

func fullEnv() map[string]string {
	return map[string]string{
		"TEST_S3_ENDPOINT":          "http://127.0.0.1:20091",
		"TEST_S3_REGION":            "us-east-1",
		"TEST_S3_BUCKET":            "aboutme-test",
		"TEST_S3_ACCESS_KEY_ID":     "mediatest-akid",
		"TEST_S3_SECRET_ACCESS_KEY": fakeSecret,
		"TEST_S3_FORCE_PATH_STYLE":  "true",
	}
}

func TestRequireTestS3_SkipsWhenUnconfigured(t *testing.T) {
	t.Parallel()
	fake := &fakeTB{}
	requireTestS3(fake, env(map[string]string{}))
	if len(fake.skips) != 1 {
		t.Errorf("skips = %v, want exactly one skip", fake.skips)
	}
	if len(fake.fatals) != 0 {
		t.Errorf("fatals = %v, want none", fake.fatals)
	}
}

// TestRequireTestS3_FailsClosedUnderRequire is the gate's own proof: with
// REQUIRE_TEST_S3=1 a missing TEST_S3_ENDPOINT is a hard failure, never a
// skip, so `make server-test-s3` can never pass vacuously.
func TestRequireTestS3_FailsClosedUnderRequire(t *testing.T) {
	t.Parallel()
	fake := &fakeTB{}
	requireTestS3(fake, env(map[string]string{"REQUIRE_TEST_S3": "1"}))
	if len(fake.fatals) != 1 {
		t.Fatalf("fatals = %v, want exactly one fatal", fake.fatals)
	}
	if len(fake.skips) != 0 {
		t.Errorf("skips = %v, want none (fail, not skip)", fake.skips)
	}
	if !strings.Contains(fake.fatals[0], "TEST_S3_ENDPOINT") || !strings.Contains(fake.fatals[0], "REQUIRE_TEST_S3") {
		t.Errorf("fatal %q should name both variables", fake.fatals[0])
	}
}

func TestRequireTestS3_ReturnsCompleteConfig(t *testing.T) {
	t.Parallel()
	fake := &fakeTB{}
	cfg := requireTestS3(fake, env(fullEnv()))
	if len(fake.fatals) != 0 || len(fake.skips) != 0 {
		t.Fatalf("fatals = %v, skips = %v, want none", fake.fatals, fake.skips)
	}
	if cfg.Endpoint != "http://127.0.0.1:20091" || cfg.Region != "us-east-1" ||
		cfg.Bucket != "aboutme-test" || cfg.AccessKeyID != "mediatest-akid" ||
		cfg.SecretAccessKey != fakeSecret || !cfg.ForcePathStyle {
		t.Errorf("cfg = %+v, want the TEST_S3_* values", cfg)
	}
}

// TestRequireTestS3_MissingVariableIsFatalAndSecretFree: a partially
// configured harness is a broken harness — fail, never skip — and the
// failure names only the missing variable, never any configured value.
func TestRequireTestS3_MissingVariableIsFatalAndSecretFree(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"TEST_S3_REGION", "TEST_S3_BUCKET", "TEST_S3_ACCESS_KEY_ID", "TEST_S3_SECRET_ACCESS_KEY"} {
		vars := fullEnv()
		vars[missing] = ""
		fake := &fakeTB{}
		requireTestS3(fake, env(vars))
		if len(fake.fatals) != 1 {
			t.Errorf("%s missing: fatals = %v, want exactly one", missing, fake.fatals)
			continue
		}
		if !strings.Contains(fake.fatals[0], missing) {
			t.Errorf("%s missing: fatal %q does not name the variable", missing, fake.fatals[0])
		}
		if strings.Contains(fake.fatals[0], fakeSecret) || strings.Contains(fake.fatals[0], "mediatest-akid") {
			t.Errorf("%s missing: fatal %q leaks a configured credential value", missing, fake.fatals[0])
		}
		if len(fake.skips) != 0 {
			t.Errorf("%s missing: skips = %v, want none", missing, fake.skips)
		}
	}
}

func TestRequireTestS3_MalformedPathStyleIsFatal(t *testing.T) {
	t.Parallel()
	vars := fullEnv()
	vars["TEST_S3_FORCE_PATH_STYLE"] = "maybe"
	fake := &fakeTB{}
	requireTestS3(fake, env(vars))
	if len(fake.fatals) != 1 || !strings.Contains(fake.fatals[0], "TEST_S3_FORCE_PATH_STYLE") {
		t.Errorf("fatals = %v, want one naming TEST_S3_FORCE_PATH_STYLE", fake.fatals)
	}
}
