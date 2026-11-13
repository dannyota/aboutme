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

	"danny.vn/aboutme/apps/server/internal/store"
)

type Provider string

const (
	ProviderGoogle   Provider = "google"
	ProviderGitHub   Provider = "github"
	ProviderLinkedIn Provider = "linkedin"
)

type Purpose string

const (
	PurposeLogin  Purpose = "login"
	PurposeLink   Purpose = "link"
	PurposeReauth Purpose = "reauth"
	PurposeView   Purpose = "view"
)

const oauthTxTTL = 10 * time.Minute

const randomTokenBytes = 32

const defaultLoginReturnPath = "/app/resumes"

type Transaction struct {
	Provider      Provider
	Purpose       Purpose
	LinkingUserID uuid.UUID
	State         string
	PKCEVerifier  string
	Nonce         string
	RedirectURI   string
	ReturnPath    string
	ResumeID      uuid.UUID
}

var ErrTransactionInvalid = errors.New("auth: oauth transaction invalid")

type TransactionStore struct {
	q   *store.Queries
	now func() time.Time
}

func NewTransactionStore(q *store.Queries) *TransactionStore {
	return &TransactionStore{q: q, now: time.Now}
}

func NewTransactionStoreForTest(q *store.Queries, now func() time.Time) *TransactionStore {
	return &TransactionStore{q: q, now: now}
}

func (s *TransactionStore) Begin(ctx context.Context, provider Provider, purpose Purpose, linkingUserID uuid.UUID, redirectURI string) (string, Transaction, error) {
	return s.begin(ctx, provider, purpose, linkingUserID, redirectURI, defaultLoginReturnPath)
}

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
	if row.ResumeID != nil {
		tx.ResumeID = *row.ResumeID
	}
	return tx
}

func (s *TransactionStore) beginView(ctx context.Context, provider Provider, resumeID uuid.UUID, redirectURI, pageSuffix string) (string, Transaction, error) {
	handle, err := randomToken()
	if err != nil {
		return "", Transaction{}, fmt.Errorf("auth: begin view transaction: generate handle: %w", err)
	}
	state, err := randomToken()
	if err != nil {
		return "", Transaction{}, fmt.Errorf("auth: begin view transaction: generate state: %w", err)
	}
	verifier := oauth2.GenerateVerifier()
	nonce, err := randomToken()
	if err != nil {
		return "", Transaction{}, fmt.Errorf("auth: begin view transaction: generate nonce: %w", err)
	}
	returnPath := defaultLoginReturnPath
	if pageSuffix != "" {
		returnPath = pageSuffix
	}

	row, err := s.q.CreateOAuthTransaction(ctx, store.CreateOAuthTransactionParams{
		HandleHash:   hashHandle(handle),
		Provider:     string(provider),
		Purpose:      string(PurposeView),
		State:        state,
		PKCEVerifier: verifier,
		Nonce:        &nonce,
		RedirectURI:  redirectURI,
		ReturnPath:   returnPath,
		ExpiresAt:    s.now().Add(oauthTxTTL),
		ResumeID:     &resumeID,
	})
	if err != nil {
		return "", Transaction{}, fmt.Errorf("auth: begin view transaction: %w", err)
	}
	return handle, transactionFromRow(row), nil
}

func randomToken() (string, error) {
	b := make([]byte, randomTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashHandle(handle string) []byte {
	sum := sha256.Sum256([]byte(handle))
	return sum[:]
}
