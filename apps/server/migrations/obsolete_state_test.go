// Migration 00017 (ADR 0028 "History"): the unused showcase review columns and
// approval index, the authorization-code binding-fill trigger, and the four
// indexes 00015 superseded are gone; Down restores them.
package migrations_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/migrations"
)

var (
	// obsoleteShowcaseColumns are the review columns 00011 left unused.
	obsoleteShowcaseColumns = []string{"review_key", "reviewed_key", "review_outcome", "reviewed_at", "first_listed_at"}
	// obsoleteShowcaseChecks are the checks on those columns.
	obsoleteShowcaseChecks = []string{
		"resume_showcase_review_key_check", "resume_showcase_reviewed_key_check",
		"resume_showcase_review_outcome_check", "resume_showcase_review_columns_check",
		"resume_showcase_first_listed_check",
	}
	// obsoleteIndexes are the approval index and the four indexes 00015
	// superseded.
	obsoleteIndexes = []string{
		"resume_showcase_listing_idx", "auth_email_jobs_claim_idx", "auth_email_jobs_outcome_idx",
		"sessions_user_id_active_idx", "oauth_grants_client_live_idx",
	}
	// supersedingIndexes serve the queries the four superseded indexes served.
	supersedingIndexes = []string{
		"auth_email_jobs_claim_order_idx", "auth_email_jobs_finished_idx",
		"sessions_user_revoked_idx", "oauth_grants_client_revoked_idx",
	}
)

const (
	showcaseColumnsQuery = `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'resume_showcase' AND column_name = ANY($1::text[])`
	showcaseChecksQuery = `SELECT count(*) FROM pg_constraint
		WHERE conrelid = 'public.resume_showcase'::regclass AND conname = ANY($1::text[])`
	indexesQuery = `SELECT count(*) FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = ANY($1::text[])`
	bindingTriggerQuery = `SELECT count(*) FROM pg_trigger
		WHERE tgname = 'oauth_authorization_codes_fill_second_factor_bindings'`
	bindingFunctionQuery = `SELECT count(*) FROM pg_proc
		WHERE proname = 'fill_oauth_authorization_code_second_factor_bindings'`
)

// queryCountNamed runs a count query whose only argument is a list of names.
func queryCountNamed(ctx context.Context, t *testing.T, db sqlExecer, query string, names []string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(ctx, query, names).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// queryCount runs a count query with no arguments.
func queryCount(ctx context.Context, t *testing.T, db sqlExecer, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(ctx, query).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// requireObsoleteStateGone fails unless every object 00017 drops is absent and
// the superseding indexes remain.
func requireObsoleteStateGone(ctx context.Context, t *testing.T, db sqlExecer) {
	t.Helper()
	if n := queryCountNamed(ctx, t, db, showcaseColumnsQuery, obsoleteShowcaseColumns); n != 0 {
		t.Errorf("showcase review columns = %d, want 0", n)
	}
	if n := queryCountNamed(ctx, t, db, showcaseChecksQuery, obsoleteShowcaseChecks); n != 0 {
		t.Errorf("showcase review checks = %d, want 0", n)
	}
	if n := queryCountNamed(ctx, t, db, indexesQuery, obsoleteIndexes); n != 0 {
		t.Errorf("obsolete indexes = %d, want 0", n)
	}
	if n := queryCount(ctx, t, db, bindingTriggerQuery); n != 0 {
		t.Errorf("binding-fill triggers = %d, want 0", n)
	}
	if n := queryCount(ctx, t, db, bindingFunctionQuery); n != 0 {
		t.Errorf("binding-fill functions = %d, want 0", n)
	}
	if n := queryCountNamed(ctx, t, db, indexesQuery, supersedingIndexes); n != len(supersedingIndexes) {
		t.Errorf("superseding indexes = %d, want %d", n, len(supersedingIndexes))
	}
}

// requireNotNullViolation fails unless err is a NOT NULL violation on column.
func requireNotNullViolation(t *testing.T, err error, column string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("error = %v (%T), want a NOT NULL violation on %s", err, err, column)
	}
	if pgErr.Code != "23502" || pgErr.ColumnName != column {
		t.Errorf("error = %s on %q, want 23502 on %q", pgErr.Code, pgErr.ColumnName, column)
	}
}

// At head nothing 00017 drops remains, and with no trigger to fill them an
// authorization code needs its grant and epoch from the caller.
func TestObsoleteStateMigrationDropsUnusedState(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	requireObsoleteStateGone(ctx, t, tx)

	userID := createTestUser(ctx, t, tx)
	clientID := insertOAuthClient(ctx, t, tx)
	grantID := insertOAuthGrant(ctx, t, tx, userID, clientID)
	insert := func(sp pgx.Tx, grant *uuid.UUID, epoch *int64) error {
		_, err := sp.Exec(ctx, `
			INSERT INTO oauth_authorization_codes (
				code_digest, client_id, user_id, scopes, code_challenge, redirect_uri,
				created_at, expires_at, grant_id, auth_epoch
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`, validOAuthDigest(), clientID, userID, oauthScopesBoth, oauthChallenge, oauthRedirectURI,
			oauthNow, oauthNow.Add(oauthCodeTTL), grant, epoch)
		return err
	}
	epoch := int64(0)
	err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, nil, &epoch) })
	requireNotNullViolation(t, err, "grant_id")
	err = withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, &grantID, nil) })
	requireNotNullViolation(t, err, "auth_epoch")
	if err = withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, &grantID, &epoch) }); err != nil {
		t.Fatalf("insert a bound code: %v", err)
	}
}

// An opt-in keeps its row through 00017. Down restores every dropped object,
// with the review columns as 00011 left them: the previous release's showcase
// read works again, the trigger fills an omitted code binding again, and
// 00011's own Down still runs.
func TestObsoleteStateMigrationUpDownUp(t *testing.T) {
	t.Parallel()
	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()
	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := conn.Close(context.Background()); closeErr != nil {
			t.Errorf("close connection: %v", closeErr)
		}
	})

	if _, err = provider.UpTo(ctx, 16); err != nil {
		t.Fatalf("UpTo(16) error: %v", err)
	}
	resumeID := seedLiveResume(ctx, t, db, "obsolete-state")
	if _, err = db.ExecContext(ctx, `
		INSERT INTO resume_showcase (resume_id, requested_at, role, card_version) VALUES ($1, now(), 'backend', $2)`,
		resumeID, showcaseHexB); err != nil {
		t.Fatalf("insert showcase row: %v", err)
	}

	if _, err = provider.UpTo(ctx, 17); err != nil {
		t.Fatalf("UpTo(17) error: %v", err)
	}
	requireObsoleteStateGone(ctx, t, conn)
	var role, cardVersion string
	if err = conn.QueryRow(ctx, `SELECT role, card_version FROM resume_showcase WHERE resume_id = $1`, resumeID).
		Scan(&role, &cardVersion); err != nil {
		t.Fatalf("read the opt-in after up: %v", err)
	}
	if role != "backend" || cardVersion != showcaseHexB {
		t.Errorf("opt-in after up = %q %q, want backend %s", role, cardVersion, showcaseHexB)
	}

	if _, err = provider.DownTo(ctx, 16); err != nil {
		t.Fatalf("DownTo(16) error: %v", err)
	}
	if n := queryCountNamed(ctx, t, conn, showcaseColumnsQuery, obsoleteShowcaseColumns); n != len(obsoleteShowcaseColumns) {
		t.Errorf("showcase review columns after down = %d, want %d", n, len(obsoleteShowcaseColumns))
	}
	if n := queryCountNamed(ctx, t, conn, showcaseChecksQuery, obsoleteShowcaseChecks); n != len(obsoleteShowcaseChecks) {
		t.Errorf("showcase review checks after down = %d, want %d", n, len(obsoleteShowcaseChecks))
	}
	if n := queryCountNamed(ctx, t, conn, indexesQuery, obsoleteIndexes); n != len(obsoleteIndexes) {
		t.Errorf("obsolete indexes after down = %d, want %d", n, len(obsoleteIndexes))
	}
	if n := queryCount(ctx, t, conn, bindingTriggerQuery); n != 1 {
		t.Errorf("binding-fill triggers after down = %d, want 1", n)
	}

	// The previous release's generated read of an opt-in.
	var (
		reviewKey                 string
		reviewedKey, outcome      *string
		reviewedAt, firstListedAt *time.Time
	)
	if err = conn.QueryRow(ctx, `
		SELECT review_key, reviewed_key, review_outcome, reviewed_at, first_listed_at
		FROM resume_showcase WHERE resume_id = $1`, resumeID).
		Scan(&reviewKey, &reviewedKey, &outcome, &reviewedAt, &firstListedAt); err != nil {
		t.Fatalf("read the review columns after down: %v", err)
	}
	if reviewKey != "0000000000000000" || reviewedKey != nil || outcome != nil || reviewedAt != nil || firstListedAt != nil {
		t.Errorf("review columns after down = %q %v %v %v %v, want the default key and nothing else",
			reviewKey, reviewedKey, outcome, reviewedAt, firstListedAt)
	}

	// The restored trigger fills an omitted binding again.
	userID := createTestUser(ctx, t, conn)
	clientID := insertOAuthClient(ctx, t, conn)
	grantID := insertOAuthGrant(ctx, t, conn, userID, clientID)
	var filled uuid.UUID
	if err = conn.QueryRow(ctx, `
		INSERT INTO oauth_authorization_codes (
			code_digest, client_id, user_id, scopes, code_challenge, redirect_uri, created_at, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING grant_id
	`, validOAuthDigest(), clientID, userID, oauthScopesBoth, oauthChallenge,
		oauthRedirectURI, oauthNow, oauthNow.Add(oauthCodeTTL)).Scan(&filled); err != nil {
		t.Fatalf("insert an unbound code after down: %v", err)
	}
	if filled != grantID {
		t.Errorf("filled grant_id = %s, want %s", filled, grantID)
	}

	if _, err = provider.DownTo(ctx, 10); err != nil {
		t.Fatalf("DownTo(10) error: %v", err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatalf("Up() after down error: %v", err)
	}
	requireObsoleteStateGone(ctx, t, conn)
}
