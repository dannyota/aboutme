package secondfactor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	totpReencryptBatchRows     int32 = 200
	totpReencryptMaxRowsPerRun int64 = 10_000
	totpReencryptMaxDuration         = 30 * time.Minute
)

var ErrTOTPReencryptFailed = errors.New("secondfactor: totp re-encryption run failed")

type TOTPReencryptFailure struct {
	Kind TOTPRecordKind
	ID   uuid.UUID
}

type TOTPReencryptResult struct {
	CredentialsReencrypted    int64
	EnrollmentsReencrypted    int64
	EnrollmentsExpiredDeleted int64
	DecryptFailures           []TOTPReencryptFailure
	CredentialsOffActiveKey   int64
	EnrollmentsOffActiveKey   int64
	Batches                   int64
	LimitReached              bool
	DurationMilliseconds      int64
}

type TOTPRotationConfig struct {
	Pool   *store.Pool
	Ring   *TOTPKeyRing
	Now    func() time.Time
	Signal *TOTPUnavailableSignal
	Logger *slog.Logger
}

type TOTPRotationRunner struct {
	pool   *store.Pool
	ring   *TOTPKeyRing
	now    func() time.Time
	signal *TOTPUnavailableSignal
	logger *slog.Logger

	batchRows int32
}

func NewTOTPRotationRunner(config TOTPRotationConfig) (*TOTPRotationRunner, error) {
	if config.Pool == nil || config.Ring == nil || config.Now == nil || config.Signal == nil {
		return nil, errors.New("secondfactor: totp rotation runner missing dependency")
	}
	return &TOTPRotationRunner{
		pool: config.Pool, ring: config.Ring, now: config.Now, signal: config.Signal, logger: config.Logger,
		batchRows: totpReencryptBatchRows,
	}, nil
}

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

func totpReencryptBatchLimit(rowsProcessed int64, batchRows int32) int32 {
	remaining := totpReencryptMaxRowsPerRun - rowsProcessed
	if remaining >= int64(batchRows) {
		return batchRows
	}
	if remaining <= 0 {
		return 0
	}
	return int32(remaining) //nolint:gosec // G115: bounded above by batchRows (int32) by the branch condition above, so this cannot overflow
}

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

func (r *TOTPRotationRunner) finish(ctx context.Context, started time.Time, result TOTPReencryptResult, err error) (TOTPReencryptResult, error) {
	result.DurationMilliseconds = r.now().Sub(started).Milliseconds()
	r.logResult(ctx, result, err)
	return result, err
}

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
