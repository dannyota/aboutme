// This adversarial suite covers write safety and cap concurrency. Every
// identifier is prefixed `wsa` so it does not collide with sibling suites in
// package resume_test.

package resume_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// wsaResumeCap is the maximum number of resumes per user.
const wsaResumeCap = 3

// wsaCapSQLState / wsaCapMessage are the exact contracted pair for a cap
// violation raised by the database trigger.
const (
	wsaCapSQLState = "23514"
	wsaCapMessage  = "resumes_user_cap_exceeded"
)

// wsaRawInsertSQL writes a resumes row with no store layer in the path at all:
// the point of the bypass tests is that the trigger, not Go code, is the
// enforcement point.
const wsaRawInsertSQL = `INSERT INTO resumes
	(user_id, title, schema_version, personal_details, content, customization)
	VALUES ($1, $2, $3, $4, $5, $6)`

// wsaBlockedOnHolderSQL answers, in ONE round trip, both halves of "is the
// contender waiting on the holder's row lock": whether the holder's pid blocks
// the contender's, and what the contender's backend is waiting on. Asking
// separately would sample two different instants, so a heavyweight lock wait
// observed by the second query need not be the one the first query saw. The
// wait-state columns are scalar subqueries so the row always comes back, even
// if the backend momentarily has no pg_stat_activity entry.
const wsaBlockedOnHolderSQL = `SELECT
	$1::int = ANY(pg_blocking_pids($2::int)),
	coalesce((SELECT wait_event_type FROM pg_stat_activity WHERE pid = $2::int), ''),
	coalesce((SELECT wait_event FROM pg_stat_activity WHERE pid = $2::int), '')`

// wsaUngrantedLocksSQL renders the locks a backend is waiting for, purely as
// failure diagnostics.
const wsaUngrantedLocksSQL = `SELECT coalesce(string_agg(
	format('%s on %s', locktype, coalesce(relation::regclass::text, '-')), ', '), '')
	FROM pg_locks WHERE pid = $1::int AND NOT granted`

var (
	// errWsaCallbackFailed is the sentinel a deliberately failing
	// idempotency callback returns, so the rollback assertions match on
	// identity.
	errWsaCallbackFailed = errors.New("wsa: callback failed after mutating")

	// errWsaRejectedCallbackRan makes any invocation on a different-hash
	// reuse observable through Execute's result without an external side
	// effect. The contracted path returns ErrIdempotencyKeyReuse before this
	// callback.
	errWsaRejectedCallbackRan = errors.New("wsa: rejected idempotency callback ran")
)

// wsaCustomizationJSON is packages/schema/fixtures/minimal.json's
// customization block, with the layout.sections.main array left as a format
// verb so a document can place exactly the content keys it declares (the
// aggregate invariant).
const wsaCustomizationJSON = `"customization":{` +
	`"font":{"family":"inter","baseSizePx":14},` +
	`"colors":{"primary":"#1a1a1a","text":"#1a1a1a","background":"#ffffff"},` +
	`"spacing":{"sectionGap":16,"entryGap":8,"lineHeight":1.4},` +
	`"heading":{"style":"normal","showRule":false},` +
	`"layout":{"columns":1,"sections":{"main":[%s],"sidebar":[]}},` +
	`"sectionDisplay":{"skill":{"style":"text"},"language":{"style":"text"}},` +
	`"pageFormat":"a4","dateFormat":"MM/YYYY"}`

// wsaUUID derives a stable, UUID-shaped id from n. Fixture IDs must be
// reproducible, so this suite never calls uuid.New.
func wsaUUID(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012d", n))
}

// wsaHash is a deterministic stand-in for the SHA-256 of a raw request body.
func wsaHash(body string) [32]byte {
	return sha256.Sum256([]byte(body))
}

// wsaHarness is one test's live-database handle set.
type wsaHarness struct {
	ctx  context.Context
	dsn  string
	pool *store.Pool
	q    *store.Queries
	st   *resume.Store
	idem *resume.IdempotencyStore
}

// wsaSetup brings up a migrated live database, a pool, and the two stores
// under test. It skips (or, under REQUIRE_TEST_DB=1, fails) without a DSN.
func wsaSetup(t *testing.T) *wsaHarness {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx := context.Background()
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	return &wsaHarness{
		ctx:  ctx,
		dsn:  dsn,
		pool: pool,
		q:    store.New(pool),
		st:   resume.NewStore(pool, docmigrate.NewIdentityProjector()),
		idem: resume.NewIdempotencyStore(pool),
	}
}

// wsaNewUser creates a fresh user with a random email, so concurrent workers
// on the shared test database never observe or disturb each other's rows.
func (h *wsaHarness) wsaNewUser(t *testing.T) uuid.UUID {
	t.Helper()
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("random email suffix: %v", err)
	}
	u, err := h.q.CreateUser(h.ctx, store.CreateUserParams{
		Email: "wsa-" + hex.EncodeToString(b[:]) + "@example.test",
		Name:  "write-safety suite",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

// wsaCount returns how many rows of table belong to userID.
func (h *wsaHarness) wsaCount(t *testing.T, table string, userID uuid.UUID) int64 {
	t.Helper()
	var n int64
	// table is a package-private constant string, never caller input.
	q := "SELECT count(*) FROM " + table + " WHERE user_id = $1"
	if err := h.pool.QueryRow(h.ctx, q, userID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// wsaSnapshot returns a full-row textual snapshot of every row of table owned
// by userID -- every column, not a projection, so "the write changed nothing"
// is provable rather than assumed.
func (h *wsaHarness) wsaSnapshot(t *testing.T, table string, userID uuid.UUID) string {
	t.Helper()
	var s string
	q := "SELECT coalesce(string_agg(x, '|' ORDER BY x), '') FROM " +
		"(SELECT r::text AS x FROM " + table + " r WHERE r.user_id = $1) s"
	if err := h.pool.QueryRow(h.ctx, q, userID).Scan(&s); err != nil {
		t.Fatalf("snapshot %s: %v", table, err)
	}
	return s
}

// wsaExecer is the shared shape of a pool, a connection, and a transaction:
// the raw-SQL bypass tests run the same INSERT through all three.
type wsaExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// wsaRawInsert inserts a resumes row directly, with no store layer involved.
func wsaRawInsert(ctx context.Context, x wsaExecer, userID uuid.UUID, title string, doc schema.Resume) error {
	personalDetails, content, customization, err := wsaSplit(doc)
	if err != nil {
		return err
	}
	_, err = x.Exec(ctx, wsaRawInsertSQL, userID, title,
		docmigrate.CurrentVersion, personalDetails, content, customization)
	return err
}

// wsaSplit decomposes a document into the three jsonb column values,
// using only the exported canonical marshal.
func wsaSplit(doc schema.Resume) (personalDetails, content, customization json.RawMessage, err error) {
	raw, err := resume.AssembleCanonical(doc)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("assemble canonical: %w", err)
	}
	var parts map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, nil, nil, fmt.Errorf("split canonical: %w", err)
	}
	return parts["personalDetails"], parts["content"], parts["customization"], nil
}

// wsaDecode turns a document literal into the typed shape the store takes.
func wsaDecode(t *testing.T, raw string) schema.Resume {
	t.Helper()
	var doc schema.Resume
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("decode fixture document: %v", err)
	}
	return doc
}

// wsaCanonicalLen is the byte length MaxDocumentBytes measures.
func wsaCanonicalLen(t *testing.T, doc schema.Resume) int {
	t.Helper()
	raw, err := resume.AssembleCanonical(doc)
	if err != nil {
		t.Fatalf("assemble canonical: %v", err)
	}
	return len(raw)
}

// wsaEmptyDocJSON is fixtures/minimal.json with a settable fullName, so each
// concurrent writer's document is distinguishable in the winner assertions.
func wsaEmptyDocJSON(fullName string) string {
	return `{"schemaVersion":2,"personalDetails":{"fullName":"` + fullName +
		`","details":[]},"content":{},` + fmt.Sprintf(wsaCustomizationJSON, "") + `}`
}

// wsaDoc is the smallest valid document, tagged with fullName.
func wsaDoc(t *testing.T, fullName string) schema.Resume {
	t.Helper()
	return wsaDecode(t, wsaEmptyDocJSON(fullName))
}

// wsaWorkDocJSON builds a one-section document from raw work-entry literals,
// placing that section exactly once in layout.
func wsaWorkDocJSON(entries []string) string {
	return `{"schemaVersion":2,"personalDetails":{"fullName":"Ada Lovelace","details":[]},` +
		`"content":{"work":{"sectionType":"work","entries":[` + strings.Join(entries, ",") + `]}},` +
		fmt.Sprintf(wsaCustomizationJSON, `"work"`) + `}`
}

// wsaWorkEntryJSON is one work entry with a deterministic id and an
// ASCII description of exactly descriptionLen bytes (no JSON escaping, so a
// character added is a byte added).
func wsaWorkEntryJSON(n, descriptionLen int) string {
	return `{"id":"` + wsaUUID(n).String() + `","description":"` +
		strings.Repeat("a", descriptionLen) + `"}`
}

// wsaDocOfCanonicalSize builds a valid-in-every-other-respect document whose
// canonical assembled form is exactly target bytes, so MaxDocumentBytes can be
// probed at the limit and at limit+1.
func wsaDocOfCanonicalSize(t *testing.T, target int) schema.Resume {
	t.Helper()
	const fullEntries = 32
	const fullLen = 16000 // below MaxRichTextBytes, so only the total bound bites
	build := func(lastLen int) schema.Resume {
		entries := make([]string, 0, fullEntries+1)
		for i := range fullEntries {
			entries = append(entries, wsaWorkEntryJSON(i, fullLen))
		}
		entries = append(entries, wsaWorkEntryJSON(fullEntries, lastLen))
		return wsaDecode(t, wsaWorkDocJSON(entries))
	}
	doc := build(0)
	diff := target - wsaCanonicalLen(t, doc)
	if diff < 0 || diff > schema.MaxRichTextBytes {
		t.Fatalf("cannot pad to %d canonical bytes: needed %d filler bytes in one entry", target, diff)
	}
	doc = build(diff)
	if got := wsaCanonicalLen(t, doc); got != target {
		t.Fatalf("built document of %d canonical bytes, want exactly %d", got, target)
	}
	return doc
}

// wsaRequireNotFound asserts the no-existence-oracle contract at one call
// site: ErrNotFound and nothing that distinguishes "not yours" from "absent".
func wsaRequireNotFound(t *testing.T, label string, err error) {
	t.Helper()
	if !errors.Is(err, resume.ErrNotFound) {
		t.Fatalf("%s: want ErrNotFound, got %#v (%v)", label, err, err)
	}
	var mismatch *resume.RevisionMismatchError
	if errors.As(err, &mismatch) {
		t.Fatalf("%s: returned a RevisionMismatchError, which is an existence oracle: %+v", label, mismatch)
	}
}

// TestCreate_Concurrent_ExactlyThreeSucceed proves the resume cap holds under
// 20-way concurrency through the store: exactly three creates commit, every
// other caller gets ErrCapExceeded, and the database ends with three rows.
func TestCreate_Concurrent_ExactlyThreeSucceed(t *testing.T) {
	h := wsaSetup(t)
	userID := h.wsaNewUser(t)

	const attempts = 20
	docs := make([]schema.Resume, attempts)
	for i := range attempts {
		docs[i] = wsaDoc(t, fmt.Sprintf("creator-%02d", i))
	}

	type outcome struct {
		got resume.Resume
		err error
	}
	results := make([]outcome, attempts)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := h.st.Create(h.ctx, userID, fmt.Sprintf("attempt-%02d", i), docs[i])
			results[i] = outcome{got: got, err: err}
		}()
	}
	close(start)
	wg.Wait()

	var succeeded, capExceeded int
	ids := map[uuid.UUID]bool{}
	for i, r := range results {
		switch {
		case r.err == nil:
			succeeded++
			if ids[r.got.ID] {
				t.Errorf("attempt %d: duplicate resume id %s returned by two creates", i, r.got.ID)
			}
			ids[r.got.ID] = true
			if r.got.Revision != 1 {
				t.Errorf("attempt %d: new resume revision = %d, want 1", i, r.got.Revision)
			}
		case errors.Is(r.err, resume.ErrCapExceeded):
			capExceeded++
		default:
			t.Errorf("attempt %d: unexpected error %#v (%v)", i, r.err, r.err)
		}
	}
	if succeeded != wsaResumeCap {
		t.Errorf("successful creates = %d, want exactly %d", succeeded, wsaResumeCap)
	}
	if capExceeded != attempts-wsaResumeCap {
		t.Errorf("ErrCapExceeded results = %d, want %d", capExceeded, attempts-wsaResumeCap)
	}
	if n := h.wsaCount(t, "resumes", userID); n != wsaResumeCap {
		t.Errorf("resumes rows = %d, want %d", n, wsaResumeCap)
	}
	listed, err := h.st.List(h.ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != wsaResumeCap {
		t.Errorf("List returned %d resumes, want %d", len(listed), wsaResumeCap)
	}
}

// TestCreate_RawSQLBypass_StillCapped proves the cap is the database trigger,
// not the Go store: a fourth row inserted with raw SQL still raises the exact
// SQLSTATE/message pair.
func TestCreate_RawSQLBypass_StillCapped(t *testing.T) {
	h := wsaSetup(t)
	userID := h.wsaNewUser(t)

	for i := range wsaResumeCap {
		if _, err := h.st.Create(h.ctx, userID, fmt.Sprintf("store-%d", i), wsaDoc(t, "Ada")); err != nil {
			t.Fatalf("store create %d: %v", i, err)
		}
	}

	err := wsaRawInsert(h.ctx, h.pool, userID, "raw-bypass", wsaDoc(t, "Ada"))
	if err == nil {
		t.Fatalf("raw INSERT of a 4th resume succeeded; the DB trigger is not enforcing the cap")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("raw INSERT error is %#v, want *pgconn.PgError", err)
	}
	if pgErr.Code != wsaCapSQLState || pgErr.Message != wsaCapMessage {
		t.Errorf("raw INSERT raised %s/%q, want %s/%q",
			pgErr.Code, pgErr.Message, wsaCapSQLState, wsaCapMessage)
	}
	if n := h.wsaCount(t, "resumes", userID); n != wsaResumeCap {
		t.Errorf("resumes rows = %d, want %d", n, wsaResumeCap)
	}
}

// TestCreate_ConcurrentRawSQLBypass_StillCapped exercises the trigger's own
// PERFORM ... FOR UPDATE with no store layer anywhere in the path: while
// one raw transaction holds an uncommitted third resume, a second raw
// transaction's fourth insert must BLOCK on the owner row rather than counting
// two committed rows and admitting itself. Deleting the trigger's lock line
// makes the second insert succeed and this test fail.
func TestCreate_ConcurrentRawSQLBypass_StillCapped(t *testing.T) {
	h := wsaSetup(t)
	userID := h.wsaNewUser(t)

	for i := range wsaResumeCap - 1 {
		if err := wsaRawInsert(h.ctx, h.pool, userID, fmt.Sprintf("seed-%d", i), wsaDoc(t, "Ada")); err != nil {
			t.Fatalf("seed raw insert %d: %v", i, err)
		}
	}

	holder := wsaConnect(t, h.dsn)
	contender := wsaConnect(t, h.dsn)
	holderPID := wsaBackendPID(h.ctx, t, holder)
	contenderPID := wsaBackendPID(h.ctx, t, contender)

	holderTx, err := holder.Begin(h.ctx)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			wsaRollback(h.ctx, t, holderTx, "holder")
		}
	}()
	if insertErr := wsaRawInsert(h.ctx, holderTx, userID, "third-uncommitted", wsaDoc(t, "Ada")); insertErr != nil {
		t.Fatalf("holder insert of the 3rd resume: %v", insertErr)
	}

	contenderTx, err := contender.Begin(h.ctx)
	if err != nil {
		t.Fatalf("begin contender tx: %v", err)
	}
	defer wsaRollback(h.ctx, t, contenderTx, "contender")

	// The document is built HERE, on the test goroutine: wsaDoc takes *testing.T
	// and can call t.Fatalf, which is only legal from the goroutine running the
	// test.
	contenderDoc := wsaDoc(t, "Ada")
	execCtx, cancelExec := context.WithTimeout(h.ctx, 60*time.Second)
	done := make(chan error, 1)
	var contenderWG sync.WaitGroup
	contenderWG.Add(1)
	go func() {
		defer contenderWG.Done()
		done <- wsaRawInsert(execCtx, contenderTx, userID, "fourth-blocked", contenderDoc)
	}()
	// Registered AFTER the two rollback defers, so it runs BEFORE them (LIFO):
	// the insert goroutine must be off contenderTx before anything rolls it
	// back or closes its connection underneath it.
	defer func() {
		cancelExec()
		contenderWG.Wait()
	}()

	// Poll from a third connection until the contender is observed blocked BY
	// the holder AND waiting on a heavyweight lock. Each poll is a network
	// round trip, so the loop paces itself: there is no sleep anywhere in this
	// test, and the exit condition is the observation itself, never elapsed
	// time.
	//
	// Both facts come from one query, at one instant. Only the trigger takes
	// FOR UPDATE on the owner row; the foreign key alone would take FOR KEY
	// SHARE, which does not conflict with itself, so a heavyweight lock wait
	// here is that PERFORM line and nothing else.
	waitCtx, cancelWait := context.WithTimeout(h.ctx, 30*time.Second)
	defer cancelWait()
	var blocked bool
	var waitEventType, waitEvent string
	for {
		select {
		case earlyErr := <-done:
			t.Fatalf("the 4th raw insert did not block on the owner row (returned %v); "+
				"the trigger's PERFORM ... FOR UPDATE is not serializing bypassing writers", earlyErr)
		default:
		}
		// Checked BEFORE the query: once waitCtx has expired the query below
		// fails with a context error, which would report the deadline as a
		// query failure and lose the pids this diagnostic names.
		if waitCtx.Err() != nil {
			t.Fatalf("timed out waiting for the 4th raw insert to block on the owner row; "+
				"holder pid %d, contender pid %d, last observed blocked=%t wait_event=%s/%s",
				holderPID, contenderPID, blocked, waitEventType, waitEvent)
		}
		if err := h.pool.QueryRow(waitCtx, wsaBlockedOnHolderSQL, holderPID, contenderPID).
			Scan(&blocked, &waitEventType, &waitEvent); err != nil {
			t.Fatalf("poll contender block state: %v", err)
		}
		if blocked && waitEventType == "Lock" {
			break
		}
	}

	var ungranted string
	if err := h.pool.QueryRow(waitCtx, wsaUngrantedLocksSQL, contenderPID).Scan(&ungranted); err != nil {
		t.Fatalf("read contender lock waits: %v", err)
	}
	t.Logf("contender pid %d is blocked by holder pid %d: wait_event=%s/%s, ungranted locks: %s",
		contenderPID, holderPID, waitEventType, waitEvent, ungranted)

	if err := holderTx.Commit(h.ctx); err != nil {
		t.Fatalf("commit holder tx: %v", err)
	}
	committed = true

	contenderErr := <-done
	if contenderErr == nil {
		t.Fatalf("the 4th raw insert succeeded once the 3rd committed; the cap was bypassed")
	}
	var pgErr *pgconn.PgError
	if !errors.As(contenderErr, &pgErr) {
		t.Fatalf("contender error is %#v, want *pgconn.PgError", contenderErr)
	}
	if pgErr.Code != wsaCapSQLState || pgErr.Message != wsaCapMessage {
		t.Errorf("contender raised %s/%q, want %s/%q",
			pgErr.Code, pgErr.Message, wsaCapSQLState, wsaCapMessage)
	}
	if n := h.wsaCount(t, "resumes", userID); n != wsaResumeCap {
		t.Errorf("resumes rows = %d, want %d", n, wsaResumeCap)
	}
}

// wsaConnect opens a dedicated connection, outside the pool, so a test can
// hold an open transaction without starving the pool.
func wsaConnect(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Logf("close connection: %v", err)
		}
	})
	return conn
}

// wsaBackendPID returns conn's server-side backend pid, the handle
// pg_blocking_pids works in.
func wsaBackendPID(ctx context.Context, t *testing.T, conn *pgx.Conn) int32 {
	t.Helper()
	var pid int32
	if err := conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("read backend pid: %v", err)
	}
	return pid
}

// wsaRollback rolls tx back, tolerating an already-finished transaction.
func wsaRollback(ctx context.Context, t *testing.T, tx pgx.Tx, label string) {
	t.Helper()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Logf("rollback %s tx: %v", label, err)
	}
}

// TestSaveDocument_ConcurrentSameRevision_OneWinner proves the revision CAS
// admits exactly one writer per revision: one caller advances the
// row to R+1 and every loser receives a *RevisionMismatchError carrying the
// winner's revision and the winner's document.
func TestSaveDocument_ConcurrentSameRevision_OneWinner(t *testing.T) {
	h := wsaSetup(t)
	userID := h.wsaNewUser(t)
	created, err := h.st.Create(h.ctx, userID, "cas-race", wsaDoc(t, "original"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Revision != 1 {
		t.Fatalf("created revision = %d, want 1", created.Revision)
	}

	const writers = 20
	names := make([]string, writers)
	docs := make([]schema.Resume, writers)
	for i := range writers {
		names[i] = fmt.Sprintf("writer-%02d", i)
		docs[i] = wsaDoc(t, names[i])
	}

	type outcome struct {
		revision int64
		err      error
	}
	results := make([]outcome, writers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rev, saveErr := h.st.SaveDocument(h.ctx, userID, created.ID, docs[i], created.Revision)
			results[i] = outcome{revision: rev, err: saveErr}
		}()
	}
	close(start)
	wg.Wait()

	winner := -1
	losers := 0
	for i, r := range results {
		if r.err == nil {
			if winner >= 0 {
				t.Fatalf("writers %d and %d both won the same-revision CAS", winner, i)
			}
			winner = i
			if r.revision != created.Revision+1 {
				t.Errorf("winner returned revision %d, want %d", r.revision, created.Revision+1)
			}
			continue
		}
		var mismatch *resume.RevisionMismatchError
		if !errors.As(r.err, &mismatch) {
			t.Errorf("writer %d: error is %#v (%v), want *RevisionMismatchError", i, r.err, r.err)
			continue
		}
		losers++
	}
	if winner < 0 {
		t.Fatalf("no writer won the same-revision CAS")
	}
	if losers != writers-1 {
		t.Errorf("losers = %d, want %d", losers, writers-1)
	}

	final, err := h.st.Get(h.ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("get after race: %v", err)
	}
	if final.Revision != created.Revision+1 {
		t.Errorf("final revision = %d, want %d (only one write may have committed)",
			final.Revision, created.Revision+1)
	}
	if got := wsaFullName(t, final.Doc); got != names[winner] {
		t.Errorf("stored fullName = %q, want the winner's %q", got, names[winner])
	}

	// Every loser's error must carry the WINNING document and revision --
	// this is the state an API conflict response serializes.
	for i, r := range results {
		if r.err == nil {
			continue
		}
		var mismatch *resume.RevisionMismatchError
		if !errors.As(r.err, &mismatch) {
			continue
		}
		if mismatch.CurrentRevision != final.Revision {
			t.Errorf("writer %d: mismatch.CurrentRevision = %d, want the winning %d",
				i, mismatch.CurrentRevision, final.Revision)
		}
		wsaRequireResumeEqual(t, fmt.Sprintf("writer %d mismatch Current", i), mismatch.Current, final)
	}
}

// wsaFullName reads personalDetails.fullName, the tag each test writes to tell
// concurrent documents apart.
func wsaFullName(t *testing.T, doc schema.Resume) string {
	t.Helper()
	if doc.PersonalDetails.FullName == nil {
		t.Fatalf("document has no personalDetails.fullName")
	}
	return *doc.PersonalDetails.FullName
}

// TestSaveDocument_MismatchCarriesWinningDoc proves the mismatch payload is
// byte-identical to a fresh Get so an API can serialize it into a conflict body,
// so a stale or partially-populated payload would be observable to clients.
func TestSaveDocument_MismatchCarriesWinningDoc(t *testing.T) {
	h := wsaSetup(t)
	userID := h.wsaNewUser(t)
	created, err := h.st.Create(h.ctx, userID, "mismatch-body", wsaDoc(t, "original"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	winningSlug := "wsa-" + strings.ReplaceAll(created.ID.String(), "-", "")[:24]
	const winningLng = "en-US"
	if _, updateErr := h.pool.Exec(h.ctx, `UPDATE resumes
		SET slug = $1, live = true, download_enabled = false,
			seo_geo_enabled = true, lng = $2
		WHERE id = $3 AND user_id = $4`, winningSlug, winningLng, created.ID, userID); updateErr != nil {
		t.Fatalf("seed non-default resume fields: %v", updateErr)
	}

	newRevision, err := h.st.SaveDocument(h.ctx, userID, created.ID, wsaDoc(t, "winner"), created.Revision)
	if err != nil {
		t.Fatalf("winning save: %v", err)
	}

	cases := []struct {
		name string
		call func() error
	}{
		{"SaveDocument", func() error {
			_, err := h.st.SaveDocument(h.ctx, userID, created.ID, wsaDoc(t, "loser"), created.Revision)
			return err
		}},
		{"SaveTitle", func() error {
			_, err := h.st.SaveTitle(h.ctx, userID, created.ID, "loser-title", created.Revision)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := h.wsaSnapshot(t, "resumes", userID)
			err := tc.call()
			var mismatch *resume.RevisionMismatchError
			if !errors.As(err, &mismatch) {
				t.Fatalf("stale %s: error is %#v (%v), want *RevisionMismatchError", tc.name, err, err)
			}
			if mismatch.CurrentRevision != newRevision {
				t.Errorf("CurrentRevision = %d, want %d", mismatch.CurrentRevision, newRevision)
			}
			fresh, err := h.st.Get(h.ctx, userID, created.ID)
			if err != nil {
				t.Fatalf("fresh Get after stale %s: %v", tc.name, err)
			}
			if fresh.Slug == nil || *fresh.Slug != winningSlug {
				t.Fatalf("fresh Slug = %v, want %q", fresh.Slug, winningSlug)
			}
			if fresh.Lng == nil || *fresh.Lng != winningLng {
				t.Fatalf("fresh Lng = %v, want %q", fresh.Lng, winningLng)
			}
			wsaRequireResumeEqual(t, "mismatch Current", mismatch.Current, fresh)
			if after := h.wsaSnapshot(t, "resumes", userID); after != before {
				t.Errorf("stale %s changed the winning row\n before: %s\n  after: %s", tc.name, before, after)
			}
		})
	}
}

// wsaRequireResumeEqual compares the complete domain value and separately
// byte-compares its canonical document. The whole-value comparison is
// intentionally reflection-based so adding a Resume field cannot silently
// weaken this acceptance assertion.
func wsaRequireResumeEqual(t *testing.T, label string, got, want resume.Resume) {
	t.Helper()
	gotCanonical, err := resume.AssembleCanonical(got.Doc)
	if err != nil {
		t.Fatalf("%s: assemble got document: %v", label, err)
	}
	wantCanonical, err := resume.AssembleCanonical(want.Doc)
	if err != nil {
		t.Fatalf("%s: assemble wanted document: %v", label, err)
	}
	if string(gotCanonical) != string(wantCanonical) {
		t.Errorf("%s: document bytes differ\n    got: %s\n   want: %s", label, gotCanonical, wantCanonical)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: complete Resume differs\n    got: %#v\n   want: %#v", label, got, want)
	}
}

// TestIdempotency_ConcurrentSameKey_OneMutationCommits proves same-user
// contenders serialize on the user row. Exactly one callback runs, exactly
// one mutation is observable, and every
// follower replays the committed response byte-for-byte from storage.
func TestIdempotency_ConcurrentSameKey_OneMutationCommits(t *testing.T) {
	h := wsaSetup(t)
	userID := h.wsaNewUser(t)

	const route = "POST /resumes"
	key := wsaUUID(9001)
	hash := wsaHash("body-A")
	responseBody := json.RawMessage(`{"created":true,"n":1}`)

	// Built on the test goroutine: Execute may invoke this callback on one of
	// the worker goroutines below, while wsaCreateParams/wsaDoc take *testing.T
	// and can call t.Fatalf only from the test goroutine.
	createParams := wsaCreateParams(t, userID, "idem-created", wsaDoc(t, "Ada"))
	mutate := func(qtx *store.Queries) (resume.StoredResponse, error) {
		if _, err := qtx.CreateResume(h.ctx, createParams); err != nil {
			return resume.StoredResponse{}, err
		}
		return resume.StoredResponse{Status: 201, Body: responseBody}, nil
	}

	const callers = 20
	type outcome struct {
		resp     resume.StoredResponse
		replayed bool
		err      error
	}
	results := make([]outcome, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp, replayed, err := h.idem.ExecuteForTest(h.ctx, userID, route, key, hash, mutate)
			results[i] = outcome{resp: resp, replayed: replayed, err: err}
		}()
	}
	close(start)
	wg.Wait()

	leaders := 0
	for i, r := range results {
		if r.err != nil {
			t.Errorf("caller %d: unexpected error %#v (%v)", i, r.err, r.err)
			continue
		}
		if !r.replayed {
			leaders++
		}
		if r.resp.Status != 201 {
			t.Errorf("caller %d: status = %d, want 201", i, r.resp.Status)
		}
	}
	if leaders != 1 {
		t.Errorf("callers reporting a fresh execution = %d, want exactly 1", leaders)
	}
	if n := h.wsaCount(t, "resumes", userID); n != 1 {
		t.Errorf("committed resume mutations = %d, want exactly 1", n)
	}
	if n := h.wsaCount(t, "idempotency_records", userID); n != 1 {
		t.Errorf("idempotency_records rows = %d, want exactly 1", n)
	}

	stored, err := h.q.GetIdempotencyRecord(h.ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: route, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("read stored record: %v", err)
	}
	if stored.ResponseStatus != 201 {
		t.Errorf("stored status = %d, want 201", stored.ResponseStatus)
	}
	for i, r := range results {
		if r.err != nil || !r.replayed {
			continue
		}
		if string(r.resp.Body) != string(stored.ResponseBody) {
			t.Errorf("caller %d: replayed body %s is not byte-identical to the stored row %s",
				i, r.resp.Body, stored.ResponseBody)
		}
	}
	// Every response, leader and follower alike, must be the SAME JSON value.
	wantValue := wsaJSONValue(t, responseBody)
	for i, r := range results {
		if r.err != nil {
			continue
		}
		if !wsaJSONEqual(t, r.resp.Body, wantValue) {
			t.Errorf("caller %d: response body %s is not the mutation's %s", i, r.resp.Body, responseBody)
		}
	}
}

// wsaCreateParams builds the generated create-resume arguments a
// transaction-scoped callback writes through.
func wsaCreateParams(t *testing.T, userID uuid.UUID, title string, doc schema.Resume) store.CreateResumeParams {
	t.Helper()
	personalDetails, content, customization, err := wsaSplit(doc)
	if err != nil {
		t.Fatalf("split document: %v", err)
	}
	return store.CreateResumeParams{
		UserID:          userID,
		Title:           title,
		SchemaVersion:   docmigrate.CurrentVersion,
		PersonalDetails: personalDetails,
		Content:         content,
		Customization:   customization,
	}
}

// wsaJSONValue decodes raw for semantic (not byte) comparison, since jsonb
// normalization is a contracted, allowed difference for replays.
func wsaJSONValue(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode json %s: %v", raw, err)
	}
	return v
}

// wsaJSONEqual reports whether raw decodes to the same JSON value as want.
func wsaJSONEqual(t *testing.T, raw json.RawMessage, want any) bool {
	t.Helper()
	return fmt.Sprintf("%#v", wsaJSONValue(t, raw)) == fmt.Sprintf("%#v", want)
}

// TestIdempotency_MutationErrorRollsBack proves a callback that performs a real
// transaction-scoped resume mutation and then
// fails leaves neither the mutation nor an idempotency record behind.
func TestIdempotency_MutationErrorRollsBack(t *testing.T) {
	const route = "POST /resumes"

	t.Run("create then fail", func(t *testing.T) {
		h := wsaSetup(t)
		userID := h.wsaNewUser(t)
		key := wsaUUID(9101)
		hash := wsaHash("body-A")
		failedParams := wsaCreateParams(t, userID, "rolled-back", wsaDoc(t, "Ada"))
		retryParams := wsaCreateParams(t, userID, "retry-committed", wsaDoc(t, "Grace"))

		before := h.wsaSnapshot(t, "resumes", userID)
		failedResp, failedReplayed, err := h.idem.ExecuteForTest(h.ctx, userID, route, key, hash,
			func(qtx *store.Queries) (resume.StoredResponse, error) {
				if _, createErr := qtx.CreateResume(h.ctx, failedParams); createErr != nil {
					return resume.StoredResponse{}, createErr
				}
				return resume.StoredResponse{Status: 201, Body: json.RawMessage(`{"ok":true}`)}, errWsaCallbackFailed
			})
		if !errors.Is(err, errWsaCallbackFailed) {
			t.Fatalf("Execute error = %#v (%v), want the callback's own error", err, err)
		}
		if failedReplayed || failedResp.Status != 0 || failedResp.Body != nil {
			t.Errorf("failed Execute returned response=%#v replayed=%t, want zero response and false",
				failedResp, failedReplayed)
		}
		if after := h.wsaSnapshot(t, "resumes", userID); after != before {
			t.Errorf("the failed callback's resume insert survived\n before: %s\n  after: %s", before, after)
		}
		if n := h.wsaCount(t, "resumes", userID); n != 0 {
			t.Errorf("resumes rows = %d, want 0", n)
		}
		if n := h.wsaCount(t, "idempotency_records", userID); n != 0 {
			t.Errorf("idempotency_records rows = %d, want 0", n)
		}

		// Nothing was recorded, so the same key+hash must run a fresh,
		// transaction-scoped mutation and commit its response with it.
		retryResp, replayed, retryErr := h.idem.ExecuteForTest(h.ctx, userID, route, key, hash,
			func(qtx *store.Queries) (resume.StoredResponse, error) {
				if _, createErr := qtx.CreateResume(h.ctx, retryParams); createErr != nil {
					return resume.StoredResponse{}, createErr
				}
				return resume.StoredResponse{Status: 201, Body: json.RawMessage(`{"ok":true}`)}, nil
			})
		if retryErr != nil {
			t.Fatalf("retry after rollback: %v", retryErr)
		}
		if replayed {
			t.Errorf("retry replayed a response, but the failed attempt must have left no record")
		}
		if retryResp.Status != 201 {
			t.Errorf("retry status = %d, want 201", retryResp.Status)
		}
		listed, listErr := h.st.List(h.ctx, userID)
		if listErr != nil {
			t.Fatalf("list committed retry mutation: %v", listErr)
		}
		if len(listed) != 1 || listed[0].Title != retryParams.Title {
			t.Errorf("committed retry resumes = %#v, want one titled %q", listed, retryParams.Title)
		}
		if n := h.wsaCount(t, "idempotency_records", userID); n != 1 {
			t.Errorf("idempotency_records after retry = %d, want 1", n)
		}
	})

	t.Run("cas title then fail", func(t *testing.T) {
		h := wsaSetup(t)
		userID := h.wsaNewUser(t)
		created, err := h.st.Create(h.ctx, userID, "keep-this-title", wsaDoc(t, "Ada"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		before := h.wsaSnapshot(t, "resumes", userID)

		key := wsaUUID(9102)
		failedResp, failedReplayed, err := h.idem.ExecuteForTest(h.ctx, userID, route, key, wsaHash("body-A"),
			func(qtx *store.Queries) (resume.StoredResponse, error) {
				if _, casErr := qtx.UpdateResumeTitleCAS(h.ctx, store.UpdateResumeTitleCASParams{
					ID: created.ID, UserID: userID, Revision: created.Revision, Title: "clobbered",
				}); casErr != nil {
					return resume.StoredResponse{}, casErr
				}
				// Prove the mutation really landed inside this transaction
				// before failing it, so the rollback assertion below is about a
				// real write and not a no-op.
				row, getErr := qtx.GetResumeForUser(h.ctx, store.GetResumeForUserParams{
					ID: created.ID, UserID: userID,
				})
				if getErr != nil {
					return resume.StoredResponse{}, getErr
				}
				if row.Title != "clobbered" {
					return resume.StoredResponse{}, fmt.Errorf(
						"wsa: title CAS did not apply inside the transaction (title=%q)", row.Title)
				}
				return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"ok":true}`)}, errWsaCallbackFailed
			})
		if !errors.Is(err, errWsaCallbackFailed) {
			t.Fatalf("Execute error = %#v (%v), want the callback's own error", err, err)
		}
		if failedReplayed || failedResp.Status != 0 || failedResp.Body != nil {
			t.Errorf("failed Execute returned response=%#v replayed=%t, want zero response and false",
				failedResp, failedReplayed)
		}
		if after := h.wsaSnapshot(t, "resumes", userID); after != before {
			t.Errorf("the failed callback's title CAS survived\n before: %s\n  after: %s", before, after)
		}
		if n := h.wsaCount(t, "idempotency_records", userID); n != 0 {
			t.Errorf("idempotency_records rows = %d, want 0", n)
		}
	})
}

// TestIdempotency_DifferentBodyNeverExecutes proves a key reused with a
// different request hash is rejected outright. Its callback never runs, and it
// writes nothing -- including when interleaved
// concurrently with valid replays of the same key.
func TestIdempotency_DifferentBodyNeverExecutes(t *testing.T) {
	h := wsaSetup(t)
	userID := h.wsaNewUser(t)

	const route = "POST /resumes"
	key := wsaUUID(9201)
	hashA := wsaHash("body-A")
	hashB := wsaHash("body-B")
	responseBody := json.RawMessage(`{"created":true}`)

	// The leader callback's parameters are built on the test goroutine: Execute
	// may invoke it on a worker goroutine below, while wsaCreateParams/wsaDoc
	// take *testing.T and can call t.Fatalf only from the test goroutine.
	leaderParams := wsaCreateParams(t, userID, "idem-leader", wsaDoc(t, "Ada"))

	leaderMutate := func(qtx *store.Queries) (resume.StoredResponse, error) {
		if _, err := qtx.CreateResume(h.ctx, leaderParams); err != nil {
			return resume.StoredResponse{}, err
		}
		return resume.StoredResponse{Status: 201, Body: responseBody}, nil
	}

	if _, replayed, err := h.idem.ExecuteForTest(h.ctx, userID, route, key, hashA, leaderMutate); err != nil {
		t.Fatalf("leader Execute: %v", err)
	} else if replayed {
		t.Fatalf("leader reported a replay on a fresh key")
	}

	resumesBefore := h.wsaSnapshot(t, "resumes", userID)
	recordsBefore := h.wsaSnapshot(t, "idempotency_records", userID)

	// Sequential rejection first: the plain contract, with no concurrency to
	// hide behind.
	rejectMutate := func(_ *store.Queries) (resume.StoredResponse, error) {
		return resume.StoredResponse{}, errWsaRejectedCallbackRan
	}
	rejectedResp, rejectedReplay, err := h.idem.ExecuteForTest(h.ctx, userID, route, key, hashB, rejectMutate)
	if !errors.Is(err, resume.ErrIdempotencyKeyReuse) {
		t.Fatalf("different-hash reuse returned %#v (%v), want ErrIdempotencyKeyReuse", err, err)
	}
	if rejectedReplay || rejectedResp.Status != 0 || rejectedResp.Body != nil {
		t.Errorf("different-hash reuse returned response=%#v replayed=%t, want zero response and false",
			rejectedResp, rejectedReplay)
	}

	// Now interleave 10 valid replays with 10 different-body reuses.
	const perArm = 10
	type outcome struct {
		resp     resume.StoredResponse
		replayed bool
		err      error
	}
	replayResults := make([]outcome, perArm)
	rejectResults := make([]outcome, perArm)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range perArm {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp, replayed, err := h.idem.ExecuteForTest(h.ctx, userID, route, key, hashA, leaderMutate)
			replayResults[i] = outcome{resp: resp, replayed: replayed, err: err}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp, replayed, err := h.idem.ExecuteForTest(h.ctx, userID, route, key, hashB, rejectMutate)
			rejectResults[i] = outcome{resp: resp, replayed: replayed, err: err}
		}()
	}
	close(start)
	wg.Wait()

	for i, r := range replayResults {
		if r.err != nil {
			t.Errorf("replay %d: unexpected error %#v (%v)", i, r.err, r.err)
			continue
		}
		if !r.replayed {
			t.Errorf("replay %d: replayed = false, want true", i)
		}
		if r.resp.Status != 201 {
			t.Errorf("replay %d: status = %d, want 201", i, r.resp.Status)
		}
		if !wsaJSONEqual(t, r.resp.Body, wsaJSONValue(t, responseBody)) {
			t.Errorf("replay %d: body %s is not the stored %s", i, r.resp.Body, responseBody)
		}
	}
	for i, r := range rejectResults {
		if !errors.Is(r.err, resume.ErrIdempotencyKeyReuse) {
			t.Errorf("reuse %d: error = %#v (%v), want ErrIdempotencyKeyReuse", i, r.err, r.err)
		}
		if r.replayed || r.resp.Status != 0 || r.resp.Body != nil {
			t.Errorf("reuse %d: response=%#v replayed=%t, want zero response and false",
				i, r.resp, r.replayed)
		}
	}
	if after := h.wsaSnapshot(t, "resumes", userID); after != resumesBefore {
		t.Errorf("resumes changed across replay/reuse traffic\n before: %s\n  after: %s", resumesBefore, after)
	}
	if after := h.wsaSnapshot(t, "idempotency_records", userID); after != recordsBefore {
		t.Errorf("idempotency_records changed across replay/reuse traffic\n before: %s\n  after: %s",
			recordsBefore, after)
	}
}

// TestValidation_RejectionWritesNothing proves the validation choke point
// sits before the transaction's writes: an invalid or oversized document
// rejected by Create or SaveDocument leaves every existing row byte-identical
// and the row count unchanged. The size cases are limit+1 at the transaction
// boundary, with the exactly-at-limit document accepted as the control.
func TestValidation_RejectionWritesNothing(t *testing.T) {
	overRichText := wsaWorkDocJSON([]string{wsaWorkEntryJSON(1, schema.MaxRichTextBytes+1)})
	duplicateEntryIDs := wsaWorkDocJSON([]string{wsaWorkEntryJSON(2, 4), wsaWorkEntryJSON(2, 5)})
	orphanLayoutKey := `{"schemaVersion":2,"personalDetails":{"fullName":"Ada","details":[]},` +
		`"content":{},` + fmt.Sprintf(wsaCustomizationJSON, `"work"`) + `}`
	reversedDates := wsaWorkDocJSON([]string{
		`{"id":"` + wsaUUID(3).String() + `","dates":{"start":{"y":2024},"end":{"y":2020},"present":false}}`,
	})

	cases := []struct {
		name string
		doc  func(t *testing.T) schema.Resume
	}{
		{"document total limit+1", func(t *testing.T) schema.Resume {
			return wsaDocOfCanonicalSize(t, resume.MaxDocumentBytes+1)
		}},
		{"rich text limit+1", func(t *testing.T) schema.Resume { return wsaDecode(t, overRichText) }},
		{"duplicate entry ids", func(t *testing.T) schema.Resume { return wsaDecode(t, duplicateEntryIDs) }},
		{"layout references a missing content key", func(t *testing.T) schema.Resume {
			return wsaDecode(t, orphanLayoutKey)
		}},
		{"reversed date range", func(t *testing.T) schema.Resume { return wsaDecode(t, reversedDates) }},
	}

	for _, tc := range cases {
		t.Run("Create/"+tc.name, func(t *testing.T) {
			h := wsaSetup(t)
			userID := h.wsaNewUser(t)
			// Two existing rows, so the snapshot compares real content, not
			// an empty set.
			for i := range wsaResumeCap - 1 {
				if _, err := h.st.Create(h.ctx, userID, fmt.Sprintf("existing-%d", i), wsaDoc(t, "Ada")); err != nil {
					t.Fatalf("seed create %d: %v", i, err)
				}
			}
			before := h.wsaSnapshot(t, "resumes", userID)

			_, err := h.st.Create(h.ctx, userID, "rejected", tc.doc(t))
			wsaRequireValidationError(t, "Create", err)
			if after := h.wsaSnapshot(t, "resumes", userID); after != before {
				t.Errorf("rejected Create changed stored rows\n before: %s\n  after: %s", before, after)
			}
			if n := h.wsaCount(t, "resumes", userID); n != wsaResumeCap-1 {
				t.Errorf("resumes rows = %d, want %d", n, wsaResumeCap-1)
			}
		})

		t.Run("SaveDocument/"+tc.name, func(t *testing.T) {
			h := wsaSetup(t)
			userID := h.wsaNewUser(t)
			created, err := h.st.Create(h.ctx, userID, "target", wsaDoc(t, "Ada"))
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			before := h.wsaSnapshot(t, "resumes", userID)

			_, err = h.st.SaveDocument(h.ctx, userID, created.ID, tc.doc(t), created.Revision)
			wsaRequireValidationError(t, "SaveDocument", err)
			if after := h.wsaSnapshot(t, "resumes", userID); after != before {
				t.Errorf("rejected SaveDocument changed stored rows\n before: %s\n  after: %s", before, after)
			}
			reread, err := h.st.Get(h.ctx, userID, created.ID)
			if err != nil {
				t.Fatalf("get after rejection: %v", err)
			}
			if reread.Revision != created.Revision {
				t.Errorf("revision moved to %d on a rejected write, want %d", reread.Revision, created.Revision)
			}
		})
	}

	t.Run("Create/document total exactly at limit is accepted", func(t *testing.T) {
		h := wsaSetup(t)
		userID := h.wsaNewUser(t)
		atLimit := wsaDocOfCanonicalSize(t, resume.MaxDocumentBytes)
		created, err := h.st.Create(h.ctx, userID, "at-limit", atLimit)
		if err != nil {
			t.Fatalf("a document of exactly MaxDocumentBytes (%d) was rejected: %v", resume.MaxDocumentBytes, err)
		}
		got, err := h.st.Get(h.ctx, userID, created.ID)
		if err != nil {
			t.Fatalf("get at-limit document: %v", err)
		}
		if n := wsaCanonicalLen(t, got.Doc); n != resume.MaxDocumentBytes {
			t.Errorf("round-tripped at-limit document is %d canonical bytes, want %d", n, resume.MaxDocumentBytes)
		}
	})
}

// wsaRequireValidationError asserts a rejection came from the validation
// pipeline rather than from the database or a nil error.
func wsaRequireValidationError(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: invalid document was accepted", label)
	}
	var invalid *resume.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("%s: error is %#v (%v), want *resume.ValidationError", label, err, err)
	}
	if len(invalid.Issues) == 0 {
		t.Errorf("%s: ValidationError carries no issues", label)
	}
}

// TestNoExistenceOracle_WrongUserSameAsNotFound proves the absence rule across
// every per-resume method: a real id owned by someone else is byte-identically
// indistinguishable from an id that never existed, and the CAS methods never
// leak a revision mismatch for a row the caller does not own.
func TestNoExistenceOracle_WrongUserSameAsNotFound(t *testing.T) {
	h := wsaSetup(t)
	ownerID := h.wsaNewUser(t)
	attackerID := h.wsaNewUser(t)

	victim, err := h.st.Create(h.ctx, ownerID, "victim", wsaDoc(t, "Ada"))
	if err != nil {
		t.Fatalf("create victim resume: %v", err)
	}
	ghostID := wsaUUID(9301)
	before := h.wsaSnapshot(t, "resumes", ownerID)

	type probeResult struct {
		value any
		err   error
	}
	probes := []struct {
		name string
		call func(id uuid.UUID) probeResult
	}{
		{"Get", func(id uuid.UUID) probeResult {
			got, err := h.st.Get(h.ctx, attackerID, id)
			return probeResult{value: got, err: err}
		}},
		{"SaveDocument/correct revision", func(id uuid.UUID) probeResult {
			revision, err := h.st.SaveDocument(h.ctx, attackerID, id, wsaDoc(t, "Mallory"), victim.Revision)
			return probeResult{value: revision, err: err}
		}},
		{"SaveDocument/stale revision", func(id uuid.UUID) probeResult {
			revision, err := h.st.SaveDocument(h.ctx, attackerID, id, wsaDoc(t, "Mallory"), victim.Revision+41)
			return probeResult{value: revision, err: err}
		}},
		{"SaveTitle/correct revision", func(id uuid.UUID) probeResult {
			revision, err := h.st.SaveTitle(h.ctx, attackerID, id, "mallory", victim.Revision)
			return probeResult{value: revision, err: err}
		}},
		{"SaveTitle/stale revision", func(id uuid.UUID) probeResult {
			revision, err := h.st.SaveTitle(h.ctx, attackerID, id, "mallory", victim.Revision+41)
			return probeResult{value: revision, err: err}
		}},
		{"Delete", func(id uuid.UUID) probeResult {
			return probeResult{err: h.st.Delete(h.ctx, attackerID, id)}
		}},
	}
	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			real := probe.call(victim.ID)
			ghost := probe.call(ghostID)
			wsaRequireNotFound(t, probe.name+" (real id, wrong owner)", real.err)
			wsaRequireNotFound(t, probe.name+" (nonexistent id)", ghost.err)
			if !reflect.DeepEqual(real.value, ghost.value) {
				t.Errorf("%s: wrong-owner return value %#v differs from nonexistent-id return value %#v",
					probe.name, real.value, ghost.value)
			}
			if real.err.Error() != ghost.err.Error() {
				t.Errorf("%s: wrong-owner error %q differs from nonexistent-id error %q",
					probe.name, real.err.Error(), ghost.err.Error())
			}
			if reflect.TypeOf(real.err) != reflect.TypeOf(ghost.err) {
				t.Errorf("%s: wrong-owner error type %T differs from nonexistent-id type %T",
					probe.name, real.err, ghost.err)
			}
		})
	}

	if after := h.wsaSnapshot(t, "resumes", ownerID); after != before {
		t.Errorf("the victim's row changed while another user probed it\n before: %s\n  after: %s", before, after)
	}
	if n := h.wsaCount(t, "resumes", attackerID); n != 0 {
		t.Errorf("attacker owns %d resumes, want 0", n)
	}
}
