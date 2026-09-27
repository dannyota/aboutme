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

// TestNativeLoopbackRedirect needs no database: nativeLoopbackRedirect is a
// pure function. The general redirect grammar already admits "localhost" for
// a client that omits application_type, so native accepts it too; see
// docs/design/mcp-client-compatibility.md rule 3. A lookalike host such as
// "localhost.evil.example" is a different hostname and never matches.
func TestNativeLoopbackRedirect(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{"IPv4 loopback", "http://127.0.0.1:20090/callback", true},
		{"IPv6 loopback", "http://[::1]:20090/callback", true},
		{"loopback with path only", "http://127.0.0.1/callback", true},
		{"localhost", "http://localhost:20090/callback", true},
		{"localhost no port", "http://localhost/callback", true},
		{"loopback-looking prefix", "http://localhost.evil.example/callback", false},
		{"https loopback", "https://127.0.0.1:20090/callback", false},
		{"non-loopback host", "http://agent.example/callback", false},
		{"malformed", "http://[::1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nativeLoopbackRedirect(tc.raw); got != tc.want {
				t.Errorf("nativeLoopbackRedirect(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

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
	r := httptest.NewRequestWithContext(context.Background(), method, "https://aboutme.example/oauth/register", strings.NewReader(body))
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

func TestRegister_ApplicationTypeCompatibility(t *testing.T) {
	const legacy = `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"],"token_endpoint_auth_method":"none"}`
	const native = `{"client_name":"aboutme MCP owner workflow","redirect_uris":["http://127.0.0.1:20090/callback"],"token_endpoint_auth_method":"none","application_type":"native"}`
	const nativeIPv6 = `{"client_name":"Agent","redirect_uris":["http://[::1]:20090/callback"],"application_type":"native"}`
	// Claude Code registers a loopback redirect naming "localhost", not an IP
	// literal; the general grammar already admits it, so native does too. See
	// docs/design/mcp-client-compatibility.md rule 3.
	const nativeLocalhost = `{"client_name":"Agent","redirect_uris":["http://localhost:33418/callback"],"application_type":"native"}`
	// Unknown registration members are ignored, never stored or echoed. See
	// docs/design/mcp-client-compatibility.md rule 2.
	const nativeWithUnknownMembers = `{"client_name":"Agent","redirect_uris":["http://127.0.0.1/callback"],"application_type":"native","client_uri":"https://agent.example"}`
	// Claude on the web derives application_type web for its https redirect;
	// see docs/design/mcp-client-compatibility.md rule 2.
	const web = `{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"application_type":"web"}`

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"legacy request", legacy, `"token_endpoint_auth_method":"none"}`},
		{"SDK native loopback request", native, `"application_type":"native"`},
		{"native IPv6 loopback request", nativeIPv6, `"application_type":"native"`},
		{"native localhost loopback request", nativeLocalhost, `"application_type":"native"`},
		{"native request with unknown members", nativeWithUnknownMembers, `"application_type":"native"`},
		{"Claude web https redirect", web, `"application_type":"web"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &registrationQueries{id: uuid.New()}
			s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
			rec := httptest.NewRecorder()
			s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", tc.body))
			if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
			}
			if len(q.created) != 1 {
				t.Fatalf("created = %d, want 1", len(q.created))
			}
			if strings.Contains(rec.Body.String(), "client_uri") {
				t.Errorf("response echoed an unknown member: %q", rec.Body.String())
			}
		})
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"non-string", `{"client_name":"Agent","redirect_uris":["http://127.0.0.1/callback"],"application_type":true}`},
		{"empty", `{"client_name":"Agent","redirect_uris":["http://127.0.0.1/callback"],"application_type":""}`},
		{"duplicate", `{"client_name":"Agent","redirect_uris":["http://127.0.0.1/callback"],"application_type":"native","application_type":"native"}`},
		{"unknown application_type", `{"client_name":"Agent","redirect_uris":["http://127.0.0.1/callback"],"application_type":"other"}`},
		{"web with http loopback redirect", `{"client_name":"Agent","redirect_uris":["http://127.0.0.1/callback"],"application_type":"web"}`},
		{"empty redirect", `{"client_name":"Agent","redirect_uris":[""],"application_type":"native"}`},
		{"non-loopback redirect", `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"],"application_type":"native"}`},
		{"mixed redirects", `{"client_name":"Agent","redirect_uris":["http://127.0.0.1/callback","https://agent.example/callback"],"application_type":"native"}`},
		{"native custom scheme redirect", `{"client_name":"Agent","redirect_uris":["com.example.app:/cb"],"application_type":"native"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &registrationQueries{id: uuid.New()}
			s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
			rec := httptest.NewRecorder()
			s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", tc.body))
			if rec.Code != http.StatusBadRequest || len(q.created) != 0 {
				t.Fatalf("response = %d created = %d", rec.Code, len(q.created))
			}
		})
	}
}

// TestRegister_TypeScriptSDKDefaultShapeIgnoresUnknownMembers registers with
// the shape the official TypeScript SDK sends: the four known optional
// members plus several members the SDK also sends that this server does not
// define. See docs/design/mcp-client-compatibility.md rule 2.
func TestRegister_TypeScriptSDKDefaultShapeIgnoresUnknownMembers(t *testing.T) {
	q := &registrationQueries{id: uuid.New()}
	s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
	body := `{"client_name":"Claude Code","redirect_uris":["http://localhost:33418/callback"],` +
		`"grant_types":["authorization_code","refresh_token"],"response_types":["code"],` +
		`"token_endpoint_auth_method":"none","application_type":"native",` +
		`"scope":"resumes:read resumes:write",` +
		`"client_uri":"https://agent.example","logo_uri":"https://agent.example/logo.png",` +
		`"software_id":"claude-code","software_version":"1.0.0",` +
		`"contacts":["team@agent.example"],"nested":{"a":1,"b":[1,2,3]}}`

	rec := httptest.NewRecorder()
	s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	if len(q.created) != 1 {
		t.Fatalf("created = %d, want 1", len(q.created))
	}
	for _, unknown := range []string{"client_uri", "logo_uri", "software_id", "software_version", "contacts", "nested"} {
		if strings.Contains(rec.Body.String(), unknown) {
			t.Errorf("response echoed unknown member %q: %s", unknown, rec.Body.String())
		}
	}
	want := `{"client_id":"` + q.id.String() + `","client_name":"Claude Code","redirect_uris":["http://localhost:33418/callback"],"token_endpoint_auth_method":"none","application_type":"native"}`
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// TestRegister_GrantTypesResponseTypesAndScope covers rule 2's three
// remaining known members and their rejected neighbors.
func TestRegister_GrantTypesResponseTypesAndScope(t *testing.T) {
	const base = `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"]`

	accepted := []struct {
		name string
		body string
	}{
		{"grant_types both", base + `,"grant_types":["authorization_code","refresh_token"]}`},
		{"grant_types authorization_code only", base + `,"grant_types":["authorization_code"]}`},
		{"response_types code", base + `,"response_types":["code"]}`},
		{"scope read", base + `,"scope":"resumes:read"}`},
		{"scope write", base + `,"scope":"resumes:write"}`},
		{"scope both", base + `,"scope":"resumes:read resumes:write"}`},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			q := &registrationQueries{id: uuid.New()}
			s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
			rec := httptest.NewRecorder()
			s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", tc.body))
			if rec.Code != http.StatusCreated {
				t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
			}
			if len(q.created) != 1 {
				t.Fatalf("created = %d, want 1", len(q.created))
			}
			if strings.Contains(rec.Body.String(), "scope") || strings.Contains(rec.Body.String(), "grant_type") || strings.Contains(rec.Body.String(), "response_type") {
				t.Errorf("response echoed a registration-only member: %q", rec.Body.String())
			}
		})
	}

	rejected := []struct {
		name string
		body string
	}{
		{"grant_types client_credentials", base + `,"grant_types":["client_credentials"]}`},
		{"grant_types implicit", base + `,"grant_types":["implicit"]}`},
		{"grant_types empty array", base + `,"grant_types":[]}`},
		{"grant_types string", base + `,"grant_types":"authorization_code"}`},
		{"grant_types duplicate member", base + `,"grant_types":["authorization_code","authorization_code"]}`},
		{"response_types token", base + `,"response_types":["token"]}`},
		{"response_types code and token", base + `,"response_types":["code","token"]}`},
		{"response_types empty array", base + `,"response_types":[]}`},
		{"scope resumes:admin", base + `,"scope":"resumes:admin"}`},
		{"scope openid", base + `,"scope":"openid"}`},
		{"scope empty string", base + `,"scope":""}`},
		{"scope double space", base + `,"scope":"resumes:read  resumes:write"}`},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			q := &registrationQueries{id: uuid.New()}
			s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
			rec := httptest.NewRecorder()
			s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", tc.body))
			if rec.Code != http.StatusBadRequest || len(q.created) != 0 {
				t.Fatalf("response = %d created = %d", rec.Code, len(q.created))
			}
		})
	}
}

// TestRegister_DuplicateUnknownMemberRejects confirms the duplicate-member
// check still applies to a member outside the closed set, even though a
// single occurrence of it is ignored.
func TestRegister_DuplicateUnknownMemberRejects(t *testing.T) {
	q := &registrationQueries{id: uuid.New()}
	s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
	body := `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"],"client_uri":"https://agent.example","client_uri":"https://other.example"}`
	rec := httptest.NewRecorder()
	s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", body))
	if rec.Code != http.StatusBadRequest || len(q.created) != 0 {
		t.Fatalf("response = %d created = %d", rec.Code, len(q.created))
	}
}

// TestRegister_OverLimitBodyWithUnknownMembersRejects confirms the
// 4,096-byte cap is enforced before unknown members are ignored, so padding
// a body with unknown metadata cannot bypass it.
func TestRegister_OverLimitBodyWithUnknownMembersRejects(t *testing.T) {
	valid := `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"],"client_uri":"https://agent.example"}`
	overLimit := valid[:len(valid)-1] + strings.Repeat(" ", 4096-len(valid)+2) + valid[len(valid)-1:]

	q := &registrationQueries{id: uuid.New()}
	s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
	rec := httptest.NewRecorder()
	s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", overLimit))
	if rec.Code != http.StatusRequestEntityTooLarge || len(q.created) != 0 {
		t.Fatalf("response = %d created = %d", rec.Code, len(q.created))
	}
}

// TestRegisteredRedirect_LocalhostMatchesOnlyItsExactRegisteredValue confirms
// that adding "localhost" to nativeLoopbackHosts does not widen what a
// registered client may authorize to: registeredRedirect still requires a
// byte-for-byte match, port included. See
// docs/design/mcp-client-compatibility.md rule 3.
func TestRegisteredRedirect_LocalhostMatchesOnlyItsExactRegisteredValue(t *testing.T) {
	client := store.OAuthClient{RedirectURIs: []byte(`["http://localhost:33418/callback"]`)}
	for _, tc := range []struct {
		name     string
		redirect string
		want     bool
	}{
		{"registered value", "http://localhost:33418/callback", true},
		{"different port", "http://localhost:33419/callback", false},
		{"IP literal instead of localhost", "http://127.0.0.1:33418/callback", false},
		{"extra path segment", "http://localhost:33418/callback/x", false},
		{"lookalike host", "http://localhost.evil.example:33418/callback", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := registeredRedirect(client, tc.redirect); got != tc.want {
				t.Errorf("registeredRedirect(%q) = %v, want %v", tc.redirect, got, tc.want)
			}
		})
	}
}

// TestValidateRedirectURI_RejectsLocalhostLookalikes confirms that widening
// native registration to "localhost" does not touch the general redirect
// grammar's userinfo and hostname rules: a client cannot register a redirect
// that only resembles "localhost".
func TestValidateRedirectURI_RejectsLocalhostLookalikes(t *testing.T) {
	for _, raw := range []string{
		"http://localhost.evil.example/cb",
		"http://localhost@evil.example/cb",
	} {
		if err := ValidateRedirectURI(raw); err == nil {
			t.Errorf("ValidateRedirectURI(%q) = nil, want error", raw)
		}
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
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), "DELETE FROM oauth_clients WHERE id = $1", stale.ID); cleanupErr != nil {
			t.Errorf("cleanup stale client: %v", cleanupErr)
		}
	})
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
