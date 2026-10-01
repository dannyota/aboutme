package showcase

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

const templatesDirectory = "../../../../packages/schema/templates"

// templateFile is one committed template preset.
type templateFile struct {
	ID            string         `json:"id"`
	Customization map[string]any `json:"customization"`
}

func readTemplateFiles(t *testing.T) []templateFile {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(templatesDirectory, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no template files under %s: %v", templatesDirectory, err)
	}
	sort.Strings(paths)
	files := make([]templateFile, 0, len(paths))
	for _, path := range paths {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		var file templateFile
		if decodeErr := json.Unmarshal(raw, &file); decodeErr != nil {
			t.Fatalf("decode %s: %v", path, decodeErr)
		}
		files = append(files, file)
	}
	return files
}

// appliedCustomization is the stored customization a template apply produces
// for file: the preset without its placement, plus the resume's own section
// order.
func appliedCustomization(t *testing.T, file templateFile, main, sidebar []string) schema.Customization {
	t.Helper()
	raw, err := json.Marshal(file.Customization)
	if err != nil {
		t.Fatalf("encode %s: %v", file.ID, err)
	}
	var customization map[string]any
	if err = json.Unmarshal(raw, &customization); err != nil {
		t.Fatalf("decode %s: %v", file.ID, err)
	}
	layout, ok := customization["layout"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no layout", file.ID)
	}
	delete(layout, "placement")
	delete(layout, "sidebarSectionTypes")
	layout["sections"] = map[string]any{"main": main, "sidebar": sidebar}
	stored, err := json.Marshal(customization)
	if err != nil {
		t.Fatalf("encode stored %s: %v", file.ID, err)
	}
	var out schema.Customization
	if err = json.Unmarshal(stored, &out); err != nil {
		t.Fatalf("decode stored %s: %v", file.ID, err)
	}
	return out
}

// baseCustomization is a valid-enough customization that matches no preset.
func baseCustomization() schema.Customization {
	return schema.Customization{
		Colors: schema.Colors{Primary: "#1d4ed8", Text: "#111111", Background: "#ffffff"},
		Layout: schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{}, Sidebar: []string{}}},
	}
}

// rowSpec describes one test resume.
type rowSpec struct {
	slug          string
	lng           string
	name          string
	headline      string
	photoKey      string
	crop          *schema.PhotoCrop
	contacts      []schema.PersonalDetail
	content       map[string]schema.Section
	customization schema.Customization
	notLive       bool
	signInToView  bool
}

func defaultSpec() rowSpec {
	return rowSpec{
		slug: "ada-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12], lng: "en",
		name: "Ada Lovelace", headline: "Engineer", customization: baseCustomization(),
	}
}

func (s rowSpec) parts(t *testing.T) (personal, content, customization []byte) {
	t.Helper()
	details := schema.PersonalDetails{FullName: &s.name, Details: s.contacts}
	if s.headline != "" {
		headline := s.headline
		details.Headline = &headline
	}
	if s.photoKey != "" {
		details.Photo = &schema.Photo{Key: s.photoKey, Crop: s.crop}
	}
	contentMap := s.content
	if contentMap == nil {
		contentMap = map[string]schema.Section{}
	}
	return mustMarshal(t, details), mustMarshal(t, contentMap), mustMarshal(t, s.customization)
}

// row is the in-memory resume row for projection tests.
func (s rowSpec) row(t *testing.T, id uuid.UUID) store.Resume {
	t.Helper()
	personal, content, customization := s.parts(t)
	slug, lng := s.slug, s.lng
	return store.Resume{
		ID: id, Slug: &slug, Live: !s.notLive, SignInToView: s.signInToView, Revision: 3, Lng: &lng,
		SchemaVersion: docmigrate.CurrentVersion, PersonalDetails: personal, Content: content, Customization: customization,
		UpdatedAt: time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC),
	}
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}
	return encoded
}

type unusedReadStore struct{}

func (unusedReadStore) GetPublicState(context.Context) (store.PublicState, error) {
	return store.PublicState{}, errors.New("unused")
}

func (unusedReadStore) GetPublicResumeBySlug(context.Context, string) (store.Resume, error) {
	return store.Resume{}, errors.New("unused")
}

func (unusedReadStore) GetPublicResumeByOwner(context.Context, store.GetPublicResumeByOwnerParams) (store.Resume, error) {
	return store.Resume{}, errors.New("unused")
}
func (unusedReadStore) ListEligiblePublicSlugs(context.Context) ([]string, error) { return nil, nil }

func testReader(t *testing.T, readStore store.PublicReadQueries) *publicresume.Reader {
	t.Helper()
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := publicresume.ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: readStore, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func snapshotOf(t *testing.T, spec rowSpec) publicresume.Snapshot {
	t.Helper()
	snapshot, err := testReader(t, unusedReadStore{}).ProjectRow(spec.row(t, uuid.New()))
	if err != nil {
		t.Fatalf("ProjectRow() error = %v", err)
	}
	return snapshot
}

// env is a live-database fixture for the showcase service.
type env struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	queries *store.Queries
	reader  *publicresume.Reader
	service *Service
	now     time.Time
}

// newEnv opens the shared test database. Its rows are shared with parallel
// tests, so a test using it never counts rows it did not create.
func newEnv(t *testing.T) *env {
	t.Helper()
	return openEnv(t, testutil.RequireMigratedTestDatabaseURL(t))
}

// newFreshEnv opens a private database, for tests that count listing rows.
func newFreshEnv(t *testing.T) *env {
	t.Helper()
	dsn, _ := testutil.NewMigratedTestDatabase(t)
	return openEnv(t, dsn)
}

func openEnv(t *testing.T, dsn string) *env {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)
	queries := store.New(pool)
	reader := testReader(t, queries)
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	service, err := New(Dependencies{DB: pool, Reader: reader, Projector: docmigrate.NewIdentityProjector(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return &env{
		ctx: ctx, pool: pool, queries: queries, reader: reader, service: service,
		now: now,
	}
}

// fixture is one committed resume owned by its own throwaway user.
type fixture struct {
	id   uuid.UUID
	slug string
	user uuid.UUID
}

// addResume commits a resume for spec and removes its owner (and with it the
// resume and any showcase row) when the test ends.
func (e *env) addResume(t *testing.T, spec rowSpec) fixture {
	t.Helper()
	personal, content, customization := spec.parts(t)
	var lng *string
	if spec.lng != "" {
		lng = &spec.lng
	}
	var userID, resumeID uuid.UUID
	if err := e.pool.QueryRow(e.ctx, `INSERT INTO users (email, name) VALUES ($1, 'Showcase Test') RETURNING id`,
		uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		if _, err := e.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("delete user: %v", err)
		}
	})
	if err := e.pool.QueryRow(e.ctx, `
		INSERT INTO resumes (user_id, title, slug, live, sign_in_to_view, schema_version, lng, personal_details, content, customization)
		VALUES ($1, 'Showcase', $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		userID, spec.slug, !spec.notLive, spec.signInToView, docmigrate.CurrentVersion, lng, personal, content, customization,
	).Scan(&resumeID); err != nil {
		t.Fatalf("insert resume: %v", err)
	}
	return fixture{id: resumeID, slug: spec.slug, user: userID}
}

// optIn runs PublishTx the way a publish request does and commits it.
func (e *env) optIn(t *testing.T, resumeID uuid.UUID, role *string) {
	t.Helper()
	e.publish(t, PublishChange{ResumeID: resumeID, Enabled: true, Role: role})
}

func (e *env) publish(t *testing.T, change PublishChange) {
	t.Helper()
	tx, err := e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err = e.service.PublishTx(e.ctx, store.New(tx), change); err != nil {
		_ = tx.Rollback(e.ctx) //nolint:errcheck // The test fails on the publish error below.
		t.Fatalf("PublishTx() error: %v", err)
	}
	if err = tx.Commit(e.ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// sync runs SyncTx the way a document write does and commits it.
func (e *env) sync(t *testing.T, resumeID uuid.UUID) {
	t.Helper()
	tx, err := e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err = e.service.SyncTx(e.ctx, store.New(tx), resumeID); err != nil {
		_ = tx.Rollback(e.ctx) //nolint:errcheck // The test fails on the sync error below.
		t.Fatalf("SyncTx() error: %v", err)
	}
	if err = tx.Commit(e.ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// listed adds a resume and opts it in. Nothing is reviewed, so it is listed at
// once.
func (e *env) listed(t *testing.T, spec rowSpec, role *string) fixture {
	t.Helper()
	f := e.addResume(t, spec)
	e.optIn(t, f.id, role)
	return f
}

func (e *env) showcaseRow(t *testing.T, id uuid.UUID) (store.ResumeShowcase, bool) {
	t.Helper()
	row, err := e.queries.GetResumeShowcase(e.ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ResumeShowcase{}, false
	}
	if err != nil {
		t.Fatalf("read showcase row: %v", err)
	}
	return row, true
}

func strp(value string) *string { return &value }

func slugsOf(page Page) []string {
	slugs := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		slugs = append(slugs, item.Slug)
	}
	return slugs
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
