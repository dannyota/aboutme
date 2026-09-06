package main

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/dbroles"
)

func TestRunRejectsConfigurationAndArguments(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		value string
	}{
		{"unknown argument", []string{"--password=secret"}, "postgres://admin@host/postgres"},
		{"missing URL", nil, ""},
		{"malformed URL", nil, "://secret"},
		{"wrong database", nil, "postgres://admin:secret@host/aboutme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := run(tt.args, func(string) string { return tt.value }, &out, func(context.Context, string) (dbroles.Result, error) {
				t.Fatal("bootstrap called")
				return dbroles.Result{}, nil
			})
			if err == nil {
				t.Fatal("run() error = nil")
			}
			if out.Len() != 0 {
				t.Fatalf("output = %q, want empty", out.String())
			}
			if strings.Contains(err.Error(), "secret") || (tt.value != "" && strings.Contains(err.Error(), tt.value)) {
				t.Fatalf("error leaked configuration: %q", err)
			}
		})
	}
}

func TestRunEmitsFixedCreatedAndVerifiedOutcomes(t *testing.T) {
	for _, result := range []dbroles.Result{{Created: 7}, {Verified: 7}} {
		t.Run(strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(resultName(result)), " ", "_")), func(t *testing.T) {
			var out bytes.Buffer
			err := run(nil, func(string) string { return "postgres://admin:secret@host/postgres" }, &out, func(ctx context.Context, _ string) (dbroles.Result, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("bootstrap context has no deadline")
				}
				remaining := time.Until(deadline)
				if remaining <= 29*time.Second || remaining > 30*time.Second {
					t.Fatalf("deadline remaining = %s", remaining)
				}
				return result, nil
			})
			if err != nil {
				t.Fatalf("run() error = %v", err)
			}
			if got, want := out.String(), resultName(result)+"\n"; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestRunSanitizesDatabaseFailure(t *testing.T) {
	const sensitive = "dial tcp admin:secret@private-host:5432"
	var out bytes.Buffer
	err := run(nil, func(string) string { return "postgres://admin:secret@host/postgres" }, &out, func(context.Context, string) (dbroles.Result, error) { return dbroles.Result{}, errors.New(sensitive) })
	if err == nil {
		t.Fatal("run() error = nil")
	}
	if strings.Contains(err.Error(), sensitive) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private-host") {
		t.Fatalf("error leaked driver detail: %q", err)
	}
	if out.Len() != 0 {
		t.Fatalf("output = %q, want empty", out.String())
	}
}

func TestCloseByReturnsAtDeadlineWhenCloseStalls(t *testing.T) {
	closer := &blockingCloser{release: make(chan struct{})}
	start := time.Now()
	closeBy(closer, start.Add(5*time.Millisecond))
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("closeBy took %s", elapsed)
	}
	close(closer.release)
}

type blockingCloser struct{ release chan struct{} }

func (c *blockingCloser) Close() error { <-c.release; return nil }

func resultName(result dbroles.Result) string {
	outcome := "verified"
	if result.Created > 0 {
		outcome = "created"
	}
	return "outcome=" + outcome + " created=" + strconv.Itoa(result.Created) + " verified=" + strconv.Itoa(result.Verified)
}
