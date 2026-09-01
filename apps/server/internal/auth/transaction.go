// Package auth implements the OAuth and session boundary described in
// docs/design/security.md.
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

// Provider values must match the database checks for transactions and identities.
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

// oauthTxTTL also governs the transaction cookie lifetime.
const oauthTxTTL = 10 * time.Minute

// randomTokenBytes gives handles, state, and nonces 256 bits of entropy.
const randomTokenBytes = 32

const defaultLoginReturnPath = "/app/resumes"

// Transaction binds an authorization start to its callback.
type Transaction struct {
	Provider      Provider
	Purpose       Purpose
	LinkingUserID uuid.UUID // zero value iff Purpose == PurposeLogin
	State         string
	PKCEVerifier  string
	Nonce         string // empty for ProviderGitHub
	RedirectURI   string
	ReturnPath    string
}

// ErrTransactionInvalid collapses unknown, expired, replayed, and wrong-provider
// handles into one no-oracle result.
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

// NewTransactionStoreForTest injects a clock for deterministic expiry tests.
func NewTransactionStoreForTest(q *store.Queries, now func() time.Time) *TransactionStore {
	return &TransactionStore{q: q, now: now}
}

// Begin stores only the handle hash and returns the raw cookie handle. Handle,
// state, and OIDC nonce are independent random values. GitHub has no nonce.
func (s *TransactionStore) Begin(ctx context.Context, provider Provider, purpose Purpose, linkingUserID uuid.UUID, redirectURI string) (string, Transaction, error) {
	return s.begin(ctx, provider, purpose, linkingUserID, redirectURI, defaultLoginReturnPath)
}

// begin stores a provider transaction with its server-validated login return
// path. Direct callers use Begin's closed default; provider start handlers pass
// the path they validated from the login page.
func (s *TransactionStore) begin(ctx context.Context, provider Provider, purpose Purpose, linkingUserID uuid.UUID, redirectURI, returnPath string) (string, Transaction, error) {
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
		ReturnPath:    returnPath,
		ExpiresAt:     s.now().Add(oauthTxTTL),
	})
	if err != nil {
		return "", Transaction{}, fmt.Errorf("auth: begin oauth transaction: %w", err)
	}

	return handle, transactionFromRow(row), nil
}

// Consume atomically claims one live transaction. Unknown, expired, replayed,
// and wrong-provider handles return ErrTransactionInvalid. Provider validation
// runs after the claim, so a mismatch also consumes the transaction and cannot
// be retried against another callback.
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

// transactionFromRow converts the database shape to the auth shape.
func transactionFromRow(row store.OAuthTransaction) Transaction {
	tx := Transaction{
		Provider:     Provider(row.Provider),
		Purpose:      Purpose(row.Purpose),
		State:        row.State,
		PKCEVerifier: row.PKCEVerifier,
		RedirectURI:  row.RedirectURI,
		ReturnPath:   row.ReturnPath,
	}
	if row.LinkingUserID != nil {
		tx.LinkingUserID = *row.LinkingUserID
	}
	if row.Nonce != nil {
		tx.Nonce = *row.Nonce
	}
	return tx
}

// randomToken returns an unpadded base64url random value.
func randomToken() (string, error) {
	b := make([]byte, randomTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashHandle returns the at-rest SHA-256 form of the bearer handle.
func hashHandle(handle string) []byte {
	sum := sha256.Sum256([]byte(handle))
	return sum[:]
}
