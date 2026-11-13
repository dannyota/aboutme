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

const (
	maxActivePasskeys            = 5
	ceremonyLifetime             = 5 * time.Minute
	cleanupLimit           int32 = 200
	securityMailLifetime         = 24 * time.Hour
	securityPayloadVersion       = 2
	sessionIdleTimeout           = 30 * 24 * time.Hour
)

const (
	purposeRegistration = "registration"
	purposeAssertion    = "assertion"
)

type FactorCounter func(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error)

func PasskeyCounter(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error) {
	credentials, err := qtx.ListWebAuthnCredentialsForUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("list passkeys: %w", err)
	}
	return len(credentials), nil
}

type Options struct {
	Pool                  *store.Pool
	Pending               *auth.PendingAuthenticationManager
	Sessions              *auth.SessionManager
	Outbox                *authmail.Outbox
	RelyingParty          *RelyingParty
	EnrollmentEnabled     bool
	TOTPKeyRing           *TOTPKeyRing
	TOTPEnrollmentEnabled bool
	TOTPIssuer            string
	TOTPSignal            *TOTPUnavailableSignal
	Counters              []FactorCounter
	Clock                 func() time.Time
	Entropy               io.Reader
	Logger                *slog.Logger
}

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

	counterEventProbe func() error
}

var _ auth.SecondFactorService = (*Service)(nil)
var _ auth.TOTPSecondFactorService = (*Service)(nil)

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

func (s *Service) PasskeyEnrollmentEnabled() bool {
	return s.enabled
}

func (s *Service) DecodePasskeyRegistration(body []byte) (auth.SecondFactorCredential, error) {
	reg, err := decodeRegistration(body)
	if err != nil {
		return nil, err
	}
	return reg, nil
}

func (s *Service) DecodePasskeyAssertion(body []byte) (auth.SecondFactorCredential, error) {
	assertion, err := decodeAssertion(body)
	if err != nil {
		return nil, err
	}
	return assertion, nil
}

func (s *Service) ActiveFactorCount(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error) {
	if _, err := qtx.GetUserForUpdate(ctx, userID); err != nil {
		return 0, fmt.Errorf("lock user: %w", err)
	}
	return s.countActiveFactors(ctx, qtx, userID)
}

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
