package accountapi

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

type operatorFixture struct {
	ctx         context.Context
	pool        *store.Pool
	queries     *store.Queries
	service     *Service
	user        store.User
	foreign     store.User
	slugs       []string
	resumeIDs   []uuid.UUID
	photoKeys   []string
	foreignSlug string
	foreignID   uuid.UUID
}

// newOperatorFixture seeds an account with three resumes (one unpublished,
// two live, each with a photo), two showcase rows, a session, and the cascade
// rows of a full account, beside a second account that must stay untouched.
func newOperatorFixture(t *testing.T) *operatorFixture {
	t.Helper()
	ctx := context.Background()
	pool, err := store.NewPool(ctx, testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	projector := docmigrate.NewIdentityProjector()
	resumes := resume.NewStore(pool, projector)
	f := &operatorFixture{ctx: ctx, pool: pool, queries: q}

	f.user, err = q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.com", Name: "Delete Me"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	f.foreign, err = q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.com", Name: "Keep Me"})
	if err != nil {
		t.Fatalf("CreateUser(foreign): %v", err)
	}
	foreignResume, err := resumes.Create(ctx, f.foreign.ID, "Foreign", validDeletionDocument(nil))
	if err != nil {
		t.Fatalf("Create(foreign resume): %v", err)
	}
	f.foreignID = foreignResume.ID
	f.foreignSlug = "keep-" + uuid.NewString()[:8]
	if _, execErr := pool.Exec(ctx, `UPDATE resumes SET slug=$2, live=true WHERE id=$1`, f.foreignID, f.foreignSlug); execErr != nil {
		t.Fatalf("configure foreign resume: %v", execErr)
	}

	for i := range 3 {
		created, createErr := resumes.Create(ctx, f.user.ID, "Owned", validDeletionDocument(nil))
		if createErr != nil {
			t.Fatalf("Create resume: %v", createErr)
		}
		key, keyErr := media.NewPhotoKey(bytes.NewReader(bytes.Repeat([]byte{byte(i + 1)}, 16)), created.ID, "png")
		if keyErr != nil {
			t.Fatalf("NewPhotoKey: %v", keyErr)
		}
		doc := validDeletionDocument(&schema.Photo{Key: key})
		if _, saveErr := resumes.SaveDocument(ctx, f.user.ID, created.ID, doc, created.Revision); saveErr != nil {
			t.Fatalf("SaveDocument: %v", saveErr)
		}
		slug := "report-" + uuid.NewString()[:8]
		if _, execErr := pool.Exec(ctx, `UPDATE resumes SET slug=$2, live=$3 WHERE id=$1`, created.ID, slug, i > 0); execErr != nil {
			t.Fatalf("configure resume: %v", execErr)
		}
		f.slugs = append(f.slugs, slug)
		f.resumeIDs = append(f.resumeIDs, created.ID)
		f.photoKeys = append(f.photoKeys, key)
	}
	for _, id := range f.resumeIDs[1:] {
		if insertErr := q.InsertResumeShowcase(ctx, store.InsertResumeShowcaseParams{
			ResumeID: id, RequestedAt: time.Now().UTC(), CardVersion: "0123456789abcdef",
		}); insertErr != nil {
			t.Fatalf("InsertResumeShowcase: %v", insertErr)
		}
	}
	registrationTime := time.Now().UTC()
	reg, err := q.CreatePasswordRegistration(ctx, store.CreatePasswordRegistrationParams{
		Email: f.user.Email, Name: "Pending", EncodedHash: []byte("hash"), TokenDigest: deletionDigest(),
		CreatedAt: registrationTime, ExpiresAt: registrationTime.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePasswordRegistration: %v", err)
	}
	seedAccountCascadeRows(ctx, t, pool, q, f.user, &reg.ID, 41)
	seedAccountCascadeRows(ctx, t, pool, q, f.foreign, nil, 73)

	f.service, err = NewOperator(OperatorDependencies{Pool: pool, Projector: projector})
	if err != nil {
		t.Fatalf("NewOperator: %v", err)
	}
	sort.Strings(f.slugs)
	return f
}

func (f *operatorFixture) assertNothingDeleted(t *testing.T) {
	t.Helper()
	if _, err := f.queries.GetUserByID(f.ctx, f.user.ID); err != nil {
		t.Fatalf("user was deleted: %v", err)
	}
	var resumes, showcase, sessions int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM resumes WHERE user_id=$1`, f.user.ID).Scan(&resumes); err != nil || resumes != 3 {
		t.Fatalf("resumes = %d, %v, want 3", resumes, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM resume_showcase WHERE resume_id = ANY($1)`, f.resumeIDs).Scan(&showcase); err != nil || showcase != 2 {
		t.Fatalf("showcase rows = %d, %v, want 2", showcase, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, f.user.ID).Scan(&sessions); err != nil || sessions == 0 {
		t.Fatalf("sessions = %d, %v, want the account's sessions kept", sessions, err)
	}
	for _, slug := range f.slugs {
		if _, err := f.queries.GetSlugTombstoneForUpdate(f.ctx, slug); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("tombstone for %q = %v, want none", slug, err)
		}
	}
}

func TestAccountBySlugReadsOnlyIDAndSlugs(t *testing.T) {
	f := newOperatorFixture(t)
	account, err := f.service.AccountBySlug(f.ctx, f.slugs[1])
	if err != nil {
		t.Fatalf("AccountBySlug: %v", err)
	}
	if account.UserID != f.user.ID || !equalStrings(account.Slugs, f.slugs) {
		t.Fatalf("account = %+v, want user %s with slugs %v", account, f.user.ID, f.slugs)
	}
	f.assertNothingDeleted(t)
	if _, err := f.service.AccountBySlug(f.ctx, "unknown-"+uuid.NewString()[:8]); !errors.Is(err, ErrSlugNotFound) {
		t.Fatalf("unknown slug error = %v, want ErrSlugNotFound", err)
	}
}

// The operator deletion removes what a self-delete removes, and reports the
// account's own slugs.
func TestDeleteAccountBySlugMatchesSelfDelete(t *testing.T) {
	f := newOperatorFixture(t)
	before, err := f.queries.GetPublicState(f.ctx)
	if err != nil {
		t.Fatalf("GetPublicState: %v", err)
	}
	account, err := f.service.DeleteAccountBySlug(f.ctx, f.slugs[0], f.user.ID)
	if err != nil {
		t.Fatalf("DeleteAccountBySlug: %v", err)
	}
	if account.UserID != f.user.ID || !equalStrings(account.Slugs, f.slugs) {
		t.Fatalf("deleted account = %+v, want user %s with slugs %v", account, f.user.ID, f.slugs)
	}
	if _, err := f.queries.GetUserByID(f.ctx, f.user.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted user lookup error = %v, want no rows", err)
	}
	if _, err := f.queries.GetUserByID(f.ctx, f.foreign.ID); err != nil {
		t.Fatalf("foreign user was changed: %v", err)
	}
	assertAccountCascadeCounts(f.ctx, t, f.pool, f.user.ID, 0)
	assertAccountCascadeCounts(f.ctx, t, f.pool, f.foreign.ID, 1)
	var showcase int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM resume_showcase WHERE resume_id = ANY($1)`, f.resumeIDs).Scan(&showcase); err != nil || showcase != 0 {
		t.Fatalf("showcase rows after delete = %d, %v, want 0", showcase, err)
	}
	for _, slug := range f.slugs {
		if _, err := f.queries.GetSlugTombstoneForUpdate(f.ctx, slug); err != nil {
			t.Errorf("tombstone %q missing: %v", slug, err)
		}
	}
	for i, id := range f.resumeIDs {
		if _, err := f.queries.GetMediaDeletionJobByObjectKey(f.ctx, store.GetMediaDeletionJobByObjectKeyParams{
			ResumeID: id, ObjectKey: f.photoKeys[i],
		}); err != nil {
			t.Errorf("media deletion job for resume %s: %v", id, err)
		}
	}
	var audits int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM lifecycle_audit_events WHERE kind='account_deleted' AND media_job_id IS NULL AND occurred_at >= $1`, f.user.CreatedAt).Scan(&audits); err != nil || audits < 1 {
		t.Fatalf("account deletion audit count = %d, %v", audits, err)
	}
	// The command runs outside the server process and leaves the durable
	// discovery generation, which the server's in-memory fences track, alone.
	if after, err := f.queries.GetPublicState(f.ctx); err != nil || after.DiscoveryGeneration != before.DiscoveryGeneration {
		t.Fatalf("discovery state = (%+v, %v), want generation %d", after, err, before.DiscoveryGeneration)
	}
	if _, err := f.service.AccountBySlug(f.ctx, f.slugs[0]); !errors.Is(err, ErrSlugNotFound) {
		t.Fatalf("tombstoned slug error = %v, want ErrSlugNotFound", err)
	}
}

func TestDeleteAccountBySlugRemovesSessionsAndGrants(t *testing.T) {
	f := newOperatorFixture(t)
	sessions := auth.NewSessionManagerWithPool(f.pool)
	if _, _, err := sessions.Issue(f.ctx, f.user.ID, "ua", "203.0.113.2"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := f.service.DeleteAccountBySlug(f.ctx, f.slugs[2], f.user.ID); err != nil {
		t.Fatalf("DeleteAccountBySlug: %v", err)
	}
	for _, table := range []string{"sessions", "oauth_grants", "oauth_tokens"} {
		var count int
		if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table+" WHERE user_id=$1", f.user.ID).Scan(&count); err != nil || count != 0 {
			t.Errorf("%s rows after delete = %d, %v, want 0", table, count, err)
		}
	}
}

func TestDeleteAccountBySlugRefusesAnotherAccount(t *testing.T) {
	f := newOperatorFixture(t)
	if _, err := f.service.DeleteAccountBySlug(f.ctx, f.slugs[0], f.foreign.ID); !errors.Is(err, ErrSlugMismatch) {
		t.Fatalf("wrong owner error = %v, want ErrSlugMismatch", err)
	}
	if _, err := f.service.DeleteAccountBySlug(f.ctx, f.slugs[0], uuid.New()); !errors.Is(err, ErrSlugMismatch) {
		t.Fatalf("unknown user error = %v, want ErrSlugMismatch", err)
	}
	if _, err := f.queries.GetUserByID(f.ctx, f.foreign.ID); err != nil {
		t.Fatalf("named account was deleted: %v", err)
	}
	f.assertNothingDeleted(t)
}

func TestDeleteAccountBySlugRefusesUnknownAndTombstonedSlug(t *testing.T) {
	f := newOperatorFixture(t)
	tombstoned := "gone-" + uuid.NewString()[:8]
	if _, err := f.queries.InsertSlugTombstone(f.ctx, store.InsertSlugTombstoneParams{Slug: tombstoned, ReleasedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("InsertSlugTombstone: %v", err)
	}
	for _, slug := range []string{tombstoned, "unknown-" + uuid.NewString()[:8]} {
		if _, err := f.service.DeleteAccountBySlug(f.ctx, slug, f.user.ID); !errors.Is(err, ErrSlugNotFound) {
			t.Fatalf("slug %q error = %v, want ErrSlugNotFound", slug, err)
		}
	}
	f.assertNothingDeleted(t)
}

// A slug that moves to another account between the preflight and the
// deletion transaction is caught by the owner check under the slug lock.
func TestDeleteAccountBySlugRefusesSlugThatMovedBeforeCommit(t *testing.T) {
	f := newOperatorFixture(t)
	reported := f.slugs[0]
	moved := false
	f.service.afterPrepare = func() {
		if moved {
			return
		}
		moved = true
		tx, err := f.pool.Begin(f.ctx)
		if err != nil {
			t.Errorf("begin move: %v", err)
			return
		}
		defer rollbackUnlessClosed(t, tx)()
		if _, err = tx.Exec(f.ctx, `UPDATE resumes SET slug = NULL, live = false WHERE slug = $1`, reported); err != nil {
			t.Errorf("clear slug: %v", err)
			return
		}
		if _, err = tx.Exec(f.ctx, `UPDATE resumes SET slug = $2 WHERE id = $1`, f.foreignID, reported); err != nil {
			t.Errorf("move slug: %v", err)
			return
		}
		if err = tx.Commit(f.ctx); err != nil {
			t.Errorf("commit move: %v", err)
		}
	}
	if _, err := f.service.DeleteAccountBySlug(f.ctx, reported, f.user.ID); !errors.Is(err, ErrSlugMismatch) {
		t.Fatalf("moved slug error = %v, want ErrSlugMismatch", err)
	}
	if !moved {
		t.Fatal("the slug never moved")
	}
	if _, err := f.queries.GetUserByID(f.ctx, f.user.ID); err != nil {
		t.Fatalf("user was deleted: %v", err)
	}
	if _, err := f.queries.GetUserByID(f.ctx, f.foreign.ID); err != nil {
		t.Fatalf("slug holder was deleted: %v", err)
	}
	var resumes int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM resumes WHERE user_id=$1`, f.user.ID).Scan(&resumes); err != nil || resumes != 3 {
		t.Fatalf("resumes = %d, %v, want 3", resumes, err)
	}
}

func TestNewOperatorRejectsMissingDependencies(t *testing.T) {
	t.Parallel()
	if _, err := NewOperator(OperatorDependencies{}); err == nil {
		t.Fatal("NewOperator accepted a nil pool")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A lost commit answer is not a rollback: when the follow-up read still finds
// the user the result is unknown, not "nothing deleted".
func TestDeleteAccountBySlugReportsUnknownCommitOutcome(t *testing.T) {
	f := newOperatorFixture(t)
	f.service.commitTx = func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.Rollback(ctx); err != nil {
			return err
		}
		return errors.New("connection reset")
	}
	if _, err := f.service.DeleteAccountBySlug(f.ctx, f.slugs[0], f.user.ID); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("error = %v, want ErrOutcomeUnknown", err)
	}
	f.assertNothingDeleted(t)
}

// When the commit lands but its answer is lost, the absent user proves it.
func TestDeleteAccountBySlugProvesCommitAfterLostAnswer(t *testing.T) {
	f := newOperatorFixture(t)
	f.service.commitTx = func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return errors.New("connection reset")
	}
	account, err := f.service.DeleteAccountBySlug(f.ctx, f.slugs[0], f.user.ID)
	if err != nil || account.UserID != f.user.ID {
		t.Fatalf("DeleteAccountBySlug = %+v, %v, want the committed deletion", account, err)
	}
	if _, err := f.queries.GetUserByID(f.ctx, f.user.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("user lookup = %v, want no rows", err)
	}
}

// One field decides the path: a request needs a session or a reported slug,
// never both and never neither.
func TestDeletionRequestNeedsExactlyOneAuthority(t *testing.T) {
	f := newOperatorFixture(t)
	for name, req := range map[string]deletionRequest{
		"both":    {userID: f.user.ID, session: &store.Session{ID: uuid.New(), UserID: f.user.ID}, reportedSlug: f.slugs[0]},
		"neither": {userID: f.user.ID},
	} {
		if _, err := f.service.deleteAccountForUser(f.ctx, req); !errors.Is(err, errDeletionRequestInvalid) {
			t.Errorf("%s: error = %v, want errDeletionRequestInvalid", name, err)
		}
	}
	f.assertNothingDeleted(t)
}
