package auth

// The second-factor service contract the routes call, its closed errors and
// view types, and the route rate policies. The factor implementation lives in
// internal/secondfactor, which imports this package.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Pending method names, in the fixed order the status response lists them:
// a passkey assertion, then a single-use recovery code.
const (
	SecondFactorMethodPasskey  = "passkey"
	SecondFactorMethodRecovery = "recovery"
)

// Closed second-factor service errors. Handlers map only these, the session
// and pending sentinels, and PendingVerificationFailure to wire responses.
var (
	ErrSecondFactorRequestInvalid     = errors.New("auth: second factor request invalid")
	ErrSecondFactorChallengeInvalid   = errors.New("auth: second factor challenge invalid")
	ErrSecondFactorVerificationFailed = errors.New("auth: second factor verification failed")
	ErrSecondFactorNotFound           = errors.New("auth: second factor not found")
	ErrSecondFactorLimitReached       = errors.New("auth: second factor limit reached")
	ErrSecondFactorEnrollmentDisabled = errors.New("auth: second factor enrollment disabled")
	errSecondFactorBodyTooLarge       = errors.New("auth: second factor body too large")
	errSecondFactorMediaTypeRejected  = errors.New("auth: second factor media type rejected")
	errSecondFactorCSRFRejected       = errors.New("auth: second factor csrf rejected")
)

// SecondFactorCredential is one decoded, bounded factor response that only
// its decoding service interprets.
type SecondFactorCredential interface {
	SecondFactorMethod() string
}

// SecondFactorOptions is an options response: a random ceremony ID and a
// standards-shaped WebAuthn publicKey object.
type SecondFactorOptions struct {
	CeremonyID string          `json:"ceremonyId"`
	PublicKey  json.RawMessage `json:"publicKey"`
}

// SecondFactorClient is the device metadata a login session records.
type SecondFactorClient struct {
	UserAgent string
	IP        string
}

// SecondFactorPasskey is the display view of one passkey.
type SecondFactorPasskey struct {
	ID         uuid.UUID
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// SecondFactorState is the account factor state, without factor material.
type SecondFactorState struct {
	Enabled                bool
	Passkeys               []SecondFactorPasskey
	RecoveryCodesRemaining int
}

// SecondFactorRegistration reports a stored passkey. RecoveryCodes is set only
// on first enrollment. Session is the replacement session.
type SecondFactorRegistration struct {
	Passkey       SecondFactorPasskey
	RecoveryCodes []string
	Session       SessionIssue
}

// SecondFactorRecoveryCodes is a regenerated set and replacement session.
type SecondFactorRecoveryCodes struct {
	Codes   []string
	Session SessionIssue
}

// SecondFactorService is the factor behavior behind the second-factor routes.
// Account methods take the authenticated session and recheck it under the
// user lock. Pending methods take the raw pending token and the browser's
// current session ID, if any.
type SecondFactorService interface {
	PasskeyEnrollmentEnabled() bool
	DecodePasskeyRegistration(body []byte) (SecondFactorCredential, error)
	DecodePasskeyAssertion(body []byte) (SecondFactorCredential, error)
	DecodeRecoveryCode(code string) (SecondFactorCredential, error)
	PendingMethods(ctx context.Context, userID uuid.UUID) ([]string, error)
	StartPasskeyAssertion(ctx context.Context, pendingToken string, sessionID *uuid.UUID) (SecondFactorOptions, error)
	// CompletePending returns the new session for login and nil for reauth,
	// which refreshes the bound session in place.
	CompletePending(ctx context.Context, pendingToken string, sessionID *uuid.UUID, credential SecondFactorCredential, client SecondFactorClient) (*SessionIssue, error)
	State(ctx context.Context, userID uuid.UUID) (SecondFactorState, error)
	StartPasskeyRegistration(ctx context.Context, sess store.Session) (SecondFactorOptions, error)
	CompletePasskeyRegistration(ctx context.Context, sess store.Session, credential SecondFactorCredential) (SecondFactorRegistration, error)
	RemovePasskey(ctx context.Context, sess store.Session, passkeyID uuid.UUID) (SessionIssue, error)
	RegenerateRecoveryCodes(ctx context.Context, sess store.Session) (SecondFactorRecoveryCodes, error)
}

// SecondFactorRatePolicies bounds factor attempts per (account, IP) and per
// IP, and factor management per (account, IP).
type SecondFactorRatePolicies struct {
	attemptAccount admissionLimiter
	attemptIP      admissionLimiter
	management     admissionLimiter
}

// NewSecondFactorRatePolicies builds the limiters with their budgeted values.
func NewSecondFactorRatePolicies() *SecondFactorRatePolicies {
	return &SecondFactorRatePolicies{
		attemptAccount: newAdmissionLimiter(secondFactorAttemptAccountLimit, secondFactorAttemptAccountWindow),
		attemptIP:      newAdmissionLimiter(secondFactorAttemptIPLimit, secondFactorAttemptIPWindow),
		management:     newAdmissionLimiter(secondFactorManagementLimit, secondFactorManagementWindow),
	}
}

// AdmitAttempt admits one pending second-factor request. The per-IP bucket
// runs first so distributed account probes are bounded by address.
func (p *SecondFactorRatePolicies) AdmitAttempt(now time.Time, userID uuid.UUID, clientIP string) RateDecision {
	addr := clientAddrFromString(clientIP)
	if d := p.attemptIP.Admit(now, ipBucketKey(addr)); !d.Allowed {
		return d
	}
	return p.attemptAccount.Admit(now, accountMutationBucketKey(userID, addr))
}

// AdmitManagement admits one factor management mutation.
func (p *SecondFactorRatePolicies) AdmitManagement(now time.Time, userID uuid.UUID, clientIP string) RateDecision {
	return p.management.Admit(now, accountMutationBucketKey(userID, clientAddrFromString(clientIP)))
}
