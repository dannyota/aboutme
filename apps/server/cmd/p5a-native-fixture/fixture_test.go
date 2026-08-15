package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/media"
)

// frozenDSN is the exact loopback-only, fixture-scoped DSN the command
// accepts. The capture script and static test assert the same literal.
const frozenDSN = "postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_p5a_fixture?sslmode=disable"

func TestParseConfigAcceptsSeed(t *testing.T) {
	root := t.TempDir()
	cmd, cfg, err := parseConfig([]string{
		"seed",
		"--database-url", frozenDSN,
		"--media-root", ".dev/p5a-fixture-media",
		"--now", "2035-01-01T00:00:00Z",
	}, root)
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cmd != "seed" {
		t.Fatalf("cmd = %q, want %q", cmd, "seed")
	}
	if cfg.DatabaseURL != frozenDSN {
		t.Fatalf("DatabaseURL = %q, want %q", cfg.DatabaseURL, frozenDSN)
	}
	if want := filepath.Join(root, ".dev/p5a-fixture-media"); cfg.MediaRoot != want {
		t.Fatalf("MediaRoot = %q, want %q", cfg.MediaRoot, want)
	}
	if want := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC); !cfg.Now.Equal(want) {
		t.Fatalf("Now = %v, want %v", cfg.Now, want)
	}
}

func TestParseConfigAcceptsCleanup(t *testing.T) {
	root := t.TempDir()
	cmd, _, err := parseConfig([]string{
		"cleanup",
		"--database-url", frozenDSN,
		"--media-root", ".dev/p5a-fixture-media",
		"--now", "2035-01-01T00:00:00Z",
	}, root)
	if err != nil {
		t.Fatalf("parseConfig returned error: %v", err)
	}
	if cmd != "cleanup" {
		t.Fatalf("cmd = %q, want %q", cmd, "cleanup")
	}
}

func TestParseConfigRejectsUnknownSubcommand(t *testing.T) {
	_, _, err := parseConfig([]string{"frobnicate", "--database-url", frozenDSN}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for unknown subcommand, got nil")
	}
	if !strings.Contains(err.Error(), "subcommand") {
		t.Fatalf("error %q should mention the subcommand", err)
	}
}

func TestParseConfigRejectsMissingSubcommand(t *testing.T) {
	_, _, err := parseConfig([]string{"--database-url", frozenDSN}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing subcommand, got nil")
	}
}

func TestParseConfigDatabaseURLValidation(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
	}{
		{"empty", ""},
		{"non-postgres scheme", "http://127.0.0.1:20432/aboutme_p5a_fixture"},
		{"non-loopback host", "postgres://aboutme:aboutme_dev@db.internal:20432/aboutme_p5a_fixture"},
		{"localhost host", "postgres://aboutme:aboutme_dev@localhost:20432/aboutme_p5a_fixture"},
		{"wrong database", "postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable"},
		{"no database name", "postgres://aboutme:aboutme_dev@127.0.0.1:20432"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"seed", "--database-url", tc.dsn, "--media-root", ".dev/p5a-fixture-media", "--now", "2035-01-01T00:00:00Z"}
			_, _, err := parseConfig(args, t.TempDir())
			if err == nil {
				t.Fatalf("expected error for dsn %q, got nil", tc.dsn)
			}
		})
	}
}

func TestParseConfigRejectsMissingDatabaseURL(t *testing.T) {
	_, _, err := parseConfig([]string{"seed", "--media-root", ".dev/p5a-fixture-media", "--now", "2035-01-01T00:00:00Z"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing --database-url, got nil")
	}
}

func TestParseConfigMediaRootValidation(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name      string
		mediaRoot string
	}{
		{"empty", ""},
		{"filesystem root", "/"},
		{"repository root", root},
		{"outside repository", "/tmp/p5a-fixture-media"},
		{"dotdot escape", filepath.Join(root, "..", "p5a-fixture-media")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"seed", "--database-url", frozenDSN, "--media-root", tc.mediaRoot, "--now", "2035-01-01T00:00:00Z"}
			_, _, err := parseConfig(args, root)
			if err == nil {
				t.Fatalf("expected error for media-root %q, got nil", tc.mediaRoot)
			}
		})
	}
}

func TestParseConfigRejectsMissingMediaRoot(t *testing.T) {
	_, _, err := parseConfig([]string{"seed", "--database-url", frozenDSN, "--now", "2035-01-01T00:00:00Z"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing --media-root, got nil")
	}
}

func TestParseConfigNowValidation(t *testing.T) {
	cases := []struct {
		name string
		now  string
	}{
		{"empty", ""},
		{"non-frozen timestamp", "2020-01-01T00:00:00Z"},
		{"malformed", "not-a-time"},
		{"non-UTC offset", "2035-01-01T00:00:00+07:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"seed", "--database-url", frozenDSN, "--media-root", ".dev/p5a-fixture-media", "--now", tc.now}
			_, _, err := parseConfig(args, t.TempDir())
			if err == nil {
				t.Fatalf("expected error for --now %q, got nil", tc.now)
			}
		})
	}
}

func TestParseConfigRejectsMissingNow(t *testing.T) {
	_, _, err := parseConfig([]string{"seed", "--database-url", frozenDSN, "--media-root", ".dev/p5a-fixture-media"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing --now, got nil")
	}
}

func TestBuildSeedPlanDeterministicValues(t *testing.T) {
	cfg := Config{
		DatabaseURL: frozenDSN,
		MediaRoot:   filepath.Join(t.TempDir(), ".dev", "p5a-fixture-media"),
		Now:         time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	plan := buildSeedPlan(cfg)

	if want := uuid.MustParse("51000000-0000-4000-8000-000000000000"); plan.OwnerID != want {
		t.Fatalf("OwnerID = %v, want %v", plan.OwnerID, want)
	}
	if plan.OwnerEmail != "p5a-fixture@example.invalid" {
		t.Fatalf("OwnerEmail = %q", plan.OwnerEmail)
	}

	wantResumes := []resumeFixture{
		{ID: uuid.MustParse("51000000-0000-4000-8000-000000000001"), Slug: "p5a-live-photo", Live: true, SEOGeo: true, Revision: 11, HasPhoto: true},
		{ID: uuid.MustParse("51000000-0000-4000-8000-000000000002"), Slug: "p5a-live-noindex", Live: true, SEOGeo: false, Revision: 12, HasPhoto: false},
		{ID: uuid.MustParse("51000000-0000-4000-8000-000000000003"), Slug: "p5a-private", Live: false, SEOGeo: false, Revision: 13, HasPhoto: false},
	}
	if len(plan.Resumes) != len(wantResumes) {
		t.Fatalf("len(Resumes) = %d, want %d", len(plan.Resumes), len(wantResumes))
	}
	for i, want := range wantResumes {
		if plan.Resumes[i] != want {
			t.Fatalf("Resumes[%d] = %+v, want %+v", i, plan.Resumes[i], want)
		}
	}

	if plan.TombstoneSlug != "p5a-renamed-old" {
		t.Fatalf("TombstoneSlug = %q, want %q", plan.TombstoneSlug, "p5a-renamed-old")
	}
	if want := cfg.Now.Add(-time.Hour); !plan.TombstoneReleasedAt.Equal(want) {
		t.Fatalf("TombstoneReleasedAt = %v, want %v (now - 1h)", plan.TombstoneReleasedAt, want)
	}
	if plan.Generation != 41 {
		t.Fatalf("Generation = %d, want 41", plan.Generation)
	}
	if plan.PhotoKey != "p5a-fixture/51000000-0000-4000-8000-000000000001.png" {
		t.Fatalf("PhotoKey = %q, want the frozen fixture key", plan.PhotoKey)
	}
}

func TestSplitResumeDocEmbeddedDocumentIsCurrentV2(t *testing.T) {
	pd, content, customization, err := splitResumeDoc(resumeV2JSON)
	if err != nil {
		t.Fatalf("splitResumeDoc: %v", err)
	}
	if len(pd) == 0 || len(content) == 0 || len(customization) == 0 {
		t.Fatalf("splitResumeDoc returned an empty column: pd=%d content=%d customization=%d", len(pd), len(content), len(customization))
	}
	var withPhoto map[string]json.RawMessage
	if err := json.Unmarshal(pd, &withPhoto); err != nil {
		t.Fatalf("personalDetails is not an object: %v", err)
	}
	if _, hasPhoto := withPhoto["photo"]; hasPhoto {
		t.Fatal("base document must not embed a photo; the seed injects it per resume")
	}
}

func TestInjectPhotoAddsOnlyTheKey(t *testing.T) {
	const key = "p5a-fixture/51000000-0000-4000-8000-000000000001.png"
	out, err := injectPhoto([]byte(`{"fullName":"Ada"}`), key)
	if err != nil {
		t.Fatalf("injectPhoto: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("injectPhoto produced invalid JSON: %v", err)
	}
	photo := string(got["photo"])
	if photo != `{"key":"`+key+`"}` {
		t.Fatalf("photo = %s, want the exact key object", photo)
	}
	if string(got["fullName"]) != `"Ada"` {
		t.Fatalf("fullName changed to %s", got["fullName"])
	}
}

// TestPhotoNormalizesToPNG pins the photo container to the ".png" key. The
// media layer re-encodes opaque sources as JPEG, so the fixture photo carries
// one semi-transparent pixel to stay a PNG; this test fails loudly if the
// source is replaced with an opaque image and the key/content type drift.
func TestPhotoNormalizesToPNG(t *testing.T) {
	normalized, err := media.NormalizePhoto(photoPNG)
	if err != nil {
		t.Fatalf("NormalizePhoto: %v", err)
	}
	if normalized.ContentType != "image/png" {
		t.Fatalf("normalized ContentType = %q, want image/png (the fixture key is .png)", normalized.ContentType)
	}
	if normalized.Extension != "png" {
		t.Fatalf("normalized Extension = %q, want png", normalized.Extension)
	}
}
