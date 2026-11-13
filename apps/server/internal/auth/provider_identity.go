package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

type ProviderSubject struct {
	Provider Provider
	Subject  string
}

type NewProviderAccount struct {
	Subject       ProviderSubject
	VerifiedEmail string
	Name          string
	AvatarKey     *string
}

var errEmailAlreadyRegistered = errors.New("auth: email already registered")

type providerAccountRaceError struct{ err error }

func (e *providerAccountRaceError) Error() string { return e.err.Error() }
func (e *providerAccountRaceError) Unwrap() error { return e.err }

func (s *Service) resolveReturningProviderTx(ctx context.Context, qtx *store.Queries, subject ProviderSubject) (store.User, bool, error) {
	identity, err := qtx.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(subject.Provider),
		ProviderUserID: subject.Subject,
	})
	if err == nil {
		usr, getErr := qtx.GetUserForUpdate(ctx, identity.UserID)
		if getErr != nil {
			return store.User{}, false, fmt.Errorf("auth: resolve returning provider: lock user: %w", getErr)
		}
		return usr, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, false, fmt.Errorf("auth: resolve returning provider: get identity: %w", err)
	}
	return store.User{}, false, nil
}

func (s *Service) createProviderAccountTx(ctx context.Context, qtx *store.Queries, account NewProviderAccount) (store.User, error) {
	if err := qtx.LockCanonicalAccountEmail(ctx, account.VerifiedEmail); err != nil {
		return store.User{}, fmt.Errorf("auth: create provider account: lock canonical email: %w", err)
	}
	if _, err := qtx.GetPasswordRegistrationByEmailForUpdate(ctx, account.VerifiedEmail); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, fmt.Errorf("auth: create provider account: lock registration: %w", err)
	}
	if _, err := qtx.GetUserByCanonicalEmail(ctx, account.VerifiedEmail); err == nil {
		return store.User{}, errEmailAlreadyRegistered
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, fmt.Errorf("auth: create provider account: get user by canonical email: %w", err)
	}

	name := account.Name
	if name == "" {
		name = emailLocalPart(account.VerifiedEmail)
	}

	usr, err := qtx.CreateUser(ctx, store.CreateUserParams{
		Email:     account.VerifiedEmail,
		Name:      name,
		AvatarKey: account.AvatarKey,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.User{}, &providerAccountRaceError{err: err}
		}
		return store.User{}, fmt.Errorf("auth: create provider account: create user: %w", err)
	}

	if _, err := qtx.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         usr.ID,
		Provider:       string(account.Subject.Provider),
		ProviderUserID: account.Subject.Subject,
	}); err != nil {
		if isUniqueViolation(err) {
			return store.User{}, &providerAccountRaceError{err: err}
		}
		return store.User{}, fmt.Errorf("auth: create provider account: create identity: %w", err)
	}

	return usr, nil
}

type providerLoginOutcome struct {
	sessionRaw string
	enrolled   bool
	userID     uuid.UUID
	verifiedAt time.Time
}

func (s *Service) issueOrDeferProviderLoginTx(ctx context.Context, qtx *store.Queries, user store.User, ua, ip string) (providerLoginOutcome, error) {
	policy, err := lockSecondFactorPolicy(ctx, qtx, user.ID)
	if err != nil {
		return providerLoginOutcome{}, err
	}
	if policy != nil {
		return providerLoginOutcome{enrolled: true, userID: user.ID, verifiedAt: s.sessionMgr.now()}, nil
	}
	issued, err := s.sessions.IssueTx(ctx, qtx, user, ua, ip)
	if err != nil {
		return providerLoginOutcome{}, err
	}
	return providerLoginOutcome{sessionRaw: issued.RawToken, userID: user.ID}, nil
}

func (s *Service) resolveProviderLogin(ctx context.Context, subject ProviderSubject, ua, ip string) (login providerLoginOutcome, found bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, ok, resolveErr := s.resolveReturningProviderTx(ctx, qtx, subject)
		if resolveErr != nil {
			return resolveErr
		}
		if !ok {
			return nil
		}
		var loginErr error
		login, loginErr = s.issueOrDeferProviderLoginTx(ctx, qtx, user, ua, ip)
		if loginErr != nil {
			return loginErr
		}
		found = true
		return nil
	})
	if err != nil {
		return providerLoginOutcome{}, false, err
	}
	return login, found, nil
}

func (s *Service) createProviderLogin(ctx context.Context, account NewProviderAccount, ua, ip string) (providerLoginOutcome, error) {
	var login providerLoginOutcome
	txErr := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, createErr := s.createProviderAccountTx(ctx, qtx, account)
		if createErr != nil {
			return createErr
		}
		var loginErr error
		login, loginErr = s.issueOrDeferProviderLoginTx(ctx, qtx, user, ua, ip)
		return loginErr
	})
	if txErr != nil {
		var raceErr *providerAccountRaceError
		if errors.As(txErr, &raceErr) || errors.Is(txErr, errEmailAlreadyRegistered) {
			return s.recoverProviderAccountRace(ctx, account, ua, ip)
		}
		return providerLoginOutcome{}, txErr
	}
	return login, nil
}

func (s *Service) recoverProviderAccountRace(ctx context.Context, account NewProviderAccount, ua, ip string) (providerLoginOutcome, error) {
	login, found, resolveErr := s.resolveProviderLogin(ctx, account.Subject, ua, ip)
	if resolveErr != nil {
		return providerLoginOutcome{}, resolveErr
	}
	if found {
		return login, nil
	}

	if _, getErr := s.q.GetUserByCanonicalEmail(ctx, account.VerifiedEmail); getErr == nil {
		return providerLoginOutcome{}, errEmailAlreadyRegistered
	} else if !errors.Is(getErr, pgx.ErrNoRows) {
		return providerLoginOutcome{}, fmt.Errorf("auth: create provider login: get user by email after race: %w", getErr)
	}
	return providerLoginOutcome{}, fmt.Errorf("auth: create provider login: unresolved account race")
}
