package authmail

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func readAADJob(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID) store.AuthEmailJob {
	t.Helper()
	var job store.AuthEmailJob
	err := pool.QueryRow(ctx, `SELECT id, kind, user_id, registration_id, reset_token_id,
		token_digest, key_id, nonce, ciphertext FROM auth_email_jobs WHERE id=$1`, id).Scan(
		&job.ID, &job.Kind, &job.UserID, &job.RegistrationID, &job.ResetTokenID,
		&job.TokenDigest, &job.KeyID, &job.Nonce, &job.Ciphertext)
	if err != nil {
		t.Fatalf("read job: %v", err)
	}
	return job
}

func TestWorker_RejectsChangedJobIdentity(t *testing.T) {
	for _, tc := range []struct {
		column string
		kind   Kind
		sql    string
	}{
		{"id", KindPasswordChanged, `UPDATE auth_email_jobs SET id=$2 WHERE id=$1`},
		{"kind", KindPasswordChanged, `UPDATE auth_email_jobs SET kind=$2 WHERE id=$1`},
		{"key_id", KindPasswordChanged, `UPDATE auth_email_jobs SET key_id=$2 WHERE id=$1`},
		{"user_id", KindPasswordChanged, `UPDATE auth_email_jobs SET user_id=$2 WHERE id=$1`},
		{"registration_id", KindVerify, `UPDATE auth_email_jobs SET registration_id=$2 WHERE id=$1`},
		{"reset_token_id", KindReset, `UPDATE auth_email_jobs SET reset_token_id=$2 WHERE id=$1`},
		{"token_digest", KindVerify, `UPDATE auth_email_jobs SET token_digest=$2 WHERE id=$1`},
	} {
		t.Run(tc.column, func(t *testing.T) {
			ctx, pool, q := newWorkerPool(t)
			clock := testutil.NewClockAtEpoch()
			// Identical key material isolates key-ID authentication from key lookup.
			ring := mustRing(t, "k-active", map[string][32]byte{
				"k-active": fixedKey(), "k-other": fixedKey(),
			}, fixedNonce())
			user, err := q.CreateUser(ctx, store.CreateUserParams{Email: "original@example.com", Name: "Fixture"})
			if err != nil {
				t.Fatal(err)
			}
			other, err := q.CreateUser(ctx, store.CreateUserParams{Email: "other@example.com", Name: "Fixture"})
			if err != nil {
				t.Fatal(err)
			}
			req := EnqueueRequest{JobID: uuid.New(), Kind: tc.kind, UserID: &user.ID,
				Payload: validPayloadForKind(tc.kind), ExpiresAt: clock.Now().Add(10 * time.Minute)}
			replacementID := uuid.New()
			var replacement any = replacementID
			switch tc.kind {
			case KindVerify:
				reg, digest := newWorkerRegistration(ctx, t, pool, clock.Now())
				req.UserID, req.RegistrationID, req.TokenDigest = nil, &reg, &digest
			case KindReset:
				digest := uniqueDigest()
				token, tokenErr := q.CreatePasswordResetToken(ctx, store.CreatePasswordResetTokenParams{
					UserID: user.ID, TokenDigest: digest[:], CreatedAt: clock.Now(), ExpiresAt: clock.Now().Add(30 * time.Minute),
				})
				if tokenErr != nil {
					t.Fatal(tokenErr)
				}
				req.UserID, req.ResetTokenID, req.TokenDigest = nil, &token.ID, &digest
			}
			if err := newTestOutbox(t, ring, clock.Now).EnqueueTx(ctx, q, req); err != nil {
				t.Fatal(err)
			}
			original := readAADJob(ctx, t, pool, req.JobID)
			if _, err := ring.Open(original.ID, tc.kind, scopeFromJob(original), sealedFromJob(original)); err != nil {
				t.Fatalf("open original: %v", err)
			}
			switch tc.column {
			case "kind":
				replacement = string(KindTOTPAdded)
			case "key_id":
				replacement = "k-other"
			case "user_id":
				replacement = other.ID
			case "registration_id":
				reg, _ := newWorkerRegistration(ctx, t, pool, clock.Now())
				digest := uniqueDigest()
				if _, err := pool.Exec(ctx, `UPDATE password_registrations SET token_digest=$2 WHERE id=$1`, *req.RegistrationID, digest[:]); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `UPDATE password_registrations SET token_digest=$2 WHERE id=$1`, reg, original.TokenDigest); err != nil {
					t.Fatal(err)
				}
				replacement = reg
			case "reset_token_id":
				digest := uniqueDigest()
				if _, err := pool.Exec(ctx, `UPDATE password_reset_tokens SET token_digest=$2 WHERE id=$1`, *req.ResetTokenID, digest[:]); err != nil {
					t.Fatal(err)
				}
				token, tokenErr := q.CreatePasswordResetToken(ctx, store.CreatePasswordResetTokenParams{
					UserID: other.ID, TokenDigest: original.TokenDigest, CreatedAt: clock.Now(), ExpiresAt: clock.Now().Add(30 * time.Minute),
				})
				if tokenErr != nil {
					t.Fatal(tokenErr)
				}
				replacement = token.ID
			case "token_digest":
				digest := uniqueDigest()
				if _, err := pool.Exec(ctx, `UPDATE password_registrations SET token_digest=$2 WHERE id=$1`, *req.RegistrationID, digest[:]); err != nil {
					t.Fatal(err)
				}
				replacement = digest[:]
			}
			if _, err := pool.Exec(ctx, tc.sql, req.JobID, replacement); err != nil {
				t.Fatalf("change %s: %v", tc.column, err)
			}
			jobID := req.JobID
			if tc.column == "id" {
				jobID = replacementID
			}
			changed := readAADJob(ctx, t, pool, jobID)
			if _, err := ring.Open(changed.ID, Kind(changed.Kind), scopeFromJob(changed), sealedFromJob(changed)); !errors.Is(err, ErrAuthentication) {
				t.Errorf("open changed %s = %v, want ErrAuthentication", tc.column, err)
			}
			sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
			worker := newTestWorker(t, pool, q, ring, sender, clock, nil, uuid.New())
			if err := worker.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if sender.count() != 0 {
				t.Errorf("changed %s sent %d messages", tc.column, sender.count())
			}
			var terminal bool
			if err := pool.QueryRow(ctx, `SELECT state='terminal' AND ciphertext IS NULL AND lease_owner IS NULL AND sent_at IS NULL FROM auth_email_jobs WHERE id=$1`, jobID).Scan(&terminal); err != nil || !terminal {
				t.Fatalf("changed job not terminal and cleared: %v", err)
			}
		})
	}
}

func TestWorker_RejectsV1Job(t *testing.T) {
	ctx, pool, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := testRing(t, 1)
	reg, digest := newWorkerRegistration(ctx, t, pool, clock.Now())
	id := enqueueVerifyJob(ctx, t, q, ring, clock.Now, reg, digest)
	sealed := sealV1Payload(t, ring, id, KindVerify, validVerifyPayload())
	if _, err := pool.Exec(ctx, `UPDATE auth_email_jobs SET key_id=$2, nonce=$3, ciphertext=$4 WHERE id=$1`, id, sealed.KeyID, sealed.Nonce[:], sealed.Ciphertext); err != nil {
		t.Fatal(err)
	}
	job := readAADJob(ctx, t, pool, id)
	if _, err := ring.Open(id, KindVerify, scopeFromJob(job), sealedFromJob(job)); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("open v1 job = %v, want ErrAuthentication", err)
	}
	sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
	worker := newTestWorker(t, pool, q, ring, sender, clock, nil, uuid.New())
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	state, lease := jobState(ctx, t, pool, id)
	if sender.count() != 0 || state != "terminal" || lease != "" {
		t.Fatalf("v1 job sends=%d state=%s lease=%s", sender.count(), state, lease)
	}
}

func sealV1Payload(t *testing.T, ring *KeyRing, id uuid.UUID, kind Kind, payload Payload) Sealed {
	t.Helper()
	plaintext, err := marshalPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	legacyAAD := append([]byte("aboutme.auth-email.v1\x00"), id[:]...)
	legacyAAD = append(legacyAAD, 0)
	legacyAAD = append(legacyAAD, kind...)
	legacyAAD = append(legacyAAD, 0)
	legacyAAD = append(legacyAAD, ring.activeID...)
	sealed := Sealed{KeyID: ring.activeID}
	copy(sealed.Nonce[:], fixedNonce())
	sealed.Ciphertext, err = sealAEAD(ring.keys[ring.activeID], sealed.Nonce[:], plaintext, legacyAAD)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func TestPrivacyNoticeWorker_RejectsV1Job(t *testing.T) {
	ctx, worker, _, id := noticeWorkerFixture(t, SendAccepted)
	job := readAADJob(ctx, t, worker.queriesDB(), id)
	payload, err := worker.ring.Open(id, KindPrivacyNotice, scopeFromJob(job), sealedFromJob(job))
	if err != nil {
		t.Fatal(err)
	}
	sealed := sealV1Payload(t, worker.ring, id, KindPrivacyNotice, payload)
	if _, updateErr := worker.queriesDB().Exec(ctx, `UPDATE auth_email_jobs SET nonce=$2, ciphertext=$3 WHERE id=$1`, id, sealed.Nonce[:], sealed.Ciphertext); updateErr != nil {
		t.Fatal(updateErr)
	}
	if runErr := worker.RunOnce(ctx); runErr != nil {
		t.Fatal(runErr)
	}
	var failed bool
	err = worker.queriesDB().QueryRow(ctx, `SELECT j.state='terminal' AND j.ciphertext IS NULL AND r.state='failed' AND r.failure_code='payload_invalid'
		FROM auth_email_jobs j JOIN privacy_notice_recipients r ON r.job_id=j.id WHERE j.id=$1`, id).Scan(&failed)
	if err != nil || !failed || noticeSenderCalls(t, worker) != 0 {
		t.Fatalf("v1 notice failed=%v sends=%d err=%v", failed, noticeSenderCalls(t, worker), err)
	}
}
