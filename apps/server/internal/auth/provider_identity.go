package auth

// Provider identity resolution follows D5: the callback resolves
// (provider, subject) first and never fetches or accepts an email claim for a
// returning identity. Only a new subject obtains a required verified email,
// which is passed through D1 (accountemail.Canonicalize) before the account
// plus identity is created atomically. See docs/design/security.md.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ProviderSubject identifies one stable provider identity. Link and reauth
// resolve on this alone; the provider email never crosses that boundary.
type ProviderSubject struct {
	Provider Provider
	Subject  string
}

// NewProviderAccount carries everything a brand-new provider account needs:
// the stable subject and the already-canonicalized, verified registration
// email. Name may be empty (the caller falls back to the email local part).
type NewProviderAccount struct {
	Subject       ProviderSubject
	VerifiedEmail string
	Name          string
	AvatarKey     *string
}

// errEmailAlreadyRegistered is the closed outcome for a verified email already
// owned by an account without this provider identity. Handlers map it to the
// existing email_already_registered redirect.
var errEmailAlreadyRegistered = errors.New("auth: email already registered")

// providerAccountRaceError wraps a unique violation raised by
// createProviderAccountTx so createProviderLogin can distinguish "the account
// create raced; roll back and re-read the subject" from any other transaction
// failure, including a unique violation surfaced later by session issuance.
type providerAccountRaceError struct{ err error }

func (e *providerAccountRaceError) Error() string { return e.err.Error() }
func (e *providerAccountRaceError) Unwrap() error { return e.err }

// resolveReturningProviderTx looks up (provider, subject) inside the caller's
// transaction. When the identity exists it loads and locks the owning user row
// and returns found=true; the caller then issues a session in the same
// transaction. When absent it returns found=false without touching the user.
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

// createProviderAccountTx reads canonical-email ownership, then creates the
// user plus identity atomically inside the caller's transaction. PostgreSQL's
// unique email and provider-subject constraints arbitrate absent-row races:
// either unique violation surfaces as *providerAccountRaceError so the caller
// rolls back the complete attempted user transaction and re-reads the subject
// first. A failed identity insert therefore cannot orphan a user.
func (s *Service) createProviderAccountTx(ctx context.Context, qtx *store.Queries, account NewProviderAccount) (store.User, error) {
	if _, err := qtx.GetUserByCanonicalEmail(ctx, account.VerifiedEmail); err == nil {
		return store.User{}, errEmailAlreadyRegistered
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, fmt.Errorf("auth: create provider account: get user by canonical email: %w", err)
	}

	name := account.Name
	if name == "" {
		// Never expose the full email as the display name.
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

// resolveProviderLogin issues a session for a returning subject in one
// transaction. found is false when the subject has no identity yet; the caller
// then obtains a required verified email and calls createProviderLogin.
func (s *Service) resolveProviderLogin(ctx context.Context, subject ProviderSubject, ua, ip string) (raw string, found bool, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, ok, resolveErr := s.resolveReturningProviderTx(ctx, qtx, subject)
		if resolveErr != nil {
			return resolveErr
		}
		if !ok {
			return nil
		}
		issued, issueErr := s.sessions.IssueTx(ctx, qtx, user, ua, ip)
		if issueErr != nil {
			return issueErr
		}
		raw, found = issued.RawToken, true
		return nil
	})
	if err != nil {
		return "", false, err
	}
	return raw, found, nil
}

// createProviderLogin creates the account described by account and issues a
// session in one transaction, recovering from a concurrent-create race by
// re-reading (provider, subject) first after the loser rolls back.
func (s *Service) createProviderLogin(ctx context.Context, account NewProviderAccount, ua, ip string) (raw string, err error) {
	var issued SessionIssue
	txErr := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, createErr := s.createProviderAccountTx(ctx, qtx, account)
		if createErr != nil {
			return createErr
		}
		var issueErr error
		issued, issueErr = s.sessions.IssueTx(ctx, qtx, user, ua, ip)
		return issueErr
	})
	if txErr != nil {
		var raceErr *providerAccountRaceError
		if errors.As(txErr, &raceErr) || errors.Is(txErr, errEmailAlreadyRegistered) {
			// Either the create raced (unique violation) or the email was owned
			// by the time the create ran. Re-read the subject first: a concurrent
			// same-subject winner means this login follows the returning path.
			return s.recoverProviderAccountRace(ctx, account, ua, ip)
		}
		return "", txErr
	}
	return issued.RawToken, nil
}

// recoverProviderAccountRace runs after the attempted user transaction rolled
// back (a unique violation) or reported an owned email. It re-reads
// (provider, subject) first: if that subject now exists it follows the
// returning-login path and issues a session for the owning user. Otherwise it
// re-reads canonical-email ownership and returns the closed
// email_already_registered outcome.
func (s *Service) recoverProviderAccountRace(ctx context.Context, account NewProviderAccount, ua, ip string) (raw string, err error) {
	raw, found, resolveErr := s.resolveProviderLogin(ctx, account.Subject, ua, ip)
	if resolveErr != nil {
		return "", resolveErr
	}
	if found {
		return raw, nil
	}

	if _, getErr := s.q.GetUserByCanonicalEmail(ctx, account.VerifiedEmail); getErr == nil {
		return "", errEmailAlreadyRegistered
	} else if !errors.Is(getErr, pgx.ErrNoRows) {
		return "", fmt.Errorf("auth: create provider login: get user by email after race: %w", getErr)
	}
	return "", fmt.Errorf("auth: create provider login: unresolved account race")
}
