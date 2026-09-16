package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/dbroles"
)

func validEnv() map[string]string {
	strong := strings.Repeat("s", 32)
	return map[string]string{
		"DATABASE_URL":      "postgres://aboutme@db.example:5432/aboutme?sslmode=verify-full",
		"MIGRATOR_PASSWORD": strong,
		"APP_PASSWORD":      strong + "x",
	}
}

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRunRejectsArgumentsAndMissingEnvironment(t *testing.T) {
	never := func(context.Context, string, dbroles.LoginPasswords) error {
		t.Fatal("set called")
		return nil
	}
	if err := run([]string{"extra"}, mapEnv(validEnv()), &bytes.Buffer{}, never); err == nil {
		t.Fatal("accepted an argument")
	}
	for _, key := range []string{"DATABASE_URL", "MIGRATOR_PASSWORD", "APP_PASSWORD"} {
		env := validEnv()
		delete(env, key)
		if err := run(nil, mapEnv(env), &bytes.Buffer{}, never); err == nil {
			t.Fatalf("accepted missing %s", key)
		}
	}
}

func TestRunReportsOutcomeWithoutSecrets(t *testing.T) {
	env := validEnv()
	var out bytes.Buffer
	var got dbroles.LoginPasswords
	err := run(nil, mapEnv(env), &out, func(_ context.Context, url string, p dbroles.LoginPasswords) error {
		if url != env["DATABASE_URL"] {
			t.Fatalf("url %q", url)
		}
		got = p
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Migrator != env["MIGRATOR_PASSWORD"] || got.App != env["APP_PASSWORD"] {
		t.Fatal("passwords not passed through")
	}
	if out.String() != "outcome=set roles=2\n" {
		t.Fatalf("output %q", out.String())
	}
}

func TestRunHidesFailureDetail(t *testing.T) {
	env := validEnv()
	err := run(nil, mapEnv(env), &bytes.Buffer{}, func(context.Context, string, dbroles.LoginPasswords) error {
		return errors.New("password " + env["APP_PASSWORD"] + " rejected")
	})
	if err == nil || strings.Contains(err.Error(), env["APP_PASSWORD"]) {
		t.Fatalf("error %v", err)
	}
}
