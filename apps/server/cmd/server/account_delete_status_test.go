package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
)

func TestAccountStatusStrictArguments(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"status"}, {"status", "BAD"}, {"status", "018F5B6A-9A3E-7C21-8B1E-000000000030"}, {"status", uuid.NewString(), "extra"}} {
		_, err := parseAccountDeleteArgs(args)
		var exit accountDeleteExitError
		if !errors.As(err, &exit) || exit.code != 2 {
			t.Fatalf("args %q: %v", args, err)
		}
	}
	parsed, err := parseAccountDeleteArgs([]string{"status", "018f5b6a-9a3e-7c21-8b1e-000000000030"})
	if err != nil || !parsed.status {
		t.Fatalf("status parse: %v", err)
	}
}

func TestAccountStatusClosedOutputAndFailure(t *testing.T) {
	t.Parallel()
	for _, present := range []bool{true, false} {
		var out bytes.Buffer
		read := func(context.Context, uuid.UUID) (bool, error) { return present, nil }
		if err := reportAccountStatus(context.Background(), uuid.Nil, &out, read); err != nil {
			t.Fatal(err)
		}
		state := "absent"
		if present {
			state = "present"
		}
		if out.String() != "{\"success\":true,\"state\":\""+state+"\"}\n" {
			t.Fatalf("output %q", out.String())
		}
	}
	var out bytes.Buffer
	read := func(context.Context, uuid.UUID) (bool, error) { return false, errors.New("private diagnostic") }
	if err := reportAccountStatus(context.Background(), uuid.Nil, &out, read); err == nil || err.Error() != "account-delete status failed" || out.Len() != 0 {
		t.Fatalf("failure: %v %q", err, out.String())
	}
	read = func(ctx context.Context, _ uuid.UUID) (bool, error) { return false, ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reportAccountStatus(ctx, uuid.Nil, &out, read); err == nil {
		t.Fatal("cancellation succeeded")
	}
	if err := reportAccountStatus(context.Background(), uuid.Nil, statusBrokenWriter{}, func(context.Context, uuid.UUID) (bool, error) { return true, nil }); err == nil {
		t.Fatal("writer failure succeeded")
	}
}

type statusBrokenWriter struct{}

func (statusBrokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestAccountStatusReducedConfiguration(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := executeAccountStatus(context.Background(), uuid.Nil, func(string) string { return "" }, &out)
	if err == nil || err.Error() != "account-delete status failed" || out.Len() != 0 {
		t.Fatalf("configuration failure: %v", err)
	}
}

func TestOperatorReadConfigurationNeedsOnlyDatabase(t *testing.T) {
	t.Parallel()
	pool, err := operatorReadPool(context.Background(), func(name string) string {
		if name != "DATABASE_URL" {
			t.Fatalf("operator read requested unrelated configuration %q", name)
		}
		return "postgres://aboutme_app@/aboutme?host=/run/postgresql"
	})
	if err != nil {
		t.Fatal(err)
	}
	closeOperatorReadPool(context.Background(), pool)
}
