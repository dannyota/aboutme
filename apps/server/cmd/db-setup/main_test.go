package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/dbroles"
)

func validEnv() map[string]string {
	strong := strings.Repeat("s", 32)
	return map[string]string{
		"DATABASE_URL":      "postgres://admin:secret@db.example:5432/aboutme",
		"MIGRATOR_PASSWORD": strong,
		"APP_PASSWORD":      strong + "x",
	}
}

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func neverEnsure(t *testing.T) ensureFunc {
	return func(context.Context, string) (dbroles.Result, error) {
		t.Fatal("ensure called")
		return dbroles.Result{}, nil
	}
}

func neverSetLogin(t *testing.T) setLoginFunc {
	return func(context.Context, string, dbroles.LoginPasswords) error {
		t.Fatal("setLogin called")
		return nil
	}
}

func TestRunRejectsArgumentsAndBadConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		value string
	}{
		{"unknown argument", []string{"--force"}, "postgres://admin@host/aboutme"},
		{"missing URL", nil, ""},
		{"malformed URL", nil, "://secret"},
		{"non-postgres scheme", nil, "mysql://admin:secret@host/aboutme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			env := map[string]string{"DATABASE_URL": tt.value}
			err := run(tt.args, mapEnv(env), &out, neverEnsure(t), neverSetLogin(t))
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

func TestRunRejectsExactlyOnePassword(t *testing.T) {
	for _, missing := range []string{"MIGRATOR_PASSWORD", "APP_PASSWORD"} {
		t.Run(missing, func(t *testing.T) {
			env := validEnv()
			delete(env, missing)
			var out bytes.Buffer
			err := run(nil, mapEnv(env), &out, neverEnsure(t), neverSetLogin(t))
			if err == nil {
				t.Fatalf("accepted %s alone unset", missing)
			}
			if out.Len() != 0 {
				t.Fatalf("output = %q, want empty", out.String())
			}
		})
	}
}

func TestRunSkipsLoginWhenNeitherPasswordSet(t *testing.T) {
	env := map[string]string{"DATABASE_URL": validEnv()["DATABASE_URL"]}
	var out bytes.Buffer
	err := run(nil, mapEnv(env), &out, func(context.Context, string) (dbroles.Result, error) {
		return dbroles.Result{Created: 2}, nil
	}, neverSetLogin(t))
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if got, want := out.String(), "outcome=created created=2 verified=0 login=skipped\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRunSetsLoginWhenBothPasswordsSet(t *testing.T) {
	env := validEnv()
	var out bytes.Buffer
	var gotURL string
	var gotPasswords dbroles.LoginPasswords
	err := run(nil, mapEnv(env), &out, func(ctx context.Context, url string) (dbroles.Result, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("ensure context has no deadline")
		}
		if remaining := time.Until(deadline); remaining <= 29*time.Second || remaining > 30*time.Second {
			t.Fatalf("deadline remaining = %s", remaining)
		}
		return dbroles.Result{Verified: 2}, nil
	}, func(_ context.Context, url string, p dbroles.LoginPasswords) error {
		gotURL = url
		gotPasswords = p
		return nil
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if gotURL != env["DATABASE_URL"] {
		t.Fatalf("setLogin url = %q", gotURL)
	}
	if gotPasswords.Migrator != env["MIGRATOR_PASSWORD"] || gotPasswords.App != env["APP_PASSWORD"] {
		t.Fatal("passwords not passed through")
	}
	if got, want := out.String(), "outcome=verified created=0 verified=2 login=set\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRunSanitizesEnsureFailure(t *testing.T) {
	const sensitive = "dial tcp admin:secret@private-host:5432"
	env := map[string]string{"DATABASE_URL": validEnv()["DATABASE_URL"]}
	var out bytes.Buffer
	err := run(nil, mapEnv(env), &out, func(context.Context, string) (dbroles.Result, error) {
		return dbroles.Result{}, errors.New(sensitive)
	}, neverSetLogin(t))
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

func TestRunSanitizesLoginFailure(t *testing.T) {
	env := validEnv()
	err := run(nil, mapEnv(env), &bytes.Buffer{}, func(context.Context, string) (dbroles.Result, error) {
		return dbroles.Result{Verified: 2}, nil
	}, func(context.Context, string, dbroles.LoginPasswords) error {
		return errors.New("password " + env["APP_PASSWORD"] + " rejected")
	})
	if err == nil || strings.Contains(err.Error(), env["APP_PASSWORD"]) {
		t.Fatalf("error %v", err)
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
