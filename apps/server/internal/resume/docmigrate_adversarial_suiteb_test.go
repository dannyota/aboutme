// Blind adversarial suite B (Phase 2A, Task 10) -- the live-database half.
//
// Every case here is derived from written contracts only: design spec §3
// ("Doc-shape migrations", "Wire-version compatibility"), decisions D4, D12,
// D13, D16, D17, D18, D19, Task 8's Interfaces block, write-path.md's
// backfill-vs-autosave sequence, and the exported API as reported by
// `go doc ./internal/resume` and `go doc ./internal/resume/docmigrate`. No
// implementation body in internal/resume, internal/resume/docmigrate, or
// internal/store was read before these tests were written and first run.
//
// Matrix rows owned here: TestGet_NeverWrites,
// TestBackfill_LosesToConcurrentAutosave, TestAutosave_AfterBackfill_NoSpurious412,
// TestBackfill_ConcurrentWithItself_AppliesOnce,
// TestBackfill_NeverPersistsInvalidProjection,
// TestProjection_UnknownStoredVersionFailsClosed and
// TestList_OneBadProjectionFailsAtomically, plus B6 (title-only write causes a
// retryable lost race) and the accept -> persist -> emit chain over the
// declared production versions. The pure converter matrix lives in
// docmigrate/suiteb_wire_adversarial_test.go.
//
// SYNTHETIC OLD VERSION. `resumes_schema_version_check` is `schema_version >= 1`
// and production's CurrentVersion is 1, so there is no room below the current
// version for a synthetic "old" row. Synthetic old versions therefore sit ABOVE
// the current one: the projector's current stays at docmigrate.CurrentVersion
// (so SaveDocument, ValidateForStore and the backfill target all agree on the
// same version), and a row seeded at schema_version = 2 is the stale one, walked
// DOWN to the current version by the pair's Down converter. Synthetic v2 is v1
// plus a required personalDetails.headline marker, so a projection that did not
// actually run is visible as a leftover marker rather than as a relabelling.
package resume_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// suiteBOldVersion is the synthetic stale schema_version stored rows carry.
// It sits above CurrentVersion because the CHECK constraint forbids 0.
const suiteBOldVersion int32 = 2

// suiteBHeadlineMarker is what synthetic v2 requires and v1 forbids.
const suiteBHeadlineMarker = "synthetic-v2-headline"

// suiteBMinimalDoc is the shape of packages/schema/fixtures/minimal.json: the
// smallest document the store's own validation pipeline accepts.
const suiteBMinimalDoc = `{
  "schemaVersion": 1,
  "personalDetails": { "fullName": "PLACEHOLDER", "details": [] },
  "content": {},
  "customization": {
    "font": { "family": "Inter", "baseSizePx": 14 },
    "colors": { "primary": "#1a1a1a", "text": "#1a1a1a", "background": "#ffffff" },
    "spacing": { "sectionGap": 16, "entryGap": 8, "lineHeight": 1.4 },
    "heading": { "style": "normal", "showRule": false },
    "layout": { "columns": 1, "sections": { "main": [], "sidebar": [] } },
    "sectionDisplay": { "skill": { "style": "text" }, "language": { "style": "text" } },
    "pageFormat": "a4",
    "dateFormat": "MM/YYYY"
  }
}`

// suiteBRequireBackfillContract pins the three declared BackfillResult values
// to the order Task 8's Interfaces block gives them, so a later renumbering
// cannot silently turn "applied" into "skipped" in every assertion below.
func suiteBRequireBackfillContract(t *testing.T) {
	t.Helper()
	for _, tc := range []struct {
		got  resume.BackfillResult
		want int
		name string
	}{
		{resume.BackfillUnknown, 0, "BackfillUnknown"},
		{resume.BackfillApplied, 1, "BackfillApplied"},
		{resume.BackfillSkippedCurrent, 2, "BackfillSkippedCurrent"},
		{resume.BackfillLostRace, 3, "BackfillLostRace"},
	} {
		if int(tc.got) != tc.want {
			t.Fatalf("%s = %d, want %d (Task 8's declared iota order)", tc.name, int(tc.got), tc.want)
		}
	}
}

// suiteBEnv is one test's live-database fixture: its own pool, its own user,
// and a store wired to a projector under test.
type suiteBEnv struct {
	pool   *store.Pool
	userID uuid.UUID
}

// suiteBSetup opens a pool against the migrated test database and creates a
// user of its own, so tests never share rows.
func suiteBSetup(t *testing.T) *suiteBEnv {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)

	ctx := t.Context()
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.WithoutCancel(ctx)) })

	var userID uuid.UUID
	email := fmt.Sprintf("suiteb-%s@example.test", uuid.NewString())
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		email, "Suite B",
	).Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	return &suiteBEnv{pool: pool, userID: userID}
}

// suiteBDoc builds a store-valid current-version document whose fullName marks
// which document a row is carrying.
func suiteBDoc(t *testing.T, fullName string) schema.Resume {
	t.Helper()
	var doc schema.Resume
	if err := json.Unmarshal([]byte(suiteBMinimalDoc), &doc); err != nil {
		t.Fatalf("decode the minimal document: %v", err)
	}
	doc.SchemaVersion = int64(docmigrate.CurrentVersion)
	doc.PersonalDetails.FullName = &fullName
	if err := resume.ValidateForStore(doc); err != nil {
		t.Fatalf("suite B's own base document is not store-valid: %v", err)
	}
	return doc
}

// suiteBParts decomposes a document into the three stored jsonb parts (D4:
// none of them carries schemaVersion).
func suiteBParts(t *testing.T, doc schema.Resume) (pd, c, cu string) {
	t.Helper()
	canonical, err := resume.AssembleCanonical(doc)
	if err != nil {
		t.Fatalf("assemble canonical document: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &m); err != nil {
		t.Fatalf("split canonical document: %v", err)
	}
	return string(m["personalDetails"]), string(m["content"]), string(m["customization"])
}

// suiteBAddHeadline stamps the synthetic v2 marker onto a personalDetails part.
func suiteBAddHeadline(t *testing.T, personalDetails string) string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(personalDetails), &m); err != nil {
		t.Fatalf("decode personalDetails: %v", err)
	}
	marker, err := json.Marshal(suiteBHeadlineMarker)
	if err != nil {
		t.Fatalf("marshal headline marker: %v", err)
	}
	m["headline"] = marker
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("re-marshal personalDetails: %v", err)
	}
	return string(raw)
}

// suiteBInsertRow seeds one resumes row directly, at whatever schema_version
// the test needs. Store.Create can only ever write CurrentVersion, so a stale
// row has to be planted.
func (e *suiteBEnv) suiteBInsertRow(t *testing.T, title string, version int32, pd, c, cu string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := e.pool.QueryRow(t.Context(),
		`INSERT INTO resumes (user_id, title, schema_version, personal_details, content, customization)
		 VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6::jsonb) RETURNING id`,
		e.userID, title, version, pd, c, cu,
	).Scan(&id); err != nil {
		t.Fatalf("seed resumes row at schema_version %d: %v", version, err)
	}
	return id
}

// suiteBSeedStaleRow plants a row at the synthetic old version carrying doc's
// content plus the v2 headline marker.
func (e *suiteBEnv) suiteBSeedStaleRow(t *testing.T, title string, doc schema.Resume) uuid.UUID {
	t.Helper()
	pd, c, cu := suiteBParts(t, doc)
	return e.suiteBInsertRow(t, title, suiteBOldVersion, suiteBAddHeadline(t, pd), c, cu)
}

// suiteBRow is a complete tamper-evident snapshot of one resumes row. xmin is
// the inserting/updating transaction id: any UPDATE at all changes it, even one
// that writes back identical values.
type suiteBRow struct {
	SchemaVersion   int32
	Revision        int64
	Title           string
	PersonalDetails string
	Content         string
	Customization   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Xmin            string
}

func (e *suiteBEnv) suiteBSnapshot(t *testing.T, id uuid.UUID) suiteBRow {
	t.Helper()
	var row suiteBRow
	if err := e.pool.QueryRow(t.Context(),
		`SELECT schema_version, revision, title, personal_details::text, content::text,
		        customization::text, created_at, updated_at, xmin::text
		   FROM resumes WHERE id = $1`, id,
	).Scan(&row.SchemaVersion, &row.Revision, &row.Title, &row.PersonalDetails, &row.Content,
		&row.Customization, &row.CreatedAt, &row.UpdatedAt, &row.Xmin); err != nil {
		t.Fatalf("snapshot resumes row: %v", err)
	}
	return row
}

// suiteBRequireUntouched fails when anything about the row moved.
func suiteBRequireUntouched(t *testing.T, what string, before, after suiteBRow) {
	t.Helper()
	moved := before.SchemaVersion != after.SchemaVersion ||
		before.Revision != after.Revision ||
		before.Title != after.Title ||
		before.PersonalDetails != after.PersonalDetails ||
		before.Content != after.Content ||
		before.Customization != after.Customization ||
		before.Xmin != after.Xmin ||
		!before.CreatedAt.Equal(after.CreatedAt) ||
		!before.UpdatedAt.Equal(after.UpdatedAt)
	if moved {
		t.Errorf("%s wrote to the row:\nbefore %+v\n after %+v", what, before, after)
	}
}

// suiteBHasHeadline reports whether the stored personalDetails still carries
// the synthetic v2 marker key.
func (e *suiteBEnv) suiteBHasHeadline(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var present bool
	if err := e.pool.QueryRow(t.Context(),
		`SELECT personal_details ? 'headline' FROM resumes WHERE id = $1`, id,
	).Scan(&present); err != nil {
		t.Fatalf("probe stored personalDetails: %v", err)
	}
	return present
}

// suiteBSyntheticProjector is the projector the live cases run against:
// current stays at docmigrate.CurrentVersion, and synthetic v2 rows walk down
// to it. down, when non-nil, replaces the well-behaved Down converter.
func suiteBSyntheticProjector(t *testing.T, down docmigrate.ConvertFunc) *docmigrate.Projector {
	t.Helper()
	current := docmigrate.CurrentVersion
	old := suiteBOldVersion

	editHeadline := func(doc json.RawMessage, set bool) (json.RawMessage, error) {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(doc, &m); err != nil {
			return nil, fmt.Errorf("synthetic converter: decode document: %w", err)
		}
		var pd map[string]json.RawMessage
		if err := json.Unmarshal(m["personalDetails"], &pd); err != nil {
			return nil, fmt.Errorf("synthetic converter: decode personalDetails: %w", err)
		}
		if set {
			marker, err := json.Marshal(suiteBHeadlineMarker)
			if err != nil {
				return nil, err
			}
			pd["headline"] = marker
		} else {
			delete(pd, "headline")
		}
		encoded, err := json.Marshal(pd)
		if err != nil {
			return nil, err
		}
		m["personalDetails"] = encoded
		target := current
		if set {
			target = old
		}
		m["schemaVersion"] = json.RawMessage(fmt.Sprintf("%d", target))
		return json.Marshal(m)
	}

	validate := func(version int32) docmigrate.ValidateFunc {
		return func(doc json.RawMessage) error {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(doc, &m); err != nil {
				return fmt.Errorf("synthetic v%d: decode document: %w", version, err)
			}
			for _, key := range []string{"schemaVersion", "personalDetails", "content", "customization"} {
				if _, present := m[key]; !present {
					return fmt.Errorf("synthetic v%d: missing %s", version, key)
				}
			}
			var got int32
			if err := json.Unmarshal(m["schemaVersion"], &got); err != nil {
				return fmt.Errorf("synthetic v%d: decode schemaVersion: %w", version, err)
			}
			if got != version {
				return fmt.Errorf("synthetic v%d: schemaVersion is %d", version, got)
			}
			var pd map[string]json.RawMessage
			if err := json.Unmarshal(m["personalDetails"], &pd); err != nil {
				return fmt.Errorf("synthetic v%d: decode personalDetails: %w", version, err)
			}
			_, hasHeadline := pd["headline"]
			if version == old && !hasHeadline {
				return fmt.Errorf("synthetic v%d: personalDetails.headline is required", version)
			}
			if version == current && hasHeadline {
				return fmt.Errorf("synthetic v%d: personalDetails.headline must be absent", version)
			}
			return nil
		}
	}

	downConverter := down
	if downConverter == nil {
		downConverter = func(doc json.RawMessage) (json.RawMessage, error) { return editHeadline(doc, false) }
	}
	p, err := docmigrate.NewProjector(
		map[int32]docmigrate.AdjacentConverters{
			current: {
				Up:   func(doc json.RawMessage) (json.RawMessage, error) { return editHeadline(doc, true) },
				Down: downConverter,
			},
		},
		map[int32]docmigrate.ValidateFunc{current: validate(current), old: validate(old)},
		[]int32{current, old}, []int32{current, old}, current,
	)
	if err != nil {
		t.Fatalf("build the synthetic projector: %v", err)
	}
	return p
}

// suiteBWaitForBlockedBy waits until want backends are queued behind gatePID.
//
// The queue is walked TRANSITIVELY on purpose. A second writer waiting for a
// row already contended by a first one is reported by pg_blocking_pids as
// blocked by that first WAITER (it holds the tuple lock), not by the gate --
// so a direct `gatePID = ANY (pg_blocking_pids(...))` test never sees past the
// first waiter, and the arrival order this test depends on could not be
// established at all.
//
// It is a bounded poll whose deadline is checked before every query; no sleep
// is ever used as the synchronization itself.
func (e *suiteBEnv) suiteBWaitForBlockedBy(t *testing.T, gatePID int32, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	const query = `
WITH RECURSIVE queue(pid) AS (
		SELECT $1::int
	UNION
		SELECT a.pid
		  FROM pg_stat_activity a
		  JOIN queue q ON q.pid = ANY (pg_blocking_pids(a.pid))
		 WHERE a.datname = current_database()
)
SELECT count(*) FROM queue WHERE pid <> $1::int`

	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			t.Fatalf("timed out waiting for %d backend(s) queued behind pid %d: %v", want, gatePID, err)
		}
		var blocked int
		if err := e.pool.QueryRow(ctx, query, gatePID).Scan(&blocked); err != nil {
			t.Fatalf("poll the lock queue: %v", err)
		}
		if blocked >= want {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
		}
	}
}

// suiteBGate holds a row lock so a test can order two writers deterministically.
type suiteBGate struct {
	pid     int32
	release func()
	commit  func(t *testing.T)
}

// suiteBLockRow opens a transaction that holds id's row lock until commit.
func (e *suiteBEnv) suiteBLockRow(t *testing.T, id uuid.UUID) *suiteBGate {
	t.Helper()
	ctx := t.Context()
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire gate connection: %v", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		t.Fatalf("begin gate transaction: %v", err)
	}
	var pid int32
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		conn.Release()
		t.Fatalf("read gate backend pid: %v", err)
	}
	var locked uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM resumes WHERE id = $1 FOR UPDATE`, id).Scan(&locked); err != nil {
		conn.Release()
		t.Fatalf("lock resumes row: %v", err)
	}
	gate := &suiteBGate{pid: pid}
	var once sync.Once
	gate.release = func() {
		once.Do(func() {
			if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil && !strings.Contains(err.Error(), "closed") {
				t.Logf("gate rollback: %v", err)
			}
			conn.Release()
		})
	}
	gate.commit = func(t *testing.T) {
		t.Helper()
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit gate transaction: %v", err)
		}
		once.Do(func() { conn.Release() })
	}
	t.Cleanup(gate.release)
	return gate
}

// TestSuiteB_Get_NeverWrites is the matrix's `TestGet_NeverWrites`: reads
// project but never write, under concurrency (spec §3 "migrate-on-read is
// projection-only (never writes during GET -- avoids revision bumps racing
// autosave)"; D18).
func TestSuiteB_Get_NeverWrites(t *testing.T) {
	env := suiteBSetup(t)
	st := resume.NewStore(env.pool, suiteBSyntheticProjector(t, nil))
	doc := suiteBDoc(t, "Never Written")
	id := env.suiteBSeedStaleRow(t, "purity", doc)

	before := env.suiteBSnapshot(t, id)
	if before.SchemaVersion != suiteBOldVersion {
		t.Fatalf("seeded schema_version is %d, want the synthetic old version %d", before.SchemaVersion, suiteBOldVersion)
	}

	got, err := st.Get(t.Context(), env.userID, id)
	if err != nil {
		t.Fatalf("Get on a stale row: %v", err)
	}
	if got.StoredSchemaVersion != suiteBOldVersion {
		t.Errorf("StoredSchemaVersion = %d, want %d (the row's own version, D18)", got.StoredSchemaVersion, suiteBOldVersion)
	}
	if got.Doc.SchemaVersion != int64(docmigrate.CurrentVersion) {
		t.Errorf("Doc.SchemaVersion = %d, want %d (always projected to current, D18)", got.Doc.SchemaVersion, docmigrate.CurrentVersion)
	}
	if got.Doc.PersonalDetails.Headline != nil {
		t.Errorf("Doc still carries the synthetic v2 headline %q: the read was not projected", *got.Doc.PersonalDetails.Headline)
	}
	if !env.suiteBHasHeadline(t, id) {
		t.Error("the stored row lost its v2 headline: Get rewrote storage")
	}

	const readers, iterations = 8, 25
	errCh := make(chan error, readers*iterations*2)
	var wg sync.WaitGroup
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				one, err := st.Get(t.Context(), env.userID, id)
				switch {
				case err != nil:
					errCh <- fmt.Errorf("concurrent Get: %w", err)
				case one.Doc.PersonalDetails.Headline != nil:
					errCh <- errors.New("concurrent Get returned an unprojected document")
				case one.StoredSchemaVersion != suiteBOldVersion:
					errCh <- fmt.Errorf("concurrent Get saw StoredSchemaVersion %d", one.StoredSchemaVersion)
				}
				list, err := st.List(t.Context(), env.userID)
				switch {
				case err != nil:
					errCh <- fmt.Errorf("concurrent List: %w", err)
				case len(list) != 1:
					errCh <- fmt.Errorf("concurrent List returned %d rows, want 1", len(list))
				case list[0].Doc.PersonalDetails.Headline != nil:
					errCh <- errors.New("concurrent List returned an unprojected document")
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	suiteBRequireUntouched(t, "concurrent Get/List", before, env.suiteBSnapshot(t, id))
}

// TestSuiteB_Backfill_LosesToConcurrentAutosave is write-path.md's sequence
// diagram, run for real: the backfill observes (vOld, rev R), an autosave
// commits rev R+1 first, and the backfill's CAS then matches zero rows.
func TestSuiteB_Backfill_LosesToConcurrentAutosave(t *testing.T) {
	suiteBRequireBackfillContract(t)
	env := suiteBSetup(t)
	st := resume.NewStore(env.pool, suiteBSyntheticProjector(t, nil))

	id := env.suiteBSeedStaleRow(t, "race", suiteBDoc(t, "Original"))
	before := env.suiteBSnapshot(t, id)

	// The gate holds the row lock so both writers queue behind it in arrival
	// order: the autosave enters the queue first and therefore commits first.
	gate := env.suiteBLockRow(t, id)

	autosaveDoc := suiteBDoc(t, "Autosave Winner")
	type saveOutcome struct {
		revision int64
		err      error
	}
	saveCh := make(chan saveOutcome, 1)
	go func() {
		rev, err := st.SaveDocument(t.Context(), env.userID, id, autosaveDoc, before.Revision)
		saveCh <- saveOutcome{revision: rev, err: err}
	}()
	env.suiteBWaitForBlockedBy(t, gate.pid, 1)

	type backfillOutcome struct {
		result resume.BackfillResult
		err    error
	}
	backfillCh := make(chan backfillOutcome, 1)
	go func() {
		res, err := st.BackfillOne(t.Context(), id)
		backfillCh <- backfillOutcome{result: res, err: err}
	}()
	env.suiteBWaitForBlockedBy(t, gate.pid, 2)

	gate.commit(t)

	save := <-saveCh
	if save.err != nil {
		t.Fatalf("the concurrent autosave failed: %v", save.err)
	}
	if want := before.Revision + 1; save.revision != want {
		t.Errorf("autosave returned revision %d, want %d", save.revision, want)
	}

	backfill := <-backfillCh
	if backfill.err != nil {
		t.Fatalf("BackfillOne returned an error instead of a race result: %v", backfill.err)
	}
	if backfill.result != resume.BackfillLostRace {
		t.Errorf("BackfillOne = %v, want BackfillLostRace (its observation went stale)", backfill.result)
	}

	after := env.suiteBSnapshot(t, id)
	if want := before.Revision + 1; after.Revision != want {
		t.Errorf("revision = %d, want %d (the autosave's, untouched by the losing backfill)", after.Revision, want)
	}
	if after.SchemaVersion != docmigrate.CurrentVersion {
		t.Errorf("schema_version = %d, want %d: the autosave persisted at the current version",
			after.SchemaVersion, docmigrate.CurrentVersion)
	}
	if env.suiteBHasHeadline(t, id) {
		t.Error("the stored document still carries the synthetic v2 headline: the autosave's document was not persisted intact")
	}
	got, err := st.Get(t.Context(), env.userID, id)
	if err != nil {
		t.Fatalf("Get after the race: %v", err)
	}
	if got.Doc.PersonalDetails.FullName == nil || *got.Doc.PersonalDetails.FullName != "Autosave Winner" {
		t.Errorf("stored document is %+v, want the autosave's document intact", got.Doc.PersonalDetails)
	}
}

// TestSuiteB_Autosave_AfterBackfill_NoSpurious412 is the exact user-visible
// property D12 exists to protect: a successful backfill changes neither
// revision nor updated_at, so an autosave holding the pre-backfill revision
// still succeeds instead of returning a spurious 412.
func TestSuiteB_Autosave_AfterBackfill_NoSpurious412(t *testing.T) {
	suiteBRequireBackfillContract(t)
	env := suiteBSetup(t)
	st := resume.NewStore(env.pool, suiteBSyntheticProjector(t, nil))

	id := env.suiteBSeedStaleRow(t, "d12", suiteBDoc(t, "Before Backfill"))
	before := env.suiteBSnapshot(t, id)

	getBefore, err := st.Get(t.Context(), env.userID, id)
	if err != nil {
		t.Fatalf("Get before backfill: %v", err)
	}
	docBefore, err := resume.AssembleCanonical(getBefore.Doc)
	if err != nil {
		t.Fatalf("assemble the pre-backfill document: %v", err)
	}

	result, err := st.BackfillOne(t.Context(), id)
	if err != nil {
		t.Fatalf("BackfillOne on a stale row: %v", err)
	}
	if result != resume.BackfillApplied {
		t.Fatalf("BackfillOne = %v, want BackfillApplied", result)
	}

	after := env.suiteBSnapshot(t, id)
	if after.SchemaVersion != docmigrate.CurrentVersion {
		t.Errorf("schema_version = %d, want %d after a successful backfill", after.SchemaVersion, docmigrate.CurrentVersion)
	}
	if after.Revision != before.Revision {
		t.Errorf("revision = %d, want %d: backfill must not bump revision (D12)", after.Revision, before.Revision)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at = %s, want %s: backfill must not touch updated_at (D12)", after.UpdatedAt, before.UpdatedAt)
	}
	if after.PersonalDetails == before.PersonalDetails {
		t.Error("backfill left personal_details byte-identical: the stored parts were not rewritten")
	}
	if env.suiteBHasHeadline(t, id) {
		t.Error("backfill did not rewrite the stored parts: the synthetic v2 headline survived")
	}

	getAfter, err := st.Get(t.Context(), env.userID, id)
	if err != nil {
		t.Fatalf("Get after backfill: %v", err)
	}
	docAfter, err := resume.AssembleCanonical(getAfter.Doc)
	if err != nil {
		t.Fatalf("assemble the post-backfill document: %v", err)
	}
	if !bytes.Equal(docBefore, docAfter) {
		t.Errorf("backfill is observable to readers (D12(i) forbids it):\nbefore %s\n after %s", docBefore, docAfter)
	}
	if getAfter.Revision != getBefore.Revision {
		t.Errorf("Get reports revision %d after backfill, want %d", getAfter.Revision, getBefore.Revision)
	}

	// The autosave still holds the revision it read BEFORE the backfill.
	newRevision, err := st.SaveDocument(t.Context(), env.userID, id, suiteBDoc(t, "After Backfill"), before.Revision)
	if err != nil {
		var mismatch *resume.RevisionMismatchError
		if errors.As(err, &mismatch) {
			t.Fatalf("backfill caused a spurious revision mismatch: the client's revision %d was rejected in favor of %d",
				before.Revision, mismatch.CurrentRevision)
		}
		t.Fatalf("SaveDocument after backfill: %v", err)
	}
	if want := before.Revision + 1; newRevision != want {
		t.Errorf("SaveDocument returned revision %d, want %d", newRevision, want)
	}
}

// TestSuiteB_Backfill_ConcurrentWithItself_AppliesOnce hammers one row with N
// concurrent backfills: at most one may apply, and the losers must be skips or
// retryable races -- never a second write, never an error.
func TestSuiteB_Backfill_ConcurrentWithItself_AppliesOnce(t *testing.T) {
	suiteBRequireBackfillContract(t)
	env := suiteBSetup(t)
	st := resume.NewStore(env.pool, suiteBSyntheticProjector(t, nil))

	id := env.suiteBSeedStaleRow(t, "self", suiteBDoc(t, "Concurrent Backfill"))
	before := env.suiteBSnapshot(t, id)

	const workers = 8
	results := make([]resume.BackfillResult, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[w], errs[w] = st.BackfillOne(t.Context(), id)
		}()
	}
	close(start)
	wg.Wait()

	applied := 0
	for w := 0; w < workers; w++ {
		if errs[w] != nil {
			t.Errorf("worker %d: BackfillOne returned an error: %v", w, errs[w])
			continue
		}
		switch results[w] {
		case resume.BackfillApplied:
			applied++
		case resume.BackfillSkippedCurrent, resume.BackfillLostRace:
		default:
			t.Errorf("worker %d: BackfillOne = %v, want Applied, SkippedCurrent or LostRace", w, results[w])
		}
	}
	if applied != 1 {
		t.Errorf("%d workers reported BackfillApplied, want exactly 1", applied)
	}

	after := env.suiteBSnapshot(t, id)
	if after.SchemaVersion != docmigrate.CurrentVersion {
		t.Errorf("schema_version = %d, want %d", after.SchemaVersion, docmigrate.CurrentVersion)
	}
	if after.Revision != before.Revision {
		t.Errorf("revision = %d, want %d: no backfill may bump revision (D12)", after.Revision, before.Revision)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at moved from %s to %s", before.UpdatedAt, after.UpdatedAt)
	}
	if env.suiteBHasHeadline(t, id) {
		t.Error("the row is still stale after a reported BackfillApplied")
	}
	if _, err := st.Get(t.Context(), env.userID, id); err != nil {
		t.Errorf("the row is unreadable after concurrent backfills: %v", err)
	}
}

// TestSuiteB_Backfill_NeverPersistsInvalidProjection covers the whole
// fail-closed arm of "a corrupt doc must surface, not silently persist": a
// converter that errors, one that emits invalid JSON, one whose output fails
// its own target schema, and one whose output passes the projector but violates
// the store's aggregate rules.
func TestSuiteB_Backfill_NeverPersistsInvalidProjection(t *testing.T) {
	suiteBRequireBackfillContract(t)

	cases := []struct {
		name string
		down docmigrate.ConvertFunc
	}{
		{
			name: "converter returns an error",
			down: func(json.RawMessage) (json.RawMessage, error) {
				return nil, errors.New("synthetic down converter refused")
			},
		},
		{
			name: "converter emits invalid JSON",
			down: func(json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(`{"schemaVersion":1,`), nil
			},
		},
		{
			name: "converter output fails its own target schema",
			down: func(doc json.RawMessage) (json.RawMessage, error) {
				// Relabels as current but keeps v2's headline, which the
				// current synthetic schema forbids.
				var m map[string]json.RawMessage
				if err := json.Unmarshal(doc, &m); err != nil {
					return nil, err
				}
				m["schemaVersion"] = json.RawMessage(fmt.Sprintf("%d", docmigrate.CurrentVersion))
				return json.Marshal(m)
			},
		},
		{
			name: "converter output violates the store's aggregate rules",
			down: func(doc json.RawMessage) (json.RawMessage, error) {
				var m map[string]json.RawMessage
				if err := json.Unmarshal(doc, &m); err != nil {
					return nil, err
				}
				var pd map[string]json.RawMessage
				if err := json.Unmarshal(m["personalDetails"], &pd); err != nil {
					return nil, err
				}
				delete(pd, "headline")
				encoded, err := json.Marshal(pd)
				if err != nil {
					return nil, err
				}
				m["personalDetails"] = encoded
				m["schemaVersion"] = json.RawMessage(fmt.Sprintf("%d", docmigrate.CurrentVersion))
				// layout.sections.main now names a section content does not
				// have: valid JSON, valid per the synthetic schema, and
				// rejected by the store's aggregate invariant.
				var cu map[string]json.RawMessage
				if decodeErr := json.Unmarshal(m["customization"], &cu); decodeErr != nil {
					return nil, decodeErr
				}
				cu["layout"] = json.RawMessage(`{"columns":1,"sections":{"main":["ghost"],"sidebar":[]}}`)
				encodedCU, err := json.Marshal(cu)
				if err != nil {
					return nil, err
				}
				m["customization"] = encodedCU
				return json.Marshal(m)
			},
		},
		{
			// Distinguishes the JSON-Schema layer from the aggregate layer:
			// a bad hex color is caught only by the embedded schema, so a
			// backfill that skipped that layer would persist a document no
			// user write could ever produce (D1/D16).
			name: "converter output violates the embedded JSON Schema",
			down: func(doc json.RawMessage) (json.RawMessage, error) {
				var m map[string]json.RawMessage
				if err := json.Unmarshal(doc, &m); err != nil {
					return nil, err
				}
				var pd map[string]json.RawMessage
				if err := json.Unmarshal(m["personalDetails"], &pd); err != nil {
					return nil, err
				}
				delete(pd, "headline")
				encoded, err := json.Marshal(pd)
				if err != nil {
					return nil, err
				}
				m["personalDetails"] = encoded
				m["schemaVersion"] = json.RawMessage(fmt.Sprintf("%d", docmigrate.CurrentVersion))
				var cu map[string]json.RawMessage
				if decodeErr := json.Unmarshal(m["customization"], &cu); decodeErr != nil {
					return nil, decodeErr
				}
				cu["colors"] = json.RawMessage(`{"primary":"NOT-A-HEX","text":"#1a1a1a","background":"#ffffff"}`)
				encodedCU, err := json.Marshal(cu)
				if err != nil {
					return nil, err
				}
				m["customization"] = encodedCU
				return json.Marshal(m)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := suiteBSetup(t)
			st := resume.NewStore(env.pool, suiteBSyntheticProjector(t, tc.down))
			id := env.suiteBSeedStaleRow(t, "invalid", suiteBDoc(t, "Untouched"))
			before := env.suiteBSnapshot(t, id)

			result, err := st.BackfillOne(t.Context(), id)
			if err == nil {
				t.Errorf("BackfillOne = %v with no error; want a closed failure", result)
			}
			suiteBRequireUntouched(t, "a failing backfill", before, env.suiteBSnapshot(t, id))
		})
	}
}

// TestSuiteB_Projection_UnknownStoredVersionFailsClosed: a stored version with
// no converter path must surface as an error from Get, never as a silently
// un-projected document, and the failing read must still not write.
func TestSuiteB_Projection_UnknownStoredVersionFailsClosed(t *testing.T) {
	cases := []struct {
		name          string
		projector     func(t *testing.T) *docmigrate.Projector
		storedVersion int32
	}{
		{
			name:          "production identity projector, stored above current",
			projector:     func(*testing.T) *docmigrate.Projector { return docmigrate.NewIdentityProjector() },
			storedVersion: 7,
		},
		{
			name:          "synthetic projector, stored beyond the declared range",
			projector:     func(t *testing.T) *docmigrate.Projector { return suiteBSyntheticProjector(t, nil) },
			storedVersion: 9,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := suiteBSetup(t)
			st := resume.NewStore(env.pool, tc.projector(t))
			pd, c, cu := suiteBParts(t, suiteBDoc(t, "Unknown Version"))
			id := env.suiteBInsertRow(t, "unknown", tc.storedVersion, pd, c, cu)
			before := env.suiteBSnapshot(t, id)

			got, err := st.Get(t.Context(), env.userID, id)
			if err == nil {
				t.Fatalf("Get returned a document for stored schema_version %d: %+v", tc.storedVersion, got.Doc)
			}
			if errors.Is(err, resume.ErrNotFound) {
				t.Errorf("Get reported ErrNotFound for an unprojectable row; a present-but-unmigratable row must not look deleted: %v", err)
			}
			if got.Doc.SchemaVersion != 0 || got.ID != uuid.Nil {
				t.Errorf("Get returned a partially populated Resume alongside its error: %+v", got)
			}
			suiteBRequireUntouched(t, "a failing Get", before, env.suiteBSnapshot(t, id))
		})
	}
}

// TestSuiteB_List_OneBadProjectionFailsAtomically: one unprojectable row makes
// the whole List fail. A silent omission or a partial result would make
// corruption look like user deletion (Task 8 Step 4, owner ruling).
func TestSuiteB_List_OneBadProjectionFailsAtomically(t *testing.T) {
	env := suiteBSetup(t)
	st := resume.NewStore(env.pool, docmigrate.NewIdentityProjector())
	ctx := t.Context()

	for _, title := range []string{"good one", "good two"} {
		if _, err := st.Create(ctx, env.userID, title, suiteBDoc(t, title)); err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
	}
	if list, err := st.List(ctx, env.userID); err != nil || len(list) != 2 {
		t.Fatalf("List of two healthy rows returned %d rows, err %v", len(list), err)
	}

	pd, c, cu := suiteBParts(t, suiteBDoc(t, "unprojectable"))
	badID := env.suiteBInsertRow(t, "bad", 9, pd, c, cu)
	before := env.suiteBSnapshot(t, badID)

	list, err := st.List(ctx, env.userID)
	if err == nil {
		t.Fatalf("List returned %d rows with no error; one unprojectable row must fail the whole call", len(list))
	}
	if list != nil {
		t.Errorf("List returned a partial result (%d rows) alongside its error", len(list))
	}
	suiteBRequireUntouched(t, "a failing List", before, env.suiteBSnapshot(t, badID))

	// The failure is scoped to the owner of the bad row, not global.
	other := suiteBSetup(t)
	otherStore := resume.NewStore(other.pool, docmigrate.NewIdentityProjector())
	if _, err := otherStore.Create(ctx, other.userID, "healthy", suiteBDoc(t, "healthy")); err != nil {
		t.Fatalf("create for the second user: %v", err)
	}
	if got, err := otherStore.List(ctx, other.userID); err != nil || len(got) != 1 {
		t.Errorf("another user's List returned %d rows, err %v; want 1, nil", len(got), err)
	}
}

// TestSuiteB_Backfill_TitleOnlyWriteCausesRetryableLostRace is B6: a
// title-only write bumps revision without touching schema_version, so the
// backfill loses the race while the row is still stale. The result must
// therefore be a retry signal, not "already done" -- a second attempt applies.
func TestSuiteB_Backfill_TitleOnlyWriteCausesRetryableLostRace(t *testing.T) {
	suiteBRequireBackfillContract(t)
	env := suiteBSetup(t)
	st := resume.NewStore(env.pool, suiteBSyntheticProjector(t, nil))

	id := env.suiteBSeedStaleRow(t, "before title", suiteBDoc(t, "B6"))
	before := env.suiteBSnapshot(t, id)

	gate := env.suiteBLockRow(t, id)

	type titleOutcome struct {
		revision int64
		err      error
	}
	titleCh := make(chan titleOutcome, 1)
	go func() {
		rev, err := st.SaveTitle(t.Context(), env.userID, id, "after title", before.Revision)
		titleCh <- titleOutcome{revision: rev, err: err}
	}()
	env.suiteBWaitForBlockedBy(t, gate.pid, 1)

	type backfillOutcome struct {
		result resume.BackfillResult
		err    error
	}
	backfillCh := make(chan backfillOutcome, 1)
	go func() {
		res, err := st.BackfillOne(t.Context(), id)
		backfillCh <- backfillOutcome{result: res, err: err}
	}()
	env.suiteBWaitForBlockedBy(t, gate.pid, 2)

	gate.commit(t)

	title := <-titleCh
	if title.err != nil {
		t.Fatalf("the concurrent SaveTitle failed: %v", title.err)
	}
	backfill := <-backfillCh
	if backfill.err != nil {
		t.Fatalf("BackfillOne returned an error instead of a race result: %v", backfill.err)
	}
	if backfill.result != resume.BackfillLostRace {
		t.Fatalf("BackfillOne = %v, want BackfillLostRace after a title-only write moved the revision", backfill.result)
	}

	stale := env.suiteBSnapshot(t, id)
	if stale.SchemaVersion != suiteBOldVersion {
		t.Errorf("schema_version = %d, want the still-stale %d: unlike the autosave case, a title write leaves the row behind",
			stale.SchemaVersion, suiteBOldVersion)
	}
	if want := before.Revision + 1; stale.Revision != want {
		t.Errorf("revision = %d, want %d (the title write's)", stale.Revision, want)
	}
	if stale.Title != "after title" {
		t.Errorf("title = %q, want %q", stale.Title, "after title")
	}

	// The lost race is a retry signal: a second attempt, with a fresh
	// observation, must apply.
	result, err := st.BackfillOne(t.Context(), id)
	if err != nil {
		t.Fatalf("second BackfillOne: %v", err)
	}
	if result != resume.BackfillApplied {
		t.Errorf("second BackfillOne = %v, want BackfillApplied; BackfillLostRace is never terminal", result)
	}
	final := env.suiteBSnapshot(t, id)
	if final.SchemaVersion != docmigrate.CurrentVersion {
		t.Errorf("schema_version = %d, want %d after the retry", final.SchemaVersion, docmigrate.CurrentVersion)
	}
	if final.Revision != stale.Revision {
		t.Errorf("revision = %d, want %d: the applied backfill must not bump it (D12)", final.Revision, stale.Revision)
	}
	if !final.UpdatedAt.Equal(stale.UpdatedAt) {
		t.Errorf("updated_at moved from %s to %s during the applied backfill", stale.UpdatedAt, final.UpdatedAt)
	}
}

// TestSuiteB_Backfill_AlreadyCurrentSkipsWithoutWriting: the third result the
// contract declares. An already-current row must be skipped with zero writes,
// not rewritten "harmlessly".
func TestSuiteB_Backfill_AlreadyCurrentSkipsWithoutWriting(t *testing.T) {
	suiteBRequireBackfillContract(t)
	env := suiteBSetup(t)
	st := resume.NewStore(env.pool, suiteBSyntheticProjector(t, nil))

	created, err := st.Create(t.Context(), env.userID, "already current", suiteBDoc(t, "Current"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before := env.suiteBSnapshot(t, created.ID)
	if before.SchemaVersion != docmigrate.CurrentVersion {
		t.Fatalf("Create wrote schema_version %d, want %d", before.SchemaVersion, docmigrate.CurrentVersion)
	}

	result, err := st.BackfillOne(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("BackfillOne on a current row: %v", err)
	}
	if result != resume.BackfillSkippedCurrent {
		t.Errorf("BackfillOne = %v, want BackfillSkippedCurrent", result)
	}
	suiteBRequireUntouched(t, "a skipped backfill", before, env.suiteBSnapshot(t, created.ID))
}

// TestSuiteB_WireAcceptPersistEmit_ProductionVersions walks the declared
// production versions end to end: accept a wire document, persist the complete
// document through the store at the current version, read it back, and emit it
// in a declared supported version. Task 8 Step 2 keeps the synthetic-v2 half of
// this out of the store deliberately (no fake v2 store codec); this is the part
// P2A can prove for real, and AC-SAVE-004 completes it over HTTP in P2B.
func TestSuiteB_WireAcceptPersistEmit_ProductionVersions(t *testing.T) {
	env := suiteBSetup(t)
	proj := docmigrate.NewIdentityProjector()
	st := resume.NewStore(env.pool, proj)
	ctx := t.Context()

	incoming, err := resume.AssembleCanonical(suiteBDoc(t, "Wire Client"))
	if err != nil {
		t.Fatalf("assemble the incoming document: %v", err)
	}

	accepted, version, err := proj.AcceptWire(incoming, docmigrate.CurrentVersion)
	if err != nil {
		t.Fatalf("AcceptWire at the declared current version: %v", err)
	}
	if version != docmigrate.CurrentVersion {
		t.Fatalf("AcceptWire returned version %d, want %d", version, docmigrate.CurrentVersion)
	}

	var doc schema.Resume
	if decodeErr := json.Unmarshal(accepted, &doc); decodeErr != nil {
		t.Fatalf("decode the accepted document: %v", decodeErr)
	}
	created, err := st.Create(ctx, env.userID, "wire", doc)
	if err != nil {
		t.Fatalf("persist the accepted document: %v", err)
	}
	if got := env.suiteBSnapshot(t, created.ID).SchemaVersion; got != docmigrate.CurrentVersion {
		t.Errorf("stored schema_version = %d, want %d", got, docmigrate.CurrentVersion)
	}

	readBack, err := st.Get(ctx, env.userID, created.ID)
	if err != nil {
		t.Fatalf("read the persisted document back: %v", err)
	}
	canonical, err := resume.AssembleCanonical(readBack.Doc)
	if err != nil {
		t.Fatalf("assemble the read-back document: %v", err)
	}
	emitted, err := proj.EmitWire(canonical, docmigrate.CurrentVersion)
	if err != nil {
		t.Fatalf("EmitWire at the declared current version: %v", err)
	}
	if !bytes.Equal(emitted, accepted) {
		t.Errorf("accept -> persist -> emit changed the document:\naccepted %s\n emitted %s", accepted, emitted)
	}
	if _, err := proj.EmitWire(canonical, docmigrate.CurrentVersion+1); !errors.Is(err, docmigrate.ErrUnsupportedVersion) {
		t.Errorf("EmitWire at an undeclared version: got %v, want ErrUnsupportedVersion", err)
	}
}

// TestSuiteB_Get_UnknownStoredFieldFailsClosed guards the strict decode on the
// read path: a stored part carrying a field the current Go type does not
// declare must fail the read, not be silently dropped and then written back
// lossily by the next save (Task 8 Step 3c).
func TestSuiteB_Get_UnknownStoredFieldFailsClosed(t *testing.T) {
	env := suiteBSetup(t)
	st := resume.NewStore(env.pool, docmigrate.NewIdentityProjector())
	ctx := t.Context()

	created, err := st.Create(ctx, env.userID, "strict", suiteBDoc(t, "Strict"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := env.pool.Exec(ctx,
		`UPDATE resumes SET personal_details = personal_details || '{"unknownField":1}'::jsonb WHERE id = $1`,
		created.ID,
	); err != nil {
		t.Fatalf("inject an unknown stored field: %v", err)
	}

	if got, err := st.Get(ctx, env.userID, created.ID); err == nil {
		t.Errorf("Get silently dropped an unknown stored field instead of failing: %+v", got.Doc.PersonalDetails)
	}
	if got, err := st.List(ctx, env.userID); err == nil {
		t.Errorf("List silently dropped an unknown stored field instead of failing: %d rows", len(got))
	}
}
