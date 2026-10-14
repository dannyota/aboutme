package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestMediaDeletionStatusClosedOutput(t *testing.T) {
	t.Parallel()
	for _, pending := range []int64{0, 1, 999} {
		var out bytes.Buffer
		err := reportMediaDeletionStatus(context.Background(), &out, func(context.Context) (int64, error) { return pending, nil })
		if err != nil {
			t.Fatal(err)
		}
		if out.String() != fmt.Sprintf("{\"success\":true,\"pending\":%d}\n", pending) {
			t.Fatalf("output %q", out.String())
		}
	}
	for _, read := range []func(context.Context) (int64, error){
		func(context.Context) (int64, error) { return 0, errors.New("private") },
		func(context.Context) (int64, error) { return -1, nil },
	} {
		var out bytes.Buffer
		err := reportMediaDeletionStatus(context.Background(), &out, read)
		if err == nil || err.Error() != "media-deletion-status failed" || out.Len() != 0 {
			t.Fatalf("failure %v %q", err, out.String())
		}
	}
	if err := reportMediaDeletionStatus(context.Background(), statusBrokenWriter{}, func(context.Context) (int64, error) { return 0, nil }); err == nil {
		t.Fatal("writer failure succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := reportMediaDeletionStatus(ctx, &out, func(ctx context.Context) (int64, error) { return 0, ctx.Err() }); err == nil || out.Len() != 0 {
		t.Fatal("canceled queue snapshot succeeded")
	}
}

func TestMediaDeletionStatusUsageAndConfiguration(t *testing.T) {
	t.Parallel()
	var exit accountDeleteExitError
	if err := runMediaDeletionStatus([]string{"extra"}); !errors.As(err, &exit) || exit.code != 2 || exit.message != "usage: server media-deletion-status" {
		t.Fatalf("usage %v", err)
	}
	var out bytes.Buffer
	if err := executeMediaDeletionStatus(context.Background(), func(string) string { return "" }, &out); err == nil || err.Error() != "media-deletion-status failed" || out.Len() != 0 {
		t.Fatalf("configuration %v", err)
	}
}
