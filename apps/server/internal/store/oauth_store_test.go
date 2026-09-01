// Phase PM task 1's live-database tests for the OAuth agent-access store
// contract: single-use code consumption under two real transactions, single
// live grant per (user, client), family revocation, rotation lineage that
// cannot cross families, the joined bearer lookup, the throttled last-used
// touch, and the bounded GC and cleanup queries — all through the generated
// query layer (store.New) so the sqlc contract and the M1–M3 database bounds
// are proven together.
package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// The generated *Queries must satisfy the agent-access surface exactly.
var _ store.OAuthQueries = (*store.Queries)(nil)

var (
	// oauthStoreNow is the fixed instant fixtures are created at, so expiry
	// and throttle boundaries name their offsets instead of racing wall time.
	oauthStoreNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	// oauthGCEpoch is a deliberately distant epoch used only by the bounded
	// GC and cleanup tests. Fixtures created there can never be confused with
	// the committed fixtures other tests in this shared database leave behind,
	// so a batch-bound assertion counts exactly this test's own rows.
	oauthGCEpoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
)

const (
	oauthStoreCodeTTL     = 60 * time.Second
	oauthStoreAccessTTL   = time.Hour
	oauthStoreFamilyTTL   = 30 * 24 * time.Hour
	oauthStoreLastUsedGap = 60 * time.Second
	oauthStoreScopesBoth  = "resumes:read resumes:write"
	oauthStoreScopesRead  = "resumes:read"
	oauthStoreChallenge   = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	oauthStoreRedirect    = "http://127.0.0.1:20090/callback"
)

// oauthStoreRedirectURIs is the canonical one-entry redirect list every client
// fixture registers.
func oauthStoreRedirectURIs() json.RawMessage {
	return json.RawMessage(`["` + oauthStoreRedirect + `"]`)
}

// newOAuthStoreTx returns a rolled-back transaction plus the underlying pool,
// matching newPasswordStoreTx. Every query goes through the transaction so
// repeated runs never accumulate rows in the shared database.
func newOAuthStoreTx(t *testing.T) (context.Context, *pgxpool.Pool, pgx.Tx, *store.Queries) {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("Rollback() error: %v", err)
		}
	})
	return ctx, pool, tx, store.New(tx)
}

func newOAuthStoreUser(ctx context.Context, t *testing.T, q *store.Queries) uuid.UUID {
	t.Helper()
	u, err := q.CreateUser(ctx, store.CreateUserParams{
		Email: uuid.NewString() + "@example.com",
		Name:  "OAuth Store Test",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u.ID
}

func newOAuthStoreClient(ctx context.Context, t *testing.T, q *store.Queries, createdAt time.Time) store.OAuthClient {
	t.Helper()
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{
		ClientName:   "Agent " + uuid.NewString(),
		RedirectURIs: oauthStoreRedirectURIs(),
		CreatedAt:    createdAt,
	})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	return client
}

func newOAuthStoreGrant(
	ctx context.Context, t *testing.T, q *store.Queries, userID, clientID uuid.UUID, scopes string,
) store.OAuthGrant {
	t.Helper()
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
		UserID:    userID,
		ClientID:  clientID,
		Scopes:    scopes,
		CreatedAt: oauthStoreNow,
	})
	if err != nil {
		t.Fatalf("UpsertOAuthGrant: %v", err)
	}
	return grant
}

// newOAuthStoreToken issues a first-of-family token, the shape the code
// exchange creates.
func newOAuthStoreToken(
	ctx context.Context, t *testing.T, q *store.Queries,
	grant store.OAuthGrant, kind string, familyID uuid.UUID, createdAt time.Time,
) store.OAuthToken {
	t.Helper()
	ttl := oauthStoreFamilyTTL
	if kind == "access" {
		ttl = oauthStoreAccessTTL
	}
	token, err := q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{
		TokenDigest:     uniqueTokenDigest(),
		Kind:            kind,
		FamilyID:        familyID,
		ClientID:        grant.ClientID,
		UserID:          grant.UserID,
		GrantID:         grant.ID,
		CreatedAt:       createdAt,
		ExpiresAt:       createdAt.Add(ttl),
		FamilyExpiresAt: createdAt.Add(oauthStoreFamilyTTL),
	})
	if err != nil {
		t.Fatalf("CreateOAuthToken(%s): %v", kind, err)
	}
	return token
}

func newOAuthStoreCode(
	ctx context.Context, t *testing.T, q *store.Queries, userID, clientID uuid.UUID, createdAt time.Time,
) store.OAuthAuthorizationCode {
	t.Helper()
	code, err := q.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{
		CodeDigest:    uniqueTokenDigest(),
		ClientID:      clientID,
		UserID:        userID,
		Scopes:        oauthStoreScopesBoth,
		CodeChallenge: oauthStoreChallenge,
		RedirectURI:   oauthStoreRedirect,
		CreatedAt:     createdAt,
	})
	if err != nil {
		t.Fatalf("CreateOAuthAuthorizationCode: %v", err)
	}
	return code
}

// ---------------------------------------------------------------------------
// Code consumption is single-success under two concurrent transactions.
// ---------------------------------------------------------------------------

func TestOAuthCodeConsumeHasOneWinnerUnderConcurrentTransactions(t *testing.T) {
	ctx, pool, _, _ := newOAuthStoreTx(t)
	seed := store.New(pool) // committed so both transactions can see the code

	userID := newOAuthStoreUser(ctx, t, seed)
	client := newOAuthStoreClient(ctx, t, seed, oauthStoreNow)
	code := newOAuthStoreCode(ctx, t, seed, userID, client.ID, oauthStoreNow)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := seed.DeleteOAuthClient(cleanupCtx, client.ID); err != nil {
			t.Errorf("cleanup delete client: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup delete user: %v", err)
		}
	})

	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(A): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthStoreTx(t, txA) })
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(B): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthStoreTx(t, txB) })

	var pidB int32
	if err := txB.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pidB); err != nil {
		t.Fatalf("B backend PID: %v", err)
	}

	consumedAt := oauthStoreNow.Add(time.Second)
	consume := func(tx pgx.Tx, family uuid.UUID) (store.OAuthAuthorizationCode, error) {
		return store.New(tx).ConsumeOAuthAuthorizationCode(ctx, store.ConsumeOAuthAuthorizationCodeParams{
			CodeDigest:     code.CodeDigest,
			ConsumedAt:     consumedAt,
			IssuedFamilyID: family,
		})
	}

	familyA, familyB := uuid.New(), uuid.New()
	if _, err := consume(txA, familyA); err != nil {
		t.Fatalf("A consume: %v", err)
	}

	type outcome struct {
		row store.OAuthAuthorizationCode
		err error
	}
	outcomeB := make(chan outcome, 1)
	go func() {
		row, consumeErr := consume(txB, familyB)
		outcomeB <- outcome{row: row, err: consumeErr}
	}()

	waitForBlockedBackend(ctx, t, pool, pidB)

	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	got := <-outcomeB
	if !errors.Is(got.err, pgx.ErrNoRows) {
		t.Fatalf("B consume error = %v, want pgx.ErrNoRows after A committed", got.err)
	}

	stored, err := seed.GetOAuthAuthorizationCodeByDigest(ctx, code.CodeDigest)
	if err != nil {
		t.Fatalf("replay lookup: %v", err)
	}
	if stored.ConsumedAt == nil {
		t.Fatal("stored code consumed_at = NULL, want the winner's consumption time")
	}
	if stored.IssuedFamilyID == nil || *stored.IssuedFamilyID != familyA {
		t.Fatalf("stored issued_family_id = %v, want A's family %s", stored.IssuedFamilyID, familyA)
	}
}

// TestOAuthCodeConsumeRejectsExpiredAndReplayedCodes proves the single-use and
// 60-second rules through the contract, and that the replay lookup still
// returns the consumed row so the caller can revoke the family it issued.
func TestOAuthCodeConsumeRejectsExpiredAndReplayedCodes(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	client := newOAuthStoreClient(ctx, t, q, oauthStoreNow)

	fresh := newOAuthStoreCode(ctx, t, q, userID, client.ID, oauthStoreNow)
	if want := oauthStoreNow.Add(oauthStoreCodeTTL); !fresh.ExpiresAt.Equal(want) {
		t.Fatalf("code expires_at = %s, want exactly created_at + 60s (%s)", fresh.ExpiresAt, want)
	}

	family := uuid.New()
	atExpiry := oauthStoreNow.Add(oauthStoreCodeTTL)
	if _, err := q.ConsumeOAuthAuthorizationCode(ctx, store.ConsumeOAuthAuthorizationCodeParams{
		CodeDigest:     fresh.CodeDigest,
		ConsumedAt:     atExpiry,
		IssuedFamilyID: family,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("consume at exactly expires_at error = %v, want pgx.ErrNoRows", err)
	}

	justInside := oauthStoreNow.Add(oauthStoreCodeTTL - time.Second)
	consumed, err := q.ConsumeOAuthAuthorizationCode(ctx, store.ConsumeOAuthAuthorizationCodeParams{
		CodeDigest:     fresh.CodeDigest,
		ConsumedAt:     justInside,
		IssuedFamilyID: family,
	})
	if err != nil {
		t.Fatalf("consume one second before expiry: %v", err)
	}
	if consumed.ConsumedAt == nil || !consumed.ConsumedAt.Equal(justInside) {
		t.Fatalf("consumed_at = %v, want %s", consumed.ConsumedAt, justInside)
	}

	if _, err := q.ConsumeOAuthAuthorizationCode(ctx, store.ConsumeOAuthAuthorizationCodeParams{
		CodeDigest:     fresh.CodeDigest,
		ConsumedAt:     justInside,
		IssuedFamilyID: uuid.New(),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("replayed consume error = %v, want pgx.ErrNoRows", err)
	}

	replay, err := q.GetOAuthAuthorizationCodeByDigestForUpdate(ctx, fresh.CodeDigest)
	if err != nil {
		t.Fatalf("locked replay lookup: %v", err)
	}
	if replay.IssuedFamilyID == nil || *replay.IssuedFamilyID != family {
		t.Fatalf("replay issued_family_id = %v, want %s", replay.IssuedFamilyID, family)
	}
}

// ---------------------------------------------------------------------------
// Grants: upsert keeps one live row; revocation is owner-scoped and bounded.
// ---------------------------------------------------------------------------

func TestOAuthGrantUpsertKeepsExactlyOneLiveRow(t *testing.T) {
	ctx, _, tx, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	client := newOAuthStoreClient(ctx, t, q, oauthStoreNow)

	first := newOAuthStoreGrant(ctx, t, q, userID, client.ID, oauthStoreScopesRead)
	second := newOAuthStoreGrant(ctx, t, q, userID, client.ID, oauthStoreScopesBoth)
	if first.ID != second.ID {
		t.Fatalf("upsert produced a new grant %s, want the existing %s refreshed", second.ID, first.ID)
	}
	if second.Scopes != oauthStoreScopesBoth {
		t.Fatalf("refreshed scopes = %q, want %q", second.Scopes, oauthStoreScopesBoth)
	}

	var rows int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM oauth_grants WHERE user_id = $1 AND client_id = $2`, userID, client.ID,
	).Scan(&rows); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if rows != 1 {
		t.Fatalf("grant rows after two upserts = %d, want 1", rows)
	}

	live, err := q.GetLiveOAuthGrant(ctx, store.GetLiveOAuthGrantParams{UserID: userID, ClientID: client.ID})
	if err != nil {
		t.Fatalf("GetLiveOAuthGrant: %v", err)
	}
	if live.ID != first.ID {
		t.Fatalf("live grant = %s, want %s", live.ID, first.ID)
	}

	count, err := q.CountLiveOAuthGrantsForUser(ctx, userID)
	if err != nil {
		t.Fatalf("CountLiveOAuthGrantsForUser: %v", err)
	}
	if count != 1 {
		t.Fatalf("live grant count = %d, want 1", count)
	}

	revoked, err := q.RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{
		ID:        first.ID,
		RevokedAt: oauthStoreNow.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("RevokeOAuthGrant: %v", err)
	}
	if revoked != 1 {
		t.Fatalf("RevokeOAuthGrant affected = %d, want 1", revoked)
	}
	if _, err := q.GetLiveOAuthGrant(ctx, store.GetLiveOAuthGrantParams{
		UserID: userID, ClientID: client.ID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("live lookup after revocation error = %v, want pgx.ErrNoRows", err)
	}

	// A revoked grant leaves room for a fresh live one under the same pair.
	third := newOAuthStoreGrant(ctx, t, q, userID, client.ID, oauthStoreScopesRead)
	if third.ID == first.ID {
		t.Fatal("upsert after revocation reused the revoked grant, want a new live row")
	}
}

func TestOAuthGrantConcurrentUpsertLeavesOneLiveGrant(t *testing.T) {
	ctx, pool, _, _ := newOAuthStoreTx(t)
	seed := store.New(pool)

	userID := newOAuthStoreUser(ctx, t, seed)
	client := newOAuthStoreClient(ctx, t, seed, oauthStoreNow)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := seed.DeleteOAuthClient(cleanupCtx, client.ID); err != nil {
			t.Errorf("cleanup delete client: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup delete user: %v", err)
		}
	})

	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(A): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthStoreTx(t, txA) })
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(B): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthStoreTx(t, txB) })

	var pidB int32
	if err := txB.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pidB); err != nil {
		t.Fatalf("B backend PID: %v", err)
	}

	upsert := func(tx pgx.Tx, scopes string) (store.OAuthGrant, error) {
		return store.New(tx).UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
			UserID:    userID,
			ClientID:  client.ID,
			Scopes:    scopes,
			CreatedAt: oauthStoreNow,
		})
	}

	grantA, err := upsert(txA, oauthStoreScopesRead)
	if err != nil {
		t.Fatalf("A upsert: %v", err)
	}

	type outcome struct {
		grant store.OAuthGrant
		err   error
	}
	outcomeB := make(chan outcome, 1)
	go func() {
		grant, upsertErr := upsert(txB, oauthStoreScopesBoth)
		outcomeB <- outcome{grant: grant, err: upsertErr}
	}()

	waitForBlockedBackend(ctx, t, pool, pidB)

	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	got := <-outcomeB
	if got.err != nil {
		t.Fatalf("B upsert error = %v, want the conflict arm to refresh A's grant", got.err)
	}
	if got.grant.ID != grantA.ID {
		t.Fatalf("B grant = %s, want A's live grant %s refreshed", got.grant.ID, grantA.ID)
	}
	if err := txB.Commit(ctx); err != nil {
		t.Fatalf("B commit: %v", err)
	}

	var live int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM oauth_grants WHERE user_id = $1 AND client_id = $2 AND revoked_at IS NULL`,
		userID, client.ID,
	).Scan(&live); err != nil {
		t.Fatalf("count live grants: %v", err)
	}
	if live != 1 {
		t.Fatalf("live grants after concurrent upserts = %d, want exactly 1", live)
	}
}

func TestOAuthGrantRevocationIsOwnerScoped(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	ownerID := newOAuthStoreUser(ctx, t, q)
	strangerID := newOAuthStoreUser(ctx, t, q)
	client := newOAuthStoreClient(ctx, t, q, oauthStoreNow)
	grant := newOAuthStoreGrant(ctx, t, q, ownerID, client.ID, oauthStoreScopesBoth)

	revokedAt := oauthStoreNow.Add(time.Minute)
	if _, err := q.RevokeOAuthGrantForUser(ctx, store.RevokeOAuthGrantForUserParams{
		ID:        grant.ID,
		UserID:    strangerID,
		RevokedAt: revokedAt,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stranger revocation error = %v, want pgx.ErrNoRows", err)
	}

	revoked, err := q.RevokeOAuthGrantForUser(ctx, store.RevokeOAuthGrantForUserParams{
		ID:        grant.ID,
		UserID:    ownerID,
		RevokedAt: revokedAt,
	})
	if err != nil {
		t.Fatalf("owner revocation: %v", err)
	}
	if revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(revokedAt) {
		t.Fatalf("revoked_at = %v, want %s", revoked.RevokedAt, revokedAt)
	}

	if _, err := q.RevokeOAuthGrantForUser(ctx, store.RevokeOAuthGrantForUserParams{
		ID:        grant.ID,
		UserID:    ownerID,
		RevokedAt: revokedAt,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second revocation error = %v, want pgx.ErrNoRows (already revoked)", err)
	}
}

func TestOAuthListLiveGrantsForUserIsBoundedAndCarriesClientName(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)

	var lastGrant store.OAuthGrant
	for i := 0; i < 3; i++ {
		client := newOAuthStoreClient(ctx, t, q, oauthStoreNow)
		lastGrant = newOAuthStoreGrant(ctx, t, q, userID, client.ID, oauthStoreScopesBoth)
	}
	token := newOAuthStoreToken(ctx, t, q, lastGrant, "access", uuid.New(), oauthStoreNow)
	usedAt := oauthStoreNow.Add(5 * time.Minute)
	if _, err := q.TouchOAuthTokenLastUsed(ctx, store.TouchOAuthTokenLastUsedParams{
		ID:          token.ID,
		Now:         usedAt,
		TouchBefore: usedAt.Add(-oauthStoreLastUsedGap),
	}); err != nil {
		t.Fatalf("TouchOAuthTokenLastUsed: %v", err)
	}

	bounded, err := q.ListLiveOAuthGrantsForUser(ctx, store.ListLiveOAuthGrantsForUserParams{
		UserID:    userID,
		LimitRows: 2,
	})
	if err != nil {
		t.Fatalf("ListLiveOAuthGrantsForUser: %v", err)
	}
	if len(bounded) != 2 {
		t.Fatalf("bounded list returned %d grants, want 2 (limit respected)", len(bounded))
	}

	all, err := q.ListLiveOAuthGrantsForUser(ctx, store.ListLiveOAuthGrantsForUserParams{
		UserID:    userID,
		LimitRows: 10,
	})
	if err != nil {
		t.Fatalf("ListLiveOAuthGrantsForUser(10): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("full list returned %d grants, want 3", len(all))
	}
	newest := all[0]
	if newest.ID != lastGrant.ID {
		t.Fatalf("first listed grant = %s, want the newest %s", newest.ID, lastGrant.ID)
	}
	if newest.ClientName == "" {
		t.Error("listed grant carries an empty client name, want the registered name")
	}
	if newest.LastUsedAt == nil || !newest.LastUsedAt.Equal(usedAt) {
		t.Fatalf("listed last_used_at = %v, want the token's %s", newest.LastUsedAt, usedAt)
	}
}

// ---------------------------------------------------------------------------
// Tokens: rotation lineage, family revocation, the joined lookup, and the
// throttled last-used touch.
// ---------------------------------------------------------------------------

func TestOAuthTokenRotationInheritsFamilyAndCannotCrossFamilies(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	client := newOAuthStoreClient(ctx, t, q, oauthStoreNow)
	grant := newOAuthStoreGrant(ctx, t, q, userID, client.ID, oauthStoreScopesBoth)

	familyOne, familyTwo := uuid.New(), uuid.New()
	first := newOAuthStoreToken(ctx, t, q, grant, "refresh", familyOne, oauthStoreNow)
	foreign := newOAuthStoreToken(ctx, t, q, grant, "refresh", familyTwo, oauthStoreNow)

	rotatedAt := oauthStoreNow.Add(time.Hour)
	successor, err := q.InsertRotatedOAuthToken(ctx, store.InsertRotatedOAuthTokenParams{
		TokenDigest: uniqueTokenDigest(),
		Kind:        "refresh",
		RotatedFrom: first.ID,
		CreatedAt:   rotatedAt,
		ExpiresAt:   rotatedAt.Add(oauthStoreFamilyTTL),
	})
	if err != nil {
		t.Fatalf("InsertRotatedOAuthToken: %v", err)
	}
	if successor.FamilyID != familyOne {
		t.Fatalf("successor family = %s, want the predecessor's %s", successor.FamilyID, familyOne)
	}
	if successor.RotatedFrom == nil || *successor.RotatedFrom != first.ID {
		t.Fatalf("successor rotated_from = %v, want %s", successor.RotatedFrom, first.ID)
	}
	if successor.GrantID != first.GrantID || successor.ClientID != first.ClientID || successor.UserID != first.UserID {
		t.Fatal("successor did not inherit the predecessor's client, user, and grant")
	}
	if !successor.FamilyExpiresAt.Equal(first.FamilyExpiresAt) {
		t.Fatalf("successor family_expires_at = %s, want the inherited %s",
			successor.FamilyExpiresAt, first.FamilyExpiresAt)
	}
	if successor.ExpiresAt.After(successor.FamilyExpiresAt) {
		t.Fatalf("successor expires_at %s outlives its family (%s)",
			successor.ExpiresAt, successor.FamilyExpiresAt)
	}

	// Superseding under the wrong family matches no row, so a caller holding
	// one family's identifier can never mark another family's token.
	if _, err := q.SupersedeOAuthToken(ctx, store.SupersedeOAuthTokenParams{
		ID:           first.ID,
		FamilyID:     familyTwo,
		SupersededAt: rotatedAt,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-family supersede error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := q.SupersedeOAuthToken(ctx, store.SupersedeOAuthTokenParams{
		ID:           foreign.ID,
		FamilyID:     familyOne,
		SupersededAt: rotatedAt,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign-token supersede error = %v, want pgx.ErrNoRows", err)
	}

	superseded, err := q.SupersedeOAuthToken(ctx, store.SupersedeOAuthTokenParams{
		ID:           first.ID,
		FamilyID:     familyOne,
		SupersededAt: rotatedAt,
	})
	if err != nil {
		t.Fatalf("same-family supersede: %v", err)
	}
	if superseded.SupersededAt == nil || !superseded.SupersededAt.Equal(rotatedAt) {
		t.Fatalf("superseded_at = %v, want %s", superseded.SupersededAt, rotatedAt)
	}
	if _, err := q.SupersedeOAuthToken(ctx, store.SupersedeOAuthTokenParams{
		ID:           first.ID,
		FamilyID:     familyOne,
		SupersededAt: rotatedAt,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second supersede error = %v, want pgx.ErrNoRows (already superseded)", err)
	}
}

func TestOAuthTokenFamilyRevocationRevokesEveryMemberOnlyInThatFamily(t *testing.T) {
	ctx, _, tx, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	client := newOAuthStoreClient(ctx, t, q, oauthStoreNow)
	grant := newOAuthStoreGrant(ctx, t, q, userID, client.ID, oauthStoreScopesBoth)

	doomed, spared := uuid.New(), uuid.New()
	refresh := newOAuthStoreToken(ctx, t, q, grant, "refresh", doomed, oauthStoreNow)
	newOAuthStoreToken(ctx, t, q, grant, "access", doomed, oauthStoreNow)
	rotatedAt := oauthStoreNow.Add(time.Hour)
	if _, err := q.InsertRotatedOAuthToken(ctx, store.InsertRotatedOAuthTokenParams{
		TokenDigest: uniqueTokenDigest(),
		Kind:        "refresh",
		RotatedFrom: refresh.ID,
		CreatedAt:   rotatedAt,
		ExpiresAt:   rotatedAt.Add(oauthStoreFamilyTTL),
	}); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	survivor := newOAuthStoreToken(ctx, t, q, grant, "access", spared, oauthStoreNow)

	// The grant row is the family-revocation serialization point.
	if _, err := q.GetOAuthGrantForUpdate(ctx, grant.ID); err != nil {
		t.Fatalf("GetOAuthGrantForUpdate: %v", err)
	}

	revokedAt := oauthStoreNow.Add(2 * time.Hour)
	revoked, err := q.RevokeOAuthTokenFamily(ctx, store.RevokeOAuthTokenFamilyParams{
		FamilyID:  doomed,
		RevokedAt: revokedAt,
	})
	if err != nil {
		t.Fatalf("RevokeOAuthTokenFamily: %v", err)
	}
	if revoked != 3 {
		t.Fatalf("family revocation affected %d rows, want all 3 members", revoked)
	}

	var live int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM oauth_tokens WHERE family_id = $1 AND revoked_at IS NULL`, doomed,
	).Scan(&live); err != nil {
		t.Fatalf("count live family members: %v", err)
	}
	if live != 0 {
		t.Fatalf("live members of the revoked family = %d, want 0", live)
	}

	stillLive, err := q.GetOAuthTokenAuthorityByDigest(ctx, survivor.TokenDigest)
	if err != nil {
		t.Fatalf("survivor lookup: %v", err)
	}
	if stillLive.OAuthToken.RevokedAt != nil {
		t.Error("a token in another family was revoked, want family-scoped revocation")
	}

	// Revoking through the grant kills every family under it in one statement.
	grantRevoked, err := q.RevokeOAuthTokensForGrant(ctx, store.RevokeOAuthTokensForGrantParams{
		GrantID:   grant.ID,
		RevokedAt: revokedAt,
	})
	if err != nil {
		t.Fatalf("RevokeOAuthTokensForGrant: %v", err)
	}
	if grantRevoked != 1 {
		t.Fatalf("grant revocation affected %d rows, want the 1 remaining live token", grantRevoked)
	}
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM oauth_tokens WHERE grant_id = $1 AND revoked_at IS NULL`, grant.ID,
	).Scan(&live); err != nil {
		t.Fatalf("count live grant tokens: %v", err)
	}
	if live != 0 {
		t.Fatalf("live tokens under the revoked grant = %d, want 0", live)
	}
}

func TestOAuthTokenAuthorityLookupReturnsGrantAndUserInOneQuery(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	client := newOAuthStoreClient(ctx, t, q, oauthStoreNow)
	grant := newOAuthStoreGrant(ctx, t, q, userID, client.ID, oauthStoreScopesRead)
	token := newOAuthStoreToken(ctx, t, q, grant, "access", uuid.New(), oauthStoreNow)

	authority, err := q.GetOAuthTokenAuthorityByDigest(ctx, token.TokenDigest)
	if err != nil {
		t.Fatalf("GetOAuthTokenAuthorityByDigest: %v", err)
	}
	if authority.OAuthToken.ID != token.ID {
		t.Fatalf("token id = %s, want %s", authority.OAuthToken.ID, token.ID)
	}
	if authority.OAuthGrant.ID != grant.ID || authority.OAuthGrant.Scopes != oauthStoreScopesRead {
		t.Fatalf("grant = %+v, want %s with scopes %q", authority.OAuthGrant, grant.ID, oauthStoreScopesRead)
	}
	if authority.User.ID != userID {
		t.Fatalf("user id = %s, want %s", authority.User.ID, userID)
	}

	if _, err := q.GetOAuthTokenAuthorityByDigest(ctx, uniqueTokenDigest()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unknown digest error = %v, want pgx.ErrNoRows", err)
	}
}

func TestOAuthTokenLastUsedTouchIsThrottled(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	client := newOAuthStoreClient(ctx, t, q, oauthStoreNow)
	grant := newOAuthStoreGrant(ctx, t, q, userID, client.ID, oauthStoreScopesBoth)
	token := newOAuthStoreToken(ctx, t, q, grant, "access", uuid.New(), oauthStoreNow)

	touch := func(at time.Time) int64 {
		t.Helper()
		affected, err := q.TouchOAuthTokenLastUsed(ctx, store.TouchOAuthTokenLastUsedParams{
			ID:          token.ID,
			Now:         at,
			TouchBefore: at.Add(-oauthStoreLastUsedGap),
		})
		if err != nil {
			t.Fatalf("TouchOAuthTokenLastUsed(%s): %v", at, err)
		}
		return affected
	}

	first := oauthStoreNow.Add(time.Minute)
	if got := touch(first); got != 1 {
		t.Fatalf("first touch affected %d rows, want 1 (last_used_at was NULL)", got)
	}
	if got := touch(first.Add(30 * time.Second)); got != 0 {
		t.Fatalf("touch 30s later affected %d rows, want 0 (throttled)", got)
	}
	if got := touch(first.Add(oauthStoreLastUsedGap)); got != 1 {
		t.Fatalf("touch 60s later affected %d rows, want 1", got)
	}

	revokedAt := first.Add(2 * oauthStoreLastUsedGap)
	if _, err := q.RevokeOAuthTokenFamily(ctx, store.RevokeOAuthTokenFamilyParams{
		FamilyID:  token.FamilyID,
		RevokedAt: revokedAt,
	}); err != nil {
		t.Fatalf("RevokeOAuthTokenFamily: %v", err)
	}
	if got := touch(revokedAt.Add(oauthStoreLastUsedGap)); got != 0 {
		t.Fatalf("touch on a revoked token affected %d rows, want 0", got)
	}
}

// TestOAuthTokenDigestColumnRejectsRawMaterial proves storage is digest-only:
// the raw M3 spellings this test builds in memory are 48 characters and can
// never reach a 32-byte digest column. The prefixes exist here and nowhere in
// the migration, the queries, or any stored fixture.
func TestOAuthTokenDigestColumnRejectsRawMaterial(t *testing.T) {
	ctx, _, tx, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	client := newOAuthStoreClient(ctx, t, q, oauthStoreNow)
	grant := newOAuthStoreGrant(ctx, t, q, userID, client.ID, oauthStoreScopesBoth)

	for _, raw := range []string{
		"amat_E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		"amrt_E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
	} {
		// Each attempt runs in its own savepoint: the first constraint
		// violation aborts the enclosing transaction, so without one the
		// second case would only ever observe "transaction is aborted".
		savepoint, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("open savepoint: %v", err)
		}
		_, err = store.New(savepoint).CreateOAuthToken(ctx, store.CreateOAuthTokenParams{
			TokenDigest:     []byte(raw),
			Kind:            "access",
			FamilyID:        uuid.New(),
			ClientID:        grant.ClientID,
			UserID:          grant.UserID,
			GrantID:         grant.ID,
			CreatedAt:       oauthStoreNow,
			ExpiresAt:       oauthStoreNow.Add(oauthStoreAccessTTL),
			FamilyExpiresAt: oauthStoreNow.Add(oauthStoreFamilyTTL),
		})
		requireConstraint(t, err, "oauth_tokens_token_digest_length_check")
		if rollbackErr := savepoint.Rollback(ctx); rollbackErr != nil {
			t.Fatalf("rollback savepoint: %v", rollbackErr)
		}
	}
}

// ---------------------------------------------------------------------------
// Bounded GC and cleanup.
// ---------------------------------------------------------------------------

func TestOAuthIdleClientGCIsBoundedAndSkipsLiveClients(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)

	idle := make([]uuid.UUID, 0, 3)
	for i := 0; i < 3; i++ {
		idle = append(idle, newOAuthStoreClient(ctx, t, q, oauthGCEpoch).ID)
	}
	grantedClient := newOAuthStoreClient(ctx, t, q, oauthGCEpoch)
	grant := newOAuthStoreGrant(ctx, t, q, userID, grantedClient.ID, oauthStoreScopesBoth)
	tokenClient := newOAuthStoreClient(ctx, t, q, oauthGCEpoch)
	tokenGrant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
		UserID:    userID,
		ClientID:  tokenClient.ID,
		Scopes:    oauthStoreScopesBoth,
		CreatedAt: oauthGCEpoch,
	})
	if err != nil {
		t.Fatalf("UpsertOAuthGrant(token client): %v", err)
	}
	liveToken := newOAuthStoreToken(ctx, t, q, tokenGrant, "refresh", uuid.New(), oauthGCEpoch)
	if _, err := q.RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{
		ID:        tokenGrant.ID,
		RevokedAt: oauthGCEpoch.Add(time.Hour),
	}); err != nil {
		t.Fatalf("revoke token client's grant: %v", err)
	}
	// The GC runs 25 hours after the epoch, so its idle cutoff (now - 24h)
	// falls one hour after the epoch: every epoch-created client is idle, and
	// a client registered two hours after the epoch is still too young.
	now := oauthGCEpoch.Add(25 * time.Hour)
	idleBefore := now.Add(-24 * time.Hour)
	freshClient := newOAuthStoreClient(ctx, t, q, idleBefore.Add(time.Hour))
	candidates, err := q.ListIdleOAuthClientCandidates(ctx, store.ListIdleOAuthClientCandidatesParams{
		IdleBefore: idleBefore,
		Now:        now,
		LimitRows:  2,
	})
	if err != nil {
		t.Fatalf("ListIdleOAuthClientCandidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("bounded candidates = %d, want 2 (limit respected)", len(candidates))
	}

	candidates, err = q.ListIdleOAuthClientCandidates(ctx, store.ListIdleOAuthClientCandidatesParams{
		IdleBefore: idleBefore,
		Now:        now,
		LimitRows:  200,
	})
	if err != nil {
		t.Fatalf("ListIdleOAuthClientCandidates(200): %v", err)
	}
	got := map[uuid.UUID]bool{}
	for _, id := range candidates {
		got[id] = true
	}
	for _, id := range idle {
		if !got[id] {
			t.Errorf("idle client %s missing from GC candidates", id)
		}
	}
	for label, id := range map[string]uuid.UUID{
		"client with a live grant":     grantedClient.ID,
		"client with a live token":     tokenClient.ID,
		"client younger than the idle": freshClient.ID,
	} {
		if got[id] {
			t.Errorf("%s (%s) appeared as a GC candidate, want protection", label, id)
		}
	}

	deleted, err := q.DeleteOAuthClients(ctx, idle)
	if err != nil {
		t.Fatalf("DeleteOAuthClients: %v", err)
	}
	if deleted != int64(len(idle)) {
		t.Fatalf("DeleteOAuthClients removed %d rows, want %d", deleted, len(idle))
	}
	if _, err := q.GetOAuthClient(ctx, idle[0]); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted client lookup error = %v, want pgx.ErrNoRows", err)
	}

	// Once the token's own family has expired, its client becomes collectable.
	afterExpiry := liveToken.FamilyExpiresAt.Add(time.Second)
	candidates, err = q.ListIdleOAuthClientCandidates(ctx, store.ListIdleOAuthClientCandidatesParams{
		IdleBefore: idleBefore,
		Now:        afterExpiry,
		LimitRows:  200,
	})
	if err != nil {
		t.Fatalf("ListIdleOAuthClientCandidates(after expiry): %v", err)
	}
	got = map[uuid.UUID]bool{}
	for _, id := range candidates {
		got[id] = true
	}
	if !got[tokenClient.ID] {
		t.Error("client whose only token expired is still protected, want it collectable")
	}
	if got[grantedClient.ID] {
		t.Errorf("client with live grant %s became collectable, want protection", grant.ID)
	}
}

func TestOAuthClientLastUsedTouchIsThrottled(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	client := newOAuthStoreClient(ctx, t, q, oauthStoreNow)
	if !client.LastUsedAt.Equal(oauthStoreNow) {
		t.Fatalf("new client last_used_at = %s, want its creation time %s", client.LastUsedAt, oauthStoreNow)
	}

	touch := func(at time.Time) int64 {
		t.Helper()
		affected, err := q.TouchOAuthClientLastUsed(ctx, store.TouchOAuthClientLastUsedParams{
			ID:          client.ID,
			Now:         at,
			TouchBefore: at.Add(-oauthStoreLastUsedGap),
		})
		if err != nil {
			t.Fatalf("TouchOAuthClientLastUsed(%s): %v", at, err)
		}
		return affected
	}

	if got := touch(oauthStoreNow.Add(30 * time.Second)); got != 0 {
		t.Fatalf("touch 30s after creation affected %d rows, want 0 (throttled)", got)
	}
	if got := touch(oauthStoreNow.Add(oauthStoreLastUsedGap)); got != 1 {
		t.Fatalf("touch 60s after creation affected %d rows, want 1", got)
	}
}

func TestOAuthCleanupQueriesRespectBatchBounds(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	client := newOAuthStoreClient(ctx, t, q, oauthGCEpoch)
	grant := newOAuthStoreGrant(ctx, t, q, userID, client.ID, oauthStoreScopesBoth)

	for i := 0; i < 5; i++ {
		newOAuthStoreCode(ctx, t, q, userID, client.ID, oauthGCEpoch)
	}
	cutoff := oauthGCEpoch.Add(time.Hour)
	deleted, err := q.DeleteExpiredOAuthAuthorizationCodes(ctx, store.DeleteExpiredOAuthAuthorizationCodesParams{
		Cutoff:    cutoff,
		LimitRows: 2,
	})
	if err != nil {
		t.Fatalf("DeleteExpiredOAuthAuthorizationCodes: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("code cleanup deleted %d rows, want 2 (limit respected)", deleted)
	}
	deleted, err = q.DeleteExpiredOAuthAuthorizationCodes(ctx, store.DeleteExpiredOAuthAuthorizationCodesParams{
		Cutoff:    cutoff,
		LimitRows: 200,
	})
	if err != nil {
		t.Fatalf("DeleteExpiredOAuthAuthorizationCodes(200): %v", err)
	}
	if deleted != 3 {
		t.Fatalf("second code cleanup deleted %d rows, want the remaining 3", deleted)
	}

	// A code that has not expired at the cutoff is never collected.
	live := newOAuthStoreCode(ctx, t, q, userID, client.ID, cutoff)
	deleted, err = q.DeleteExpiredOAuthAuthorizationCodes(ctx, store.DeleteExpiredOAuthAuthorizationCodesParams{
		Cutoff:    cutoff,
		LimitRows: 200,
	})
	if err != nil {
		t.Fatalf("DeleteExpiredOAuthAuthorizationCodes(live): %v", err)
	}
	if deleted != 0 {
		t.Fatalf("cleanup deleted %d live codes, want 0", deleted)
	}
	if _, err := q.GetOAuthAuthorizationCodeByDigest(ctx, live.CodeDigest); err != nil {
		t.Fatalf("live code lookup after cleanup: %v", err)
	}

	// Access tokens leave as soon as they expire; refresh tokens are retained
	// until their family dies, so a replayed superseded refresh token can
	// still be recognized.
	for i := 0; i < 5; i++ {
		newOAuthStoreToken(ctx, t, q, grant, "access", uuid.New(), oauthGCEpoch)
	}
	refresh := newOAuthStoreToken(ctx, t, q, grant, "refresh", uuid.New(), oauthGCEpoch)
	tokenCutoff := oauthGCEpoch.Add(oauthStoreAccessTTL + time.Second)
	deleted, err = q.DeleteTerminalOAuthTokens(ctx, store.DeleteTerminalOAuthTokensParams{
		Cutoff:    tokenCutoff,
		LimitRows: 2,
	})
	if err != nil {
		t.Fatalf("DeleteTerminalOAuthTokens: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("token cleanup deleted %d rows, want 2 (limit respected)", deleted)
	}
	deleted, err = q.DeleteTerminalOAuthTokens(ctx, store.DeleteTerminalOAuthTokensParams{
		Cutoff:    tokenCutoff,
		LimitRows: 200,
	})
	if err != nil {
		t.Fatalf("DeleteTerminalOAuthTokens(200): %v", err)
	}
	if deleted != 3 {
		t.Fatalf("second token cleanup deleted %d rows, want the remaining 3 access tokens", deleted)
	}
	if _, err := q.GetOAuthTokenAuthorityByDigest(ctx, refresh.TokenDigest); err != nil {
		t.Fatalf("refresh token was collected before its family expired: %v", err)
	}

	afterFamily := refresh.FamilyExpiresAt.Add(time.Second)
	deleted, err = q.DeleteTerminalOAuthTokens(ctx, store.DeleteTerminalOAuthTokensParams{
		Cutoff:    afterFamily,
		LimitRows: 200,
	})
	if err != nil {
		t.Fatalf("DeleteTerminalOAuthTokens(after family expiry): %v", err)
	}
	if deleted != 1 {
		t.Fatalf("cleanup after family expiry deleted %d rows, want the 1 refresh token", deleted)
	}

	// The frozen M1/M5 ceiling is enforced by the query, not merely by callers:
	// a future sweep must not turn a caller bug into an unbounded request-path
	// delete by passing a value larger than 200.
	for i := 0; i < 201; i++ {
		newOAuthStoreCode(ctx, t, q, userID, client.ID, oauthGCEpoch)
	}
	deleted, err = q.DeleteExpiredOAuthAuthorizationCodes(ctx, store.DeleteExpiredOAuthAuthorizationCodesParams{
		Cutoff:    cutoff,
		LimitRows: 201,
	})
	if err != nil {
		t.Fatalf("DeleteExpiredOAuthAuthorizationCodes(201): %v", err)
	}
	if deleted != 200 {
		t.Fatalf("oversized code cleanup deleted %d rows, want the frozen batch maximum 200", deleted)
	}

	for i := 0; i < 201; i++ {
		newOAuthStoreToken(ctx, t, q, grant, "access", uuid.New(), oauthGCEpoch)
	}
	deleted, err = q.DeleteTerminalOAuthTokens(ctx, store.DeleteTerminalOAuthTokensParams{
		Cutoff:    tokenCutoff,
		LimitRows: 201,
	})
	if err != nil {
		t.Fatalf("DeleteTerminalOAuthTokens(201): %v", err)
	}
	if deleted != 200 {
		t.Fatalf("oversized token cleanup deleted %d rows, want the frozen batch maximum 200", deleted)
	}

	for i := 0; i < 201; i++ {
		newOAuthStoreClient(ctx, t, q, oauthGCEpoch)
	}
	candidates, err := q.ListIdleOAuthClientCandidates(ctx, store.ListIdleOAuthClientCandidatesParams{
		IdleBefore: oauthGCEpoch.Add(24 * time.Hour),
		Now:        oauthGCEpoch.Add(25 * time.Hour),
		LimitRows:  201,
	})
	if err != nil {
		t.Fatalf("ListIdleOAuthClientCandidates(201): %v", err)
	}
	if len(candidates) != 200 {
		t.Fatalf("oversized idle-client sweep selected %d rows, want the frozen batch maximum 200", len(candidates))
	}
}

func rollbackOAuthStoreTx(t *testing.T, tx pgx.Tx) {
	t.Helper()
	if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("rollback test transaction: %v", err)
	}
}
