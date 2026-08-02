// Package auth implements the OAuth transaction core: the short-lived,
// single-use transaction store that ties an /authorize redirect to its
// /callback (state, PKCE, nonce), and the __Host-oauth-tx cookie (see
// cookie.go) that carries its handle between the two. See
// docs/specs/aboutme-design.md §3.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Provider identifies the external OAuth/OIDC identity provider an OAuth
// transaction (and, later, an identities row) belongs to. Values match
// the schema's CHECK (provider IN (...)) on both oauth_transactions and
// identities exactly.
type Provider string

// The supported OAuth/OIDC providers.
const (
	ProviderGoogle   Provider = "google"
	ProviderGitHub   Provider = "github"
	ProviderLinkedIn Provider = "linkedin"
)

// Purpose identifies why an OAuth transaction was started. Values match
// the schema's CHECK (purpose IN (...)) on oauth_transactions exactly.
type Purpose string

// The reasons an OAuth transaction can be started.
const (
	PurposeLogin  Purpose = "login"
	PurposeLink   Purpose = "link"
	PurposeReauth Purpose = "reauth"
)

// oauthTxTTL is how long a transaction remains valid after Begin, and the
// cookie's Max-Age=600 (see cookie.go's oauthTxCookieMaxAge): the cookie
// and the database row it points at always expire together.
const oauthTxTTL = 10 * time.Minute

// randomTokenBytes is the size, in raw bytes, of a generated handle,
// state, or nonce before base64url-encoding: 32 raw bytes yields the
// 43-character base64.RawURLEncoding string the spec requires.
const randomTokenBytes = 32

// Transaction is the in-flight OAuth transaction Begin creates and
// Consume later returns: everything the caller needs to build the
// provider authorize URL (Begin) or complete the callback (Consume).
type Transaction struct {
	Provider      Provider
	Purpose       Purpose
	LinkingUserID uuid.UUID // zero value iff Purpose == PurposeLogin
	State         string
	PKCEVerifier  string
	Nonce         string // empty for ProviderGitHub
	RedirectURI   string
}

// ErrTransactionInvalid is returned by Consume for every way a
// transaction can fail to validate: unknown handle, expired, already
// consumed, or provider mismatch. These four are deliberately collapsed
// into one sentinel rather than distinguished -- see Consume's doc
// comment -- so a caller (and, in turn, an attacker probing a handle)
// never gets an oracle to learn which of the four actually happened.
var ErrTransactionInvalid = errors.New("auth: oauth transaction invalid")

// TransactionStore creates and atomically consumes OAuth transactions
// backed by the oauth_transactions table.
type TransactionStore struct {
	q   *store.Queries
	now func() time.Time
}

// NewTransactionStore builds a TransactionStore backed by q, using the
// real wall clock.
func NewTransactionStore(q *store.Queries) *TransactionStore {
	return &TransactionStore{q: q, now: time.Now}
}

// NewTransactionStoreForTest builds a TransactionStore backed by q that
// uses now instead of the real wall clock. It exists so tests (which, by
// this package's own convention, live in the external package
// auth_test and therefore cannot reach TransactionStore's unexported
// clock field directly) can exercise Begin/Consume's expiry logic
// deterministically -- e.g. advancing a fake clock past oauthTxTTL --
// without a real sleep. Every non-test caller uses NewTransactionStore.
func NewTransactionStoreForTest(q *store.Queries, now func() time.Time) *TransactionStore {
	return &TransactionStore{q: q, now: now}
}

// Begin creates a transaction row, returning the raw cookie handle (never
// persisted in cleartext -- only its SHA-256 hash is, in handle_hash) and
// the Transaction for the caller to build the provider authorize URL
// from. The handle, state, and nonce are each 32 crypto/rand bytes,
// base64.RawURLEncoding-encoded; the PKCE verifier comes from the pinned
// golang.org/x/oauth2's own oauth2.GenerateVerifier(). nonce is left
// empty for ProviderGitHub, which has no OIDC ID token to bind one to.
func (s *TransactionStore) Begin(ctx context.Context, provider Provider, purpose Purpose, linkingUserID uuid.UUID, redirectURI string) (string, Transaction, error) {
	handle, err := randomToken()
	if err != nil {
		return "", Transaction{}, fmt.Errorf("auth: begin oauth transaction: generate handle: %w", err)
	}
	state, err := randomToken()
	if err != nil {
		return "", Transaction{}, fmt.Errorf("auth: begin oauth transaction: generate state: %w", err)
	}
	verifier := oauth2.GenerateVerifier()

	var nonce string
	if provider != ProviderGitHub {
		nonce, err = randomToken()
		if err != nil {
			return "", Transaction{}, fmt.Errorf("auth: begin oauth transaction: generate nonce: %w", err)
		}
	}

	var linkingUserIDParam *uuid.UUID
	if linkingUserID != uuid.Nil {
		linkingUserIDParam = &linkingUserID
	}
	var nonceParam *string
	if nonce != "" {
		nonceParam = &nonce
	}

	row, err := s.q.CreateOAuthTransaction(ctx, store.CreateOAuthTransactionParams{
		HandleHash:    hashHandle(handle),
		Provider:      string(provider),
		Purpose:       string(purpose),
		LinkingUserID: linkingUserIDParam,
		State:         state,
		PKCEVerifier:  verifier,
		Nonce:         nonceParam,
		RedirectURI:   redirectURI,
		ExpiresAt:     s.now().Add(oauthTxTTL),
	})
	if err != nil {
		return "", Transaction{}, fmt.Errorf("auth: begin oauth transaction: %w", err)
	}

	return handle, transactionFromRow(row), nil
}

// Consume atomically marks the transaction identified by handle consumed
// and returns it, or ErrTransactionInvalid. It collapses four distinct
// failure modes into that one sentinel, deliberately:
//
//   - handle unknown (never issued, or from a different database/env)
//   - the transaction has expired (past oauthTxTTL since Begin)
//   - the transaction was already consumed (replay)
//   - expectedProvider != the transaction's own Provider
//
// The first three are already indistinguishable at the SQL layer: the
// underlying UPDATE's WHERE clause (handle_hash = $1 AND consumed_at IS
// NULL AND expires_at > $2) matches no row for any of them, so pgx
// reports the same pgx.ErrNoRows regardless of which is true. The fourth
// -- expectedProvider must equal tx.Provider -- is the RFC 9700 §4.4
// mix-up defense: a transaction created for one provider must never
// validate against a different provider's callback endpoint, even though
// the __Host-oauth-tx cookie is Path=/ and would be sent to any
// /api/v1/auth/*/callback path. It is checked here in Go, after the row
// has already been atomically claimed by the UPDATE above -- not before
// -- so a mismatched-provider attempt still burns the transaction (it
// can't be retried against the correct provider either), and every
// failure path returns the exact same error: an attacker probing a
// handle gets no oracle to distinguish "wrong provider" from "already
// used" from "never existed".
func (s *TransactionStore) Consume(ctx context.Context, handle string, expectedProvider Provider) (Transaction, error) {
	now := s.now()
	row, err := s.q.ConsumeOAuthTransaction(ctx, store.ConsumeOAuthTransactionParams{
		HandleHash: hashHandle(handle),
		ConsumedAt: &now,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Transaction{}, ErrTransactionInvalid
		}
		return Transaction{}, fmt.Errorf("auth: consume oauth transaction: %w", err)
	}

	if row.Provider != string(expectedProvider) {
		return Transaction{}, ErrTransactionInvalid
	}

	return transactionFromRow(row), nil
}

// transactionFromRow converts a store.OAuthTransaction row (the
// generated, database-shaped type) into this package's own Transaction
// (the shape callers outside internal/store work with).
func transactionFromRow(row store.OAuthTransaction) Transaction {
	tx := Transaction{
		Provider:     Provider(row.Provider),
		Purpose:      Purpose(row.Purpose),
		State:        row.State,
		PKCEVerifier: row.PKCEVerifier,
		RedirectURI:  row.RedirectURI,
	}
	if row.LinkingUserID != nil {
		tx.LinkingUserID = *row.LinkingUserID
	}
	if row.Nonce != nil {
		tx.Nonce = *row.Nonce
	}
	return tx
}

// randomToken returns a 43-character base64url (no padding) encoding of
// randomTokenBytes cryptographically random bytes -- used for the
// transaction handle, the OAuth state parameter, and the OIDC nonce
// alike.
func randomToken() (string, error) {
	b := make([]byte, randomTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashHandle returns the SHA-256 hash of handle, the form stored in
// oauth_transactions.handle_hash: the handle is a bearer credential,
// hashed at rest exactly like the session token (schema.sql's
// oauth_transactions comment, design decision 3), so a database read (or
// leak) never discloses a usable handle.
func hashHandle(handle string) []byte {
	sum := sha256.Sum256([]byte(handle))
	return sum[:]
}
