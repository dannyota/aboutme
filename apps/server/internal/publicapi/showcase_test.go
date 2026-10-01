package publicapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/showcase"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

type fakeShowcaseListing struct {
	mu      sync.Mutex
	page    showcase.Page
	err     error
	filters []showcase.Filter
}

func (f *fakeShowcaseListing) List(_ context.Context, filter showcase.Filter) (showcase.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.filters = append(f.filters, filter)
	return f.page, f.err
}

func (f *fakeShowcaseListing) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.filters)
}

func showcaseRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	request.RemoteAddr = "192.0.2.50:1234"
	return request
}

func serveShowcase(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requireShowcaseHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	header := response.Header()
	if got := header.Get("Cache-Control"); got != "no-store, no-transform" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := header.Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Errorf("X-Robots-Tag = %q, want noindex, nofollow", got)
	}
	for _, name := range []string{"Set-Cookie", "ETag", "Last-Modified"} {
		if got := header.Values(name); len(got) != 0 {
			t.Errorf("%s = %v, want none", name, got)
		}
	}
}

func newTestShowcaseHandler(listing ShowcaseListing, clock func() time.Time) http.Handler {
	return newShowcaseHandler(listing, nil, clock)
}

// AC-SHOW-014: the listing is never stored, never indexed, and sets no cookie.
func TestShowcaseResponseHeadersAndBody(t *testing.T) {
	t.Parallel()
	role, template := "backend", "ats-plain"
	listing := &fakeShowcaseListing{page: showcase.Page{
		Items: []showcase.Item{{
			Slug: "ada-lovelace", CardVersion: "0123456789abcdef", ImageText: "Ada Lovelace · Engineer",
			Language: "en", TemplateID: &template, Role: &role,
		}},
		Page: 1, PageCount: 1, Total: 1,
	}}
	handler := newTestShowcaseHandler(listing, nil)

	get := showcaseRequest(t, http.MethodGet, showcasePath)
	get.Header.Set("Cookie", "__Host-session=abc; other=1")
	response := serveShowcase(handler, get)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d %s", response.Code, response.Body)
	}
	requireShowcaseHeaders(t, response)
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	want := `{"items":[{"slug":"ada-lovelace","cardVersion":"0123456789abcdef","imageText":"Ada Lovelace · Engineer","language":"en","templateId":"ats-plain","role":"backend"}],"page":1,"pageCount":1,"total":1}` + "\n"
	if response.Body.String() != want {
		t.Fatalf("body = %s, want %s", response.Body, want)
	}

	head := serveShowcase(handler, showcaseRequest(t, http.MethodHead, showcasePath))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD = %d with %d body bytes, want 200 and none", head.Code, head.Body.Len())
	}
	requireShowcaseHeaders(t, head)
	if head.Header().Get("Content-Length") != response.Header().Get("Content-Length") || head.Header().Get("Content-Length") == "" {
		t.Errorf("HEAD Content-Length = %q, want the GET length %q", head.Header().Get("Content-Length"), response.Header().Get("Content-Length"))
	}
}

func TestShowcaseEmptyListingEncodesEmptyItems(t *testing.T) {
	t.Parallel()
	handler := newTestShowcaseHandler(&fakeShowcaseListing{page: showcase.Page{Items: []showcase.Item{}, Page: 1}}, nil)
	response := serveShowcase(handler, showcaseRequest(t, http.MethodGet, showcasePath))
	if response.Body.String() != `{"items":[],"page":1,"pageCount":0,"total":0}`+"\n" {
		t.Fatalf("body = %s", response.Body)
	}
}

// AC-SHOW-014: every parameter is optional, each at most once; anything else
// is request_invalid and never reaches the listing.
func TestShowcaseQueryParsing(t *testing.T) {
	t.Parallel()
	valid := map[string]showcase.Filter{
		"":                       {Page: 1},
		"role=backend":           {Role: "backend", Page: 1},
		"role=other":             {Role: "other", Page: 1},
		"lang=vi":                {Lang: "vi", Page: 1},
		"lang=en":                {Lang: "en", Page: 1},
		"template=custom":        {Template: "custom", Page: 1},
		"template=ats-plain":     {Template: "ats-plain", Page: 1},
		"page=1":                 {Page: 1},
		"page=100":               {Page: 100},
		"page=7":                 {Page: 7},
		"role=qa&lang=en&page=3": {Role: "qa", Lang: "en", Page: 3},
		"page=2&template=custom&lang=vi&role=devops": {Role: "devops", Lang: "vi", Template: "custom", Page: 2},
	}
	for query, want := range valid {
		listing := &fakeShowcaseListing{}
		target := showcasePath
		if query != "" {
			target += "?" + query
		}
		response := serveShowcase(newTestShowcaseHandler(listing, nil), showcaseRequest(t, http.MethodGet, target))
		if response.Code != http.StatusOK {
			t.Errorf("%q: status = %d %s", query, response.Code, response.Body)
			continue
		}
		if len(listing.filters) != 1 || listing.filters[0] != want {
			t.Errorf("%q: filter = %+v, want %+v", query, listing.filters, want)
		}
	}

	invalid := []string{
		"foo=1", "role=backend&foo=1", "Role=backend", "role=", "lang=", "template=", "page=",
		"role=backend&role=qa", "page=1&page=2", "lang=vi&lang=vi",
		"role=Backend", "role=designer", "role=backend%20", "lang=other", "lang=VI", "lang=fr",
		"template=unknown", "template=Custom", "template=ats-plain-2",
		"page=0", "page=101", "page=1000", "page=-1", "page=+1", "page=01", "page=1.0", "page=abc", "page=1%20",
		"page=99999999999999999999", "role=backend;lang=vi", "role=%zz", "%zz=1", "role", "&&", "=1", "role=qa&", "&role=qa", "role=qa&&lang=en",
	}
	for _, query := range invalid {
		listing := &fakeShowcaseListing{}
		response := serveShowcase(newTestShowcaseHandler(listing, nil), showcaseRequest(t, http.MethodGet, showcasePath+"?"+query))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"request_invalid"`) {
			t.Errorf("%q: response = %d %s, want 400 request_invalid", query, response.Code, response.Body)
		}
		requireShowcaseHeaders(t, response)
		if listing.calls() != 0 {
			t.Errorf("%q: the listing ran for an invalid request", query)
		}
	}

	forced := serveShowcase(newTestShowcaseHandler(&fakeShowcaseListing{}, nil), showcaseRequest(t, http.MethodGet, showcasePath+"?"))
	if forced.Code != http.StatusBadRequest {
		t.Errorf("a bare question mark = %d, want 400", forced.Code)
	}
}

func TestShowcaseRejectsOtherMethodsBodiesAndEncodings(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		response := serveShowcase(newTestShowcaseHandler(&fakeShowcaseListing{}, nil), showcaseRequest(t, method, showcasePath))
		if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s = %d Allow %q, want 405 with GET, HEAD", method, response.Code, response.Header().Get("Allow"))
		}
		requireShowcaseHeaders(t, response)
	}
	withBody := httptest.NewRequestWithContext(t.Context(), http.MethodGet, showcasePath, strings.NewReader("x"))
	withBody.RemoteAddr = "192.0.2.50:1234"
	if response := serveShowcase(newTestShowcaseHandler(&fakeShowcaseListing{}, nil), withBody); response.Code != http.StatusBadRequest {
		t.Errorf("GET with a body = %d, want 400", response.Code)
	}
	encoded := showcaseRequest(t, http.MethodGet, showcasePath)
	encoded.Header.Set("Content-Encoding", "gzip")
	if response := serveShowcase(newTestShowcaseHandler(&fakeShowcaseListing{}, nil), encoded); response.Code != http.StatusBadRequest {
		t.Errorf("GET with a content encoding = %d, want 400", response.Code)
	}
}

// AC-SHOW-014: the route has its own limit of 60 a minute per IP.
func TestShowcaseRateLimit(t *testing.T) {
	t.Parallel()
	clock := testutil.NewClockAtEpoch()
	listing := &fakeShowcaseListing{}
	handler := newTestShowcaseHandler(listing, clock.Now)
	for i := range showcaseRequestsPerMinute {
		if response := serveShowcase(handler, showcaseRequest(t, http.MethodGet, showcasePath)); response.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i+1, response.Code)
		}
	}
	limited := serveShowcase(handler, showcaseRequest(t, http.MethodGet, showcasePath))
	if limited.Code != http.StatusTooManyRequests || !strings.Contains(limited.Body.String(), `"code":"rate_limited"`) {
		t.Fatalf("request 61 = %d %s, want 429 rate_limited", limited.Code, limited.Body)
	}
	if limited.Header().Get("Retry-After") == "" {
		t.Error("429 has no Retry-After")
	}
	requireShowcaseHeaders(t, limited)
	if listing.calls() != showcaseRequestsPerMinute {
		t.Errorf("the listing ran %d times, want %d", listing.calls(), showcaseRequestsPerMinute)
	}

	other := showcaseRequest(t, http.MethodGet, showcasePath)
	other.RemoteAddr = "192.0.2.99:1234"
	if response := serveShowcase(handler, other); response.Code != http.StatusOK {
		t.Errorf("another IP = %d, want 200", response.Code)
	}
	clock.Advance(time.Minute)
	if response := serveShowcase(handler, showcaseRequest(t, http.MethodGet, showcasePath)); response.Code != http.StatusOK {
		t.Errorf("after a minute = %d, want 200", response.Code)
	}
}

func TestShowcaseListingFailureIsUnavailable(t *testing.T) {
	t.Parallel()
	listing := &fakeShowcaseListing{err: context.DeadlineExceeded}
	response := serveShowcase(newTestShowcaseHandler(listing, nil), showcaseRequest(t, http.MethodGet, showcasePath))
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("response = %d Retry-After %q, want 503 and 1", response.Code, response.Header().Get("Retry-After"))
	}
	requireShowcaseHeaders(t, response)
	if strings.Contains(response.Body.String(), "deadline") {
		t.Errorf("body %s leaks the failure", response.Body)
	}
}

func TestShowcaseRouteDispatch(t *testing.T) {
	t.Parallel()
	if got := classifyPublicRoute(showcasePath); got != publicRouteShowcase {
		t.Fatalf("classifyPublicRoute(%q) = %d, want the showcase route", showcasePath, got)
	}
	routes := &Service{}
	for path, want := range map[string]bool{
		showcasePath: true, showcasePath + "/": false, showcasePath + ".json": false, "/api/v1/public/showcase/x": false,
	} {
		if routes.Recognizes(path) != want {
			t.Errorf("Recognizes(%q) = %t, want %t", path, !want, want)
		}
	}

	missing := serveShowcase(routes, showcaseRequest(t, http.MethodGet, showcasePath))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("a service without a showcase handler = %d, want 404", missing.Code)
	}
	requireShowcaseHeaders(t, missing)

	routes = &Service{showcase: newTestShowcaseHandler(&fakeShowcaseListing{page: showcase.Page{Items: []showcase.Item{}, Page: 1}}, nil)}
	if served := serveShowcase(routes, showcaseRequest(t, http.MethodGet, showcasePath)); served.Code != http.StatusOK {
		t.Fatalf("a service with a showcase handler = %d, want 200", served.Code)
	}
}

// contactSentinels holds a value of every contact type, visible and hidden, so
// a leak of any one of them shows in a response.
var contactSentinels = map[schema.Type]string{
	schema.Email:      "sentinel-mail@example.test",
	schema.Phone:      "+84 900 000 111",
	schema.Website:    "https://sentinel-web.example.test/path",
	schema.Github:     "https://github.com/sentinel-gh",
	schema.Linkedin:   "https://www.linkedin.com/in/sentinel-li",
	schema.Twitter:    "https://x.com/sentinel-tw",
	schema.Location:   "Sentinel City Street 12",
	schema.TypeCustom: "sentinel-custom-value",
}

// AC-SHOW-002: no contact value of any type, and nothing from the resume body,
// reaches the listing response.
func TestShowcaseResponseNeverCarriesContactValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	dsn, _ := testutil.NewMigratedTestDatabase(t)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)
	queries := store.New(pool)
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	origin := mustPublicOrigin(t)
	projector := docmigrate.NewIdentityProjector()
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: queries, Projector: projector, Coordinator: coordinator, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := showcase.New(showcase.Dependencies{DB: pool, Reader: reader, Projector: projector})
	if err != nil {
		t.Fatal(err)
	}

	name, headline := "Ada Lovelace", "Analytical engineer"
	contacts := make([]schema.PersonalDetail, 0, len(contactSentinels)+1)
	for contactType, value := range contactSentinels {
		contacts = append(contacts, schema.PersonalDetail{ID: uuid.NewString(), Type: contactType, Value: value})
	}
	contacts = append(contacts, schema.PersonalDetail{ID: uuid.NewString(), Type: schema.Email, Value: "sentinel-hidden@example.test", IsHidden: true})
	bodySentinel := "sentinel-employer-body"
	content := map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: uuid.NewString(), JobTitle: &bodySentinel, Employer: &bodySentinel}}),
	}
	personal := mustJSONBytes(t, schema.PersonalDetails{FullName: &name, Headline: &headline, Details: contacts})
	customization := mustJSONBytes(t, schema.Customization{
		Colors: schema.Colors{Primary: "#1d4ed8", Text: "#111111", Background: "#ffffff"},
		Layout: schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{"work"}, Sidebar: []string{}}},
	})
	var userID, resumeID uuid.UUID
	if err = pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, 'Sentinel') RETURNING id`, uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	slug := "sc-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	if err = pool.QueryRow(ctx, `
		INSERT INTO resumes (user_id, title, slug, live, schema_version, lng, personal_details, content, customization)
		VALUES ($1, 'Sentinel', $2, true, $3, 'en', $4, $5, $6) RETURNING id`,
		userID, slug, docmigrate.CurrentVersion, personal, mustJSONBytes(t, content), customization).Scan(&resumeID); err != nil {
		t.Fatalf("insert resume: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.PublishTx(ctx, store.New(tx), showcase.PublishChange{ResumeID: resumeID, Enabled: true}); err != nil {
		t.Fatalf("PublishTx() error: %v", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	handler := newTestShowcaseHandler(service, nil)
	response := serveShowcase(handler, showcaseRequest(t, http.MethodGet, showcasePath))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d %s", response.Code, response.Body)
	}
	requireShowcaseHeaders(t, response)
	body := response.Body.String()
	if !strings.Contains(body, slug) || !strings.Contains(body, "Ada Lovelace · Analytical engineer") {
		t.Fatalf("body = %s, want the listed resume and its image text", body)
	}
	lower := strings.ToLower(body)
	for _, value := range contactSentinels {
		if strings.Contains(lower, strings.ToLower(value)) {
			t.Errorf("body holds the contact value %q: %s", value, body)
		}
	}
	for _, leak := range []string{"sentinel", "hidden", bodySentinel, userID.String(), resumeID.String(), "example.test"} {
		if strings.Contains(lower, strings.ToLower(leak)) {
			t.Errorf("body holds %q: %s", leak, body)
		}
	}
	var decoded struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &decoded); err != nil || len(decoded.Items) != 1 || decoded.Total != 1 {
		t.Fatalf("decode body: %v %s", err, body)
	}
	if len(decoded.Items[0]) != 6 {
		t.Fatalf("item fields = %v, want exactly six", decoded.Items[0])
	}
}

func mustJSONBytes(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}
	return encoded
}
