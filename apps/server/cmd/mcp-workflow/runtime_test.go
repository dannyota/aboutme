package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func makePrivateDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, privateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, privateDirectoryMode); err != nil {
		t.Fatal(err)
	}
}

// testRepository creates a main checkout with a .git directory and one linked
// worktree whose .git file points through commondir to the main one.
func testRepository(t *testing.T) (mainRoot, worktree string) {
	t.Helper()
	base := t.TempDir()
	mainRoot, worktree = filepath.Join(base, "main"), filepath.Join(base, "worktree")
	private := filepath.Join(mainRoot, ".git", "worktrees", "feature")
	makePrivateDirectory(t, private)
	makePrivateDirectory(t, worktree)
	writeRaw(t, filepath.Join(worktree, ".git"), 0o600, "gitdir: "+private+"\n")
	writeRaw(t, filepath.Join(private, "commondir"), 0o600, "../..\n")
	return mainRoot, worktree
}

func launcherLayout(t *testing.T, root string, controlParts ...string) (string, string) {
	t.Helper()
	control := filepath.Join(append([]string{root}, controlParts...)...)
	makePrivateDirectory(t, control)
	run := filepath.Join(control, "run-1")
	browser := filepath.Join(run, browserDirectory)
	makePrivateDirectory(t, run)
	makePrivateDirectory(t, browser)
	return run, browser
}

func TestRecoveryRunnerArgumentsBindModeToControlRoot(t *testing.T) {
	mainRoot, _ := testRepository(t)
	productionRun, productionBrowser := launcherLayout(t, mainRoot, ".dev", "mcp-workflow")
	localRun, localBrowser := launcherLayout(t, mainRoot, ".dev", "mcp-workflow-local")
	config, err := parseRunnerArgsFrom([]string{modeProduction, productionRun, productionBrowser}, mainRoot)
	if err != nil || config.Origin != productionOrigin || config.Control.root != filepath.Dir(productionRun) || config.local() {
		t.Fatalf("production config = %+v, %v", config, err)
	}
	config, err = parseRunnerArgsFrom([]string{modeLocal, localRun, localBrowser}, mainRoot)
	if err != nil || config.Origin != localOrigin || !config.local() {
		t.Fatalf("local config = %+v, %v", config, err)
	}
	for name, args := range map[string][]string{
		"local in production root":  {modeLocal, productionRun, productionBrowser},
		"production in local root":  {modeProduction, localRun, localBrowser},
		"unknown mode":              {"staging", localRun, localBrowser},
		"extra argument":            {modeLocal, localRun, localBrowser, "https://example.com"},
		"relative run":              {modeLocal, "run", "run/browser"},
		"browser outside run":       {modeLocal, localRun, productionBrowser},
		"unclean run":               {modeLocal, localRun + "/.", localBrowser},
		"missing arguments":         {modeLocal},
		"resume identifier in args": {modeLocal, localRun, fakeSourceID},
	} {
		if _, parseErr := parseRunnerArgsFrom(args, mainRoot); !errors.Is(parseErr, errWorkflowBlocked) {
			t.Fatalf("%s accepted: %v", name, parseErr)
		}
	}
}

func TestRecoveryProductionControlRootIsPinnedToMainCheckout(t *testing.T) {
	mainRoot, worktree := testRepository(t)
	if got, err := mainControlRoot(filepath.Join(worktree, "apps", "server")); err != nil || got != filepath.Join(mainRoot, ".dev", "mcp-workflow") {
		t.Fatalf("control root from worktree = %q, %v", got, err)
	}
	mainRun, mainBrowser := launcherLayout(t, mainRoot, ".dev", "mcp-workflow")
	worktreeRun, worktreeBrowser := launcherLayout(t, worktree, ".dev", "mcp-workflow")
	if _, err := parseRunnerArgsFrom([]string{modeProduction, mainRun, mainBrowser}, worktree); err != nil {
		t.Fatalf("worktree run rejected the shared control root: %v", err)
	}
	if _, err := parseRunnerArgsFrom([]string{modeProduction, worktreeRun, worktreeBrowser}, worktree); !errors.Is(err, errWorkflowBlocked) {
		t.Fatalf("worktree-local production control root accepted: %v", err)
	}
	outside := t.TempDir()
	if _, err := mainControlRoot(outside); !errors.Is(err, errWorkflowBlocked) {
		t.Fatal("resolved a control root outside any repository")
	}
	broken := filepath.Join(t.TempDir(), "broken")
	makePrivateDirectory(t, broken)
	writeRaw(t, filepath.Join(broken, ".git"), 0o600, "gitdir: "+filepath.Join(broken, "missing")+"\n")
	if _, err := mainControlRoot(broken); !errors.Is(err, errWorkflowBlocked) {
		t.Fatal("resolved a control root from a worktree without commondir")
	}
}

func TestRecoveryRuntimeDepsWireEveryBoundaryWithoutNetwork(t *testing.T) {
	mainRoot, _ := testRepository(t)
	run, browser := launcherLayout(t, mainRoot, ".dev", "mcp-workflow-local")
	config, err := parseRunnerArgsFrom([]string{modeLocal, run, browser}, mainRoot)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := newRuntimeDeps(config)
	if err != nil {
		t.Fatal(err)
	}
	if deps.Connect == nil || deps.Observations == nil || deps.Tokens == nil || deps.Revocation == nil ||
		deps.WaitCandidate == nil || deps.NewKey == nil || deps.LocalNow == nil {
		t.Fatal("runtime left a boundary unwired")
	}
	if _, _, tokenErr := deps.Tokens(context.Background()); !errors.Is(tokenErr, errRuntime) {
		t.Fatal("tokens available before authorization")
	}
	key, err := deps.NewKey()
	if err != nil || !validUUID(key) {
		t.Fatalf("key = %q, %v", key, err)
	}
	if revocation, ok := deps.Revocation.(httpRevocation); !ok || revocation.origin != localOrigin || revocation.client.Jar != nil {
		t.Fatal("revocation does not use the restricted same-origin client")
	}
}

// x509.SystemCertPool honors SSL_CERT_FILE and SSL_CERT_DIR, so a production
// run must refuse rather than silently trust whatever root those name, for
// example a local-mode override inherited from the launcher's environment.
func TestRecoveryProductionRefusesAnInheritedTLSOverride(t *testing.T) {
	mainRoot, _ := testRepository(t)
	run, browser := launcherLayout(t, mainRoot, ".dev", "mcp-workflow")
	config, err := parseRunnerArgsFrom([]string{modeProduction, run, browser}, mainRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, key, value string
	}{
		{"SSL_CERT_FILE", "SSL_CERT_FILE", "/dev/null"},
		{"SSL_CERT_DIR", "SSL_CERT_DIR", "/dev/null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, depsErr := newRuntimeDeps(config); !errors.Is(depsErr, errTLSOverride) {
				t.Fatalf("newRuntimeDeps error = %v, want %v", depsErr, errTLSOverride)
			}
		})
	}
}

// The same override is the local runner's intended way to trust the exported
// development root, so it must not trip the production-only refusal.
func TestRecoveryLocalKeepsAnExplicitTLSOverride(t *testing.T) {
	mainRoot, _ := testRepository(t)
	run, browser := launcherLayout(t, mainRoot, ".dev", "mcp-workflow-local")
	config, err := parseRunnerArgsFrom([]string{modeLocal, run, browser}, mainRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", "/dev/null")
	t.Setenv("SSL_CERT_DIR", "/dev/null")
	if _, depsErr := newRuntimeDeps(config); errors.Is(depsErr, errTLSOverride) {
		t.Fatal("local mode refused its own intended trust override")
	}
}

func TestRecoveryMainRejectsInvalidArgumentsBeforeAnyWork(t *testing.T) {
	var report strings.Builder
	if status := execute([]string{modeProduction}, &report); status != 2 {
		t.Fatalf("status = %d", status)
	}
	if report.String() != "mcp-workflow: "+reasonArguments+"\n" {
		t.Fatalf("report = %q", report.String())
	}
}

func TestRecoverySignalCancelsTheRunContext(t *testing.T) {
	ctx, stop := newRunContext()
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM did not cancel the run context")
	}
}

func TestRecoveryCanceledRunStillRemovesOwnerContent(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	ctx, cancel := context.WithCancel(context.Background())
	deps := h.deps()
	deps.WaitCandidate = func(waitContext context.Context, run privateArtifacts) error {
		cancel()
		return waitForCandidate(waitContext, run)
	}
	if _, err := runWorkflow(ctx, h.config, deps); !errors.Is(err, errCandidate) {
		t.Fatalf("err = %v", err)
	}
	if h.exists(h.config.Run, sourceName) || h.exists(h.config.Control, createIntentName) || h.fake.called("create_resume") != 0 {
		t.Fatal("canceled run kept owner content or reached create")
	}
}

func TestRecoveryBrowserResultIsExactRawBytes(t *testing.T) {
	for name, test := range map[string]struct {
		result string
		ok     bool
	}{
		"exact bytes":      {"completed", true},
		"JSON string":      {`"completed"`, false},
		"trailing newline": {"completed\n", false},
		"failure code":     {"login_failed", false},
	} {
		t.Run(name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "browser")
			makePrivateDirectory(t, directory)
			for file, content := range map[string]string{"browser-ready": "ready", "browser-result.json": test.result} {
				if writeErr := atomicPrivateWrite(directory, file, []byte(content)); writeErr != nil {
					t.Fatal(writeErr)
				}
			}
			coordinator := browserCoordinator{directory: directory, mode: modeLocal, origin: localOrigin, wait: time.Second}
			err := coordinator.Open(context.Background(), localOrigin+"/oauth/authorize")
			if (err == nil) != test.ok {
				t.Fatalf("err = %v, want ok = %t", err, test.ok)
			}
			if removeErr := removeBrowserFiles(directory); removeErr != nil {
				t.Fatal(removeErr)
			}
		})
	}
}
