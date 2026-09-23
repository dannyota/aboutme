package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const sfOrigin = "https://aboutme.vn"

// sfCredential is the fake service's decoded credential.
type sfCredential struct{ method string }

func (c sfCredential) SecondFactorMethod() string { return c.method }

// sfService is a scripted SecondFactorService that records every call.
type sfService struct {
	mu            sync.Mutex
	enabled       bool
	calls         []string
	methods       []string
	state         auth.SecondFactorState
	registration  auth.SecondFactorRegistration
	issue         *auth.SessionIssue
	replacement   auth.SessionIssue
	err           error
	completeCalls int
}

func (s *sfService) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *sfService) workCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, call := range s.calls {
		if !strings.HasPrefix(call, "decode") && call != "enabled" {
			out = append(out, call)
		}
	}
	return out
}

func (s *sfService) PasskeyEnrollmentEnabled() bool { s.record("enabled"); return s.enabled }

func (s *sfService) decode(body []byte, method string) (auth.SecondFactorCredential, error) {
	s.record("decode")
	if string(body) != `{"ok":true}` {
		return nil, auth.ErrSecondFactorRequestInvalid
	}
	return sfCredential{method: method}, nil
}

func (s *sfService) DecodePasskeyRegistration(body []byte) (auth.SecondFactorCredential, error) {
	return s.decode(body, auth.SecondFactorMethodPasskey)
}

func (s *sfService) DecodePasskeyAssertion(body []byte) (auth.SecondFactorCredential, error) {
	return s.decode(body, auth.SecondFactorMethodPasskey)
}

func (s *sfService) DecodeRecoveryCode(code string) (auth.SecondFactorCredential, error) {
	s.record("decode")
	if code != "amr_valid" {
		return nil, auth.ErrSecondFactorRequestInvalid
	}
	return sfCredential{method: auth.SecondFactorMethodRecovery}, nil
}

func (s *sfService) PendingMethods(context.Context, uuid.UUID) ([]string, error) {
	s.record("methods")
	return s.methods, s.err
}

func (s *sfService) StartPasskeyAssertion(context.Context, string, *uuid.UUID) (auth.SecondFactorOptions, error) {
	s.record("start_assertion")
	return auth.SecondFactorOptions{CeremonyID: "c", PublicKey: json.RawMessage(`{}`)}, s.err
}

func (s *sfService) CompletePending(context.Context, string, *uuid.UUID, auth.SecondFactorCredential, auth.SecondFactorClient) (*auth.SessionIssue, error) {
	s.record("complete_pending")
	return s.issue, s.err
}

func (s *sfService) State(context.Context, uuid.UUID) (auth.SecondFactorState, error) {
	s.record("state")
	return s.state, s.err
}

func (s *sfService) StartPasskeyRegistration(context.Context, store.Session) (auth.SecondFactorOptions, error) {
	s.record("start_registration")
	return auth.SecondFactorOptions{CeremonyID: "c", PublicKey: json.RawMessage(`{}`)}, s.err
}

func (s *sfService) CompletePasskeyRegistration(context.Context, store.Session, auth.SecondFactorCredential) (auth.SecondFactorRegistration, error) {
	s.record("complete_registration")
	s.mu.Lock()
	s.completeCalls++
	s.mu.Unlock()
	return s.registration, s.err
}

func (s *sfService) RemovePasskey(context.Context, store.Session, uuid.UUID) (auth.SessionIssue, error) {
	s.record("remove")
	return s.replacement, s.err
}

func (s *sfService) RegenerateRecoveryCodes(context.Context, store.Session) (auth.SecondFactorRecoveryCodes, error) {
	s.record("regenerate")
	return auth.SecondFactorRecoveryCodes{Codes: []string{"amr_new"}, Session: s.replacement}, s.err
}

// sfMux builds the routes over service with no TOTP dependency wired. pool
// may be nil for tests that fail before any database work.
func sfMux(t *testing.T, service *sfService, pool *store.Pool) *http.ServeMux {
	t.Helper()
	return sfMuxWithTOTP(t, service, nil, pool)
}

// sfMuxWithTOTP builds the routes over service and totp. A nil totp matches
// sfMux: every TOTP route then answers as unregistered.
func sfMuxWithTOTP(t *testing.T, service *sfService, totp auth.TOTPSecondFactorService, pool *store.Pool) *http.ServeMux {
	t.Helper()
	sessions := auth.NewSessionManager(store.New(nil))
	pending := auth.NewPendingAuthenticationManager(nil, nil)
	if pool != nil {
		sessions = auth.NewSessionManagerWithPool(pool)
		pending = auth.NewPendingAuthenticationManager(pool, nil)
	}
	handlers, err := auth.NewSecondFactorHandlers(auth.SecondFactorHandlerOptions{
		Service: service, TOTP: totp, Pending: pending, Sessions: sessions, Limits: auth.NewSecondFactorRatePolicies(),
		PublicOrigin: sfOrigin, Clock: time.Now,
	})
	if err != nil {
		t.Fatalf("NewSecondFactorHandlers() error = %v", err)
	}
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)
	return mux
}

// sfTOTPService is a scripted TOTPSecondFactorService that records every
// call, mirroring sfService's shape.
type sfTOTPService struct {
	mu          sync.Mutex
	enabled     bool
	calls       []string
	state       bool
	start       auth.TOTPEnrollmentStart
	completion  auth.TOTPEnrollmentComplete
	replacement auth.SessionIssue
	err         error
}

func (s *sfTOTPService) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *sfTOTPService) workCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, call := range s.calls {
		if !strings.HasPrefix(call, "decode") && call != "enabled" {
			out = append(out, call)
		}
	}
	return out
}

func (s *sfTOTPService) TOTPEnrollmentEnabled() bool { s.record("enabled"); return s.enabled }

func (s *sfTOTPService) DecodeTOTPCode(body []byte) (auth.SecondFactorCredential, error) {
	s.record("decode")
	var payload struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Code != "123456" {
		return nil, auth.ErrSecondFactorRequestInvalid
	}
	return sfCredential{method: auth.SecondFactorMethodTOTP}, nil
}

func (s *sfTOTPService) DecodeTOTPEnrollmentCompletion(body []byte) (auth.SecondFactorCredential, error) {
	s.record("decode")
	var payload struct {
		EnrollmentID string `json:"enrollmentId"`
		Code         string `json:"code"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil, auth.ErrSecondFactorRequestInvalid
	}
	if payload.EnrollmentID == "bad-enrollment-shape" {
		return nil, auth.ErrTOTPEnrollmentInvalid
	}
	if payload.Code != "123456" {
		return nil, auth.ErrSecondFactorRequestInvalid
	}
	return sfCredential{method: auth.SecondFactorMethodTOTP}, nil
}

func (s *sfTOTPService) TOTPState(context.Context, uuid.UUID) (bool, error) {
	s.record("totp_state")
	return s.state, s.err
}

func (s *sfTOTPService) StartTOTPEnrollment(context.Context, store.Session) (auth.TOTPEnrollmentStart, error) {
	s.record("start_totp_enrollment")
	return s.start, s.err
}

func (s *sfTOTPService) CompleteTOTPEnrollment(context.Context, store.Session, auth.SecondFactorCredential) (auth.TOTPEnrollmentComplete, error) {
	s.record("complete_totp_enrollment")
	return s.completion, s.err
}

func (s *sfTOTPService) RemoveTOTP(context.Context, store.Session) (auth.SessionIssue, error) {
	s.record("remove_totp")
	return s.replacement, s.err
}

type sfRequest struct {
	method, path, body, contentType, origin, csrf string
	cookies                                       []*http.Cookie
}

func sfServe(t *testing.T, mux http.Handler, req sfRequest) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), req.method, req.path, strings.NewReader(req.body))
	if req.body == "" {
		r = httptest.NewRequestWithContext(t.Context(), req.method, req.path, nil)
	}
	if req.contentType != "" {
		r.Header.Set("Content-Type", req.contentType)
	}
	if req.origin != "" {
		r.Header.Set("Origin", req.origin)
	}
	if req.csrf != "" {
		r.Header.Set(auth.CSRFHeaderName, req.csrf)
	}
	for _, cookie := range req.cookies {
		r.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func sfErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope %q: %v", rec.Body.String(), err)
	}
	return envelope.Error.Code
}

// sfClearsCookie reports whether rec expires the named cookie.
func sfClearsCookie(rec *httptest.ResponseRecorder, name string) bool {
	for _, header := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(header, name+"=;") && strings.Contains(header, "Max-Age=0") {
			return true
		}
	}
	return false
}

func sfSetsCookie(rec *httptest.ResponseRecorder, name, value string) bool {
	for _, header := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(header, name+"="+value+";") {
			return true
		}
	}
	return false
}

func TestSecondFactorRegistrationOptions_DisabledMatchesUnregisteredRoute(t *testing.T) {
	service := &sfService{}
	mux := sfMux(t, service, nil)
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		req := sfRequest{method: method, path: auth.SecondFactorPasskeyOptionsPath, body: `{}`, contentType: "application/json", origin: sfOrigin}
		got := sfServe(t, mux, req)
		want := sfServe(t, api.NotFound(), req)
		if got.Code != want.Code || got.Body.String() != want.Body.String() || got.Header().Get("Content-Type") != want.Header().Get("Content-Type") {
			t.Fatalf("%s disabled options = %d %s, want %d %s", method, got.Code, got.Body, want.Code, want.Body)
		}
	}
	if calls := service.workCalls(); len(calls) != 0 {
		t.Fatalf("disabled options reached service work: %v", calls)
	}
}

func TestSecondFactorRegistrationComplete_DisabledIsUniformNotFound(t *testing.T) {
	service := &sfService{}
	mux := sfMux(t, service, nil)
	req := sfRequest{method: http.MethodPost, path: auth.SecondFactorPasskeysPath, body: `{"ok":true}`, contentType: "application/json", origin: sfOrigin}
	got := sfServe(t, mux, req)
	want := sfServe(t, api.NotFound(), req)
	if got.Code != http.StatusNotFound || got.Body.String() != want.Body.String() || len(got.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("disabled completion without a session = %d %s", got.Code, got.Body)
	}
	if service.completeCalls != 0 {
		t.Fatal("disabled completion without a session reached the service")
	}
}

// TestSecondFactorPending_ChecksRunInContractOrder proves media type, body
// size, and strict JSON fail before the pending cookie is read, and the
// missing cookie fails before any service work.
func TestSecondFactorPending_ChecksRunInContractOrder(t *testing.T) {
	service := &sfService{enabled: true}
	mux := sfMux(t, service, nil)
	oversized := `{"ok":true,"pad":"` + strings.Repeat("a", 32768) + `"}`
	cases := []struct {
		name string
		req  sfRequest
		code int
		err  string
	}{
		{"media type first", sfRequest{method: http.MethodPost, path: auth.SecondFactorPendingPasskeyPath, body: oversized, contentType: "text/plain"}, 415, "media_type_unsupported"},
		{"body size second", sfRequest{method: http.MethodPost, path: auth.SecondFactorPendingPasskeyPath, body: oversized, contentType: "application/json"}, 413, "body_too_large"},
		{"strict json third", sfRequest{method: http.MethodPost, path: auth.SecondFactorPendingPasskeyPath, body: `{"ok":false}`, contentType: "application/json"}, 400, "request_invalid"},
		{"pending cookie fourth", sfRequest{method: http.MethodPost, path: auth.SecondFactorPendingPasskeyPath, body: `{"ok":true}`, contentType: "application/json"}, 401, "authentication_required"},
		{"options body is exactly empty", sfRequest{method: http.MethodPost, path: auth.SecondFactorPendingOptionsPath, body: `{"a":"b"}`, contentType: "application/json"}, 400, "request_invalid"},
		{"options without cookie", sfRequest{method: http.MethodPost, path: auth.SecondFactorPendingOptionsPath, body: `{}`, contentType: "application/json"}, 401, "authentication_required"},
		{"recovery body limit", sfRequest{method: http.MethodPost, path: auth.SecondFactorPendingRecoveryPath, body: `{"code":"` + strings.Repeat("a", 4096) + `"}`, contentType: "application/json"}, 413, "body_too_large"},
		{"recovery unknown field", sfRequest{method: http.MethodPost, path: auth.SecondFactorPendingRecoveryPath, body: `{"code":"amr_valid","x":"y"}`, contentType: "application/json"}, 400, "request_invalid"},
		{"recovery shape", sfRequest{method: http.MethodPost, path: auth.SecondFactorPendingRecoveryPath, body: `{"code":"nope"}`, contentType: "application/json"}, 400, "request_invalid"},
		{"recovery without cookie", sfRequest{method: http.MethodPost, path: auth.SecondFactorPendingRecoveryPath, body: `{"code":"amr_valid"}`, contentType: "application/json"}, 401, "authentication_required"},
		{"status without cookie", sfRequest{method: http.MethodGet, path: auth.SecondFactorPendingPath}, 401, "authentication_required"},
	}
	for _, tc := range cases {
		rec := sfServe(t, mux, tc.req)
		if rec.Code != tc.code || sfErrorCode(t, rec) != tc.err {
			t.Errorf("%s = %d %s, want %d %s", tc.name, rec.Code, rec.Body, tc.code, tc.err)
		}
		if rec.Header().Get("Cache-Control") != api.CacheControlNoStore {
			t.Errorf("%s Cache-Control = %q", tc.name, rec.Header().Get("Cache-Control"))
		}
		if tc.code == 401 && !sfClearsCookie(rec, "__Host-auth-pending") {
			t.Errorf("%s did not clear the pending cookie", tc.name)
		}
	}
	if calls := service.workCalls(); len(calls) != 0 {
		t.Fatalf("rejected pending requests reached service work: %v", calls)
	}
}

func TestSecondFactorAccountRoutes_RequireASession(t *testing.T) {
	service := &sfService{enabled: true}
	mux := sfMux(t, service, nil)
	pendingOnly := []*http.Cookie{{Name: "__Host-auth-pending", Value: "pending-token"}}
	for _, req := range []sfRequest{
		{method: http.MethodGet, path: auth.SecondFactorStatePath, cookies: pendingOnly},
		{method: http.MethodPost, path: auth.SecondFactorPasskeyOptionsPath, body: `{}`, contentType: "application/json", origin: sfOrigin},
		{method: http.MethodPost, path: auth.SecondFactorPasskeysPath, body: `{"ok":true}`, contentType: "application/json", origin: sfOrigin},
		{method: http.MethodDelete, path: auth.SecondFactorPasskeysPath + "/" + uuid.NewString(), origin: sfOrigin},
		{method: http.MethodPost, path: auth.SecondFactorRecoveryCodesPath, origin: sfOrigin, cookies: pendingOnly},
	} {
		rec := sfServe(t, mux, req)
		if rec.Code != http.StatusUnauthorized || sfErrorCode(t, rec) != "authentication_required" {
			t.Errorf("%s %s without a session = %d %s", req.method, req.path, rec.Code, rec.Body)
		}
	}
	if calls := service.workCalls(); len(calls) != 0 {
		t.Fatalf("unauthenticated account requests reached service work: %v", calls)
	}
}

// TestSecondFactorTOTPRoutes_UnwiredMatchesUnregisteredRoute proves every
// TOTP route is registered on the mux (a real pattern, not method_not_allowed
// from a miss) but answers byte-identical to an unregistered route while no
// TOTP dependency is wired, the same shape passkey registration options uses
// while its flag is off.
func TestSecondFactorTOTPRoutes_UnwiredMatchesUnregisteredRoute(t *testing.T) {
	service := &sfService{enabled: true}
	mux := sfMux(t, service, nil)
	for _, req := range []sfRequest{
		{method: http.MethodPost, path: auth.SecondFactorPendingTOTPPath, body: `{"code":"123456"}`, contentType: "application/json"},
		{method: http.MethodPost, path: auth.SecondFactorTOTPEnrollmentPath, body: `{}`, contentType: "application/json", origin: sfOrigin},
		{method: http.MethodPut, path: auth.SecondFactorTOTPEnrollmentPath, body: `{"enrollmentId":"x","code":"123456"}`, contentType: "application/json", origin: sfOrigin},
	} {
		_, pattern := mux.Handler(httptest.NewRequestWithContext(t.Context(), req.method, req.path, nil))
		if pattern == "" {
			t.Errorf("%s %s is not routed at all, want a registered but unwired pattern", req.method, req.path)
		}
		got := sfServe(t, mux, req)
		want := sfServe(t, api.NotFound(), req)
		if got.Code != http.StatusNotFound || got.Body.String() != want.Body.String() {
			t.Errorf("%s %s unwired = %d %s, want the unregistered-route body", req.method, req.path, got.Code, got.Body)
		}
	}
	if calls := service.workCalls(); len(calls) != 0 {
		t.Fatalf("unwired TOTP requests reached the passkey and recovery service: %v", calls)
	}
}

// TestSecondFactorTOTPEnrollmentStart_DisabledMatchesUnregisteredRoute
// mirrors TestSecondFactorRegistrationOptions_DisabledMatchesUnregisteredRoute
// for TOTP: the flag is rechecked before any session or database work.
func TestSecondFactorTOTPEnrollmentStart_DisabledMatchesUnregisteredRoute(t *testing.T) {
	totp := &sfTOTPService{}
	mux := sfMuxWithTOTP(t, &sfService{}, totp, nil)
	req := sfRequest{method: http.MethodPost, path: auth.SecondFactorTOTPEnrollmentPath, body: `{}`, contentType: "application/json", origin: sfOrigin}
	got := sfServe(t, mux, req)
	want := sfServe(t, api.NotFound(), req)
	if got.Code != http.StatusNotFound || got.Body.String() != want.Body.String() {
		t.Fatalf("disabled start = %d %s, want the unregistered-route body", got.Code, got.Body)
	}
	if calls := totp.workCalls(); len(calls) != 0 {
		t.Fatalf("disabled start reached service work: %v", calls)
	}
}

// TestSecondFactorTOTPEnrollmentComplete_DisabledIsUniformNotFound mirrors
// TestSecondFactorRegistrationComplete_DisabledIsUniformNotFound: a disabled
// completion attempt without a session cannot reach the service at all.
func TestSecondFactorTOTPEnrollmentComplete_DisabledIsUniformNotFound(t *testing.T) {
	totp := &sfTOTPService{}
	mux := sfMuxWithTOTP(t, &sfService{}, totp, nil)
	req := sfRequest{method: http.MethodPut, path: auth.SecondFactorTOTPEnrollmentPath, body: `{"enrollmentId":"x","code":"123456"}`, contentType: "application/json", origin: sfOrigin}
	got := sfServe(t, mux, req)
	want := sfServe(t, api.NotFound(), req)
	if got.Code != http.StatusNotFound || got.Body.String() != want.Body.String() || len(got.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("disabled completion without a session = %d %s", got.Code, got.Body)
	}
}

func TestSecondFactorTOTPEnrollment_MethodNotAllowed(t *testing.T) {
	totp := &sfTOTPService{enabled: true}
	mux := sfMuxWithTOTP(t, &sfService{}, totp, nil)
	rec := sfServe(t, mux, sfRequest{method: http.MethodDelete, path: auth.SecondFactorTOTPEnrollmentPath})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE on the enrollment path = %d, want 405", rec.Code)
	}
}

func TestSecondFactorRatePolicies_BoundAttemptsPerAccountAndIP(t *testing.T) {
	limits := auth.NewSecondFactorRatePolicies()
	now := time.Now()
	account := uuid.New()
	for i := 0; i < 10; i++ {
		if d := limits.AdmitAttempt(now, account, "203.0.113.9"); !d.Allowed {
			t.Fatalf("attempt %d rejected", i+1)
		}
	}
	if d := limits.AdmitAttempt(now, account, "203.0.113.9"); d.Allowed || d.RetryAfterSeconds < 1 {
		t.Fatalf("eleventh attempt for one account and IP = %+v, want a rejection", d)
	}
	if d := limits.AdmitAttempt(now, uuid.New(), "203.0.113.9"); !d.Allowed {
		t.Fatal("another account on the same IP was rejected before the IP budget")
	}
	ip := "198.51.100.4"
	for i := 0; i < 30; i++ {
		limits.AdmitAttempt(now, uuid.New(), ip)
	}
	if d := limits.AdmitAttempt(now, uuid.New(), ip); d.Allowed {
		t.Fatal("the 31st attempt from one IP in a minute was admitted")
	}
}

func TestSecondFactorWriters_MapClosedErrors(t *testing.T) {
	if !errors.Is(auth.FailPendingVerification(auth.ErrSecondFactorVerificationFailed, nil), auth.ErrSecondFactorVerificationFailed) {
		t.Fatal("a pending verification failure does not unwrap to the service error")
	}
	body, err := json.Marshal(auth.SecondFactorOptions{CeremonyID: "id", PublicKey: json.RawMessage(`{"a":1}`)})
	if err != nil || !bytes.Equal(body, []byte(`{"ceremonyId":"id","publicKey":{"a":1}}`)) {
		t.Fatalf("options encode as %s, %v", body, err)
	}
}
