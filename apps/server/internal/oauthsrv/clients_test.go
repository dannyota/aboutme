package oauthsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

var registerTestNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

type registrationQueries struct {
	store.OAuthQueries
	beforeCreate func()
	created      []store.CreateOAuthClientParams
	err          error
	id           uuid.UUID
}

func (q *registrationQueries) CreateOAuthClient(_ context.Context, p store.CreateOAuthClientParams) (store.OAuthClient, error) {
	if q.beforeCreate != nil {
		q.beforeCreate()
	}
	q.created = append(q.created, p)
	if q.err != nil {
		return store.OAuthClient{}, q.err
	}
	return store.OAuthClient{ID: q.id}, nil
}

type registrationAdmissionFake struct {
	allowed bool
	retry   int
	calls   int
}

func (a *registrationAdmissionFake) AdmitRegister(_ time.Time, _ *http.Request) (bool, int) {
	a.calls++
	return a.allowed, a.retry
}

func (a *registrationAdmissionFake) AdmitToken(_ time.Time, _ *http.Request) (bool, int) {
	return true, 0
}

func (a *registrationAdmissionFake) AdmitGrant(clientID uuid.UUID, _ time.Time) (grantAttempt, bool, int) {
	return grantAttempt{clientID: clientID, leaseID: 1}, true, 0
}

func (a *registrationAdmissionFake) FinishGrant(grantAttempt, grantAttemptResult) {}

func newRegistrationService(t *testing.T, q *registrationQueries, admission *registrationAdmissionFake) *Service {
	t.Helper()
	s, _ := newRegistrationServiceAndPool(t, q, admission)
	return s
}

func newRegistrationServiceAndPool(t *testing.T, q *registrationQueries, admission *registrationAdmissionFake) (*Service, *store.Pool) {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() error: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	s, err := NewService(ctx, ServiceDependencies{
		Pool:              pool,
		Queries:           q,
		Clock:             func() time.Time { return registerTestNow },
		Entropy:           bytes.NewReader(make([]byte, 32)),
		PublicOrigin:      "https://aboutme.example",
		RegisterAdmission: admission,
		TokenAdmission:    admission,
		LiveGrantLimit:    10,
	})
	if err != nil {
		t.Fatalf("NewService() error: %v", err)
	}
	return s, pool
}

func registerRequest(method, contentType, body string) *http.Request {
	r := httptest.NewRequest(method, "https://aboutme.example/oauth/register", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r
}

func TestRegister_CreatesCanonicalClient(t *testing.T) {
	q := &registrationQueries{id: uuid.MustParse("c34b7e8e-5d58-4c7c-ae79-40cac4acff53")}
	s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})

	rec := httptest.NewRecorder()
	s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", `{"client_name":"e\u0301","redirect_uris":["https://agent.example/callback"],"token_endpoint_auth_method":"none"}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	want := `{"client_id":"c34b7e8e-5d58-4c7c-ae79-40cac4acff53","client_name":"é","redirect_uris":["https://agent.example/callback"],"token_endpoint_auth_method":"none"}`
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if len(q.created) != 1 {
		t.Fatalf("created = %d, want 1", len(q.created))
	}
	if got := q.created[0].ClientName; got != "é" {
		t.Errorf("stored client name = %q, want canonical NFC spelling", got)
	}
	if got := string(q.created[0].RedirectURIs); got != `["https://agent.example/callback"]` {
		t.Errorf("stored redirect URIs = %s", got)
	}
	if got := q.created[0].CreatedAt; !got.Equal(registerTestNow) {
		t.Errorf("created at = %s, want injected clock %s", got, registerTestNow)
	}
}

func TestRegister_RejectsRouteAndJSONMatrixWithClosedBody(t *testing.T) {
	valid := `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"]}`
	withinLimit := valid + strings.Repeat(" ", 4096-len(valid))
	overLimit := withinLimit + " "
	cases := []struct {
		name        string
		method      string
		contentType string
		body        string
		status      int
	}{
		{"method", http.MethodGet, "application/json", valid, http.StatusMethodNotAllowed},
		{"media type", http.MethodPost, "application/json; charset=utf-8", valid, http.StatusUnsupportedMediaType},
		{"too large", http.MethodPost, "application/json", overLimit, http.StatusRequestEntityTooLarge},
		{"duplicate field", http.MethodPost, "application/json", `{"client_name":"Agent","client_name":"Other","redirect_uris":["https://agent.example/callback"]}`, http.StatusBadRequest},
		{"unknown field", http.MethodPost, "application/json", `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"],"logo_uri":"https://agent.example/logo"}`, http.StatusBadRequest},
		{"wrong name scalar", http.MethodPost, "application/json", `{"client_name":1,"redirect_uris":["https://agent.example/callback"]}`, http.StatusBadRequest},
		{"wrong redirect scalar", http.MethodPost, "application/json", `{"client_name":"Agent","redirect_uris":"https://agent.example/callback"}`, http.StatusBadRequest},
		{"empty name", http.MethodPost, "application/json", `{"client_name":"","redirect_uris":["https://agent.example/callback"]}`, http.StatusBadRequest},
		{"name over 64 code points", http.MethodPost, "application/json", `{"client_name":"` + strings.Repeat("a", 65) + `","redirect_uris":["https://agent.example/callback"]}`, http.StatusBadRequest},
		{"control in name", http.MethodPost, "application/json", `{"client_name":"Agent\u0000","redirect_uris":["https://agent.example/callback"]}`, http.StatusBadRequest},
		{"no redirect", http.MethodPost, "application/json", `{"client_name":"Agent","redirect_uris":[]}`, http.StatusBadRequest},
		{"six redirects", http.MethodPost, "application/json", `{"client_name":"Agent","redirect_uris":["https://a.example","https://b.example","https://c.example","https://d.example","https://e.example","https://f.example"]}`, http.StatusBadRequest},
		{"invalid redirect", http.MethodPost, "application/json", `{"client_name":"Agent","redirect_uris":["http://agent.example/callback"]}`, http.StatusBadRequest},
		{"auth method", http.MethodPost, "application/json", `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"],"token_endpoint_auth_method":"client_secret_basic"}`, http.StatusBadRequest},
		{"auth method null", http.MethodPost, "application/json", `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"],"token_endpoint_auth_method":null}`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &registrationQueries{id: uuid.New()}
			s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
			rec := httptest.NewRecorder()
			s.HandleRegister(rec, registerRequest(tc.method, tc.contentType, tc.body))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.status, rec.Body.String())
			}
			if got, want := rec.Body.String(), `{"error":"invalid_request","error_description":"The request is invalid."}`; got != want {
				t.Errorf("body = %q, want closed body %q", got, want)
			}
			if len(q.created) != 0 {
				t.Errorf("created = %d, want no database write", len(q.created))
			}
		})
	}

	t.Run("4096 bytes is accepted", func(t *testing.T) {
		q := &registrationQueries{id: uuid.New()}
		s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
		rec := httptest.NewRecorder()
		s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", withinLimit))
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
		}
	})
}

func TestRegister_AdmissionDenialAndCookieIsolation(t *testing.T) {
	t.Run("admission", func(t *testing.T) {
		q := &registrationQueries{id: uuid.New()}
		admission := &registrationAdmissionFake{allowed: false, retry: 47}
		s := newRegistrationService(t, q, admission)
		rec := httptest.NewRecorder()
		s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"]}`))
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
		}
		if got := rec.Header().Get("Retry-After"); got != "47" {
			t.Errorf("Retry-After = %q, want 47", got)
		}
		if admission.calls != 1 || len(q.created) != 0 {
			t.Errorf("admission calls = %d, creates = %d; want 1, 0", admission.calls, len(q.created))
		}
	})

	t.Run("cookie isolation", func(t *testing.T) {
		body := `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"]}`
		firstQ := &registrationQueries{id: uuid.MustParse("7375e109-0570-4b69-a9b4-5a083d19db2e")}
		first := httptest.NewRecorder()
		newRegistrationService(t, firstQ, &registrationAdmissionFake{allowed: true}).HandleRegister(first, registerRequest(http.MethodPost, "application/json", body))

		secondQ := &registrationQueries{id: uuid.MustParse("7375e109-0570-4b69-a9b4-5a083d19db2e")}
		secondReq := registerRequest(http.MethodPost, "application/json", body)
		secondReq.AddCookie(&http.Cookie{Name: "__Host-session", Value: "valid-session-material"})
		second := httptest.NewRecorder()
		newRegistrationService(t, secondQ, &registrationAdmissionFake{allowed: true}).HandleRegister(second, secondReq)

		if first.Code != second.Code || first.Body.String() != second.Body.String() || first.Header().Get("Content-Type") != second.Header().Get("Content-Type") {
			t.Errorf("cookie request changed response: without=%d %q %q; with=%d %q %q", first.Code, first.Body.String(), first.Header().Get("Content-Type"), second.Code, second.Body.String(), second.Header().Get("Content-Type"))
		}
	})
}

func TestRegister_InternalFailuresUseClosedServerError(t *testing.T) {
	const body = `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"]}`
	const want = `{"error":"server_error","error_description":"The server encountered an error."}`

	t.Run("client create", func(t *testing.T) {
		q := &registrationQueries{id: uuid.New(), err: errors.New("database unavailable")}
		s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
		rec := httptest.NewRecorder()
		s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", body))
		if rec.Code != http.StatusInternalServerError || rec.Body.String() != want {
			t.Fatalf("response = %d %q, want 500 %q", rec.Code, rec.Body.String(), want)
		}
		if strings.Contains(rec.Body.String(), "Agent") {
			t.Error("server error echoed request material")
		}
	})

	t.Run("GC before create", func(t *testing.T) {
		q := &registrationQueries{id: uuid.New()}
		s, pool := newRegistrationServiceAndPool(t, q, &registrationAdmissionFake{allowed: true})
		pool.Close(context.Background())
		rec := httptest.NewRecorder()
		s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", body))
		if rec.Code != http.StatusInternalServerError || rec.Body.String() != want {
			t.Fatalf("response = %d %q, want 500 %q", rec.Code, rec.Body.String(), want)
		}
		if len(q.created) != 0 {
			t.Errorf("CreateOAuthClient calls = %d, want 0 after GC failure", len(q.created))
		}
	})
}

func TestRegister_SuccessSweepsBeforeCreatingClient(t *testing.T) {
	q := &registrationQueries{id: uuid.New()}
	s, pool := newRegistrationServiceAndPool(t, q, &registrationAdmissionFake{allowed: true})
	ctx := context.Background()
	stale, err := store.New(pool).CreateOAuthClient(ctx, store.CreateOAuthClientParams{
		ClientName:   "stale client",
		RedirectURIs: []byte(`["https://agent.example/callback"]`),
		CreatedAt:    registerTestNow.Add(-25 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateOAuthClient(stale): %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM oauth_clients WHERE id = $1", stale.ID) })
	q.beforeCreate = func() {
		if _, err := store.New(pool).GetOAuthClient(ctx, stale.ID); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("stale client exists when CreateOAuthClient begins: %v", err)
		}
	}

	rec := httptest.NewRecorder()
	s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"]}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	if _, err := store.New(pool).GetOAuthClient(ctx, stale.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale client remains after successful registration: %v", err)
	}
	if len(q.created) != 1 {
		t.Errorf("CreateOAuthClient calls = %d, want 1", len(q.created))
	}
}

func TestRegister_HostileClientNamesRemainCanonicalJSONText(t *testing.T) {
	cases := []struct {
		name       string
		clientName string
		markup     bool
	}{
		{"markup", `Agent <img src=x onerror=alert(1)>`, true},
		{"right-to-left override", "Agent\u202eRTL", false},
		{"zero-width space", "Agent\u200bZero", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &registrationQueries{id: uuid.New()}
			s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
			body, err := json.Marshal(map[string]any{
				"client_name":   tc.clientName,
				"redirect_uris": []string{"https://agent.example/callback"},
			})
			if err != nil {
				t.Fatalf("Marshal request: %v", err)
			}
			rec := httptest.NewRecorder()
			s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", string(body)))
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
			}
			if len(q.created) != 1 || q.created[0].ClientName != tc.clientName {
				t.Fatalf("stored client names = %#v, want canonical %q", q.created, tc.clientName)
			}
			var response struct {
				ClientName string `json:"client_name"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("response is not JSON with a string client_name: %v", err)
			}
			if response.ClientName != tc.clientName {
				t.Errorf("decoded client name = %q, want canonical %q", response.ClientName, tc.clientName)
			}
			if tc.markup && strings.Contains(rec.Body.String(), tc.clientName) {
				t.Errorf("wire response contains raw executable markup: %q", rec.Body.String())
			}
		})
	}
}

func TestNewService_RejectsRequiredDependencies(t *testing.T) {
	ctx := context.Background()
	_, err := NewService(ctx, ServiceDependencies{})
	if err == nil {
		t.Fatal("NewService() error = nil, want missing dependency error")
	}
}
