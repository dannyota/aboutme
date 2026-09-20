package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestSecondFactorStore_RecoveryCodeHasOneConsumer(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	digest := make([]byte, 32)
	digest[0] = 1
	if _, err := q.CreateSecondFactorRecoveryCode(ctx, store.CreateSecondFactorRecoveryCodeParams{
		UserID: userID, CodeDigest: digest, CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateSecondFactorRecoveryCode: %v", err)
	}

	if _, err := q.ConsumeSecondFactorRecoveryCode(ctx, store.ConsumeSecondFactorRecoveryCodeParams{
		UserID: userID, CodeDigest: digest,
	}); err != nil {
		t.Fatalf("first ConsumeSecondFactorRecoveryCode: %v", err)
	}
	_, err := q.ConsumeSecondFactorRecoveryCode(ctx, store.ConsumeSecondFactorRecoveryCodeParams{
		UserID: userID, CodeDigest: digest,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("second ConsumeSecondFactorRecoveryCode error = %v, want pgx.ErrNoRows", err)
	}
}

func TestSecondFactorStore_RecoveryCodeHasOneConcurrentConsumer(t *testing.T) {
	ctx, pool, _, _ := newOAuthStoreTx(t)
	seed := store.New(pool)
	userID := newOAuthStoreUser(ctx, t, seed)
	digest := uniqueTokenDigest()
	if _, err := seed.CreateSecondFactorRecoveryCode(ctx, store.CreateSecondFactorRecoveryCodeParams{
		UserID: userID, CodeDigest: digest, CreatedAt: oauthStoreNow,
	}); err != nil {
		t.Fatalf("CreateSecondFactorRecoveryCode: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup user: %v", err)
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

	consume := func(tx pgx.Tx) (store.SecondFactorRecoveryCode, error) {
		return store.New(tx).ConsumeSecondFactorRecoveryCode(ctx, store.ConsumeSecondFactorRecoveryCodeParams{UserID: userID, CodeDigest: digest})
	}
	if _, err := consume(txA); err != nil {
		t.Fatalf("A consume: %v", err)
	}
	type outcome struct{ err error }
	outcomeB := make(chan outcome, 1)
	go func() {
		_, err := consume(txB)
		outcomeB <- outcome{err: err}
	}()
	waitForBlockedBackend(ctx, t, pool, pidB)
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	if got := <-outcomeB; !errors.Is(got.err, pgx.ErrNoRows) {
		t.Errorf("B consume error = %v, want pgx.ErrNoRows", got.err)
	}
}

func TestSecondFactorStore_PasskeyListUsesCreationOrder(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	handle := make([]byte, 32)
	handle[0] = 1
	if _, err := q.CreateSecondFactorPolicy(ctx, store.CreateSecondFactorPolicyParams{
		UserID: userID, WebauthnUserHandle: handle, EnabledAt: now,
	}); err != nil {
		t.Fatalf("CreateSecondFactorPolicy: %v", err)
	}
	for i := byte(2); i < 4; i++ {
		credentialID := make([]byte, 16)
		credentialID[0] = i
		if _, err := q.CreateWebAuthnCredential(ctx, store.CreateWebAuthnCredentialParams{
			UserID: userID, CredentialID: credentialID, PublicKey: []byte{1}, SignCount: 0,
			BackupEligible: false, BackupState: false, Transports: []string{"internal"}, CreatedAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("CreateWebAuthnCredential(%d): %v", i, err)
		}
	}
	credentials, err := q.ListWebAuthnCredentialsForUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListWebAuthnCredentialsForUser: %v", err)
	}
	if len(credentials) != 2 || credentials[0].CreatedAt.After(credentials[1].CreatedAt) {
		t.Errorf("credentials = %+v, want ascending creation order", credentials)
	}
}

func TestSecondFactorStore_IssuersPersistFactorAuthority(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	factorVerifiedAt := now.Add(-time.Minute)

	session, err := q.CreateSession(ctx, store.CreateSessionParams{
		UserID: userID, TokenHash: uniqueTokenDigest(), CSRFSecret: uniqueTokenDigest(),
		CreatedAt: now, LastSeenAt: now, ReauthenticatedAt: now,
		AbsoluteExpiresAt: now.Add(24 * time.Hour), AuthEpoch: 7,
		SecondFactorVerifiedAt: &factorVerifiedAt,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.AuthEpoch != 7 || session.SecondFactorVerifiedAt == nil || !session.SecondFactorVerifiedAt.Equal(factorVerifiedAt) {
		t.Errorf("session factor authority = %+v, want epoch 7 and factor proof %s", session, factorVerifiedAt)
	}

	client := newOAuthStoreClient(ctx, t, q, now)
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
		UserID: userID, ClientID: client.ID, Scopes: oauthStoreScopesRead, CreatedAt: now, AuthEpoch: 7,
	})
	if err != nil {
		t.Fatalf("UpsertOAuthGrant: %v", err)
	}
	if grant.AuthEpoch != 7 {
		t.Errorf("grant auth epoch = %d, want 7", grant.AuthEpoch)
	}

	grantID := grant.ID
	authEpoch := int64(7)
	code, err := q.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{
		CodeDigest: uniqueTokenDigest(), ClientID: client.ID, UserID: userID,
		GrantID: &grantID, AuthEpoch: &authEpoch, Scopes: oauthStoreScopesRead,
		CodeChallenge: oauthStoreChallenge, RedirectURI: oauthStoreRedirect, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateOAuthAuthorizationCode: %v", err)
	}
	if code.GrantID != grant.ID || code.AuthEpoch != 7 {
		t.Errorf("code authority = %+v, want grant %s at epoch 7", code, grant.ID)
	}
}

func TestSecondFactorStore_FifthPendingFailureHasOneWinner(t *testing.T) {
	ctx, pool, _, _ := newOAuthStoreTx(t)
	seed := store.New(pool)
	userID := newOAuthStoreUser(ctx, t, seed)
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	pending := createSecondFactorPending(ctx, t, seed, userID, now)
	locked, err := seed.GetPendingAuthenticationByTokenDigestForUpdate(ctx, pending.TokenDigest)
	if err != nil {
		t.Fatalf("GetPendingAuthenticationByTokenDigestForUpdate: %v", err)
	}
	if locked.ID != pending.ID {
		t.Errorf("locked pending ID = %s, want %s", locked.ID, pending.ID)
	}

	for attempt := 1; attempt <= 4; attempt++ {
		updated, recordErr := seed.RecordPendingAuthenticationFailure(ctx, store.RecordPendingAuthenticationFailureParams{
			ID: pending.ID, AttemptedAt: now.Add(time.Duration(attempt) * time.Second),
		})
		if recordErr != nil {
			t.Fatalf("RecordPendingAuthenticationFailure(%d): %v", attempt, recordErr)
		}
		if updated.FailedAttempts != int32(attempt) {
			t.Errorf("failed attempts after %d = %d", attempt, updated.FailedAttempts)
		}
	}
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
	if pidErr := txB.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pidB); pidErr != nil {
		t.Fatalf("B backend PID: %v", pidErr)
	}
	fail := func(tx pgx.Tx) (store.PendingAuthentication, error) {
		return store.New(tx).RecordPendingAuthenticationFailure(ctx, store.RecordPendingAuthenticationFailureParams{ID: pending.ID, AttemptedAt: now.Add(5 * time.Second)})
	}
	winner, err := fail(txA)
	if err != nil {
		t.Fatalf("A failure: %v", err)
	}
	if winner.FailedAttempts != 5 || winner.ConsumedAt == nil {
		t.Errorf("A failure result = %+v, want consumed fifth failure", winner)
	}
	type outcome struct{ err error }
	outcomeB := make(chan outcome, 1)
	go func() {
		_, failureErr := fail(txB)
		outcomeB <- outcome{err: failureErr}
	}()
	waitForBlockedBackend(ctx, t, pool, pidB)
	if commitErr := txA.Commit(ctx); commitErr != nil {
		t.Fatalf("A commit: %v", commitErr)
	}
	if got := <-outcomeB; !errors.Is(got.err, pgx.ErrNoRows) {
		t.Errorf("B failure error = %v, want pgx.ErrNoRows", got.err)
	}
	if rollbackErr := txB.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("B rollback: %v", rollbackErr)
	}
	stored, err := seed.GetPendingAuthenticationByTokenDigestForUpdate(ctx, pending.TokenDigest)
	if err != nil {
		t.Fatalf("stored pending: %v", err)
	}
	if stored.FailedAttempts != 5 || stored.ConsumedAt == nil {
		t.Errorf("stored pending = %+v, want only the consumed fifth failure", stored)
	}
}

func TestSecondFactorStore_ConsumesOldestLivePendingAuthentication(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	oldest := createSecondFactorPending(ctx, t, q, userID, now.Add(-4*time.Second))
	for i := 1; i < 4; i++ {
		createSecondFactorPending(ctx, t, q, userID, now.Add(time.Duration(-4+i)*time.Second))
	}
	if _, err := q.ConsumeOldestLivePendingAuthentication(ctx, store.ConsumeOldestLivePendingAuthenticationParams{UserID: userID, ConsumedAt: now}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("consume with fewer than five rows error = %v, want pgx.ErrNoRows", err)
	}
	stillLive, err := q.GetPendingAuthenticationByTokenDigestForUpdate(ctx, oldest.TokenDigest)
	if err != nil {
		t.Fatalf("oldest pending after under-cap consume: %v", err)
	}
	if stillLive.ConsumedAt != nil {
		t.Error("under-cap consume modified oldest pending")
	}
	createSecondFactorPending(ctx, t, q, userID, now)

	consumed, err := q.ConsumeOldestLivePendingAuthentication(ctx, store.ConsumeOldestLivePendingAuthenticationParams{
		UserID: userID, ConsumedAt: now,
	})
	if err != nil {
		t.Fatalf("ConsumeOldestLivePendingAuthentication: %v", err)
	}
	if consumed.ID != oldest.ID || consumed.UserID != userID || string(consumed.TokenDigest) != string(oldest.TokenDigest) || consumed.ConsumedAt == nil {
		t.Errorf("consumed pending = %+v, want oldest %s marked consumed", consumed, oldest.ID)
	}
}

func TestSecondFactorStore_ConsumesPriorCeremonyForBinding(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	pending := createSecondFactorPending(ctx, t, q, userID, now)
	pendingID := pending.ID
	first := createSecondFactorCeremony(ctx, t, q, userID, &pendingID, now)
	createSecondFactorCeremony(ctx, t, q, userID, &pendingID, now.Add(time.Second))

	count, err := q.ConsumeLiveWebAuthnCeremoniesForBinding(ctx, store.ConsumeLiveWebAuthnCeremoniesForBindingParams{
		UserID: userID, Purpose: "assertion", PendingAuthenticationID: &pendingID, ConsumedAt: now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("ConsumeLiveWebAuthnCeremoniesForBinding: %v", err)
	}
	if count != 2 {
		t.Errorf("consumed ceremonies = %d, want 2", count)
	}
	locked, err := q.GetWebAuthnCeremonyByTokenDigestForUpdate(ctx, first.TokenDigest)
	if err != nil {
		t.Fatalf("GetWebAuthnCeremonyByTokenDigestForUpdate: %v", err)
	}
	if locked.ConsumedAt == nil {
		t.Error("prior ceremony remains live")
	}
}

func TestSecondFactorStore_CeremonyClaimHasOneConcurrentWinner(t *testing.T) {
	ctx, pool, _, _ := newOAuthStoreTx(t)
	seed := store.New(pool)
	userID := newOAuthStoreUser(ctx, t, seed)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	pending := createSecondFactorPending(ctx, t, seed, userID, now)
	pendingID := pending.ID
	ceremony := createSecondFactorCeremony(ctx, t, seed, userID, &pendingID, now)
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup user: %v", err)
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
	claim := func(tx pgx.Tx) (store.WebauthnCeremony, error) {
		return store.New(tx).ClaimWebAuthnCeremony(ctx, store.ClaimWebAuthnCeremonyParams{TokenDigest: ceremony.TokenDigest, ConsumedAt: now.Add(time.Second)})
	}
	if _, err := claim(txA); err != nil {
		t.Fatalf("A claim: %v", err)
	}
	type outcome struct{ err error }
	outcomeB := make(chan outcome, 1)
	go func() {
		_, err := claim(txB)
		outcomeB <- outcome{err: err}
	}()
	waitForBlockedBackend(ctx, t, pool, pidB)
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	if got := <-outcomeB; !errors.Is(got.err, pgx.ErrNoRows) {
		t.Errorf("B claim error = %v, want pgx.ErrNoRows", got.err)
	}
}

func TestSecondFactorStore_FinalPasskeyRemovalHasOneConcurrentWinner(t *testing.T) {
	ctx, pool, _, _ := newOAuthStoreTx(t)
	seed := store.New(pool)
	userID := newOAuthStoreUser(ctx, t, seed)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	credentialID := make([]byte, 16)
	credentialID[0] = 1
	credential, err := seed.CreateWebAuthnCredential(ctx, store.CreateWebAuthnCredentialParams{
		UserID: userID, CredentialID: credentialID, PublicKey: []byte{1}, BackupEligible: false,
		BackupState: false, Transports: []string{"internal"}, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateWebAuthnCredential: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); cleanupErr != nil {
			t.Errorf("cleanup user: %v", cleanupErr)
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
	remove := func(tx pgx.Tx) (store.WebauthnCredential, error) {
		return store.New(tx).DeleteWebAuthnCredentialForUser(ctx, store.DeleteWebAuthnCredentialForUserParams{ID: credential.ID, UserID: userID})
	}
	if _, err := remove(txA); err != nil {
		t.Fatalf("A remove: %v", err)
	}
	type outcome struct{ err error }
	outcomeB := make(chan outcome, 1)
	go func() {
		_, err := remove(txB)
		outcomeB <- outcome{err: err}
	}()
	waitForBlockedBackend(ctx, t, pool, pidB)
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	if got := <-outcomeB; !errors.Is(got.err, pgx.ErrNoRows) {
		t.Errorf("B remove error = %v, want pgx.ErrNoRows", got.err)
	}
}

func TestSecondFactorStore_AdvancesEpochUpdatesProofAndKeepsCredentialCounterMonotonic(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	user, err := q.AdvanceUserAuthEpoch(ctx, userID)
	if err != nil {
		t.Fatalf("AdvanceUserAuthEpoch: %v", err)
	}
	if user.AuthEpoch != 1 {
		t.Errorf("auth epoch = %d, want 1", user.AuthEpoch)
	}
	session, err := q.CreateSession(ctx, store.CreateSessionParams{
		UserID: userID, TokenHash: uniqueTokenDigest(), CSRFSecret: uniqueTokenDigest(),
		CreatedAt: now, LastSeenAt: now, ReauthenticatedAt: now, AbsoluteExpiresAt: now.Add(time.Hour), AuthEpoch: user.AuthEpoch,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	updatedSession, err := q.UpdateCurrentSessionProofs(ctx, store.UpdateCurrentSessionProofsParams{
		ID: session.ID, UserID: userID, AuthEpoch: user.AuthEpoch,
		VerifiedAt: now.Add(time.Minute), Now: now.Add(time.Minute), IdleCutoff: now.Add(-30*24*time.Hour + time.Minute),
	})
	if err != nil {
		t.Fatalf("UpdateCurrentSessionProofs: %v", err)
	}
	if updatedSession.SecondFactorVerifiedAt == nil || !updatedSession.ReauthenticatedAt.Equal(now.Add(time.Minute)) {
		t.Errorf("updated session = %+v, want both proof timestamps", updatedSession)
	}

	credentialID := make([]byte, 16)
	credentialID[0] = 1
	credential, err := q.CreateWebAuthnCredential(ctx, store.CreateWebAuthnCredentialParams{
		UserID: userID, CredentialID: credentialID, PublicKey: []byte{1}, BackupEligible: true,
		BackupState: false, Transports: []string{"internal"}, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateWebAuthnCredential: %v", err)
	}
	updatedCredential, err := q.UpdateWebAuthnCredentialAfterAssertion(ctx, store.UpdateWebAuthnCredentialAfterAssertionParams{
		ID: credential.ID, UserID: userID, SignCount: 3, BackupEligible: true, BackupState: true, LastUsedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("UpdateWebAuthnCredentialAfterAssertion: %v", err)
	}
	if updatedCredential.SignCount != 3 || !updatedCredential.BackupState || updatedCredential.LastUsedAt == nil {
		t.Errorf("updated credential = %+v", updatedCredential)
	}
	if _, err := q.UpdateWebAuthnCredentialAfterAssertion(ctx, store.UpdateWebAuthnCredentialAfterAssertionParams{
		ID: credential.ID, UserID: userID, SignCount: 2, BackupEligible: true, BackupState: true, LastUsedAt: now.Add(2 * time.Minute),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("non-monotonic counter update error = %v, want pgx.ErrNoRows", err)
	}
}

func TestSecondFactorStore_DoesNotUpdateProofsForDeadSessions(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	idleCutoff := now.Add(-30 * 24 * time.Hour)

	idleSession, err := q.CreateSession(ctx, store.CreateSessionParams{
		UserID: userID, TokenHash: uniqueTokenDigest(), CSRFSecret: uniqueTokenDigest(),
		CreatedAt: now.Add(-31 * 24 * time.Hour), LastSeenAt: idleCutoff.Add(-time.Second), ReauthenticatedAt: now,
		AbsoluteExpiresAt: now.Add(time.Hour), AuthEpoch: 0,
	})
	if err != nil {
		t.Fatalf("CreateSession(idle): %v", err)
	}
	if _, updateErr := q.UpdateCurrentSessionProofs(ctx, store.UpdateCurrentSessionProofsParams{
		ID: idleSession.ID, UserID: userID, AuthEpoch: 0, VerifiedAt: now, Now: now, IdleCutoff: idleCutoff,
	}); !errors.Is(updateErr, pgx.ErrNoRows) {
		t.Errorf("idle session proof update error = %v, want pgx.ErrNoRows", updateErr)
	}

	rotatedSession, err := q.CreateSession(ctx, store.CreateSessionParams{
		UserID: userID, TokenHash: uniqueTokenDigest(), CSRFSecret: uniqueTokenDigest(),
		CreatedAt: now, LastSeenAt: now, ReauthenticatedAt: now, AbsoluteExpiresAt: now.Add(time.Hour), AuthEpoch: 0,
	})
	if err != nil {
		t.Fatalf("CreateSession(rotated): %v", err)
	}
	if _, err := q.BeginSessionRotation(ctx, store.BeginSessionRotationParams{ID: rotatedSession.ID, RotationGraceUntil: ptrTime(now.Add(-time.Second))}); err != nil {
		t.Fatalf("BeginSessionRotation: %v", err)
	}
	if _, err := q.UpdateCurrentSessionProofs(ctx, store.UpdateCurrentSessionProofsParams{
		ID: rotatedSession.ID, UserID: userID, AuthEpoch: 0, VerifiedAt: now, Now: now, IdleCutoff: idleCutoff,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("rotation-expired session proof update error = %v, want pgx.ErrNoRows", err)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func createSecondFactorPending(ctx context.Context, t *testing.T, q *store.Queries, userID uuid.UUID, createdAt time.Time) store.PendingAuthentication {
	t.Helper()
	pending, err := q.CreatePendingAuthentication(ctx, store.CreatePendingAuthenticationParams{
		TokenDigest: uniqueTokenDigest(), CSRFSecret: uniqueTokenDigest(), UserID: userID,
		Purpose: "login", AuthEpoch: 0, PrimaryVerifiedAt: createdAt, ReturnPath: "/app/resumes",
		CreatedAt: createdAt, ExpiresAt: createdAt.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreatePendingAuthentication: %v", err)
	}
	return pending
}

func createSecondFactorCeremony(ctx context.Context, t *testing.T, q *store.Queries, userID uuid.UUID, pendingID *uuid.UUID, createdAt time.Time) store.WebauthnCeremony {
	t.Helper()
	ceremony, err := q.CreateWebAuthnCeremony(ctx, store.CreateWebAuthnCeremonyParams{
		TokenDigest: uniqueTokenDigest(), ChallengeDigest: uniqueTokenDigest(), UserID: userID,
		Purpose: "assertion", AuthEpoch: 0, PendingAuthenticationID: pendingID,
		CreatedAt: createdAt, ExpiresAt: createdAt.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateWebAuthnCeremony: %v", err)
	}
	return ceremony
}
