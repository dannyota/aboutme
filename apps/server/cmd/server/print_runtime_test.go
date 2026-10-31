package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/printrender"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

func TestPrintStartupKeepsLinuxSandboxFailureFatal(t *testing.T) {
	want := printrender.ErrInvalidConfig
	renderer, queue, err := newPrintRuntime("linux", printrender.Config{}, func(printrender.Config) (*printrender.Renderer, error) {
		return nil, want
	})
	if !errors.Is(err, want) || renderer != nil || queue != nil {
		t.Fatalf("startup = %v, %v, %v; want fatal sandbox failure", renderer, queue, err)
	}
}

func TestPrintStartupDisablesUnsupportedPlatforms(t *testing.T) {
	for _, platform := range []string{"darwin", "freebsd"} {
		t.Run(platform, func(t *testing.T) {
			renderer, queue, err := newPrintRuntime(platform, printrender.Config{}, func(printrender.Config) (*printrender.Renderer, error) {
				t.Fatal("unsupported platform attempted browser initialization")
				return nil, nil
			})
			if err != nil || renderer != nil || queue == nil {
				t.Fatalf("startup = %v, %v, %v", renderer, queue, err)
			}
			if readyErr := queue.Ready(); !errors.Is(readyErr, renderjob.ErrClosed) {
				t.Fatalf("disabled queue readiness = %v", readyErr)
			}
			_, err = queue.Render(t.Context(), renderjob.Request{Format: renderjob.PDF, Prepare: func(context.Context) (renderjob.Snapshot, error) {
				t.Fatal("disabled queue prepared a render")
				return renderjob.Snapshot{}, nil
			}})
			if !errors.Is(err, renderjob.ErrClosed) {
				t.Fatalf("disabled render = %v", err)
			}
			handler, err := privatePrintHandler(queue, false)
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/internal-render/print/redeem", nil))
			if recorder.Code != http.StatusServiceUnavailable || recorder.Body.String() != "{\"error\":{\"code\":\"temporarily_unavailable\",\"message\":\"service temporarily unavailable\"}}\n" {
				t.Fatalf("private print = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
