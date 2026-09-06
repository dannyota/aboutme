package printrender

import (
	"context"
	"fmt"
	"syscall"
	"testing"
)

func TestDefaultVersionProbeUsesIsolatedBrowserProcess(t *testing.T) {
	t.Setenv("ABOUTME_PARENT_SECRET_SENTINEL", "must-not-reach-version-probe")
	executable := writeExecutable(t, `#!/bin/sh
if [ -n "${ABOUTME_PARENT_SECRET_SENTINEL+x}" ]; then
	exit 9
fi
printf '%s %s %s %s\n' "$(/bin/ps -o pgid= -p $$)" "$TZ" "$LANG" "$LC_ALL"
`)

	output, err := defaultHooks().version(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	var probeGroup int
	var timezone, language, locale string
	if _, parseErr := fmt.Sscan(output, &probeGroup, &timezone, &language, &locale); parseErr != nil {
		t.Fatalf("parse version probe output: %v", parseErr)
	}
	serverGroup, err := syscall.Getpgid(syscall.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if probeGroup == serverGroup {
		t.Fatalf("version probe process group = server process group %d", serverGroup)
	}
	if timezone != "UTC" || language != "C.UTF-8" || locale != "C.UTF-8" {
		t.Fatalf("version probe environment = %q %q %q", timezone, language, locale)
	}
}
