package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type PasswordQueries interface {
	GetUserForUpdate(context.Context, uuid.UUID) (User, error)
	GetUserByCanonicalEmail(context.Context, string) (User, error)
	GetPasswordCredential(context.Context, uuid.UUID) (PasswordCredential, error)
	GetPasswordCredentialForUpdate(context.Context, uuid.UUID) (PasswordCredential, error)
	UpsertPasswordCredential(context.Context, UpsertPasswordCredentialParams) (PasswordCredential, error)
	GetPasswordRegistrationByEmailForUpdate(context.Context, string) (PasswordRegistration, error)
	GetPasswordRegistrationByDigest(context.Context, []byte) (PasswordRegistration, error)
	GetPasswordRegistrationForUpdate(context.Context, uuid.UUID) (PasswordRegistration, error)
	CreatePasswordRegistration(context.Context, CreatePasswordRegistrationParams) (PasswordRegistration, error)
	DeletePasswordRegistration(context.Context, uuid.UUID) (int64, error)
	GetPasswordResetTokenByUserForUpdate(context.Context, uuid.UUID) (PasswordResetToken, error)
	GetPasswordResetTokenByDigest(context.Context, []byte) (PasswordResetToken, error)
	GetPasswordResetTokenForUpdate(context.Context, uuid.UUID) (PasswordResetToken, error)
	CreatePasswordResetToken(context.Context, CreatePasswordResetTokenParams) (PasswordResetToken, error)
	DeletePasswordResetToken(context.Context, uuid.UUID) (int64, error)
	GetSessionByIDForUpdate(context.Context, uuid.UUID) (Session, error)
	CreateSession(context.Context, CreateSessionParams) (Session, error)
	RevokeAllSessions(context.Context, RevokeAllSessionsParams) (int64, error)
	CreateAuthEmailJob(context.Context, CreateAuthEmailJobParams) (AuthEmailJob, error)
	ListLiveAuthEmailJobKeyIDs(context.Context, time.Time) ([]string, error)
	ClaimAuthEmailJobs(context.Context, ClaimAuthEmailJobsParams) ([]AuthEmailJob, error)
	GetLeasedAuthEmailJobForUpdate(context.Context, GetLeasedAuthEmailJobForUpdateParams) (AuthEmailJob, error)
	MarkAuthEmailJobSent(context.Context, MarkAuthEmailJobSentParams) (int64, error)
	MarkAuthEmailJobTerminal(context.Context, MarkAuthEmailJobTerminalParams) (int64, error)
	RequeueAuthEmailJob(context.Context, RequeueAuthEmailJobParams) (int64, error)
	RequeueExpiredAuthEmailLeases(context.Context, RequeueExpiredAuthEmailLeasesParams) (int64, error)
	CleanupExpiredPasswordRegistrations(context.Context, CleanupExpiredPasswordRegistrationsParams) (int64, error)
	CleanupExpiredPasswordResetTokens(context.Context, CleanupExpiredPasswordResetTokensParams) (int64, error)
	CleanupFinishedAuthEmailJobs(context.Context, CleanupFinishedAuthEmailJobsParams) (int64, error)
}

var _ PasswordQueries = (*Queries)(nil)
