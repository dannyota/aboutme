package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/showcase"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

const (
	reviewNameSentinel     = "Sentinel Person Name"
	reviewHeadlineSentinel = "Sentinel Headline Text"
	reviewHexA             = "0123456789abcdef"
	reviewHexB             = "fedcba9876543210"
)

func TestParseShowcaseReviewCommand(t *testing.T) {
	t.Parallel()
	valid := map[string]showcaseReviewCommand{
		"pending":                            {action: "pending"},
		"show ada-lovelace":                  {action: "show", slug: "ada-lovelace"},
		"approve ada-lovelace " + reviewHexA: {action: "approve", slug: "ada-lovelace", key: reviewHexA},
		"decline ada-lovelace " + reviewHexB: {action: "decline", slug: "ada-lovelace", key: reviewHexB},
	}
	for line, want := range valid {
		got, err := parseShowcaseReviewCommand(strings.Fields(line))
		if err != nil || got != want {
			t.Errorf("parse(%q) = %+v, %v, want %+v", line, got, err, want)
		}
	}
	for _, line := range []string{
		"", "list", "pending extra", "show", "show ada-lovelace extra", "show Ada", "show a",
		"approve", "approve ada-lovelace", "approve ada-lovelace " + reviewHexA + " extra",
		"approve ada-lovelace 0123", "approve ada-lovelace 0123456789ABCDEF", "approve ada-lovelace 0123456789abcdeg",
		"approve Ada-Lovelace " + reviewHexA, "decline ada-lovelace", "decline  " + reviewHexA,
	} {
		if _, err := parseShowcaseReviewCommand(strings.Fields(line)); err == nil {
			t.Errorf("parse(%q) accepted invalid arguments", line)
		}
	}
}

func TestShowcaseReviewDispatchRoutesTheCommand(t *testing.T) {
	t.Parallel()
	err := dispatch([]string{showcaseReviewCommandName, "bogus"})
	if err == nil || err.Error() != errShowcaseReviewUsage.Error() {
		t.Fatalf("dispatch(showcase-review bogus) error = %v, want the usage error", err)
	}
}

func TestExecuteShowcaseReviewConfigurationFailureIsFixedAndSafe(t *testing.T) {
	t.Parallel()
	const privateDSN = "postgres://private-user:private-password@/private-db?sslmode=%"
	var stdout bytes.Buffer
	err := executeShowcaseReview(context.Background(), showcaseReviewCommand{action: "pending"}, func(name string) string {
		if name != "DATABASE_URL" {
			t.Fatalf("unexpected environment read %q", name)
		}
		return privateDSN
	}, &stdout)
	if err == nil || stdout.Len() != 0 || strings.Contains(err.Error(), privateDSN) {
		t.Fatalf("configuration failure = %v, stdout %q, want a fixed error with no output", err, stdout.String())
	}
	if err.Error() != "showcase review database configuration is invalid" {
		t.Fatalf("error = %q", err)
	}
}

type reviewFixture struct {
	id   uuid.UUID
	slug string
}

// seedReviewResume commits a resume whose name and headline are sentinels,
// with a pending showcase row, and removes its owner afterwards.
func seedReviewResume(t *testing.T, pool *pgxpool.Pool) reviewFixture {
	t.Helper()
	ctx := t.Context()
	name, headline := reviewNameSentinel, reviewHeadlineSentinel
	personal, err := json.Marshal(schema.PersonalDetails{FullName: &name, Headline: &headline})
	if err != nil {
		t.Fatal(err)
	}
	var userID, resumeID uuid.UUID
	if err = pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, 'Review') RETURNING id`, uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); cleanupErr != nil {
			t.Errorf("delete user: %v", cleanupErr)
		}
	})
	slug := "rv-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err = pool.QueryRow(ctx, `
		INSERT INTO resumes (user_id, title, slug, live, schema_version, lng, personal_details, content, customization)
		VALUES ($1, 'Review', $2, true, $3, 'en', $4, '{}'::jsonb, '{}'::jsonb) RETURNING id`,
		userID, slug, docmigrate.CurrentVersion, personal).Scan(&resumeID); err != nil {
		t.Fatalf("insert resume: %v", err)
	}
	if err = store.New(pool).InsertResumeShowcase(ctx, store.InsertResumeShowcaseParams{
		ResumeID: resumeID, RequestedAt: time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC), ReviewKey: reviewHexA, CardVersion: reviewHexB,
	}); err != nil {
		t.Fatalf("insert showcase row: %v", err)
	}
	return reviewFixture{id: resumeID, slug: slug}
}

func runReview(t *testing.T, pool *pgxpool.Pool, args ...string) string {
	t.Helper()
	command, err := parseShowcaseReviewCommand(args)
	if err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	var output bytes.Buffer
	reviewer := showcase.NewReviewer(pool, func() time.Time { return time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC) })
	var stale showcaseStaleExitError
	if err = runShowcaseReviewCommand(t.Context(), reviewer, command, &output); err != nil && !errors.As(err, &stale) {
		t.Fatalf("run %v: %v", args, err)
	}
	return output.String()
}

// runReviewWithEnd runs a command the way the one-shot task does and returns
// its output and error.
func runReviewWithEnd(t *testing.T, pool *pgxpool.Pool, args ...string) (string, error) {
	t.Helper()
	command, err := parseShowcaseReviewCommand(args)
	if err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	var output bytes.Buffer
	err = runShowcaseReviewWithEnd(t.Context(), showcase.NewReviewer(pool, nil), command, &output)
	return output.String(), err
}

// AC-SHOW-006: a stale key exits with code 3 and keeps its line, and every
// finished run ends with the sentinel line.
func TestShowcaseReviewExitCodeAndEndLine(t *testing.T) {
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := seedReviewResume(t, pool)

	for _, args := range [][]string{{"pending"}, {"show", f.slug}} {
		output, runErr := runReviewWithEnd(t, pool, args...)
		if runErr != nil || !strings.HasSuffix(output, "\n"+showcaseReviewEnd+"\n") {
			t.Fatalf("%v = %q, %v, want success ending with the sentinel", args, output, runErr)
		}
	}
	stale, runErr := runReviewWithEnd(t, pool, "approve", f.slug, reviewHexB)
	var staleExit showcaseStaleExitError
	if !errors.As(runErr, &staleExit) {
		t.Fatalf("stale approve error = %v, want the stale exit error", runErr)
	}
	if !strings.Contains(stale, "stale: ") || !strings.Contains(stale, "nothing changed\n") || !strings.HasSuffix(stale, "\n"+showcaseReviewEnd+"\n") {
		t.Fatalf("stale output = %q", stale)
	}
	if showcaseStaleExitCode != 3 {
		t.Fatalf("stale exit code = %d, want 3", showcaseStaleExitCode)
	}
	approved, runErr := runReviewWithEnd(t, pool, "approve", f.slug, reviewHexA)
	if runErr != nil || approved != "approved "+f.slug+" "+reviewHexA+"\n"+showcaseReviewEnd+"\n" {
		t.Fatalf("approve = %q, %v", approved, runErr)
	}
}

func TestShowcaseReviewUsageErrorPrintsNoEndLine(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	err := executeShowcaseReview(context.Background(), showcaseReviewCommand{action: "pending"}, func(string) string { return "" }, &output)
	if err == nil || strings.Contains(output.String(), showcaseReviewEnd) {
		t.Fatalf("configuration failure = %v with output %q, want an error and no end line", err, output.String())
	}
	if _, parseErr := parseShowcaseReviewCommand([]string{"bogus"}); parseErr == nil {
		t.Fatal("a usage error parsed")
	}
}

func requireNoPersonalText(t *testing.T, label, output string) {
	t.Helper()
	lower := strings.ToLower(output)
	for _, leak := range []string{"sentinel", "person name", "headline text"} {
		if strings.Contains(lower, leak) {
			t.Errorf("%s output holds %q: %s", label, leak, output)
		}
	}
}

// AC-SHOW-006, AC-SHOW-015: pending lists slug, key, version, and date; a stale
// key does nothing and says so; approve and decline print slugs and keys only.
func TestShowcaseReviewCommands(t *testing.T) {
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := seedReviewResume(t, pool)

	pending := runReview(t, pool, "pending")
	wantLine := f.slug + "\t" + reviewHexA + "\t" + reviewHexB + "\t2026-09-28T09:00:00Z"
	if !strings.Contains(pending, wantLine+"\n") {
		t.Fatalf("pending output %q lacks the line %q", pending, wantLine)
	}
	requireNoPersonalText(t, "pending", pending)

	stale := runReview(t, pool, "approve", f.slug, reviewHexB)
	if !strings.HasPrefix(stale, "stale: ") || !strings.Contains(stale, "nothing changed") || !strings.Contains(stale, reviewHexB) || !strings.Contains(stale, f.slug) {
		t.Fatalf("stale approve output = %q, want it to say nothing changed", stale)
	}
	requireNoPersonalText(t, "stale approve", stale)
	if staleDecline := runReview(t, pool, "decline", f.slug, reviewHexB); !strings.HasPrefix(staleDecline, "stale: ") {
		t.Fatalf("stale decline output = %q", staleDecline)
	}
	row, err := store.New(pool).GetResumeShowcase(t.Context(), f.id)
	if err != nil || row.ReviewOutcome != nil || row.ReviewedKey != nil || row.FirstListedAt != nil {
		t.Fatalf("a stale key changed the row: %+v, %v", row, err)
	}

	approved := runReview(t, pool, "approve", f.slug, reviewHexA)
	if approved != "approved "+f.slug+" "+reviewHexA+"\n" {
		t.Fatalf("approve output = %q", approved)
	}
	requireNoPersonalText(t, "approve", approved)
	row, err = store.New(pool).GetResumeShowcase(t.Context(), f.id)
	if err != nil || row.ReviewOutcome == nil || *row.ReviewOutcome != "approved" || row.FirstListedAt == nil {
		t.Fatalf("approved row = %+v, %v", row, err)
	}
	if after := runReview(t, pool, "pending"); strings.Contains(after, f.slug) {
		t.Fatalf("pending still lists an approved resume: %s", after)
	}

	shown := runReview(t, pool, "show", f.slug)
	for _, want := range []string{"slug: " + f.slug, "state: listed", "review_key: " + reviewHexA, "card_version: " + reviewHexB, "outcome: approved"} {
		if !strings.Contains(shown, want+"\n") {
			t.Errorf("show output lacks %q: %s", want, shown)
		}
	}
	requireNoPersonalText(t, "show", shown)

	declined := runReview(t, pool, "decline", f.slug, reviewHexA)
	if declined != "declined "+f.slug+" "+reviewHexA+"\n" {
		t.Fatalf("decline output = %q", declined)
	}
	requireNoPersonalText(t, "decline", declined)
	if shown = runReview(t, pool, "show", f.slug); !strings.Contains(shown, "state: declined\n") {
		t.Fatalf("show after decline = %s", shown)
	}
}

func TestShowcaseReviewShowWithoutAnOptIn(t *testing.T) {
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	got := runReview(t, pool, "show", "nobody-"+strings.ReplaceAll(uuid.NewString(), "-", "")[:8])
	if !strings.Contains(got, "has no showcase request") {
		t.Fatalf("show output = %q", got)
	}
}
