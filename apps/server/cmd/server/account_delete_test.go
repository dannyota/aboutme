package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/accountapi"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/publicapi"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/realtime"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// Sentinel values that must never reach the command's output or log.
const (
	reportedEmailLocal = "sentinel-owner"
	reportedName       = "Sentinel Owner Name"
	reportedHeadline   = "Sentinel Owner Headline"
)

func TestParseAccountDeleteArgs(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	for _, args := range [][]string{
		nil,
		{"show"},
		{"show", "valid-slug", "extra"},
		{"show", "BAD"},
		{"show", "a--b"},
		{"show", "guide"},
		{"confirm", "valid-slug"},
		{"confirm", "valid-slug", id, "extra"},
		{"confirm", "valid-slug", "not-a-uuid"},
		{"confirm", "valid-slug", strings.ToUpper(id)},
		{"confirm", "valid-slug", "{" + id + "}"},
		{"confirm", "valid-slug", strings.ReplaceAll(id, "-", "")},
		{"confirm", "BAD", id},
		{"delete", "valid-slug"},
	} {
		_, err := parseAccountDeleteArgs(args)
		var exit accountDeleteExitError
		if !errors.As(err, &exit) || exit.code != accountDeleteUsageExitCode || exit.message != accountDeleteUsage {
			t.Errorf("parseAccountDeleteArgs(%q) = %v, want usage exit %d", args, err, accountDeleteUsageExitCode)
		}
	}
	show, err := parseAccountDeleteArgs([]string{"show", "valid-slug"})
	if err != nil || show.confirm || show.slug != "valid-slug" {
		t.Fatalf("show args = %+v, %v", show, err)
	}
	confirm, err := parseAccountDeleteArgs([]string{"confirm", "valid-slug", id})
	if err != nil || !confirm.confirm || confirm.userID.String() != id {
		t.Fatalf("confirm args = %+v, %v", confirm, err)
	}
}

func TestExecuteAccountDeleteConfigurationFailureIsFixed(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := executeAccountDelete(context.Background(), accountDeleteArgs{slug: "valid-slug"}, func(string) string { return "" }, &stdout, &stderr)
	var exit accountDeleteExitError
	if err == nil || errors.As(err, &exit) {
		t.Fatalf("error = %v, want a plain failure with exit 1", err)
	}
	if stdout.Len() != 0 || err.Error() != "account-delete failed; nothing was deleted" {
		t.Fatalf("stdout = %q, error = %q", stdout.String(), err)
	}
}

type reportedAccount struct {
	dsn       string
	pool      *store.Pool
	userID    uuid.UUID
	foreignID uuid.UUID
	slugs     []string
	resumeIDs []uuid.UUID
	foreign   string
}

func seedReportedAccount(t *testing.T) *reportedAccount {
	t.Helper()
	ctx := context.Background()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	resumes := resume.NewStore(pool, docmigrate.NewIdentityProjector())
	a := &reportedAccount{dsn: dsn, pool: pool}

	owner, err := q.CreateUser(ctx, store.CreateUserParams{Email: reportedEmailLocal + "-" + uuid.NewString() + "@example.com", Name: reportedName})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	other, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.com", Name: "Other"})
	if err != nil {
		t.Fatalf("CreateUser(other): %v", err)
	}
	a.userID, a.foreignID = owner.ID, other.ID
	for i := range 2 {
		created, createErr := resumes.Create(ctx, owner.ID, "Owned", reportedDocument())
		if createErr != nil {
			t.Fatalf("Create: %v", createErr)
		}
		slug := fmt.Sprintf("reported-%s-%d", uuid.NewString()[:8], i)
		if _, execErr := pool.Exec(ctx, `UPDATE resumes SET slug=$2, live=true WHERE id=$1`, created.ID, slug); execErr != nil {
			t.Fatalf("set slug: %v", execErr)
		}
		if insertErr := q.InsertResumeShowcase(ctx, store.InsertResumeShowcaseParams{
			ResumeID: created.ID, RequestedAt: time.Now().UTC(), CardVersion: "0123456789abcdef",
		}); insertErr != nil {
			t.Fatalf("InsertResumeShowcase: %v", insertErr)
		}
		a.slugs = append(a.slugs, slug)
		a.resumeIDs = append(a.resumeIDs, created.ID)
	}
	sort.Strings(a.slugs)
	foreignResume, err := resumes.Create(ctx, other.ID, "Other", reportedDocument())
	if err != nil {
		t.Fatalf("Create(other): %v", err)
	}
	a.foreign = "other-" + uuid.NewString()[:8]
	if _, execErr := pool.Exec(ctx, `UPDATE resumes SET slug=$2, live=true WHERE id=$1`, foreignResume.ID, a.foreign); execErr != nil {
		t.Fatalf("set foreign slug: %v", execErr)
	}
	if _, _, err := auth.NewSessionManagerWithPool(pool).Issue(ctx, owner.ID, "ua", "203.0.113.2"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return a
}

func reportedDocument() schema.Resume {
	name, headline := reportedName, reportedHeadline
	return schema.Resume{
		SchemaVersion:   int64(schema.CurrentVersion),
		PersonalDetails: schema.PersonalDetails{FullName: &name, Headline: &headline},
		Content:         map[string]schema.Section{},
		Customization: schema.Customization{
			Font:           schema.Font{Family: schema.Inter, BaseSizePx: 14},
			Colors:         schema.Colors{Primary: "#1a1a1a", Text: "#1a1a1a", Background: "#ffffff"},
			Spacing:        schema.Spacing{SectionGap: 16, EntryGap: 8, LineHeight: 1.4},
			Heading:        schema.Heading{Style: schema.Normal},
			Layout:         schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{}, Sidebar: []string{}}},
			SectionDisplay: schema.SectionDisplay{Skill: schema.SkillClass{Style: schema.Text}, Language: schema.LanguageClass{Style: schema.Text}},
			PageFormat:     schema.A4, DateFormat: schema.MmYyyy,
		},
	}
}

func (a *reportedAccount) run(t *testing.T, args accountDeleteArgs) (stdout, stderr string, err error) {
	t.Helper()
	var out, diag bytes.Buffer
	err = executeAccountDelete(context.Background(), args, func(name string) string {
		if name != "DATABASE_URL" {
			t.Fatalf("unexpected environment read %q", name)
		}
		return a.dsn
	}, &out, &diag)
	return out.String(), diag.String(), err
}

func (a *reportedAccount) assertNoPersonalData(t *testing.T, stdout, stderr string) {
	t.Helper()
	for _, sentinel := range []string{reportedEmailLocal, reportedName, reportedHeadline, "example.com", "203.0.113"} {
		if strings.Contains(stdout, sentinel) || strings.Contains(stderr, sentinel) {
			t.Errorf("output or log holds %q:\nstdout=%q\nstderr=%q", sentinel, stdout, stderr)
		}
	}
}

func (a *reportedAccount) assertIntact(t *testing.T) {
	t.Helper()
	var users, resumes, showcase, sessions int
	ctx := context.Background()
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=$1`, a.userID).Scan(&users); err != nil || users != 1 {
		t.Fatalf("users = %d, %v, want 1", users, err)
	}
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM resumes WHERE user_id=$1`, a.userID).Scan(&resumes); err != nil || resumes != 2 {
		t.Fatalf("resumes = %d, %v, want 2", resumes, err)
	}
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM resume_showcase WHERE resume_id = ANY($1)`, a.resumeIDs).Scan(&showcase); err != nil || showcase != 2 {
		t.Fatalf("showcase rows = %d, %v, want 2", showcase, err)
	}
	if err := a.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, a.userID).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("sessions = %d, %v, want 1", sessions, err)
	}
}

func assertExit(t *testing.T, err error, want int) {
	t.Helper()
	var exit accountDeleteExitError
	if !errors.As(err, &exit) || exit.code != want {
		t.Fatalf("error = %v, want exit %d", err, want)
	}
}

func TestAccountDeleteShowPrintsOnlyUserIDAndSlugs(t *testing.T) {
	a := seedReportedAccount(t)
	stdout, stderr, err := a.run(t, accountDeleteArgs{slug: a.slugs[1]})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	want := fmt.Sprintf("account user=%s slugs=%s\naccount-delete: end\n", a.userID, strings.Join(a.slugs, ","))
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	a.assertNoPersonalData(t, stdout, stderr)
	a.assertIntact(t)
}

func TestAccountDeleteUnknownAndTombstonedSlugExit4(t *testing.T) {
	a := seedReportedAccount(t)
	tombstoned := "gone-" + uuid.NewString()[:8]
	if _, err := a.pool.Exec(context.Background(), `INSERT INTO slug_tombstones (slug) VALUES ($1)`, tombstoned); err != nil {
		t.Fatalf("seed tombstone: %v", err)
	}
	t.Cleanup(func() {
		if _, err := a.pool.Exec(context.Background(), `DELETE FROM slug_tombstones WHERE slug=$1`, tombstoned); err != nil {
			t.Errorf("delete tombstone: %v", err)
		}
	})
	for _, args := range []accountDeleteArgs{
		{slug: tombstoned},
		{slug: "unknown-" + uuid.NewString()[:8]},
		{confirm: true, slug: tombstoned, userID: a.userID},
		{confirm: true, slug: "unknown-" + uuid.NewString()[:8], userID: a.userID},
	} {
		stdout, stderr, err := a.run(t, args)
		assertExit(t, err, accountDeleteNotFoundExitCode)
		if want := "not found: " + args.slug + "\naccount-delete: end\n"; stdout != want {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
		a.assertNoPersonalData(t, stdout, stderr)
	}
	a.assertIntact(t)
}

func TestAccountDeleteConfirmMismatchDeletesNothing(t *testing.T) {
	a := seedReportedAccount(t)
	for _, userID := range []uuid.UUID{a.foreignID, uuid.New()} {
		stdout, stderr, err := a.run(t, accountDeleteArgs{confirm: true, slug: a.slugs[0], userID: userID})
		assertExit(t, err, accountDeleteMismatchExitCode)
		want := fmt.Sprintf("mismatch: %s is not owned by %s; nothing deleted\naccount-delete: end\n", a.slugs[0], userID)
		if stdout != want {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
		a.assertNoPersonalData(t, stdout, stderr)
	}
	var users int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE id=$1`, a.foreignID).Scan(&users); err != nil || users != 1 {
		t.Fatalf("named account count = %d, %v, want 1", users, err)
	}
	a.assertIntact(t)
}

func TestAccountDeleteConfirmDeletesTheAccount(t *testing.T) {
	a := seedReportedAccount(t)
	stdout, stderr, err := a.run(t, accountDeleteArgs{confirm: true, slug: a.slugs[0], userID: a.userID})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	want := fmt.Sprintf("deleted user=%s slugs=%s\naccount-delete: end\n", a.userID, strings.Join(a.slugs, ","))
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	a.assertNoPersonalData(t, stdout, stderr)
	if !strings.Contains(stderr, a.userID.String()) || !strings.Contains(stderr, `"outcome":"deleted"`) {
		t.Errorf("log lacks the audit record: %q", stderr)
	}

	ctx := context.Background()
	q := store.New(a.pool)
	if _, getErr := q.GetUserByID(ctx, a.userID); !errors.Is(getErr, pgx.ErrNoRows) {
		t.Fatalf("user lookup = %v, want no rows", getErr)
	}
	for _, check := range []struct {
		query string
		arg   any
	}{
		{`SELECT count(*) FROM resumes WHERE user_id=$1`, a.userID},
		{`SELECT count(*) FROM resume_showcase WHERE resume_id = ANY($1)`, a.resumeIDs},
		{`SELECT count(*) FROM sessions WHERE user_id=$1`, a.userID},
		{`SELECT count(*) FROM oauth_grants WHERE user_id=$1`, a.userID},
	} {
		var count int
		if scanErr := a.pool.QueryRow(ctx, check.query, check.arg).Scan(&count); scanErr != nil || count != 0 {
			t.Errorf("%s = %d, %v, want 0", check.query, count, scanErr)
		}
	}
	for _, slug := range a.slugs {
		if _, tombErr := q.GetSlugTombstoneForUpdate(ctx, slug); tombErr != nil {
			t.Errorf("tombstone %q missing: %v", slug, tombErr)
		}
	}
	if _, getErr := q.GetUserByID(ctx, a.foreignID); getErr != nil {
		t.Fatalf("other account changed: %v", getErr)
	}

	// The slugs now count as tombstoned: a second run finds nothing.
	_, _, err = a.run(t, accountDeleteArgs{confirm: true, slug: a.slugs[0], userID: a.userID})
	assertExit(t, err, accountDeleteNotFoundExitCode)
}

func TestReportAccountDeleteUnknownOutcomeExits3WithoutEndLine(t *testing.T) {
	t.Parallel()
	var out, diag bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&diag, nil))
	args := accountDeleteArgs{confirm: true, slug: "valid-slug", userID: uuid.New()}
	err := reportAccountDelete(&out, logger, args, accountapi.Account{}, fmt.Errorf("wrapped: %w", accountapi.ErrOutcomeUnknown))
	assertExit(t, err, accountDeleteUnknownExitCode)
	if want := "result unknown: run account-delete status " + args.userID.String() + " after proving the attempt stopped\n"; out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
	if !strings.Contains(diag.String(), `"outcome":"unknown"`) {
		t.Fatalf("log = %q, want the unknown outcome", diag.String())
	}
}

func TestEvictDiscoveryOnDeleteDropsOnlyDiscoveryEntries(t *testing.T) {
	t.Parallel()
	cache, err := publiccache.New(8, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	sitemap := publiccache.Key{RouteClass: publicapi.DiscoveryRouteClass, Representation: publicstate.RepresentationSitemap, Variant: "default", Generation: 1}
	page := publiccache.Key{RouteClass: "resume", Representation: publicstate.RepresentationMarkdown, Variant: "default", Generation: 1}
	put := func() {
		cache.Put(sitemap, publiccache.Value{Status: 200, Body: []byte("sitemap")})
		cache.Put(page, publiccache.Value{Status: 200, Body: []byte("page")})
	}
	var forwarded []realtime.Change
	observe := evictDiscoveryOnDelete(cache, func(c realtime.Change) { forwarded = append(forwarded, c) })

	put()
	observe(realtime.Change{ResumeID: uuid.New(), Revision: 2})
	if _, ok := cache.Get(sitemap); !ok {
		t.Fatal("an update evicted the sitemap")
	}
	observe(realtime.Change{ResumeID: uuid.New(), Revision: 3, Deleted: true})
	if _, ok := cache.Get(sitemap); ok {
		t.Fatal("a delete left the cached sitemap")
	}
	if _, ok := cache.Get(page); !ok {
		t.Fatal("a delete evicted a non-discovery entry")
	}
	if len(forwarded) != 2 {
		t.Fatalf("forwarded %d changes, want 2", len(forwarded))
	}
	evictDiscoveryOnDelete(cache, nil)(realtime.Change{Deleted: true})
}

func TestAccountDeleteResultWriterFailureHasNoSuccessExit(t *testing.T) {
	t.Parallel()
	for _, code := range []int{0, accountDeleteNotFoundExitCode, accountDeleteMismatchExitCode} {
		if err := accountDeleteResult(statusBrokenWriter{}, code, "fixed result"); err == nil || err.Error() != "account-delete result could not be written" {
			t.Fatalf("write failure for exit %d = %v", code, err)
		}
	}
}

// The known card version must pass before deletion and fail afterwards even
// while the card collaborator still has its cached bytes.
type deletionCards struct{}

func (deletionCards) Version(publicresume.Snapshot) (string, error) {
	return "0123456789abcdef", nil
}

func (deletionCards) Stored(context.Context, uuid.UUID) (previewcard.StoredCard, bool, error) {
	return previewcard.StoredCard{Version: "0123456789abcdef", PNG: []byte("synthetic card")}, true, nil
}

func (deletionCards) Build(context.Context, publicresume.Snapshot, renderjob.Priority) ([]byte, error) {
	return nil, errors.New("unexpected render")
}

func TestAccountDeletionRevokesKnownVersionedCard(t *testing.T) {
	a := seedReportedAccount(t)
	q := store.New(a.pool)
	state, err := q.GetPublicState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: state.DiscoveryGeneration})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: q, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(8, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := publicapi.NewService(publicapi.ServiceDependencies{
		Reader: reader, DiscoveryStore: q, Coordinator: coordinator, Cache: cache,
		Renderer: directrender.New(renderOrigin, http.DefaultClient), PublicOrigin: origin,
		AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Cards: deletionCards{}, Clock: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	read := func(path string) int {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		request.RemoteAddr = "192.0.2.40:1234"
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, request)
		return response.Code
	}
	card := "/api/v1/public/resumes/" + a.slugs[0] + "/og/0123456789abcdef.png"
	if code := read(card); code != http.StatusOK {
		t.Fatalf("known card before deletion = %d", code)
	}
	if _, _, err := a.run(t, accountDeleteArgs{confirm: true, slug: a.slugs[0], userID: a.userID}); err != nil {
		t.Fatal(err)
	}
	for _, slug := range a.slugs {
		for _, path := range []string{
			"/" + slug, "/" + slug + ".md", "/api/v1/public/resumes/" + slug,
			"/api/v1/public/resumes/" + slug + "/photo", "/api/v1/public/resumes/" + slug + "/pdf",
			"/api/v1/public/resumes/" + slug + "/og.png", "/api/v1/public/resumes/" + slug + "/og/0123456789abcdef.png",
		} {
			if code := read(path); code != http.StatusNotFound {
				t.Errorf("deleted public route %s = %d", path, code)
			}
		}
	}
}
