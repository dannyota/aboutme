package auth_test

// These tests prove the D5 account-creation race rules: an owned email blocks a
// new subject, a subject-collision rolls the whole attempted user back (no
// orphan), and concurrent same-subject first logins converge to one user, one
// identity, and two sessions.

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// TestCreateProviderAccountTx_EmailCollisionReturnsClosedError proves a new
// subject cannot create an account against an email another user already owns.
func TestCreateProviderAccountTx_EmailCollisionReturnsClosedError(t *testing.T) {
	pool := newTestPool(t)
	q := store.New(pool)
	svc, err := auth.NewServiceForTest(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, pool, "", "", "")
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	ctx := context.Background()

	email := uniqueEmail(t)
	if _, createErr := q.CreateUser(ctx, store.CreateUserParams{Email: email, Name: "Owner"}); createErr != nil {
		t.Fatalf("CreateUser() error = %v", createErr)
	}

	subject := auth.ProviderSubject{Provider: auth.ProviderGoogle, Subject: uniqueSubject(t)}
	account := auth.NewProviderAccount{Subject: subject, VerifiedEmail: email, Name: "Collision"}

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		qtx := q.WithTx(tx)
		_, createErr := auth.CreateProviderAccountTxForTest(ctx, svc, qtx, account)
		return createErr
	})
	if !auth.IsEmailAlreadyRegisteredForTest(err) {
		t.Fatalf("createProviderAccountTx() error = %v, want the closed email-already-registered outcome", err)
	}

	// The attempted new user and identity must not exist.
	if _, getErr := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: subject.Subject,
	}); !errors.Is(getErr, pgx.ErrNoRows) {
		t.Errorf("identity was created despite the email collision, want no identity row")
	}
}

// TestCreateProviderAccountTx_SubjectCollisionRollsBackUser proves a
// same-subject collision rolls the whole attempted user transaction back, so a
// failed identity insert cannot orphan a user.
func TestCreateProviderAccountTx_SubjectCollisionRollsBackUser(t *testing.T) {
	pool := newTestPool(t)
	q := store.New(pool)
	svc, err := auth.NewServiceForTest(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, pool, "", "", "")
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	ctx := context.Background()

	// The winner owns the subject under a different email.
	winner, err := q.CreateUser(ctx, store.CreateUserParams{Email: uniqueEmail(t), Name: "Winner"})
	if err != nil {
		t.Fatalf("CreateUser(winner) error = %v", err)
	}
	subject := uniqueSubject(t)
	if _, createErr := q.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         winner.ID,
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: subject,
	}); createErr != nil {
		t.Fatalf("CreateIdentity(winner) error = %v", createErr)
	}

	// The loser tries the same subject under a different email.
	loserEmail := uniqueEmail(t)
	account := auth.NewProviderAccount{
		Subject:       auth.ProviderSubject{Provider: auth.ProviderGoogle, Subject: subject},
		VerifiedEmail: loserEmail,
		Name:          "Loser",
	}

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		qtx := q.WithTx(tx)
		_, createErr := auth.CreateProviderAccountTxForTest(ctx, svc, qtx, account)
		return createErr
	})
	if err == nil {
		t.Fatal("createProviderAccountTx() error = nil, want a subject unique violation")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("createProviderAccountTx() error = %v, want a unique-violation-shaped error (code 23505)", err)
	}

	// The loser's user must have rolled back: no orphan user, and the subject
	// still belongs to the winner.
	if _, getErr := q.GetUserByEmail(ctx, loserEmail); !errors.Is(getErr, pgx.ErrNoRows) {
		t.Errorf("loser's user row survived the rollback (email %q), want no orphan user", loserEmail)
	}
	identity, getErr := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: subject,
	})
	if getErr != nil {
		t.Fatalf("GetIdentityByProviderSubject() error = %v", getErr)
	}
	if identity.UserID != winner.ID {
		t.Errorf("subject moved to user %v, want it to remain with winner %v (no cross-account subject movement)", identity.UserID, winner.ID)
	}
}

// TestProviderConcurrentFirstLogin_ReReadsSubject fires two same-subject,
// same-email first logins concurrently. Both must succeed and converge to one
// user, one identity, and two independently issued sessions -- a login-only
// race never surfaces the authenticated-link conflict.
func TestProviderConcurrentFirstLogin_ReReadsSubject(t *testing.T) {
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	ctx := context.Background()

	subject := uniqueSubject(t)
	email := uniqueEmail(t)

	// Register two distinct codes that both resolve the same subject + email.
	txCookies := make([]struct {
		cookie *http.Cookie
		state  string
		code   string
	}, 2)
	for i := range txCookies {
		cookie, state, nonce := beginGoogle(t, handler)
		code := "code-race-" + uuid.NewString()
		p.RegisterCode(code, oidctest.Claims{
			Subject:       subject,
			Email:         email,
			EmailVerified: ptrTrue(),
			Nonce:         nonce,
		})
		txCookies[i] = struct {
			cookie *http.Cookie
			state  string
			code   string
		}{cookie, state, code}
	}

	responses := make([]*http.Response, len(txCookies))
	var wg sync.WaitGroup
	for i := range txCookies {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			responses[i] = doCallback(t, handler, txCookies[i].code, txCookies[i].state, txCookies[i].cookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
		}(i)
	}
	wg.Wait()

	for i, resp := range responses {
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("attempt %d status = %d, want %d", i, resp.StatusCode, http.StatusFound)
		}
		if loc := resp.Header.Get("Location"); loc != testPublicOrigin+"/" {
			t.Fatalf("attempt %d Location = %q, want the success target (both same-subject logins must succeed)", i, loc)
		}
		if extractCookie(resp, auth.SessionCookieName) == nil {
			t.Fatalf("attempt %d got no session cookie", i)
		}
	}

	inspector := newRowInspectorPool(t)
	var userCount int
	if err := inspector.QueryRow(ctx, `SELECT count(*) FROM users WHERE email = $1`, email).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Errorf("users row count for %q = %d, want exactly 1", email, userCount)
	}

	var identityCount int
	if err := inspector.QueryRow(ctx,
		`SELECT count(*) FROM identities WHERE provider = 'google' AND provider_user_id = $1`, subject,
	).Scan(&identityCount); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if identityCount != 1 {
		t.Errorf("identities row count for subject %q = %d, want exactly 1", subject, identityCount)
	}

	usr, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail() error = %v", err)
	}
	var sessionCount int
	if err := inspector.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, usr.ID).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionCount != 2 {
		t.Errorf("sessions row count = %d, want exactly 2 (one independently issued session per successful login)", sessionCount)
	}
}
