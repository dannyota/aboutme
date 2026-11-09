package printrender

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/chromedp"
)

// streamSession is a cdp.Session that answers IO.read with one result.
type streamSession struct{ result string }

func (s streamSession) Call(_ context.Context, method string, _, result any) error {
	if method != "IO.read" {
		return fmt.Errorf("unexpected method %s", method)
	}
	return jsonv2.Unmarshal([]byte(s.result), result, chromedp.DefaultUnmarshalOptions)
}

func (streamSession) Subscribe(string) (<-chan jsonv2.Value, func()) {
	events := make(chan jsonv2.Value)
	close(events)
	return events, func() {}
}

func TestPDFStreamKeepsRawAndBase64ChunksUndecoded(t *testing.T) {
	for _, test := range []struct {
		name   string
		result string
		want   streamChunk
	}{
		{"raw text", `{"base64Encoded":false,"data":"%PDF-1.7\n","eof":false}`, streamChunk{data: "%PDF-1.7\n"}},
		{"base64", `{"base64Encoded":true,"data":"JVBERi0=","eof":true}`, streamChunk{data: "JVBERi0=", base64: true, eof: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := &cdpPDFStream{session: streamSession{result: test.result}, handle: "stream-1"}
			chunk, err := stream.read(t.Context(), 64*1024)
			if err != nil || chunk != test.want {
				t.Fatalf("chunk = %+v, %v; want %+v", chunk, err, test.want)
			}
		})
	}
}

func TestBrowserProfileIsRendererOwnedAndRemovedAfterJoinedTeardown(t *testing.T) {
	directory := t.TempDir()
	record, present := filepath.Join(directory, "arguments"), filepath.Join(directory, "profile-present")
	executable := writeExecutable(t, fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" > '%s'
for argument in "$@"; do
	case "$argument" in
	--user-data-dir=*) [ -d "${argument#--user-data-dir=}" ] && : > '%s' ;;
	esac
done
exit 7
`, record, present))
	err := withBrowser(t.Context(), executable, "http://127.0.0.1:20030", testSupervisorCommand(t), false, func(ctx context.Context) error {
		return chromedp.Do(ctx)
	})
	if err == nil {
		t.Fatal("a browser that exits at once started")
	}
	recorded, readErr := os.ReadFile(record)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var profiles []string
	for _, argument := range strings.Split(strings.TrimSpace(string(recorded)), "\n") {
		if profile, ok := strings.CutPrefix(argument, "--user-data-dir="); ok {
			profiles = append(profiles, profile)
		}
	}
	// chromedp scans for and kills processes by numeric PID after the browser
	// exits only when it creates the profile itself, named chromedp-runner.
	if len(profiles) != 1 || !strings.HasPrefix(filepath.Base(profiles[0]), browserProfilePrefix) {
		t.Fatalf("browser profiles = %q, want one renderer-owned profile", profiles)
	}
	if _, statErr := os.Stat(present); statErr != nil {
		t.Fatal("the profile did not exist while the browser ran")
	}
	if _, statErr := os.Lstat(profiles[0]); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("profile after teardown: %v, want removed", statErr)
	}
}

func TestBrowserProfileRemovalFailureFailsTheAttemptWithoutPrivateText(t *testing.T) {
	supervisor := testSupervisorCommand(t)
	supervisor.removeProfile = func(profile string) error {
		if err := os.RemoveAll(profile); err != nil {
			return err
		}
		return &os.PathError{Op: "unlinkat", Path: "/private/path/TOKEN=private-value", Err: syscall.EACCES}
	}
	for _, test := range []struct {
		name     string
		diagnose bool
		runErr   error
		want     error
		stage    string
	}{
		{"successful render", false, nil, ErrRenderFailed, "profile-cleanup"},
		{"successful readiness", true, nil, ErrUnavailable, "profile-cleanup"},
		{"earlier render error", false, ErrOutputTooLarge, ErrOutputTooLarge, ""},
		{"earlier readiness error", true, errors.New("private-value document-text"), ErrUnavailable, "launch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := withBrowser(t.Context(), "/bin/true", "http://127.0.0.1:20030", supervisor, test.diagnose, func(context.Context) error {
				return test.runErr
			})
			if test.stage == "" {
				if !errors.Is(err, test.want) || strings.Contains(err.Error(), "profile-cleanup") {
					t.Fatalf("error = %v, want the earlier %v", err, test.want)
				}
				return
			}
			detail := "permission denied"
			if test.stage == "launch" {
				detail = "detail redacted"
			}
			assertInitializationError(t, err, test.want, test.stage, detail)
		})
	}
}

func TestBrowserLaunchReadsTheDevToolsAddressFromOutput(t *testing.T) {
	record := filepath.Join(t.TempDir(), "arguments")
	executable := writeExecutable(t, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > '%s'\nexit 7\n", record))
	err := withBrowser(t.Context(), executable, "http://127.0.0.1:20030", testSupervisorCommand(t), false, func(ctx context.Context) error {
		return chromedp.Do(ctx)
	})
	if err == nil {
		t.Fatal("a browser that exits at once started")
	}
	recorded, readErr := os.ReadFile(record)
	if readErr != nil {
		t.Fatal(readErr)
	}
	arguments := strings.Split(strings.TrimSpace(string(recorded)), "\n")
	if !slices.Contains(arguments, "--remote-debugging-port=0") || slices.Contains(arguments, "--remote-debugging-pipe") {
		t.Fatalf("browser transport arguments = %q", arguments)
	}
}
