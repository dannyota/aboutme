package secondfactor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/store"
)

// Service limits and lifetimes from docs/design/budgets.md.
const (
	maxActivePasskeys            = 5
	ceremonyLifetime             = 5 * time.Minute
	cleanupLimit           int32 = 200
	securityMailLifetime         = 24 * time.Hour
	securityPayloadVersion       = 2
	// sessionIdleTimeout matches the session idle timeout in
	// docs/design/security.md; proof updates never revive an idle session.
	sessionIdleTimeout = 30 * 24 * time.Hour
)

// Ceremony purposes stored in webauthn_ceremonies.purpose.
const (
	purposeRegistration = "registration"
	purposeAssertion    = "assertion"
)

// FactorCounter counts one factor type's active credentials. It must be a
// plain read with no row lock; ActiveFactorCount supplies the user lock.
type FactorCounter func(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error)

// PasskeyCounter counts active passkeys.
func PasskeyCounter(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error) {
	credentials, err := qtx.ListWebAuthnCredentialsForUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("list passkeys: %w", err)
	}
	return len(credentials), nil
}

// Options is the service dependency set. Counters lists one counter per
// factor type; nil registers PasskeyCounter and TOTPCounter. Logger may be
// nil. TOTPKeyRing, TOTPIssuer, and TOTPSignal are required only when
// TOTPEnrollmentEnabled is true; a nil TOTPSignal defaults to a silent one
// (docs/design/totp-key-management.md).
type Options struct {
	// Pool is the database pool every factor transaction runs on. Required.
	Pool *store.Pool
	// Pending locks and claims live pending authentications. Required.
	Pending *auth.PendingAuthenticationManager
	// Sessions issues a session on pending completion and replaces the
	// current one after a factor change. Required.
	Sessions *auth.SessionManager
	// Outbox enqueues security mail in the caller's transaction. Required.
	Outbox *authmail.Outbox
	// RelyingParty is the WebAuthn relying-party policy. Required.
	RelyingParty *RelyingParty
	// EnrollmentEnabled is the passkey enrollment flag.
	EnrollmentEnabled bool
	// TOTPKeyRing seals and opens TOTP secrets. Required when
	// TOTPEnrollmentEnabled is true.
	TOTPKeyRing *TOTPKeyRing
	// TOTPEnrollmentEnabled is the TOTP enrollment flag.
	TOTPEnrollmentEnabled bool
	// TOTPIssuer is the provisioning URI issuer. It must pass
	// ValidateTOTPIssuer when TOTPEnrollmentEnabled is true.
	TOTPIssuer string
	// TOTPSignal emits the shared totp_unavailable signal. Nil defaults to a
	// silent signal.
	TOTPSignal *TOTPUnavailableSignal
	// Counters lists one counter per factor type. Nil registers
	// PasskeyCounter and TOTPCounter.
	Counters []FactorCounter
	// Clock returns the current time. Required.
	Clock func() time.Time
	// Entropy is the source of every random value the service draws.
	// Required.
	Entropy io.Reader
	// Logger receives cleanup warnings and security-event logs. It may be
	// nil.
	Logger *slog.Logger
}

// Service implements auth.SecondFactorService for passkeys and recovery
// codes. Every mutation follows the lock order in
// docs/design/second-factor-authentication.md.
type Service struct {
	pool        *store.Pool
	q           *store.Queries
	pending     *auth.PendingAuthenticationManager
	sessions    *auth.SessionManager
	outbox      *authmail.Outbox
	rp          *RelyingParty
	enabled     bool
	totpRing    *TOTPKeyRing
	totpEnabled bool
	totpIssuer  string
	totpSignal  *TOTPUnavailableSignal
	counters    []FactorCounter
	now         func() time.Time
	entropy     io.Reader
	logger      *slog.Logger

	// counterEventProbe is nil in production. Rollback tests set it to fail
	// the counter security-event write.
	counterEventProbe func() error
}

var _ auth.SecondFactorService = (*Service)(nil)
var _ auth.TOTPSecondFactorService = (*Service)(nil)

// New validates every dependency and returns the service. Enabling TOTP
// enrollment without a key ring or a valid issuer fails construction closed,
// the same way a malformed key ring fails startup
// (docs/design/totp-key-management.md#key-ring).
func New(opts Options) (*Service, error) {
	if opts.Pool == nil || opts.Pending == nil || opts.Sessions == nil || opts.Outbox == nil ||
		opts.RelyingParty == nil || opts.Clock == nil || opts.Entropy == nil {
		return nil, errors.New("secondfactor: missing dependency")
	}
	if opts.TOTPEnrollmentEnabled {
		if opts.TOTPKeyRing == nil {
			return nil, errors.New("secondfactor: totp enrollment enabled without a key ring")
		}
		if err := ValidateTOTPIssuer(opts.TOTPIssuer); err != nil {
			return nil, fmt.Errorf("secondfactor: totp issuer: %w", err)
		}
	}
	counters := opts.Counters
	if counters == nil {
		counters = []FactorCounter{PasskeyCounter, TOTPCounter}
	}
	signal := opts.TOTPSignal
	if signal == nil {
		signal = NewTOTPUnavailableSignal(opts.Clock, nil)
	}
	return &Service{
		pool: opts.Pool, q: store.New(opts.Pool), pending: opts.Pending, sessions: opts.Sessions,
		outbox: opts.Outbox, rp: opts.RelyingParty, enabled: opts.EnrollmentEnabled, counters: counters,
		totpRing: opts.TOTPKeyRing, totpEnabled: opts.TOTPEnrollmentEnabled, totpIssuer: opts.TOTPIssuer, totpSignal: signal,
		now: opts.Clock, entropy: opts.Entropy, logger: opts.Logger,
	}, nil
}

// PasskeyEnrollmentEnabled reports the passkey enrollment flag.
func (s *Service) PasskeyEnrollmentEnabled() bool {
	return s.enabled
}

// DecodePasskeyRegistration strictly decodes a registration completion.
func (s *Service) DecodePasskeyRegistration(body []byte) (auth.SecondFactorCredential, error) {
	reg, err := decodeRegistration(body)
	if err != nil {
		return nil, err
	}
	return reg, nil
}

// DecodePasskeyAssertion strictly decodes an assertion completion.
func (s *Service) DecodePasskeyAssertion(body []byte) (auth.SecondFactorCredential, error) {
	assertion, err := decodeAssertion(body)
	if err != nil {
		return nil, err
	}
	return assertion, nil
}

// ActiveFactorCount takes the user lock and sums every registered factor
// counter. It is the only authority for final versus non-final removal.
func (s *Service) ActiveFactorCount(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error) {
	if _, err := qtx.GetUserForUpdate(ctx, userID); err != nil {
		return 0, fmt.Errorf("lock user: %w", err)
	}
	return s.countActiveFactors(ctx, qtx, userID)
}

// countActiveFactors sums every registered counter. Counters are plain reads,
// so this also runs inside a read-only snapshot.
func (s *Service) countActiveFactors(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error) {
	total := 0
	for _, count := range s.counters {
		n, err := count(ctx, qtx, userID)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// PendingMethods lists the pending methods in fixed order: passkey when an
// active passkey exists, then recovery while a code remains.
func (s *Service) PendingMethods(ctx context.Context, userID uuid.UUID) ([]string, error) {
	credentials, err := s.q.ListWebAuthnCredentialsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("secondfactor: list passkeys: %w", err)
	}
	totpCount, err := s.q.CountTOTPCredentialsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("secondfactor: count totp credentials: %w", err)
	}
	codes, err := s.q.CountSecondFactorRecoveryCodes(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("secondfactor: count recovery codes: %w", err)
	}
	// Fixed order passkey, totp, then recovery
	// ("Release surface"; AC-AUTH-028). A cool-down or key failure still
	// lists totp: PendingMethods only reports existence, not availability.
	methods := make([]string, 0, 3)
	if len(credentials) > 0 {
		methods = append(methods, auth.SecondFactorMethodPasskey)
	}
	if totpCount > 0 {
		methods = append(methods, auth.SecondFactorMethodTOTP)
	}
	if codes > 0 {
		methods = append(methods, auth.SecondFactorMethodRecovery)
	}
	return methods, nil
}

// State reads enforcement, passkeys in (created_at, id) order, and the
// remaining recovery-code count from one read-only REPEATABLE READ snapshot
// that takes no row lock. Enforcement is derived from the active-factor
// counters and the code count in that snapshot: the policy row exists exactly
// while an active factor exists, and codes exist only under a policy, so the
// view is never torn by a concurrent enrollment or removal.
func (s *Service) State(ctx context.Context, userID uuid.UUID) (auth.SecondFactorState, error) {
	var state auth.SecondFactorState
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	err := pgx.BeginTxFunc(ctx, s.pool, options, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		factors, err := s.countActiveFactors(ctx, qtx, userID)
		if err != nil {
			return err
		}
		credentials, err := qtx.ListWebAuthnCredentialsForUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("list passkeys: %w", err)
		}
		codes, err := qtx.CountSecondFactorRecoveryCodes(ctx, userID)
		if err != nil {
			return fmt.Errorf("count recovery codes: %w", err)
		}
		state = auth.SecondFactorState{Enabled: factors > 0 || codes > 0, Passkeys: passkeyViews(credentials), RecoveryCodesRemaining: int(codes)}
		return nil
	})
	if err != nil {
		return auth.SecondFactorState{}, fmt.Errorf("secondfactor: state: %w", err)
	}
	return state, nil
}

func (s *Service) random(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(s.entropy, b); err != nil {
		return nil, fmt.Errorf("secondfactor: entropy: %w", err)
	}
	return b, nil
}

func passkeyView(credential store.WebauthnCredential) auth.SecondFactorPasskey {
	return auth.SecondFactorPasskey{ID: credential.ID, CreatedAt: credential.CreatedAt, LastUsedAt: credential.LastUsedAt}
}

func passkeyViews(credentials []store.WebauthnCredential) []auth.SecondFactorPasskey {
	views := make([]auth.SecondFactorPasskey, 0, len(credentials))
	for _, credential := range credentials {
		views = append(views, passkeyView(credential))
	}
	return views
}
