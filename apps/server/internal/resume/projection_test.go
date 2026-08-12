// projection_test.go proves against a live database that reads project without
// writing, strict decode is load-bearing, and List fails atomically when one
// row cannot be projected or decoded.
//
// Every identifier here is prefixed `pj` so this file never collides with
// the sibling suites that share package resume_test.
package resume_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// --- synthetic two-version projector (see docmigrate_test.go for the
// rationale: v2 is the immutable v1 schema retargeted, so a projected
// document still strict-decodes into the CURRENT Go types) ---

const pjV2Prefix = "v2! "

func pjV1RawSchema(t *testing.T) []byte {
	t.Helper()
	released, err := schema.ReleasedSchemaFor(1)
	if err != nil {
		t.Fatalf("schema.ReleasedSchemaFor(1): %v", err)
	}
	return released.RawSchema
}

func pjDerivedV2Schema(t *testing.T) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(pjV1RawSchema(t)))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("decode v1 schema: %v", err)
	}
	doc["$id"] = "https://aboutme.vn/schema/resume/v2"
	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		t.Fatal("v1 schema has no $defs object")
	}
	schemaVersion, ok := defs["schemaVersion"].(map[string]any)
	if !ok {
		t.Fatal("v1 schema has no $defs/schemaVersion object")
	}
	schemaVersion["const"] = json.Number("2")
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode derived v2 schema: %v", err)
	}
	return out
}

func pjValidator(t *testing.T, raw []byte) docmigrate.ValidateFunc {
	t.Helper()
	var head struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil || head.ID == "" {
		t.Fatalf("schema has no usable $id (err=%v)", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	c.UseLoader(jsonschema.SchemeURLLoader{})
	if addErr := c.AddResource(head.ID, doc); addErr != nil {
		t.Fatalf("add schema resource: %v", addErr)
	}
	sch, err := c.Compile(head.ID)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return func(d json.RawMessage) error {
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(d))
		if err != nil {
			return fmt.Errorf("parse instance: %w", err)
		}
		return sch.Validate(inst)
	}
}

func pjHeadlineConverter(t *testing.T, target int32, add bool) docmigrate.ConvertFunc {
	t.Helper()
	return func(doc json.RawMessage) (json.RawMessage, error) {
		dec := json.NewDecoder(bytes.NewReader(doc))
		dec.UseNumber()
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return nil, err
		}
		pd, ok := m["personalDetails"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("personalDetails is not an object")
		}
		if h, ok := pd["headline"].(string); ok {
			if add {
				pd["headline"] = pjV2Prefix + h
			} else {
				if !strings.HasPrefix(h, pjV2Prefix) {
					return nil, fmt.Errorf("headline %q does not carry the %q prefix", h, pjV2Prefix)
				}
				pd["headline"] = strings.TrimPrefix(h, pjV2Prefix)
			}
		}
		m["schemaVersion"] = json.Number(fmt.Sprintf("%d", target))
		return json.Marshal(m)
	}
}

// pjSyntheticProjector makes every ordinary row -- written at
// docmigrate.CurrentVersion (1) by the store itself -- an OLD-version row,
// which is the only way to get one past the resumes_schema_version_check
// (schema_version >= 1) while exactly one version is released.
func pjSyntheticProjector(t *testing.T) *docmigrate.Projector {
	t.Helper()
	p, err := docmigrate.NewProjector(
		map[int32]docmigrate.AdjacentConverters{
			1: {Up: pjHeadlineConverter(t, 2, true), Down: pjHeadlineConverter(t, 1, false)},
		},
		map[int32]docmigrate.ValidateFunc{
			1: pjValidator(t, pjV1RawSchema(t)),
			2: pjValidator(t, pjDerivedV2Schema(t)),
		},
		[]int32{1, 2}, []int32{1, 2}, 2,
	)
	if err != nil {
		t.Fatalf("NewProjector(synthetic v2): %v", err)
	}
	return p
}

// pjStore builds a resume.Store backed by proj against the live test
// database, returning the raw pool too so a test can observe and corrupt
// rows without going through the store's own checks.
func pjStore(t *testing.T, proj *docmigrate.Projector) (*resume.Store, *store.Queries, *store.Pool, context.Context) {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })

	return resume.NewStore(pool, proj), store.New(pool), pool, ctx
}

// pjRowSnapshot is every byte of a resumes row a read must leave alone: the
// three stored jsonb parts rendered as text, the row's own schema_version,
// its revision, and updated_at.
type pjRowSnapshot struct {
	PersonalDetails string
	Content         string
	Customization   string
	SchemaVersion   int32
	Revision        int64
	UpdatedAt       time.Time
}

func pjSnapshot(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID) pjRowSnapshot {
	t.Helper()
	var s pjRowSnapshot
	err := pool.QueryRow(ctx, `SELECT personal_details::text, content::text, customization::text,
	                                  schema_version, revision, updated_at
	                           FROM resumes WHERE id = $1`, id).
		Scan(&s.PersonalDetails, &s.Content, &s.Customization, &s.SchemaVersion, &s.Revision, &s.UpdatedAt)
	if err != nil {
		t.Fatalf("snapshot row %s: %v", id, err)
	}
	return s
}

func pjAssertRowUntouched(t *testing.T, before, after pjRowSnapshot, label string) {
	t.Helper()
	if before.PersonalDetails != after.PersonalDetails {
		t.Errorf("%s: personal_details changed:\n before %s\n after  %s", label, before.PersonalDetails, after.PersonalDetails)
	}
	if before.Content != after.Content {
		t.Errorf("%s: content changed:\n before %s\n after  %s", label, before.Content, after.Content)
	}
	if before.Customization != after.Customization {
		t.Errorf("%s: customization changed:\n before %s\n after  %s", label, before.Customization, after.Customization)
	}
	if before.SchemaVersion != after.SchemaVersion {
		t.Errorf("%s: schema_version changed: %d -> %d", label, before.SchemaVersion, after.SchemaVersion)
	}
	if before.Revision != after.Revision {
		t.Errorf("%s: revision changed: %d -> %d", label, before.Revision, after.Revision)
	}
	if !before.UpdatedAt.Equal(after.UpdatedAt) {
		t.Errorf("%s: updated_at changed: %v -> %v", label, before.UpdatedAt, after.UpdatedAt)
	}
}

func pjHeadline(t *testing.T, doc schema.Resume) string {
	t.Helper()
	if doc.PersonalDetails.Headline == nil {
		t.Fatal("fixture document has no headline; the projection assertion needs one")
	}
	return *doc.PersonalDetails.Headline
}

// --- Reads project and never write ---

func TestStore_Integration_Get_ProjectsOldVersionRow_WithoutWriting(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, pjSyntheticProjector(t))
	userID := createTestUser(t, q)
	doc := validDocForTest(t)
	stored := pjHeadline(t, doc)

	created, err := s.Create(ctx, userID, "Projected", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	before := pjSnapshot(ctx, t, pool, created.ID)
	if before.SchemaVersion != 1 {
		t.Fatalf("seeded schema_version = %d, want 1 (the row must be BELOW the projector's current)", before.SchemaVersion)
	}

	got, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	if want := pjV2Prefix + stored; pjHeadline(t, got.Doc) != want {
		t.Errorf("Get().Doc headline = %q, want %q (the read did not project)", pjHeadline(t, got.Doc), want)
	}
	if got.Doc.SchemaVersion != 2 {
		t.Errorf("Get().Doc.SchemaVersion = %d, want 2 (the projector's current version)", got.Doc.SchemaVersion)
	}
	if got.StoredSchemaVersion != 1 {
		t.Errorf("Get().StoredSchemaVersion = %d, want 1 (the row's own version, before projection)", got.StoredSchemaVersion)
	}

	pjAssertRowUntouched(t, before, pjSnapshot(ctx, t, pool, created.ID), "after Get")

	// The same must hold under concurrency: a read is pure, so no number of
	// simultaneous readers may produce a write, a revision bump, or a
	// changed updated_at.
	const readers = 16
	var wg sync.WaitGroup
	errs := make([]error, readers)
	for i := range readers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, getErr := s.Get(ctx, userID, created.ID)
			if getErr != nil {
				errs[i] = getErr
				return
			}
			if r.Doc.PersonalDetails.Headline == nil || *r.Doc.PersonalDetails.Headline != pjV2Prefix+stored {
				errs[i] = fmt.Errorf("concurrent Get returned an unprojected document")
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent Get %d: %v", i, err)
		}
	}
	pjAssertRowUntouched(t, before, pjSnapshot(ctx, t, pool, created.ID), "after 16 concurrent Gets")

	// List projects the same way and is equally pure.
	list, err := s.List(ctx, userID)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List() returned %d rows, want 1", len(list))
	}
	if want := pjV2Prefix + stored; pjHeadline(t, list[0].Doc) != want {
		t.Errorf("List()[0].Doc headline = %q, want %q", pjHeadline(t, list[0].Doc), want)
	}
	pjAssertRowUntouched(t, before, pjSnapshot(ctx, t, pool, created.ID), "after List")
}

// TestStore_Integration_Get_UnknownStoredVersion_FailsClosed: a row claiming
// a version no converter chain reaches must fail the read, never be served
// as if it were current.
func TestStore_Integration_Get_UnknownStoredVersion_FailsClosed(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, docmigrate.NewIdentityProjector())
	userID := createTestUser(t, q)

	created, err := s.Create(ctx, userID, "Unknown version", validDocForTest(t))
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE resumes SET schema_version = 9 WHERE id = $1`, created.ID); err != nil {
		t.Fatalf("force schema_version 9: %v", err)
	}

	if _, err := s.Get(ctx, userID, created.ID); err == nil {
		t.Error("Get() returned nil error for a row at an unconvertible schema_version")
	}
}

// --- The strict decode on the read path is load-bearing ---

// TestStore_Integration_Get_UnknownFieldInStoredPart_FailsClosed is the
// DISCRIMINATING test for resume.DecodeParts on the read path. Swap
// projectRow's DecodeParts for a plain json.Unmarshal and this is the test
// that fails: the injected field is silently dropped on read, and the next
// SaveDocument would then persist the truncated document.
func TestStore_Integration_Get_UnknownFieldInStoredPart_FailsClosed(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, docmigrate.NewIdentityProjector())
	userID := createTestUser(t, q)

	created, err := s.Create(ctx, userID, "Unknown field", validDocForTest(t))
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if _, execErr := pool.Exec(ctx,
		`UPDATE resumes SET personal_details = personal_details || '{"unknownField":1}'::jsonb WHERE id = $1`,
		created.ID); execErr != nil {
		t.Fatalf("inject unknown field: %v", execErr)
	}

	got, err := s.Get(ctx, userID, created.ID)
	if err == nil {
		t.Fatalf("Get() returned nil error for a stored part carrying an unknown field; the field was silently dropped (got %+v)", got.Doc.PersonalDetails)
	}
	if !strings.Contains(err.Error(), "unknownField") {
		t.Errorf("Get() error = %v, want it to name the offending field", err)
	}
}

// --- List is fail-closed and atomic ---

// TestStore_Integration_List_OneCorruptRow_FailsWholeList: a partial list
// would make corruption look like the user deleting a resume, so one
// undecodable row fails the whole call with no partial result.
func TestStore_Integration_List_OneCorruptRow_FailsWholeList(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, docmigrate.NewIdentityProjector())
	userID := createTestUser(t, q)

	var ids []uuid.UUID
	for i := range 3 {
		created, err := s.Create(ctx, userID, fmt.Sprintf("Resume %d", i), validDocForTest(t))
		if err != nil {
			t.Fatalf("Create(%d) error: %v", i, err)
		}
		ids = append(ids, created.ID)
	}

	list, err := s.List(ctx, userID)
	if err != nil {
		t.Fatalf("List() before corruption: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List() before corruption returned %d rows, want 3", len(list))
	}

	// Corrupt exactly the middle row.
	if _, execErr := pool.Exec(ctx,
		`UPDATE resumes SET content = content || '{"__corrupt": {"sectionType": "nope"}}'::jsonb WHERE id = $1`,
		ids[1]); execErr != nil {
		t.Fatalf("corrupt row: %v", execErr)
	}

	got, err := s.List(ctx, userID)
	if err == nil {
		t.Fatalf("List() returned nil error with a corrupt row present (%d rows)", len(got))
	}
	if got != nil {
		t.Errorf("List() returned a partial result (%d rows) alongside its error; want nil", len(got))
	}
}

// pjFixturesDirExists keeps this file honest about the fixture corpus it
// leans on through validDocForTest: a missing fixtures directory must fail
// loudly here rather than as a confusing decode error inside a live test.
func TestProjection_FixtureCorpusPresent(t *testing.T) {
	t.Parallel()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "packages", "schema", "fixtures"))
	if err != nil {
		t.Fatalf("resolving fixtures dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "full.json")); err != nil {
		t.Fatalf("fixtures/full.json missing: %v", err)
	}
}
