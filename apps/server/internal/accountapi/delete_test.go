package accountapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestDeleteHandlerRequiresCookieSession(t *testing.T) {
	s := &Service{
		sessions:     auth.NewSessionManager(store.New(nil)),
		publicOrigin: "https://aboutme.example",
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "https://aboutme.example/api/v1/me", nil)
	req.Header.Set("Origin", "https://aboutme.example")
	s.DeleteHandler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE /me without session = %d, want 401", recorder.Code)
	}
}

func TestDeleteHandlerSuccessIsBodylessAndClearsBrowserState(t *testing.T) {
	env := newDeletionEnvironment(t)
	recorder := httptest.NewRecorder()
	req := authenticatedDeleteRequest(env)
	env.service.DeleteHandler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Fatalf("DELETE /me = (%d, %q), want bodyless 204", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store, no-transform" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := recorder.Header().Get("Clear-Site-Data"); got != `"cookies", "storage"` {
		t.Errorf("Clear-Site-Data = %q", got)
	}
	cookies := recorder.Result().Cookies()
	wantCleared := map[string]bool{"__Host-session": false, auth.OAuthTxCookieName: false}
	for _, cookie := range cookies {
		if _, ok := wantCleared[cookie.Name]; ok && cookie.MaxAge < 0 {
			wantCleared[cookie.Name] = true
		}
	}
	for name, cleared := range wantCleared {
		if !cleared {
			t.Errorf("cookie %s was not cleared", name)
		}
	}
}

func TestDeleteHandlerAuthCSRFAndRequestMatrix(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   string
		mutate func(deletionEnvironment, *http.Request)
	}{
		{name: "missing session", status: 401, code: "session_required", mutate: func(_ deletionEnvironment, r *http.Request) { r.Header.Del("Cookie") }},
		{name: "missing origin", status: 403, code: "csrf_rejected", mutate: func(_ deletionEnvironment, r *http.Request) { r.Header.Del("Origin") }},
		{name: "wrong csrf", status: 403, code: "csrf_rejected", mutate: func(_ deletionEnvironment, r *http.Request) { r.Header.Set(auth.CSRFHeaderName, "wrong") }},
		{name: "query", status: 400, code: "request_invalid", mutate: func(_ deletionEnvironment, r *http.Request) { r.URL.RawQuery = "x=1" }},
		{name: "body", status: 400, code: "request_invalid", mutate: func(_ deletionEnvironment, r *http.Request) {
			r.Body = io.NopCloser(bytes.NewBufferString("{}"))
			r.ContentLength = 2
			r.Header.Set("Content-Type", "application/json")
		}},
		{name: "conditional", status: 400, code: "request_invalid", mutate: func(_ deletionEnvironment, r *http.Request) { r.Header.Set("If-Match", `"r1"`) }},
		{name: "idempotency", status: 400, code: "request_invalid", mutate: func(_ deletionEnvironment, r *http.Request) { r.Header.Set("Idempotency-Key", uuid.NewString()) }},
		{name: "stale reauth", status: 403, code: "reauth_required", mutate: func(env deletionEnvironment, r *http.Request) {
			stale := time.Now().UTC().Add(-16 * time.Minute)
			if _, err := env.service.pool.Exec(r.Context(), `UPDATE sessions SET reauthenticated_at=$2 WHERE id=$1`, env.session.ID, stale); err != nil {
				t.Fatalf("expire reauth: %v", err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newDeletionEnvironment(t)
			req := authenticatedDeleteRequest(env)
			test.mutate(env, req)
			recorder := httptest.NewRecorder()
			env.service.DeleteHandler().ServeHTTP(recorder, req)
			if recorder.Code != test.status || !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"`+test.code+`"`)) {
				t.Fatalf("DELETE /me = %d %s, want %d %s", recorder.Code, recorder.Body.String(), test.status, test.code)
			}
			if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
				t.Fatalf("rejected request deleted user: %v", err)
			}
		})
	}
}

func TestDeleteHandlerHasIndependentFivePerMinuteLimit(t *testing.T) {
	env := newDeletionEnvironment(t)
	stale := time.Now().UTC().Add(-16 * time.Minute)
	if _, err := env.service.pool.Exec(env.ctx, `UPDATE sessions SET reauthenticated_at=$2 WHERE id=$1`, env.session.ID, stale); err != nil {
		t.Fatalf("expire reauth: %v", err)
	}
	for attempt := 1; attempt <= 6; attempt++ {
		recorder := httptest.NewRecorder()
		env.service.DeleteHandler().ServeHTTP(recorder, authenticatedDeleteRequest(env))
		want := http.StatusForbidden
		if attempt == 6 {
			want = http.StatusTooManyRequests
		}
		if recorder.Code != want {
			t.Fatalf("attempt %d status = %d body=%s, want %d", attempt, recorder.Code, recorder.Body.String(), want)
		}
	}
}

func authenticatedDeleteRequest(env deletionEnvironment) *http.Request {
	req := httptest.NewRequestWithContext(env.ctx, http.MethodDelete, "https://aboutme.example/api/v1/me", nil)
	req.RemoteAddr = "203.0.113.2:12345"
	req.AddCookie(&http.Cookie{Name: "__Host-session", Value: env.rawSession})
	req.Header.Set("Origin", "https://aboutme.example")
	req.Header.Set(auth.CSRFHeaderName, base64.RawURLEncoding.EncodeToString(env.session.CSRFSecret))
	return req
}

func TestValidateDeleteRequestRejectsEveryInputChannel(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{name: "query", mutate: func(r *http.Request) { r.URL.RawQuery = "x=1" }},
		{name: "body", mutate: func(r *http.Request) { r.Body = io.NopCloser(bytes.NewBufferString("x")); r.ContentLength = 1 }},
		{name: "hidden body", mutate: func(r *http.Request) { r.Body = io.NopCloser(bytes.NewBufferString("x")); r.ContentLength = 0 }},
		{name: "transfer encoding", mutate: func(r *http.Request) { r.TransferEncoding = []string{"chunked"} }},
		{name: "schema header", mutate: func(r *http.Request) { r.Header.Set("X-Resume-Schema-Version", "2") }},
		{name: "conditional header", mutate: func(r *http.Request) { r.Header.Set("If-Match", `"r1"`) }},
		{name: "idempotency header", mutate: func(r *http.Request) { r.Header.Set("Idempotency-Key", uuid.NewString()) }},
		{name: "bearer header", mutate: func(r *http.Request) { r.Header.Set("Authorization", "Bearer x") }},
		{name: "content type", mutate: func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "https://aboutme.example/api/v1/me", nil)
			test.mutate(req)
			if err := validateDeleteRequest(req); !errors.Is(err, errDeleteRequestInvalid) {
				t.Fatalf("validateDeleteRequest() error = %v, want request invalid", err)
			}
		})
	}
}

func TestDeleteAccountCommitsThreeResumePrivacyLifecycle(t *testing.T) {
	ctx := context.Background()
	pool, err := store.NewPool(ctx, testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	projector := docmigrate.NewIdentityProjector()
	resumes := resume.NewStore(pool, projector)

	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.com", Name: "Delete Me"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	foreign, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.com", Name: "Keep Me"})
	if err != nil {
		t.Fatalf("CreateUser(foreign): %v", err)
	}
	if _, createErr := resumes.Create(ctx, foreign.ID, "Foreign", validDeletionDocument(nil)); createErr != nil {
		t.Fatalf("Create(foreign resume): %v", createErr)
	}

	type fixture struct {
		id    uuid.UUID
		slug  string
		photo string
	}
	fixtures := make([]fixture, 3)
	for i := range fixtures {
		created, createErr := resumes.Create(ctx, user.ID, "Owned", validDeletionDocument(nil))
		if createErr != nil {
			t.Fatalf("Create resume: %v", createErr)
		}
		key, keyErr := media.NewPhotoKey(bytes.NewReader(bytes.Repeat([]byte{byte(i + 1)}, 16)), created.ID, "png")
		if keyErr != nil {
			t.Fatalf("NewPhotoKey: %v", keyErr)
		}
		doc := validDeletionDocument(&schema.Photo{Key: key})
		if _, saveErr := resumes.SaveDocument(ctx, user.ID, created.ID, doc, created.Revision); saveErr != nil {
			t.Fatalf("SaveDocument: %v", saveErr)
		}
		slug := "delete-" + uuid.NewString()[:8]
		live := i > 0
		seo := i == 2
		if _, execErr := pool.Exec(ctx, `UPDATE resumes SET slug=$2, live=$3, seo_geo_enabled=$4 WHERE id=$1`, created.ID, slug, live, seo); execErr != nil {
			t.Fatalf("configure resume: %v", execErr)
		}
		fixtures[i] = fixture{id: created.ID, slug: slug, photo: key}
	}
	registrationTime := time.Now().UTC()
	reg, err := q.CreatePasswordRegistration(ctx, store.CreatePasswordRegistrationParams{
		Email: user.Email, Name: "Pending", EncodedHash: []byte("hash"), TokenDigest: deletionDigest(),
		CreatedAt: registrationTime, ExpiresAt: registrationTime.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePasswordRegistration: %v", err)
	}
	deletedEmailJobs := seedAccountCascadeRows(ctx, t, pool, q, user, &reg.ID, 41)
	foreignEmailJobs := seedAccountCascadeRows(ctx, t, pool, q, foreign, nil, 73)

	sessions := auth.NewSessionManagerWithPool(pool)
	_, sess, err := sessions.Issue(ctx, user.ID, "ua", "203.0.113.2")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	state, err := q.GetPublicState(ctx)
	if err != nil {
		t.Fatalf("GetPublicState: %v", err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: state.DiscoveryGeneration})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	service, err := New(Dependencies{
		Pool: pool, Sessions: sessions, Coordinator: coordinator, Media: inertMedia{},
		Projector: projector, PublicOrigin: "https://aboutme.example",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := service.deleteAccount(ctx, sess); err != nil {
		t.Fatalf("deleteAccount: %v", err)
	}

	if _, err := q.GetUserByID(ctx, user.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted user lookup error = %v, want no rows", err)
	}
	if _, err := q.GetPasswordRegistrationForUpdate(ctx, reg.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("pending registration lookup error = %v, want no rows", err)
	}
	if _, err := q.GetUserByID(ctx, foreign.ID); err != nil {
		t.Fatalf("foreign user was changed: %v", err)
	}
	assertAccountCascadeCounts(ctx, t, pool, user.ID, 0)
	assertAccountCascadeCounts(ctx, t, pool, foreign.ID, 1)
	assertRowsAbsent(ctx, t, pool, "auth_email_jobs", deletedEmailJobs)
	assertRowsPresent(ctx, t, pool, "auth_email_jobs", foreignEmailJobs)
	for _, item := range fixtures {
		tombstone, err := q.GetSlugTombstoneForUpdate(ctx, item.slug)
		if err != nil || tombstone.ReleasedByUserID != nil {
			t.Errorf("tombstone %q = (%+v, %v), want retained with nil owner", item.slug, tombstone, err)
		}
		if _, err := q.GetMediaDeletionJobByObjectKey(ctx, store.GetMediaDeletionJobByObjectKeyParams{ResumeID: item.id, ObjectKey: item.photo}); err != nil {
			t.Errorf("media job %q: %v", item.photo, err)
		}
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM lifecycle_audit_events WHERE kind='account_deleted' AND media_job_id IS NULL AND occurred_at >= $1`, sess.CreatedAt).Scan(&auditCount); err != nil || auditCount < 1 {
		t.Fatalf("account deletion audit count = %d, error=%v", auditCount, err)
	}
	if got, err := q.GetPublicState(ctx); err != nil || got.DiscoveryGeneration != state.DiscoveryGeneration+1 {
		t.Fatalf("discovery state = (%+v, %v), want generation %d", got, err, state.DiscoveryGeneration+1)
	}
	if err := coordinator.Ready(); err != nil {
		t.Fatalf("coordinator readiness after delete: %v", err)
	}
}

func seedAccountCascadeRows(
	ctx context.Context,
	t *testing.T,
	pool *store.Pool,
	q *store.Queries,
	user store.User,
	registrationID *uuid.UUID,
	seed byte,
) []uuid.UUID {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	digest := deletionDigest()
	if _, err := q.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID: user.ID, Provider: "github", ProviderUserID: uuid.NewString(),
	}); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	if _, err := q.UpsertPasswordCredential(ctx, store.UpsertPasswordCredentialParams{
		UserID: user.ID, EncodedHash: []byte("encoded-hash"), CreatedAt: now, ChangedAt: now,
	}); err != nil {
		t.Fatalf("UpsertPasswordCredential: %v", err)
	}
	if _, _, err := auth.NewSessionManagerWithPool(pool).Issue(ctx, user.ID, "cascade fixture", "203.0.113.3"); err != nil {
		t.Fatalf("Issue cascade session: %v", err)
	}
	reset, err := q.CreatePasswordResetToken(ctx, store.CreatePasswordResetTokenParams{
		UserID: user.ID, TokenDigest: digest, CreatedAt: now, ExpiresAt: now.Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreatePasswordResetToken: %v", err)
	}

	emailJobIDs := []uuid.UUID{uuid.New(), uuid.New()}
	if registrationID != nil {
		emailJobIDs = append(emailJobIDs, uuid.New())
		if _, execErr := pool.Exec(ctx, `
			INSERT INTO auth_email_jobs
				(id, kind, state, registration_id, token_digest, attempts, created_at, expires_at,
				 key_id, nonce, ciphertext, lease_owner, lease_expires_at)
			VALUES ($1, 'verify', 'leased', $2, $3, 1, $4, $5,
				'key', $6, $7, 'worker', $8)`,
			emailJobIDs[2], *registrationID, deletionDigest(), now,
			now.Add(time.Hour), bytes.Repeat([]byte{seed}, 12), []byte("ciphertext"), now.Add(time.Minute)); execErr != nil {
			t.Fatalf("seed claimed verify email job: %v", execErr)
		}
	}
	if _, execErr := pool.Exec(ctx, `
		INSERT INTO auth_email_jobs
			(id, kind, state, reset_token_id, token_digest, attempts, created_at, expires_at,
			 key_id, nonce, ciphertext, lease_owner, lease_expires_at)
		VALUES ($1, 'reset', 'leased', $2, $3, 1, $4, $5,
			'key', $6, $7, 'worker', $8)`,
		emailJobIDs[0], reset.ID, digest, now, now.Add(time.Hour),
		bytes.Repeat([]byte{seed}, 12), []byte("ciphertext"), now.Add(time.Minute)); execErr != nil {
		t.Fatalf("seed claimed reset email job: %v", execErr)
	}
	if _, execErr := pool.Exec(ctx, `
		INSERT INTO auth_email_jobs
			(id, kind, state, user_id, attempts, created_at, expires_at, sent_at)
		VALUES ($1, 'password_changed', 'sent', $2, 1, $3, $4, $3)`,
		emailJobIDs[1], user.ID, now, now.Add(time.Hour)); execErr != nil {
		t.Fatalf("seed sent password-changed email job: %v", execErr)
	}

	linkingUserID := user.ID
	if _, createErr := q.CreateOAuthTransaction(ctx, store.CreateOAuthTransactionParams{
		HandleHash: digest, Provider: "github", Purpose: "link", LinkingUserID: &linkingUserID,
		State: uuid.NewString(), PKCEVerifier: "verifier", RedirectURI: "https://aboutme.example/callback",
		ReturnPath: "/app/resumes", ExpiresAt: now.Add(10 * time.Minute),
	}); createErr != nil {
		t.Fatalf("CreateOAuthTransaction: %v", createErr)
	}
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{
		ClientName: "Cascade fixture", RedirectURIs: []byte(`["https://client.example/callback"]`), CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
		UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("UpsertOAuthGrant: %v", err)
	}
	if _, err := q.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{
		CodeDigest: digest, ClientID: client.ID, UserID: user.ID, Scopes: "resumes:read",
		CodeChallenge: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RedirectURI:   "https://client.example/callback", CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateOAuthAuthorizationCode: %v", err)
	}
	if _, err := q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{
		TokenDigest: digest, Kind: "access", FamilyID: uuid.New(), ClientID: client.ID,
		UserID: user.ID, GrantID: grant.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		FamilyExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateOAuthToken: %v", err)
	}
	if _, err := q.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
		UserID: user.ID, Route: "cascade-test", IdempotencyKey: uuid.New(), RequestHash: digest,
		ResponseStatus: http.StatusCreated, ResponseBody: []byte(`{"ok":true}`),
		ResponseHeaders: []byte(`{}`), ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateIdempotencyRecord: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes) VALUES ($1, 1, 13)`, user.ID); err != nil {
		t.Fatalf("seed idempotency usage: %v", err)
	}
	return emailJobIDs
}

func deletionDigest() []byte {
	id := uuid.New()
	digest := append([]byte(nil), id[:]...)
	return append(digest, id[:]...)
}

func assertAccountCascadeCounts(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID, want int) {
	t.Helper()
	for _, item := range []struct {
		table  string
		column string
	}{
		{table: "identities", column: "user_id"},
		{table: "password_credentials", column: "user_id"},
		{table: "password_reset_tokens", column: "user_id"},
		{table: "sessions", column: "user_id"},
		{table: "oauth_transactions", column: "linking_user_id"},
		{table: "oauth_authorization_codes", column: "user_id"},
		{table: "oauth_grants", column: "user_id"},
		{table: "oauth_tokens", column: "user_id"},
		{table: "resumes", column: "user_id"},
		{table: "idempotency_records", column: "user_id"},
		{table: "idempotency_usage", column: "user_id"},
	} {
		var count int
		query := "SELECT count(*) FROM " + item.table + " WHERE " + item.column + "=$1"
		if err := pool.QueryRow(ctx, query, userID).Scan(&count); err != nil || count != want {
			t.Errorf("%s rows for user = %d, error=%v, want %d", item.table, count, err, want)
		}
	}
}

func assertRowsAbsent(ctx context.Context, t *testing.T, pool *store.Pool, table string, ids []uuid.UUID) {
	t.Helper()
	for _, id := range ids {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id).Scan(&count); err != nil || count != 0 {
			t.Errorf("%s row %s count = %d, error=%v, want 0", table, id, count, err)
		}
	}
}

func assertRowsPresent(ctx context.Context, t *testing.T, pool *store.Pool, table string, ids []uuid.UUID) {
	t.Helper()
	for _, id := range ids {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", id).Scan(&count); err != nil || count != 1 {
			t.Errorf("%s row %s count = %d, error=%v, want 1", table, id, count, err)
		}
	}
}

func TestDeleteAccountAmbiguousCommitRecovery(t *testing.T) {
	t.Run("committed", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		env.service.commitTx = func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			return errors.New("lost commit response")
		}
		if err := env.service.deleteAccount(env.ctx, env.session); err != nil {
			t.Fatalf("deleteAccount: %v", err)
		}
		if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("committed recovery user lookup = %v, want no rows", err)
		}
		if err := env.coordinator.Ready(); err != nil {
			t.Fatalf("coordinator readiness: %v", err)
		}
	})

	t.Run("rolled back", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		env.service.commitTx = func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil {
				return err
			}
			return errors.New("lost rollback response")
		}
		if err := env.service.deleteAccount(env.ctx, env.session); err == nil {
			t.Fatal("deleteAccount error = nil, want recovered rollback failure")
		}
		if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
			t.Fatalf("rolled-back recovery removed user: %v", err)
		}
		if err := env.coordinator.Ready(); err != nil {
			t.Fatalf("coordinator readiness: %v", err)
		}
	})

	t.Run("unresolved", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		env.service.commitTx = func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil {
				return err
			}
			if _, err := env.service.pool.Exec(context.WithoutCancel(ctx), `UPDATE users SET name='changed during recovery' WHERE id=$1`, env.user.ID); err != nil {
				return err
			}
			return errors.New("lost commit response")
		}
		if err := env.service.deleteAccount(env.ctx, env.session); err == nil {
			t.Fatal("deleteAccount error = nil, want unresolved proof")
		}
		if err := env.coordinator.Ready(); err == nil {
			t.Fatal("coordinator readiness error = nil, want fail-closed unresolved state")
		}
	})
}

func TestDeleteAccountRebuildsPlanAfterConcurrentCreate(t *testing.T) {
	env := newDeletionEnvironment(t)
	created := make(chan uuid.UUID, 1)
	env.service.afterClose = func() {
		env.service.afterClose = nil
		row, err := resume.NewStore(env.service.pool, env.service.projector).Create(
			env.ctx, env.user.ID, "Racing resume", validDeletionDocument(nil),
		)
		if err != nil {
			t.Fatalf("concurrent Create: %v", err)
		}
		created <- row.ID
	}
	if err := env.service.deleteAccount(env.ctx, env.session); err != nil {
		t.Fatalf("deleteAccount after stale plan: %v", err)
	}
	resumeID := <-created
	if _, err := env.queries.GetResumeForUser(env.ctx, store.GetResumeForUserParams{ID: resumeID, UserID: env.user.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("racing resume survived deletion: %v", err)
	}
}

func TestDeleteAccountRebuildsPlanAfterConcurrentRevision(t *testing.T) {
	env := newDeletionEnvironment(t)
	resumeStore := resume.NewStore(env.service.pool, env.service.projector)
	row, err := resumeStore.Create(env.ctx, env.user.ID, "Racing revision", validDeletionDocument(nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	env.service.afterPrepare = func() {
		env.service.afterPrepare = nil
		transition, beginErr := env.coordinator.Begin(env.ctx, publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
			ID: row.ID, ExpectedRevision: row.Revision, Class: publicstate.NonDraining,
		}}})
		if beginErr != nil {
			t.Fatalf("begin concurrent revision: %v", beginErr)
		}
		if closeErr := transition.Close(env.ctx, time.Now().Add(time.Second)); closeErr != nil {
			t.Fatalf("close concurrent revision: %v", closeErr)
		}
		saved, saveErr := resumeStore.SaveDocument(env.ctx, env.user.ID, row.ID, validDeletionDocument(nil), row.Revision)
		if saveErr != nil {
			t.Fatalf("concurrent SaveDocument: %v", saveErr)
		}
		if commitErr := transition.Commit(publicstate.CommittedState{ResumeRevisions: map[uuid.UUID]int64{row.ID: saved}}); commitErr != nil {
			t.Fatalf("commit concurrent revision: %v", commitErr)
		}
	}
	if err := env.service.deleteAccount(env.ctx, env.session); err != nil {
		t.Fatalf("deleteAccount after stale revision: %v", err)
	}
	if _, err := env.queries.GetResumeForUser(env.ctx, store.GetResumeForUserParams{ID: row.ID, UserID: env.user.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("revised resume survived deletion: %v", err)
	}
}

func TestDeleteAccountSecondAttemptIsSessionRequired(t *testing.T) {
	env := newDeletionEnvironment(t)
	if err := env.service.deleteAccount(env.ctx, env.session); err != nil {
		t.Fatalf("first deleteAccount: %v", err)
	}
	if err := env.service.deleteAccount(env.ctx, env.session); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("second deleteAccount error = %v, want invalid session", err)
	}
}

func TestDeleteAccountStopsAfterThreeStalePlans(t *testing.T) {
	env := newDeletionEnvironment(t)
	attempts := 0
	env.service.afterClose = func() {
		attempts++
		if _, err := resume.NewStore(env.service.pool, env.service.projector).Create(
			env.ctx, env.user.ID, "Racing resume", validDeletionDocument(nil),
		); err != nil {
			t.Fatalf("concurrent Create: %v", err)
		}
	}
	if err := env.service.deleteAccount(env.ctx, env.session); !errors.Is(err, errAccountChanged) {
		t.Fatalf("deleteAccount error = %v, want account changed", err)
	}
	if attempts != deletePlanAttempts {
		t.Fatalf("fresh plan attempts = %d, want %d", attempts, deletePlanAttempts)
	}
	if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
		t.Fatalf("user deleted after bounded stale plans: %v", err)
	}
	if err := env.coordinator.Ready(); err != nil {
		t.Fatalf("coordinator readiness: %v", err)
	}
}

func TestDeleteAccountRechecksConcreteSessionAfterDrain(t *testing.T) {
	t.Run("revoked", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		env.service.afterClose = func() {
			env.service.afterClose = nil
			now := time.Now().UTC()
			if err := env.queries.RevokeSession(env.ctx, store.RevokeSessionParams{ID: env.session.ID, RevokedAt: &now}); err != nil {
				t.Fatalf("RevokeSession: %v", err)
			}
		}
		if err := env.service.deleteAccount(env.ctx, env.session); !errors.Is(err, auth.ErrSessionInvalid) {
			t.Fatalf("deleteAccount error = %v, want invalid session", err)
		}
		if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
			t.Fatalf("user deleted with revoked concrete session: %v", err)
		}
		if err := env.coordinator.Ready(); err != nil {
			t.Fatalf("coordinator readiness: %v", err)
		}
	})

	t.Run("reauth expired", func(t *testing.T) {
		env := newDeletionEnvironment(t)
		env.service.afterClose = func() {
			env.service.afterClose = nil
			stale := time.Now().UTC().Add(-16 * time.Minute)
			if _, err := env.service.pool.Exec(env.ctx, `UPDATE sessions SET reauthenticated_at=$2 WHERE id=$1`, env.session.ID, stale); err != nil {
				t.Fatalf("expire reauth: %v", err)
			}
		}
		if err := env.service.deleteAccount(env.ctx, env.session); !errors.Is(err, auth.ErrReauthRequired) {
			t.Fatalf("deleteAccount error = %v, want reauth required", err)
		}
		if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
			t.Fatalf("user deleted with stale reauth: %v", err)
		}
	})
}

func TestDeleteAccountDrainTimeoutReopensUnchangedAdmission(t *testing.T) {
	env := newDeletionEnvironment(t)
	state, err := env.queries.GetPublicState(env.ctx)
	if err != nil {
		t.Fatalf("GetPublicState: %v", err)
	}
	lease, err := env.coordinator.AcquireDiscovery(env.ctx, state.DiscoveryGeneration, publicstate.RepresentationSitemap)
	if err != nil {
		t.Fatalf("AcquireDiscovery: %v", err)
	}
	defer lease.Release()
	env.service.now = func() time.Time { return time.Unix(0, 0) }

	err = env.service.deleteAccount(env.ctx, env.session)
	var timeout *publicstate.DrainTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("deleteAccount error = %v, want drain timeout", err)
	}
	if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
		t.Fatalf("drain timeout deleted user: %v", err)
	}
	if err := env.coordinator.Ready(); err != nil {
		t.Fatalf("coordinator readiness after drain timeout: %v", err)
	}
}

func TestDeleteAccountCanceledAfterDrainReopensAdmission(t *testing.T) {
	env := newDeletionEnvironment(t)
	ctx, cancel := context.WithCancel(env.ctx)
	env.service.afterClose = cancel
	err := env.service.deleteAccount(ctx, env.session)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("deleteAccount error = %v, want context canceled", err)
	}
	if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
		t.Fatalf("canceled deletion removed user: %v", err)
	}
	if err := env.coordinator.Ready(); err != nil {
		t.Fatalf("coordinator readiness after cancellation: %v", err)
	}
}

func TestDeleteAccountDrainsEveryPublicRepresentationBeforeCommit(t *testing.T) {
	env := newDeletionEnvironment(t)
	row, err := resume.NewStore(env.service.pool, env.service.projector).Create(
		env.ctx, env.user.ID, "Public resume", validDeletionDocument(nil),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	resumeRepresentations := []publicstate.Representation{
		publicstate.RepresentationHTML, publicstate.RepresentationJSON,
		publicstate.RepresentationPhoto, publicstate.RepresentationPDF,
		publicstate.RepresentationPNG, publicstate.RepresentationMarkdown,
		publicstate.RepresentationSSE,
	}
	for _, representation := range resumeRepresentations {
		lease, acquireErr := env.coordinator.AcquireResume(env.ctx, row.ID, row.Revision, representation)
		if acquireErr != nil {
			t.Fatalf("AcquireResume(%s): %v", representation, acquireErr)
		}
		if cancelErr := lease.OnCancel(lease.Release); cancelErr != nil {
			t.Fatalf("OnCancel(%s): %v", representation, cancelErr)
		}
	}
	state, err := env.queries.GetPublicState(env.ctx)
	if err != nil {
		t.Fatalf("GetPublicState: %v", err)
	}
	for _, representation := range []publicstate.Representation{
		publicstate.RepresentationSitemap, publicstate.RepresentationLLMS,
	} {
		lease, err := env.coordinator.AcquireDiscovery(env.ctx, state.DiscoveryGeneration, representation)
		if err != nil {
			t.Fatalf("AcquireDiscovery(%s): %v", representation, err)
		}
		if err := lease.OnCancel(lease.Release); err != nil {
			t.Fatalf("OnCancel(%s): %v", representation, err)
		}
	}
	if err := env.service.deleteAccount(env.ctx, env.session); err != nil {
		t.Fatalf("deleteAccount: %v", err)
	}
	if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted user lookup = %v, want no rows", err)
	}
}

func TestDeleteAccountInvalidStoredPhotoRollsBack(t *testing.T) {
	tests := []struct {
		name string
		key  func(uuid.UUID) string
	}{
		{name: "malformed", key: func(id uuid.UUID) string { return "resumes/" + id.String() + "/photo-not-hex.png" }},
		{name: "cross resume", key: func(uuid.UUID) string {
			other := uuid.New()
			return "resumes/" + other.String() + "/photo-0123456789abcdef0123456789abcdef.png"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newDeletionEnvironment(t)
			resumeStore := resume.NewStore(env.service.pool, env.service.projector)
			row, err := resumeStore.Create(env.ctx, env.user.ID, "Photo", validDeletionDocument(nil))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			doc := validDeletionDocument(&schema.Photo{Key: test.key(row.ID)})
			if _, err := resumeStore.SaveDocument(env.ctx, env.user.ID, row.ID, doc, row.Revision); err != nil {
				t.Fatalf("SaveDocument test fixture: %v", err)
			}
			if err := env.service.deleteAccount(env.ctx, env.session); err == nil {
				t.Fatal("deleteAccount error = nil, want invalid stored photo failure")
			}
			if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
				t.Fatalf("invalid photo deleted account: %v", err)
			}
			if err := env.coordinator.Ready(); err != nil {
				t.Fatalf("coordinator readiness: %v", err)
			}
		})
	}
}

func TestDeleteAccountAuditFailureRollsBackTombstoneQueueAndUser(t *testing.T) {
	env := newDeletionEnvironment(t)
	resumeStore := resume.NewStore(env.service.pool, env.service.projector)
	row, err := resumeStore.Create(env.ctx, env.user.ID, "Rollback", validDeletionDocument(nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	photo, err := media.NewPhotoKey(bytes.NewReader(bytes.Repeat([]byte{9}, 16)), row.ID, "png")
	if err != nil {
		t.Fatalf("NewPhotoKey: %v", err)
	}
	if _, err := resumeStore.SaveDocument(env.ctx, env.user.ID, row.ID, validDeletionDocument(&schema.Photo{Key: photo}), row.Revision); err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}
	slug := "rollback-" + uuid.NewString()[:8]
	if _, err := env.service.pool.Exec(env.ctx, `UPDATE resumes SET slug=$2 WHERE id=$1`, row.ID, slug); err != nil {
		t.Fatalf("set slug: %v", err)
	}
	collisionID := uuid.Must(uuid.NewV7())
	when := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := env.queries.InsertAccountDeletedAuditEvent(env.ctx, store.InsertAccountDeletedAuditEventParams{ID: collisionID, OccurredAt: when}); err != nil {
		t.Fatalf("seed audit collision: %v", err)
	}
	env.service.newAuditID = func() (uuid.UUID, error) { return collisionID, nil }

	if err := env.service.deleteAccount(env.ctx, env.session); err == nil {
		t.Fatal("deleteAccount error = nil, want audit insertion failure")
	}
	if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
		t.Fatalf("audit failure deleted user: %v", err)
	}
	if _, err := env.queries.GetSlugTombstoneForUpdate(env.ctx, slug); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("audit failure retained tombstone: %v", err)
	}
	if _, err := env.queries.GetMediaDeletionJobByObjectKey(env.ctx, store.GetMediaDeletionJobByObjectKeyParams{ResumeID: row.ID, ObjectKey: photo}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("audit failure retained media job: %v", err)
	}
	if err := env.coordinator.Ready(); err != nil {
		t.Fatalf("coordinator readiness: %v", err)
	}
}

func TestDeleteAccountQueueFailureRollsBackTombstoneAndUser(t *testing.T) {
	env := newDeletionEnvironment(t)
	resumeStore := resume.NewStore(env.service.pool, env.service.projector)
	row, err := resumeStore.Create(env.ctx, env.user.ID, "Rollback", validDeletionDocument(nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	photo, err := media.NewPhotoKey(bytes.NewReader(bytes.Repeat([]byte{8}, 16)), row.ID, "png")
	if err != nil {
		t.Fatalf("NewPhotoKey: %v", err)
	}
	if _, err := resumeStore.SaveDocument(env.ctx, env.user.ID, row.ID, validDeletionDocument(&schema.Photo{Key: photo}), row.Revision); err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}
	slug := "queue-rollback-" + uuid.NewString()[:8]
	if _, err := env.service.pool.Exec(env.ctx, `UPDATE resumes SET slug=$2 WHERE id=$1`, row.ID, slug); err != nil {
		t.Fatalf("set slug: %v", err)
	}
	env.service.enqueueMediaJob = func(context.Context, *store.Queries, store.EnqueueMediaDeletionJobParams) (int64, error) {
		return 0, errors.New("injected queue failure")
	}

	if err := env.service.deleteAccount(env.ctx, env.session); err == nil {
		t.Fatal("deleteAccount error = nil, want queue insertion failure")
	}
	if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
		t.Fatalf("queue failure deleted user: %v", err)
	}
	if _, err := env.queries.GetSlugTombstoneForUpdate(env.ctx, slug); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("queue failure retained tombstone: %v", err)
	}
	if err := env.coordinator.Ready(); err != nil {
		t.Fatalf("coordinator readiness: %v", err)
	}
}

func TestDeleteAccountTombstoneFailureRollsBackUser(t *testing.T) {
	env := newDeletionEnvironment(t)
	resumeStore := resume.NewStore(env.service.pool, env.service.projector)
	row, err := resumeStore.Create(env.ctx, env.user.ID, "Rollback", validDeletionDocument(nil))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	slug := "tombstone-rollback-" + uuid.NewString()[:8]
	if _, execErr := env.service.pool.Exec(env.ctx, `UPDATE resumes SET slug=$2 WHERE id=$1`, row.ID, slug); execErr != nil {
		t.Fatalf("set slug: %v", execErr)
	}
	seededAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, insertErr := env.queries.InsertSlugTombstone(env.ctx, store.InsertSlugTombstoneParams{
		Slug: slug, ReleasedAt: seededAt,
	}); insertErr != nil {
		t.Fatalf("seed tombstone collision: %v", insertErr)
	}
	state, err := env.queries.GetPublicState(env.ctx)
	if err != nil {
		t.Fatalf("GetPublicState: %v", err)
	}

	if err := env.service.deleteAccount(env.ctx, env.session); err == nil {
		t.Fatal("deleteAccount error = nil, want tombstone insertion failure")
	}
	if _, err := env.queries.GetUserByID(env.ctx, env.user.ID); err != nil {
		t.Fatalf("tombstone failure deleted user: %v", err)
	}
	if tombstone, err := env.queries.GetSlugTombstoneForUpdate(env.ctx, slug); err != nil || !tombstone.ReleasedAt.Equal(seededAt) {
		t.Fatalf("seed tombstone changed = (%+v, %v)", tombstone, err)
	}
	if got, err := env.queries.GetPublicState(env.ctx); err != nil || got.DiscoveryGeneration != state.DiscoveryGeneration {
		t.Fatalf("discovery changed after tombstone failure = (%+v, %v)", got, err)
	}
	if err := env.coordinator.Ready(); err != nil {
		t.Fatalf("coordinator readiness: %v", err)
	}
}

type deletionEnvironment struct {
	ctx         context.Context
	service     *Service
	queries     *store.Queries
	coordinator *publicstate.Coordinator
	user        store.User
	session     store.Session
	rawSession  string
}

func newDeletionEnvironment(t *testing.T) deletionEnvironment {
	t.Helper()
	ctx := context.Background()
	pool, err := store.NewPool(ctx, testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.com", Name: "Delete Me"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sessions := auth.NewSessionManagerWithPool(pool)
	rawSession, session, err := sessions.Issue(ctx, user.ID, "ua", "203.0.113.2")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	state, err := q.GetPublicState(ctx)
	if err != nil {
		t.Fatalf("GetPublicState: %v", err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: state.DiscoveryGeneration})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	service, err := New(Dependencies{
		Pool: pool, Sessions: sessions, Coordinator: coordinator, Media: inertMedia{},
		Projector: docmigrate.NewIdentityProjector(), PublicOrigin: "https://aboutme.example",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return deletionEnvironment{ctx: ctx, service: service, queries: q, coordinator: coordinator, user: user, session: session, rawSession: rawSession}
}

func validDeletionDocument(photo *schema.Photo) schema.Resume {
	return schema.Resume{
		SchemaVersion:   int64(schema.CurrentVersion),
		PersonalDetails: schema.PersonalDetails{FullName: stringPointer("Ada Lovelace"), Photo: photo},
		Content:         map[string]schema.Section{},
		Customization: schema.Customization{
			Font:           schema.Font{Family: schema.Inter, BaseSizePx: 14},
			Colors:         schema.Colors{Primary: "#1a1a1a", Text: "#1a1a1a", Background: "#ffffff"},
			Spacing:        schema.Spacing{SectionGap: 16, EntryGap: 8, LineHeight: 1.4},
			Heading:        schema.Heading{Style: schema.Normal},
			Layout:         schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{}, Sidebar: []string{}}},
			SectionDisplay: schema.SectionDisplay{Skill: schema.SkillClass{Style: schema.Text}, Language: schema.LanguageClass{Style: schema.Text}},
			PageFormat:     schema.A4, DateFormat: schema.MmYyyy,
		},
	}
}

func stringPointer(value string) *string { return &value }

type inertMedia struct{}

func (inertMedia) Put(context.Context, string, string, io.Reader, int64) (media.PutOutcome, error) {
	return media.PutNotCreated, errors.New("not used")
}
func (inertMedia) Get(context.Context, string) (io.ReadCloser, string, error) {
	return nil, "", media.ErrNotFound
}
func (inertMedia) Delete(context.Context, string) error { return media.ErrNotFound }
func (inertMedia) ListPage(context.Context, string, string, int) ([]media.Object, string, error) {
	return nil, "", nil
}
