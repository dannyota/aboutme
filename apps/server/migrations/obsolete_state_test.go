// Migration 00017: the authorization-code binding-fill trigger and the four
// indexes 00015 superseded are gone, Down restores them, and the showcase
// review columns that earlier releases read stay.
package migrations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/migrations"
)

var (
	// obsoleteIndexes are the four indexes 00015 superseded.
	obsoleteIndexes = []string{
		"auth_email_jobs_claim_idx", "auth_email_jobs_outcome_idx",
		"sessions_user_id_active_idx", "oauth_grants_client_live_idx",
	}
	// supersedingIndexes serve the queries the four superseded indexes served.
	supersedingIndexes = []string{
		"auth_email_jobs_claim_order_idx", "auth_email_jobs_finished_idx",
		"sessions_user_revoked_idx", "oauth_grants_client_revoked_idx",
	}
	// keptShowcaseColumns are the unused review columns earlier releases read.
	keptShowcaseColumns = []string{"review_key", "reviewed_key", "review_outcome", "reviewed_at", "first_listed_at"}
)

const (
	indexesQuery = `SELECT count(*) FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = ANY($1::text[])`
	showcaseColumnsQuery = `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'resume_showcase' AND column_name = ANY($1::text[])`
	bindingTriggerQuery = `SELECT count(*) FROM pg_trigger
		WHERE tgname = 'oauth_authorization_codes_fill_second_factor_bindings'`
	bindingFunctionQuery = `SELECT count(*) FROM pg_proc
		WHERE proname = 'fill_oauth_authorization_code_second_factor_bindings'`
	// releasedShowcaseRead is GetResumeShowcase as v0.8.28 generated it.
	releasedShowcaseRead = `SELECT resume_id, requested_at, role, review_key, card_version, template_id, reviewed_key, review_outcome, reviewed_at, first_listed_at FROM resume_showcase
WHERE resume_id = $1`
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
	if n := queryCountNamed(ctx, t, db, indexesQuery, obsoleteIndexes); n != 0 {
		t.Errorf("superseded indexes = %d, want 0", n)
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

// At head nothing 00017 drops remains, the previous release's showcase read
// still runs, and with no trigger to fill them an authorization code needs its
// grant and epoch from the caller.
func TestObsoleteStateMigrationDropsUnusedState(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	requireObsoleteStateGone(ctx, t, tx)

	if n := queryCountNamed(ctx, t, tx, showcaseColumnsQuery, keptShowcaseColumns); n != len(keptShowcaseColumns) {
		t.Errorf("showcase review columns = %d, want %d", n, len(keptShowcaseColumns))
	}
	err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, releasedShowcaseRead, uuid.New()).Scan()
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("v0.8.28 showcase read = %v, want no rows", err)
	}

	userID := createTestUser(ctx, t, tx)
	clientID := insertOAuthClient(ctx, t, tx)
	grantID := insertOAuthGrant(ctx, t, tx, userID, clientID)
	insert := func(sp pgx.Tx, grant *uuid.UUID, epoch *int64) error {
		_, execErr := sp.Exec(ctx, `
			INSERT INTO oauth_authorization_codes (
				code_digest, client_id, user_id, scopes, code_challenge, redirect_uri,
				created_at, expires_at, grant_id, auth_epoch
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`, validOAuthDigest(), clientID, userID, oauthScopesBoth, oauthChallenge, oauthRedirectURI,
			oauthNow, oauthNow.Add(oauthCodeTTL), grant, epoch)
		return execErr
	}
	epoch := int64(0)
	err = withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, nil, &epoch) })
	requireNotNullViolation(t, err, "grant_id")
	err = withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, &grantID, nil) })
	requireNotNullViolation(t, err, "auth_epoch")
	if err = withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, &grantID, &epoch) }); err != nil {
		t.Fatalf("insert a bound code: %v", err)
	}
}

// Down restores the four indexes and the trigger, which fills an omitted code
// binding again, and Up drops them once more.
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

	if _, err = provider.UpTo(ctx, 17); err != nil {
		t.Fatalf("UpTo(17) error: %v", err)
	}
	requireObsoleteStateGone(ctx, t, conn)

	if _, err = provider.DownTo(ctx, 16); err != nil {
		t.Fatalf("DownTo(16) error: %v", err)
	}
	if n := queryCountNamed(ctx, t, conn, indexesQuery, obsoleteIndexes); n != len(obsoleteIndexes) {
		t.Errorf("superseded indexes after down = %d, want %d", n, len(obsoleteIndexes))
	}
	if n := queryCount(ctx, t, conn, bindingTriggerQuery); n != 1 {
		t.Errorf("binding-fill triggers after down = %d, want 1", n)
	}
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

	if _, err = provider.Up(ctx); err != nil {
		t.Fatalf("Up() after down error: %v", err)
	}
	requireObsoleteStateGone(ctx, t, conn)
}
