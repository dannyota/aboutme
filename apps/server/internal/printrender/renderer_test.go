package printrender

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/google/uuid"
)

func TestNewValidatesConfigurationWithoutGeneratingAuthority(t *testing.T) {
	origin := testRenderOrigin(t)
	executable := writeExecutable(t, "#!/bin/sh\nprintf 'Google Chrome for Testing 151.0.7922.34\\n'\n")

	var versionCalls atomic.Int32
	hooks := defaultHooks()
	hooks.euid = func() int { return 1000 }
	hooks.version = func(context.Context, string) (string, error) {
		versionCalls.Add(1)
		return "Google Chrome for Testing 151.0.7922.34", nil
	}
	hooks.sandboxSupported = func() bool { return true }
	hooks.ready = func(context.Context, *Renderer) error { return nil }

	renderer, err := New(Config{BrowserExecutable: executable, RenderOrigin: origin, testHooks: hooks})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := renderer.origin, "http://127.0.0.1:20030"; got != want {
		t.Fatalf("origin = %q, want %q", got, want)
	}
	if versionCalls.Load() != 1 {
		t.Fatalf("version calls = %d, want 1", versionCalls.Load())
	}
}

func TestDefaultVersionProbeDoesNotInheritParentEnvironment(t *testing.T) {
	t.Setenv("ABOUTME_PARENT_SECRET_SENTINEL", "must-not-reach-version-probe")
	executable := writeExecutable(t, `#!/bin/sh
if [ -n "${ABOUTME_PARENT_SECRET_SENTINEL+x}" ]; then
	exit 9
fi
printf 'Google Chrome for Testing 151.0.7922.34\n'
`)
	got, err := defaultHooks().version(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Google Chrome for Testing 151.0.7922.34" {
		t.Fatalf("version = %q", got)
	}
}

func TestNewRejectsUnsafeRuntimeAndWrongBrowser(t *testing.T) {
	origin := testRenderOrigin(t)
	executable := writeExecutable(t, "#!/bin/sh\nexit 0\n")

	tests := []struct {
		name   string
		config Config
	}{
		{"missing executable", Config{RenderOrigin: origin}},
		{"missing origin", Config{BrowserExecutable: executable}},
		{"root", configWithHooks(executable, origin, func(h *runtimeHooks) { h.euid = func() int { return 0 } })},
		{"unsupported sandbox", configWithHooks(executable, origin, func(h *runtimeHooks) { h.sandboxSupported = func() bool { return false } })},
		{"wrong version", configWithHooks(executable, origin, func(h *runtimeHooks) {
			h.version = func(context.Context, string) (string, error) { return "Google Chrome 150.0.0.0", nil }
		})},
		{"opaque version failure", configWithHooks(executable, origin, func(h *runtimeHooks) {
			h.version = func(context.Context, string) (string, error) { return "", errors.New("secret path detail") }
		})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.config)
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error = %v, want ErrInvalidConfig", err)
			}
			if strings.Contains(err.Error(), "secret path detail") {
				t.Fatalf("error leaked dependency detail: %v", err)
			}
		})
	}
}

func TestReadyProbesBrowserOnce(t *testing.T) {
	origin := testRenderOrigin(t)
	executable := writeExecutable(t, "#!/bin/sh\nexit 0\n")
	var calls atomic.Int32
	hooks := defaultHooks()
	hooks.euid = func() int { return 1000 }
	hooks.sandboxSupported = func() bool { return true }
	hooks.version = func(context.Context, string) (string, error) { return expectedBrowserVersion, nil }
	hooks.ready = func(context.Context, *Renderer) error {
		calls.Add(1)
		return errors.New("browser startup detail")
	}
	renderer, err := New(Config{BrowserExecutable: executable, RenderOrigin: origin, testHooks: hooks})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := renderer.Ready(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Ready() error = %v, want ErrUnavailable", err)
		} else if strings.Contains(err.Error(), "browser startup detail") {
			t.Fatalf("Ready() leaked dependency detail: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("readiness probes = %d, want 1", calls.Load())
	}
}

func TestRenderRejectsUnknownFormatAndCanceledContextBeforeLaunch(t *testing.T) {
	renderer := &Renderer{render: func(context.Context, renderjob.Navigation) ([]byte, error) {
		t.Fatal("browser launched")
		return nil, nil
	}}
	if _, err := renderer.Render(context.Background(), renderjob.Navigation{Format: "svg"}); !errors.Is(err, ErrRenderFailed) {
		t.Fatalf("unknown format error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := renderer.Render(ctx, renderjob.Navigation{Format: renderjob.PDF}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
}

func TestRenderRejectsMalformedNavigationWithoutLaunch(t *testing.T) {
	launched := false
	renderer := &Renderer{render: func(context.Context, renderjob.Navigation) ([]byte, error) {
		launched = true
		return nil, nil
	}}
	for _, navigation := range []renderjob.Navigation{
		validTestNavigation(renderjob.Format("svg")),
		func() renderjob.Navigation {
			value := validTestNavigation(renderjob.PDF)
			value.Capability = "short"
			return value
		}(),
		func() renderjob.Navigation {
			value := validTestNavigation(renderjob.PDF)
			value.Capability = "abcdefghijklmnopqrstuvwxyzABCDEFGH012345679"
			return value
		}(),
		func() renderjob.Navigation {
			value := validTestNavigation(renderjob.PDF)
			value.ResumeID = uuid.Nil
			return value
		}(),
		func() renderjob.Navigation {
			value := validTestNavigation(renderjob.PDF)
			value.JobID = uuid.Nil
			return value
		}(),
	} {
		if _, err := renderer.Render(context.Background(), navigation); !errors.Is(err, ErrRenderFailed) {
			t.Fatalf("error = %v, want ErrRenderFailed", err)
		}
	}
	if launched {
		t.Fatal("invalid navigation launched browser")
	}
}

func configWithHooks(executable string, origin directrender.RenderOrigin, mutate func(*runtimeHooks)) Config {
	hooks := defaultHooks()
	hooks.euid = func() int { return 1000 }
	hooks.sandboxSupported = func() bool { return true }
	hooks.version = func(context.Context, string) (string, error) { return expectedBrowserVersion, nil }
	hooks.ready = func(context.Context, *Renderer) error { return nil }
	mutate(hooks)
	return Config{BrowserExecutable: executable, RenderOrigin: origin, testHooks: hooks}
}

func testRenderOrigin(t *testing.T) directrender.RenderOrigin {
	t.Helper()
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "test")
	if err != nil {
		t.Fatal(err)
	}
	return origin
}

func writeExecutable(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chrome")
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
