package secondfactor

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Recovery-code shape from docs/design/second-factor-authentication.md: ten
// codes of 128 random bits, shown as "amr_" plus 26 Crockford Base32
// characters in hyphen groups of 5, 5, 5, 5, 5, and 1.
const (
	recoveryCodeCount    = 10
	recoveryCodeBytes    = 16
	recoveryCodeChars    = 26
	recoveryCodePrefix   = "amr_"
	recoveryCodeMaxInput = 128
	crockfordAlphabet    = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

// recoveryDigestDomain separates recovery-code digests from every other
// SHA-256 use.
var recoveryDigestDomain = []byte("aboutme.recovery-code.v1\x00")

type recoveryValue [recoveryCodeBytes]byte

// recoveryCredential is a canonical recovery code awaiting verification.
type recoveryCredential struct {
	value recoveryValue
}

// SecondFactorMethod names the recovery method.
func (*recoveryCredential) SecondFactorMethod() string { return auth.SecondFactorMethodRecovery }

// DecodeRecoveryCode canonicalizes a submitted code before any database work.
// It accepts ASCII hyphens and spaces anywhere, any letter case, and the
// Crockford aliases O for 0 and I or L for 1; every other shape is rejected.
func (s *Service) DecodeRecoveryCode(code string) (auth.SecondFactorCredential, error) {
	value, err := parseRecoveryCode(code)
	if err != nil {
		return nil, err
	}
	return &recoveryCredential{value: value}, nil
}

// generateRecoveryCodes draws a fresh set of distinct codes from entropy.
func generateRecoveryCodes(entropy io.Reader) ([]recoveryValue, []string, error) {
	values := make([]recoveryValue, 0, recoveryCodeCount)
	display := make([]string, 0, recoveryCodeCount)
	seen := make(map[recoveryValue]bool, recoveryCodeCount)
	for len(values) < recoveryCodeCount {
		var value recoveryValue
		if _, err := io.ReadFull(entropy, value[:]); err != nil {
			return nil, nil, fmt.Errorf("secondfactor: generate recovery code: %w", err)
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		values = append(values, value)
		display = append(display, formatRecoveryCode(value))
	}
	return values, display, nil
}

// formatRecoveryCode encodes 128 bits as 26 Crockford characters. The 130-bit
// character stream carries two leading zero bits.
func formatRecoveryCode(value recoveryValue) string {
	var b strings.Builder
	b.WriteString(recoveryCodePrefix)
	for c := 0; c < recoveryCodeChars; c++ {
		if c > 0 && c%5 == 0 {
			b.WriteByte('-')
		}
		var n byte
		for bit := 0; bit < 5; bit++ {
			n <<= 1
			i := c*5 + bit - 2
			if i >= 0 && value[i/8]&(0x80>>(i%8)) != 0 {
				n |= 1
			}
		}
		b.WriteByte(crockfordAlphabet[n])
	}
	return b.String()
}

func parseRecoveryCode(input string) (recoveryValue, error) {
	var value recoveryValue
	if input == "" || len(input) > recoveryCodeMaxInput {
		return value, errRequestInvalid
	}
	var canonical strings.Builder
	for i := 0; i < len(input); i++ {
		c := input[i]
		switch {
		case c == '-' || c == ' ':
			continue
		case c >= 0x80:
			return value, errRequestInvalid
		case c >= 'a' && c <= 'z':
			c -= 'a' - 'A'
		}
		canonical.WriteByte(c)
	}
	text := canonical.String()
	if !strings.HasPrefix(text, strings.ToUpper(recoveryCodePrefix)) || len(text) != len(recoveryCodePrefix)+recoveryCodeChars {
		return value, errRequestInvalid
	}
	for c, char := range []byte(text[len(recoveryCodePrefix):]) {
		switch char {
		case 'O':
			char = '0'
		case 'I', 'L':
			char = '1'
		}
		n := strings.IndexByte(crockfordAlphabet, char)
		if n < 0 {
			return value, errRequestInvalid
		}
		for bit := 0; bit < 5; bit++ {
			set := n&(0x10>>bit) != 0
			i := c*5 + bit - 2
			switch {
			case i < 0 && set:
				return value, errRequestInvalid
			case i >= 0 && set:
				value[i/8] |= 0x80 >> (i % 8)
			}
		}
	}
	return value, nil
}

// recoveryCodeDigest binds a code to its account under a separate domain.
func recoveryCodeDigest(userID uuid.UUID, value recoveryValue) []byte {
	h := sha256.New()
	h.Write(recoveryDigestDomain)
	h.Write(userID[:])
	h.Write(value[:])
	return h.Sum(nil)
}

func insertRecoveryCodes(ctx context.Context, qtx *store.Queries, userID uuid.UUID, values []recoveryValue, now time.Time) error {
	for _, value := range values {
		if _, err := qtx.CreateSecondFactorRecoveryCode(ctx, store.CreateSecondFactorRecoveryCodeParams{
			UserID: userID, CodeDigest: recoveryCodeDigest(userID, value), CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("create recovery code: %w", err)
		}
	}
	return nil
}

// completeRecovery locks the user, deletes exactly one matching unused digest,
// records the notification with the remaining count, and completes the
// pending authentication in one transaction. A code that matches nothing is
// one failed attempt.
func (s *Service) completeRecovery(ctx context.Context, pendingToken string, sessionID *uuid.UUID, code *recoveryCredential, client auth.SecondFactorClient) (*auth.SessionIssue, error) {
	var issue *auth.SessionIssue
	_, err := s.pending.WithLivePending(ctx, pendingToken, sessionID, lockPolicyBeforePending,
		func(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked) error {
			now := s.now()
			_, consumeErr := qtx.ConsumeSecondFactorRecoveryCode(ctx, store.ConsumeSecondFactorRecoveryCodeParams{
				UserID: locked.User.ID, CodeDigest: recoveryCodeDigest(locked.User.ID, code.value),
			})
			if errors.Is(consumeErr, pgx.ErrNoRows) {
				return s.failAttempt(locked.User)
			}
			if consumeErr != nil {
				return fmt.Errorf("consume recovery code: %w", consumeErr)
			}
			remaining, countErr := qtx.CountSecondFactorRecoveryCodes(ctx, locked.User.ID)
			if countErr != nil {
				return fmt.Errorf("count recovery codes: %w", countErr)
			}
			left := int(remaining)
			if mailErr := s.enqueueSecurityMail(ctx, qtx, locked.User, authmail.KindRecoveryCodeUsed, now, &left); mailErr != nil {
				return mailErr
			}
			var finishErr error
			issue, finishErr = s.finishPending(ctx, qtx, locked, client, now)
			return finishErr
		})
	if err != nil {
		return nil, err
	}
	return issue, nil
}

// RegenerateRecoveryCodes replaces every digest of an enrolled account, then
// advances the epoch, revokes other sessions and agent grants, rotates the
// current session, and records the notification in the same transaction. It
// requires recent primary and factor proof.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, current store.Session) (auth.SecondFactorRecoveryCodes, error) {
	values, display, err := generateRecoveryCodes(s.entropy)
	if err != nil {
		return auth.SecondFactorRecoveryCodes{}, err
	}
	var issue auth.SessionIssue
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		now := s.now()
		user, sess, policy, lockErr := lockAccount(ctx, qtx, current, now)
		if lockErr != nil {
			return lockErr
		}
		if reauthErr := auth.RequireRecentSecondFactorReauth(user, policy, sess, now); reauthErr != nil {
			return reauthErr
		}
		if policy == nil {
			return auth.ErrSecondFactorNotFound
		}
		if _, deleteErr := qtx.DeleteSecondFactorRecoveryCodesForUser(ctx, user.ID); deleteErr != nil {
			return fmt.Errorf("delete recovery codes: %w", deleteErr)
		}
		if insertErr := insertRecoveryCodes(ctx, qtx, user.ID, values, now); insertErr != nil {
			return insertErr
		}
		var changeErr error
		issue, changeErr = s.commitFactorChange(ctx, qtx, user, sess, sess.SecondFactorVerifiedAt, authmail.KindRecoveryCodesRegenerated, now)
		return changeErr
	})
	if err != nil {
		return auth.SecondFactorRecoveryCodes{}, err
	}
	return auth.SecondFactorRecoveryCodes{Codes: display, Session: issue}, nil
}

// finishPending consumes the pending row and grants its purpose. Login issues
// a fresh session with both proof times set to now; reauth sets both on the
// bound session.
func (s *Service) finishPending(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked, client auth.SecondFactorClient, now time.Time) (*auth.SessionIssue, error) {
	if err := s.pending.ClaimTx(ctx, qtx, locked.Pending); err != nil {
		return nil, err
	}
	proofs := store.UpdateCurrentSessionProofsParams{
		VerifiedAt: now, UserID: locked.User.ID, AuthEpoch: locked.User.AuthEpoch,
		IdleCutoff: now.Add(-sessionIdleTimeout), Now: now,
	}
	if locked.Pending.Purpose == auth.PendingAuthenticationPurposeLogin {
		issued, err := s.sessions.IssueTx(ctx, qtx, locked.User, client.UserAgent, client.IP)
		if err != nil {
			return nil, err
		}
		proofs.ID = issued.Session.ID
		if issued.Session, err = qtx.UpdateCurrentSessionProofs(ctx, proofs); err != nil {
			return nil, fmt.Errorf("set session proofs: %w", err)
		}
		return &issued, nil
	}
	if locked.Session == nil {
		return nil, auth.ErrPendingAuthenticationRequired
	}
	proofs.ID = locked.Session.ID
	if _, err := qtx.UpdateCurrentSessionProofs(ctx, proofs); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, auth.ErrPendingAuthenticationRequired
		}
		return nil, fmt.Errorf("set session proofs: %w", err)
	}
	return nil, nil
}

// failAttempt reports one failed verification. The fifth failure enqueues the
// exhaustion notification in the same transaction.
func (s *Service) failAttempt(user store.User) error {
	return auth.FailPendingVerification(auth.ErrSecondFactorVerificationFailed,
		func(ctx context.Context, qtx *store.Queries, _ store.PendingAuthentication) error {
			return s.enqueueSecurityMail(ctx, qtx, user, authmail.KindSecondFactorAttemptsExhausted, s.now(), nil)
		})
}

// enqueueSecurityMail records one version 2 security notification that
// expires 24 hours after the event, rounded down to whole UTC seconds.
func (s *Service) enqueueSecurityMail(ctx context.Context, qtx *store.Queries, user store.User, kind authmail.Kind, now time.Time, remaining *int) error {
	jobID, err := uuid.NewV7FromReader(s.entropy)
	if err != nil {
		return fmt.Errorf("security mail id: %w", err)
	}
	occurred := now.UTC().Truncate(time.Second)
	userID := user.ID
	if err = s.outbox.EnqueueTx(ctx, qtx, authmail.EnqueueRequest{
		JobID: jobID, Kind: kind, UserID: &userID, ExpiresAt: occurred.Add(securityMailLifetime),
		Payload: authmail.Payload{
			Version: securityPayloadVersion, To: user.Email, OccurredAt: occurred.Format(time.RFC3339),
			RemainingRecoveryCodes: remaining,
		},
	}); err != nil {
		return fmt.Errorf("enqueue security mail: %w", err)
	}
	return nil
}
