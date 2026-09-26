package viewapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	mathrand "math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/viewcount"
)

// Route tests for docs/design/viewer-analytics/delivery.md, "API", and the
// OpenAPI operations startPublicResumeView and collectPublicResumeView.

const testOrigin = "https://aboutme.vn"

type memoryStore struct {
	resume viewcount.LiveResume
	days   []viewcount.DayCell
}

func (s *memoryStore) LiveResumeBySlug(_ context.Context, slug string) (viewcount.LiveResume, error) {
	if slug != "ada-lovelace" {
		return viewcount.LiveResume{}, viewcount.ErrNotFound
	}
	return s.resume, nil
}

func (s *memoryStore) LiveResumeByID(_ context.Context, id uuid.UUID) (viewcount.LiveResume, error) {
	if id != s.resume.ID {
		return viewcount.LiveResume{}, viewcount.ErrNotFound
	}
	return s.resume, nil
}

func (s *memoryStore) AddCounts(_ context.Context, days []viewcount.DayCell, _ []viewcount.SignalCell) error {
	s.days = append(s.days, days...)
	return nil
}

type noOwnerQueries struct{}

func (noOwnerQueries) ListViewSummaries(context.Context, store.ListViewSummariesParams) ([]store.ListViewSummariesRow, error) {
	return nil, nil
}

func (noOwnerQueries) GetOwnedViewResume(context.Context, store.GetOwnedViewResumeParams) (store.GetOwnedViewResumeRow, error) {
	return store.GetOwnedViewResumeRow{}, nil
}

func (noOwnerQueries) ListResumeViewDays(context.Context, store.ListResumeViewDaysParams) ([]store.ListResumeViewDaysRow, error) {
	return nil, nil
}

func (noOwnerQueries) ListResumeShareSignals(context.Context, store.ListResumeShareSignalsParams) ([]store.ListResumeShareSignalsRow, error) {
	return nil, nil
}

type routeFixture struct {
	now     time.Time
	store   *memoryStore
	counter *viewcount.Counter
	mux     *http.ServeMux
}

func newRouteFixture(t *testing.T) *routeFixture {
	t.Helper()
	f := &routeFixture{
		now: time.Date(2026, time.September, 26, 3, 0, 0, 0, time.UTC),
		store: &memoryStore{resume: viewcount.LiveResume{
			ID: uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000b1"), Owner: uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000a1"),
		}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	counter, err := viewcount.New(viewcount.Config{
		Store: f.store, Logger: logger, Now: func() time.Time { return f.now }, Random: mathrand.NewChaCha8([32]byte{3}),
	})
	if err != nil {
		t.Fatalf("viewcount.New: %v", err)
	}
	f.counter = counter
	service, err := New(Dependencies{
		Counter: counter, Queries: noOwnerQueries{}, Sessions: auth.NewSessionManager(nil), PublicOrigin: testOrigin,
		TrustedProxies: api.LoopbackTrustedProxies(), Now: func() time.Time { return f.now }, Logger: logger,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.mux = http.NewServeMux()
	service.RegisterRoutes(f.mux)
	return f
}

// request sends a same-origin JSON POST through a trusted proxy for addr.
func (f *routeFixture) request(method, path, body string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:40000"
	r.Header.Set("X-Real-IP", "203.0.113.5")
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", "application/json")
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error body %q: %v", w.Body.String(), err)
	}
	return envelope.Error.Code
}

const startPath = "/api/v1/public/resumes/ada-lovelace/views/start"
const collectPath = "/api/v1/public/views/collect"

func TestPublicRoutesRejectMalformedRequests(t *testing.T) {
	f := newRouteFixture(t)
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		edit       func(*http.Request)
		wantStatus int
		wantCode   string
	}{
		{"get", http.MethodGet, startPath, "", nil, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"escaped collect path", http.MethodPost, "/api/v1/public/views/%63ollect", "{}", nil, http.StatusNotFound, "not_found"},
		{"escaped start path", http.MethodPost, "/api/v1/public/resumes/ada-lovelace/views/%73tart", "{}", nil, http.StatusNotFound, "not_found"},
		{"missing origin", http.MethodPost, startPath, "{}", func(r *http.Request) { r.Header.Del("Origin") }, http.StatusForbidden, "origin_rejected"},
		{"cross-site origin", http.MethodPost, startPath, "{}", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, http.StatusForbidden, "origin_rejected"},
		{"repeated origin", http.MethodPost, startPath, "{}", func(r *http.Request) { r.Header.Add("Origin", testOrigin) }, http.StatusForbidden, "origin_rejected"},
		{"form body", http.MethodPost, startPath, "{}", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, http.StatusUnsupportedMediaType, "media_type_unsupported"},
		{"large body", http.MethodPost, collectPath, strings.Repeat(" ", maxBodyBytes+1), nil, http.StatusRequestEntityTooLarge, "body_too_large"},
		{"start with fields", http.MethodPost, startPath, `{"mode":"ask"}`, nil, http.StatusBadRequest, "request_invalid"},
		{"start with null", http.MethodPost, startPath, `null`, nil, http.StatusBadRequest, "request_invalid"},
		{"start with two values", http.MethodPost, startPath, `{} {}`, nil, http.StatusBadRequest, "request_invalid"},
		{"unknown slug", http.MethodPost, "/api/v1/public/resumes/nobody-here/views/start", "{}", nil, http.StatusNotFound, "public_not_found"},
		{"invalid slug", http.MethodPost, "/api/v1/public/resumes/A_B/views/start", "{}", nil, http.StatusNotFound, "public_not_found"},
		{"collect missing solution", http.MethodPost, collectPath, `{"token":"AAAAAAAAAAAAAAAAAAAA","challenge":{"parameters":{},"signature":"00"}}`, nil, http.StatusBadRequest, "request_invalid"},
		{"collect extra field", http.MethodPost, collectPath, `{"token":"AAAAAAAAAAAAAAAAAAAA","challenge":{"parameters":{},"signature":"00"},"solution":{"counter":1,"derivedKey":"00"},"referrer":"x"}`, nil, http.StatusBadRequest, "request_invalid"},
		{"collect bad key", http.MethodPost, collectPath, `{"token":"AAAAAAAAAAAAAAAAAAAA","challenge":{"parameters":{},"signature":"00"},"solution":{"counter":1,"derivedKey":"XYZ"}}`, nil, http.StatusBadRequest, "request_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := f.request(test.method, test.path, test.body, test.edit)
			if w.Code != test.wantStatus || errorCode(t, w) != test.wantCode {
				t.Fatalf("status %d body %s; want %d %s", w.Code, w.Body.String(), test.wantStatus, test.wantCode)
			}
		})
	}
}

type startData struct {
	Data struct {
		Owner     bool                `json:"owner"`
		Token     string              `json:"token"`
		Challenge viewcount.Challenge `json:"challenge"`
	} `json:"data"`
}

func (f *routeFixture) startAndSolve(t *testing.T, edit func(*http.Request)) string {
	t.Helper()
	w := f.request(http.MethodPost, startPath, "{}", edit)
	if w.Code != http.StatusOK {
		t.Fatalf("start status %d: %s", w.Code, w.Body.String())
	}
	var started startData
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil || started.Data.Owner || started.Data.Token == "" {
		t.Fatalf("start body %s: %v", w.Body.String(), err)
	}
	solution, err := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: started.Data.Challenge, DeriveKey: altcha.DeriveKeyPBKDF2()})
	if err != nil || solution == nil {
		t.Fatalf("solve: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"token": started.Data.Token, "challenge": started.Data.Challenge,
		"solution": map[string]any{"counter": solution.Counter, "derivedKey": solution.DerivedKey},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(body)
}

func (f *routeFixture) totals(t *testing.T) viewcount.DayCell {
	t.Helper()
	if err := f.counter.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	var total viewcount.DayCell
	for _, cell := range f.store.days {
		total.Counted += cell.Counted
		total.Bot += cell.Bot
		total.Datacenter += cell.Datacenter
		total.Invalid += cell.Invalid
	}
	return total
}

func TestStartAndCollectCountAView(t *testing.T) {
	f := newRouteFixture(t)
	body := f.startAndSolve(t, nil)
	f.now = f.now.Add(9 * time.Second)
	w := f.request(http.MethodPost, collectPath, body, nil)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("collect = %d %q, want 204 and no body", w.Code, w.Body.String())
	}
	// A replay still answers 204, so a client cannot tell what it hit.
	if w = f.request(http.MethodPost, collectPath, body, nil); w.Code != http.StatusNoContent {
		t.Fatalf("replay = %d, want 204", w.Code)
	}
	if total := f.totals(t); total.Counted != 1 || total.Invalid != 1 {
		t.Fatalf("totals = %+v, want one counted and one invalid", total)
	}
}

func TestEdgeLabelsAreReadOnlyFromTheTrustedProxy(t *testing.T) {
	f := newRouteFixture(t)
	labeled := func(r *http.Request) { r.Header.Set(botLabelHeader, "1") }
	body := f.startAndSolve(t, nil)
	f.now = f.now.Add(9 * time.Second)
	f.request(http.MethodPost, collectPath, body, labeled)

	// The same header straight from a client socket is ignored.
	direct := func(r *http.Request) {
		r.RemoteAddr = "198.51.100.7:5000"
		r.Header.Del("X-Real-IP")
		r.Header.Set(datacenterLabelHeader, "1")
	}
	body = f.startAndSolve(t, direct)
	f.now = f.now.Add(9 * time.Second)
	f.request(http.MethodPost, collectPath, body, direct)

	if total := f.totals(t); total.Bot != 1 || total.Datacenter != 0 || total.Counted != 1 {
		t.Fatalf("totals = %+v, want one bot from the proxy and one counted direct view", total)
	}
}

func TestLabelSetNeedsExactlyOneValueOfOne(t *testing.T) {
	for _, test := range []struct {
		values []string
		want   bool
	}{
		{nil, false}, {[]string{"1"}, true}, {[]string{"0"}, false}, {[]string{"1", "1"}, false}, {[]string{" 1"}, false},
	} {
		header := http.Header{}
		for _, value := range test.values {
			header.Add(botLabelHeader, value)
		}
		if got := labelSet(header, botLabelHeader); got != test.want {
			t.Errorf("labelSet(%q) = %v, want %v", test.values, got, test.want)
		}
	}
}

func TestOwnerRoutesNeedASession(t *testing.T) {
	f := newRouteFixture(t)
	for _, path := range []string{"/api/v1/views", "/api/v1/views/018f5b6a-9a3e-7c21-8b1e-0000000000b1"} {
		w := f.request(http.MethodGet, path, "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s without a session = %d, want 401", path, w.Code)
		}
		if w = f.request(http.MethodPost, path, "{}", nil); w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s = %d, want 405", path, w.Code)
		}
	}
}

func TestDenseDaysAndMonths(t *testing.T) {
	first := time.Date(2026, time.June, 29, 0, 0, 0, 0, time.UTC)
	rows := []store.ListResumeViewDaysRow{
		{Day: pgtype.Date{Time: time.Date(2025, time.October, 5, 0, 0, 0, 0, time.UTC), Valid: true}, Counted: 4, Bot: 1},
		{Day: pgtype.Date{Time: first, Valid: true}, Counted: 2, Crawler: 3},
		{Day: pgtype.Date{Time: time.Date(2026, time.September, 26, 0, 0, 0, 0, time.UTC), Valid: true}, Counted: 1, Invalid: 1},
	}
	days := denseDays(rows, first)
	if len(days) != detailDays || days[0].Date != "2026-06-29" || days[0].Real != 2 || days[89].Date != "2026-09-26" || days[89].Invalid != 1 {
		t.Fatalf("days = first %+v last %+v (len %d)", days[0], days[len(days)-1], len(days))
	}
	monthly := months(rows, time.Date(2025, time.October, 1, 0, 0, 0, 0, time.UTC))
	if len(monthly) != detailMonth || monthly[0].Month != "2025-10" || monthly[0].Real != 4 || monthly[0].Filtered != 1 ||
		monthly[11].Month != "2026-09" || monthly[11].Real != 1 || monthly[8].Filtered != 3 {
		t.Fatalf("months = %+v", monthly)
	}
}
