package secondfactor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Re-encryption run bounds from docs/design/totp-key-management.md
// "Rotation" and docs/design/budgets.md "TOTP re-encryption" (AC-SEC-008):
// at most 200 rows locked per transaction, and at most 10,000 rows across at
// most 30 minutes per run.
const (
	totpReencryptBatchRows     int32 = 200
	totpReencryptMaxRowsPerRun int64 = 10_000
	totpReencryptMaxDuration         = 30 * time.Minute
)

// ErrTOTPReencryptFailed reports that a proactive re-encryption run ended
// with at least one row it could not move onto the active key. A decrypt
// failure leaves that row unchanged, so the run still fails even though it
// finishes every other row its row and time budget allowed
// (docs/design/totp-key-management.md "Rotation").
var ErrTOTPReencryptFailed = errors.New("secondfactor: totp re-encryption run failed")

// TOTPReencryptFailure names one row a re-encryption run could not decrypt.
// It carries no key, secret, nonce, ciphertext, email, or account ID
// (docs/design/totp-key-management.md "Rotation").
type TOTPReencryptFailure struct {
	// Kind is the record kind the row belongs to: credential or enrollment.
	Kind TOTPRecordKind
	// ID is the row's own internal ID, never the account ID.
	ID uuid.UUID
}

// TOTPReencryptResult reports one proactive re-encryption run's counts and
// touched row IDs only, never a key, secret, nonce, ciphertext, email, or
// account ID (docs/design/totp-key-management.md "Rotation").
type TOTPReencryptResult struct {
	// CredentialsReencrypted counts credential rows this run moved onto the
	// active key.
	CredentialsReencrypted int64
	// EnrollmentsReencrypted counts unexpired enrollment rows this run moved
	// onto the active key.
	EnrollmentsReencrypted int64
	// EnrollmentsExpiredDeleted counts expired enrollment rows this run
	// deleted without decrypting.
	EnrollmentsExpiredDeleted int64
	// DecryptFailures lists every row this run could not decrypt and left
	// unchanged.
	DecryptFailures []TOTPReencryptFailure
	// CredentialsOffActiveKey is the unlocked count(*) of every credential
	// row off the active key, taken after the batches.
	CredentialsOffActiveKey int64
	// EnrollmentsOffActiveKey is the unlocked count(*) of every enrollment
	// row off the active key, taken after the batches.
	EnrollmentsOffActiveKey int64
	// Batches counts the transactions this run committed, credentials first
	// and then enrollments.
	Batches int64
	// LimitReached reports whether the run stopped because it reached the
	// 10,000-row or 30-minute bound rather than exhausting every row off the
	// active key.
	LimitReached bool
	// DurationMilliseconds is the run's wall time under the injected clock.
	DurationMilliseconds int64
}

// TOTPRotationConfig supplies the proactive re-encryption runner's fixed
// dependencies. Logger may be nil.
type TOTPRotationConfig struct {
	Pool   *store.Pool
	Ring   *TOTPKeyRing
	Now    func() time.Time
	Signal *TOTPUnavailableSignal
	Logger *slog.Logger
}

// TOTPRotationRunner runs the bounded proactive re-encryption command
// (docs/design/totp-key-management.md "Rotation"; AC-SEC-008). It takes no
// user lock: every row it touches is locked directly with FOR UPDATE SKIP
// LOCKED, so it never waits behind a live factor transaction's user,
// session, policy, or credential lock. A skipped row belongs to a live
// factor transaction and waits for a later run.
type TOTPRotationRunner struct {
	pool   *store.Pool
	ring   *TOTPKeyRing
	now    func() time.Time
	signal *TOTPUnavailableSignal
	logger *slog.Logger

	// batchRows is the per-transaction row limit. It defaults to
	// totpReencryptBatchRows; tests in this package may set it directly to
	// exercise afterID paging without inserting hundreds of rows.
	batchRows int32
}

// NewTOTPRotationRunner validates config and returns the runner.
func NewTOTPRotationRunner(config TOTPRotationConfig) (*TOTPRotationRunner, error) {
	if config.Pool == nil || config.Ring == nil || config.Now == nil || config.Signal == nil {
		return nil, errors.New("secondfactor: totp rotation runner missing dependency")
	}
	return &TOTPRotationRunner{
		pool: config.Pool, ring: config.Ring, now: config.Now, signal: config.Signal, logger: config.Logger,
		batchRows: totpReencryptBatchRows,
	}, nil
}

// Run processes at most 10,000 rows in at most 30 minutes by the injected
// clock: every credential batch first, then every enrollment batch, each
// batch its own transaction of at most 200 rows locked FOR UPDATE SKIP
// LOCKED in id order. Enrollments already expired are deleted without
// decrypting; unexpired enrollments are resealed like credentials. A run
// with nothing to rewrite only counts. Run returns ErrTOTPReencryptFailed
// when any row failed to decrypt, after finishing every other row the run's
// budget allowed (docs/design/totp-key-management.md "Rotation").
func (r *TOTPRotationRunner) Run(ctx context.Context) (TOTPReencryptResult, error) {
	started := r.now()
	deadline := started.Add(totpReencryptMaxDuration)
	activeKeyID := r.ring.ActiveKeyID()

	runCtx, cancel := context.WithTimeout(ctx, totpReencryptMaxDuration)
	defer cancel()

	var result TOTPReencryptResult
	rowsProcessed := int64(0)

	afterID := uuid.Nil
	for rowsProcessed < totpReencryptMaxRowsPerRun && r.now().Before(deadline) {
		limit := totpReencryptBatchLimit(rowsProcessed, r.batchRows)
		n, lastID, err := r.reencryptCredentialBatch(runCtx, activeKeyID, afterID, limit, &result)
		if err != nil {
			return r.finish(ctx, started, result, err)
		}
		if n == 0 {
			break
		}
		rowsProcessed += n
		result.Batches++
		afterID = lastID
		if n < int64(limit) {
			break
		}
	}
	afterID = uuid.Nil
	for rowsProcessed < totpReencryptMaxRowsPerRun && r.now().Before(deadline) {
		limit := totpReencryptBatchLimit(rowsProcessed, r.batchRows)
		n, lastID, err := r.reencryptEnrollmentBatch(runCtx, activeKeyID, afterID, limit, &result)
		if err != nil {
			return r.finish(ctx, started, result, err)
		}
		if n == 0 {
			break
		}
		rowsProcessed += n
		result.Batches++
		afterID = lastID
		if n < int64(limit) {
			break
		}
	}
	result.LimitReached = rowsProcessed >= totpReencryptMaxRowsPerRun || !r.now().Before(deadline)

	q := store.New(r.pool)
	credentialsOff, err := q.CountTOTPCredentialsOffActiveKey(runCtx, activeKeyID)
	if err != nil {
		return r.finish(ctx, started, result, fmt.Errorf("secondfactor: count credentials off active key: %w", err))
	}
	enrollmentsOff, err := q.CountTOTPEnrollmentsOffActiveKey(runCtx, activeKeyID)
	if err != nil {
		return r.finish(ctx, started, result, fmt.Errorf("secondfactor: count enrollments off active key: %w", err))
	}
	result.CredentialsOffActiveKey = credentialsOff
	result.EnrollmentsOffActiveKey = enrollmentsOff

	var runErr error
	if len(result.DecryptFailures) > 0 {
		runErr = ErrTOTPReencryptFailed
	}
	return r.finish(ctx, started, result, runErr)
}

// totpReencryptBatchLimit returns the next batch's row limit: at most
// batchRows, and never more than the run's remaining 10,000-row budget.
func totpReencryptBatchLimit(rowsProcessed int64, batchRows int32) int32 {
	remaining := totpReencryptMaxRowsPerRun - rowsProcessed
	if remaining >= int64(batchRows) {
		return batchRows
	}
	if remaining <= 0 {
		return 0
	}
	// remaining is strictly less than batchRows, an int32, in this branch,
	// so it always fits without truncation.
	return int32(remaining) //nolint:gosec // G115: bounded above by batchRows (int32) by the branch condition above, so this cannot overflow
}

// reencryptCredentialBatch locks and processes one batch of credential rows
// off the active key, in id order after afterID, in one transaction. It
// returns the number of rows the batch locked, whether resealed or left
// unchanged after a decrypt failure, so the caller's row budget counts
// every row this run touched, and the batch's last row ID so the caller can
// page past it: a row that stays off the active key (a decrypt failure)
// would otherwise be reselected by every later batch of the same run
// (docs/design/totp-key-management.md "Rotation").
func (r *TOTPRotationRunner) reencryptCredentialBatch(ctx context.Context, activeKeyID string, afterID uuid.UUID, limit int32, result *TOTPReencryptResult) (int64, uuid.UUID, error) {
	var locked int64
	lastID := afterID
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		qtx := store.New(tx)
		rows, listErr := qtx.ListTOTPCredentialsOffActiveKey(ctx, store.ListTOTPCredentialsOffActiveKeyParams{
			ActiveKeyID: activeKeyID, AfterID: afterID, LimitRows: limit,
		})
		if listErr != nil {
			return fmt.Errorf("secondfactor: list credentials off active key: %w", listErr)
		}
		locked = int64(len(rows))
		now := r.now()
		for _, row := range rows {
			if rowErr := r.reencryptCredentialRow(ctx, qtx, row, now, result); rowErr != nil {
				return rowErr
			}
			lastID = row.ID
		}
		return nil
	})
	if txErr != nil {
		return 0, afterID, txErr
	}
	return locked, lastID, nil
}

// reencryptCredentialRow opens row under its stored key and, on success,
// reseals it under the active key with a fresh nonce, the same row ID, and
// the same credential binding (docs/design/totp-key-management.md
// "Sealing"). A decrypt failure leaves the row unchanged, is counted in
// result, and does not stop the batch.
func (r *TOTPRotationRunner) reencryptCredentialRow(ctx context.Context, qtx *store.Queries, row store.TotpCredential, now time.Time, result *TOTPReencryptResult) error {
	sealed := SealedTOTPSecret{KeyID: row.KeyID, Ciphertext: row.Ciphertext, Version: int(row.FormatVersion)}
	copy(sealed.Nonce[:], row.Nonce)
	secret, err := r.ring.Open(TOTPRecordKindCredential, row.UserID, row.ID, sealed)
	if err != nil {
		result.DecryptFailures = append(result.DecryptFailures, TOTPReencryptFailure{Kind: TOTPRecordKindCredential, ID: row.ID})
		r.signal.EmitDecryptFailure(TOTPRecordKindCredential, row.ID)
		return nil //nolint:nilerr // the failure is counted in result and reported through ErrTOTPReencryptFailed after the run, not per row ("Rotation": counted and skipped, the run continues)
	}
	resealed, err := r.ring.Seal(TOTPRecordKindCredential, row.UserID, row.ID, secret)
	if err != nil {
		return fmt.Errorf("secondfactor: reseal credential: %w", err)
	}
	affected, err := qtx.ReencryptTOTPCredential(ctx, store.ReencryptTOTPCredentialParams{
		NewKeyID: resealed.KeyID, Nonce: resealed.Nonce[:], Ciphertext: resealed.Ciphertext,
		Now: now, ID: row.ID, OldKeyID: row.KeyID,
	})
	if err != nil {
		return fmt.Errorf("secondfactor: reencrypt credential: %w", err)
	}
	if affected == 1 {
		result.CredentialsReencrypted++
	}
	return nil
}

// reencryptEnrollmentBatch locks and processes one batch of enrollment rows
// off the active key, in id order after afterID, in one transaction, the
// same shape and afterID paging as reencryptCredentialBatch.
func (r *TOTPRotationRunner) reencryptEnrollmentBatch(ctx context.Context, activeKeyID string, afterID uuid.UUID, limit int32, result *TOTPReencryptResult) (int64, uuid.UUID, error) {
	var locked int64
	lastID := afterID
	txErr := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		qtx := store.New(tx)
		rows, listErr := qtx.ListTOTPEnrollmentsOffActiveKey(ctx, store.ListTOTPEnrollmentsOffActiveKeyParams{
			ActiveKeyID: activeKeyID, AfterID: afterID, LimitRows: limit,
		})
		if listErr != nil {
			return fmt.Errorf("secondfactor: list enrollments off active key: %w", listErr)
		}
		locked = int64(len(rows))
		now := r.now()
		for _, row := range rows {
			if rowErr := r.reencryptEnrollmentRow(ctx, qtx, row, now, result); rowErr != nil {
				return rowErr
			}
			lastID = row.ID
		}
		return nil
	})
	if txErr != nil {
		return 0, afterID, txErr
	}
	return locked, lastID, nil
}

// reencryptEnrollmentRow deletes row without decrypting when it is already
// expired, and otherwise opens and reseals it exactly like
// reencryptCredentialRow, under the enrollment binding
// (docs/design/totp-key-management.md "Rotation").
func (r *TOTPRotationRunner) reencryptEnrollmentRow(ctx context.Context, qtx *store.Queries, row store.TotpEnrollment, now time.Time, result *TOTPReencryptResult) error {
	if !row.ExpiresAt.After(now) {
		affected, err := qtx.DeleteTOTPEnrollmentByID(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("secondfactor: delete expired enrollment: %w", err)
		}
		result.EnrollmentsExpiredDeleted += affected
		return nil
	}
	sealed := SealedTOTPSecret{KeyID: row.KeyID, Ciphertext: row.Ciphertext, Version: int(row.FormatVersion)}
	copy(sealed.Nonce[:], row.Nonce)
	secret, err := r.ring.Open(TOTPRecordKindEnrollment, row.UserID, row.ID, sealed)
	if err != nil {
		result.DecryptFailures = append(result.DecryptFailures, TOTPReencryptFailure{Kind: TOTPRecordKindEnrollment, ID: row.ID})
		r.signal.EmitDecryptFailure(TOTPRecordKindEnrollment, row.ID)
		return nil //nolint:nilerr // the failure is counted in result and reported through ErrTOTPReencryptFailed after the run, not per row ("Rotation": counted and skipped, the run continues)
	}
	resealed, err := r.ring.Seal(TOTPRecordKindEnrollment, row.UserID, row.ID, secret)
	if err != nil {
		return fmt.Errorf("secondfactor: reseal enrollment: %w", err)
	}
	affected, err := qtx.ReencryptTOTPEnrollment(ctx, store.ReencryptTOTPEnrollmentParams{
		NewKeyID: resealed.KeyID, Nonce: resealed.Nonce[:], Ciphertext: resealed.Ciphertext,
		ID: row.ID, OldKeyID: row.KeyID,
	})
	if err != nil {
		return fmt.Errorf("secondfactor: reencrypt enrollment: %w", err)
	}
	if affected == 1 {
		result.EnrollmentsReencrypted++
	}
	return nil
}

// finish sets result's duration under the injected clock, logs the run
// summary, and returns result and err unchanged.
func (r *TOTPRotationRunner) finish(ctx context.Context, started time.Time, result TOTPReencryptResult, err error) (TOTPReencryptResult, error) {
	result.DurationMilliseconds = r.now().Sub(started).Milliseconds()
	r.logResult(ctx, result, err)
	return result, err
}

// logResult logs one summary line per run: counts and durations only, never
// a key, secret, nonce, ciphertext, email, or account ID.
func (r *TOTPRotationRunner) logResult(ctx context.Context, result TOTPReencryptResult, err error) {
	if r.logger == nil {
		return
	}
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelError
	}
	r.logger.Log(ctx, level, "totp re-encryption run",
		"credentials_reencrypted", result.CredentialsReencrypted,
		"enrollments_reencrypted", result.EnrollmentsReencrypted,
		"enrollments_expired_deleted", result.EnrollmentsExpiredDeleted,
		"decrypt_failures", len(result.DecryptFailures),
		"credentials_off_active_key", result.CredentialsOffActiveKey,
		"enrollments_off_active_key", result.EnrollmentsOffActiveKey,
		"batches", result.Batches,
		"limit_reached", result.LimitReached,
		"duration_ms", result.DurationMilliseconds,
	)
}
