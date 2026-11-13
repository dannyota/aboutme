package auth

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	SecondFactorMethodPasskey  = "passkey"
	SecondFactorMethodRecovery = "recovery"
)

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

type SecondFactorCredential interface {
	SecondFactorMethod() string
}

type SecondFactorOptions struct {
	CeremonyID string          `json:"ceremonyId"`
	PublicKey  json.RawMessage `json:"publicKey"`
}

type SecondFactorClient struct {
	UserAgent string
	IP        string
}

type SecondFactorPasskey struct {
	ID         uuid.UUID
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type SecondFactorState struct {
	Enabled                bool
	Passkeys               []SecondFactorPasskey
	RecoveryCodesRemaining int
}

type SecondFactorRegistration struct {
	Passkey       SecondFactorPasskey
	RecoveryCodes []string
	Session       SessionIssue
}

type SecondFactorRecoveryCodes struct {
	Codes   []string
	Session SessionIssue
}

type SecondFactorService interface {
	PasskeyEnrollmentEnabled() bool
	DecodePasskeyRegistration(body []byte) (SecondFactorCredential, error)
	DecodePasskeyAssertion(body []byte) (SecondFactorCredential, error)
	DecodeRecoveryCode(code string) (SecondFactorCredential, error)
	PendingMethods(ctx context.Context, userID uuid.UUID) ([]string, error)
	StartPasskeyAssertion(ctx context.Context, pendingToken string, sessionID *uuid.UUID) (SecondFactorOptions, error)
	CompletePending(ctx context.Context, pendingToken string, sessionID *uuid.UUID, credential SecondFactorCredential, client SecondFactorClient) (*SessionIssue, error)
	State(ctx context.Context, userID uuid.UUID) (SecondFactorState, error)
	StartPasskeyRegistration(ctx context.Context, sess store.Session) (SecondFactorOptions, error)
	CompletePasskeyRegistration(ctx context.Context, sess store.Session, credential SecondFactorCredential) (SecondFactorRegistration, error)
	RemovePasskey(ctx context.Context, sess store.Session, passkeyID uuid.UUID) (SessionIssue, error)
	RegenerateRecoveryCodes(ctx context.Context, sess store.Session) (SecondFactorRecoveryCodes, error)
}

type SecondFactorRatePolicies struct {
	attemptAccount admissionLimiter
	attemptIP      admissionLimiter
	management     admissionLimiter
}

func NewSecondFactorRatePolicies() *SecondFactorRatePolicies {
	return &SecondFactorRatePolicies{
		attemptAccount: newAdmissionLimiter(secondFactorAttemptAccountLimit, secondFactorAttemptAccountWindow),
		attemptIP:      newAdmissionLimiter(secondFactorAttemptIPLimit, secondFactorAttemptIPWindow),
		management:     newAdmissionLimiter(secondFactorManagementLimit, secondFactorManagementWindow),
	}
}

func (p *SecondFactorRatePolicies) AdmitAttempt(now time.Time, userID uuid.UUID, clientIP string) RateDecision {
	addr := clientAddrFromString(clientIP)
	if d := p.attemptIP.Admit(now, ipBucketKey(addr)); !d.Allowed {
		return d
	}
	return p.attemptAccount.Admit(now, accountMutationBucketKey(userID, addr))
}

func (p *SecondFactorRatePolicies) AdmitManagement(now time.Time, userID uuid.UUID, clientIP string) RateDecision {
	return p.management.Admit(now, accountMutationBucketKey(userID, clientAddrFromString(clientIP)))
}
