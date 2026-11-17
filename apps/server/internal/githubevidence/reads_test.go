package githubevidence

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

const (
	testGitHubUserID = int64(583231)
	repositoryJSON   = `{"id":101,"full_name":"octocat/hello","owner":{"id":583231,"login":"octocat"},"private":false,"fork":false,"archived":true,"created_at":"2020-01-02T03:04:05Z","stargazers_count":5}`
	pullRequestJSON  = `{"id":9001,"number":42,"title":"Fix the thing","merged":true,"merged_at":"2026-01-02T03:04:05Z","user":{"id":583231,"login":"octocat"},"html_url":"https://github.com/octocat/hello/pull/42","body":"x"}`
	releaseJSON      = `{"id":7001,"tag_name":"v1.0.0","name":"First","draft":false,"prerelease":true,"published_at":"2026-02-03T04:05:06Z","author":{"id":583231,"login":"octocat","type":"User"},"assets":[]}`
)

type seenRequest struct {
	method string
	uri    string
	header http.Header
}

type fakeAPI struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	requests []seenRequest
	routes   map[string]func(w http.ResponseWriter, r *http.Request)
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{t: t, routes: map[string]func(http.ResponseWriter, *http.Request){}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, seenRequest{method: r.Method, uri: r.RequestURI, header: r.Header.Clone()})
		handle := f.routes[r.URL.EscapedPath()]
		f.mu.Unlock()
		if handle == nil {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		handle(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeAPI) on(path string, handle func(w http.ResponseWriter, r *http.Request)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[path] = handle
}

func (f *fakeAPI) json(path string, status int, body string) {
	f.on(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		if _, err := io.WriteString(w, body); err != nil {
			f.t.Error(err)
		}
	})
}

func (f *fakeAPI) seen() []seenRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seenRequest(nil), f.requests...)
}

type readerFixture struct {
	reader  *githubReader
	reg     *registry
	revoked *revocations
	account uuid.UUID
}

func newReaderFixture(t *testing.T, endpoint string, transport http.RoundTripper, now func() time.Time) readerFixture {
	t.Helper()
	revoked := &revocations{}
	reg := newRegistry(time.Hour, revoked.revoke)
	c := newTestClient(endpoint, transport, now)
	c.hold = reg.hold
	account := uuid.New()
	session, ok := reg.install(account, reg.hold(sentinelToken), 1, 1, now())
	if !ok {
		t.Fatal("install a live session")
	}
	t.Cleanup(func() {
		reg.close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := reg.wait(ctx); err != nil {
			t.Error(err)
		}
	})
	return readerFixture{reader: &githubReader{client: c, registry: reg, account: account, session: session}, reg: reg, revoked: revoked, account: account}
}

func fixedClock() func() time.Time {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return now }
}

func TestReads_EveryReadCarriesTheFixedHeaders(t *testing.T) {
	api := newFakeAPI(t)
	api.json("/repos/octocat/hello", http.StatusOK, repositoryJSON)
	api.json("/repos/octocat/hello/pulls/42", http.StatusOK, pullRequestJSON)
	api.json("/repos/octocat/hello/releases/7001", http.StatusOK, releaseJSON)
	api.json("/repos/octocat/hello/releases/tags/release%2F1.0", http.StatusOK, releaseJSON)
	api.json("/repositories/101", http.StatusOK, repositoryJSON)
	api.json("/user/repos", http.StatusOK, `[]`)
	api.json("/search/issues", http.StatusOK, `{"incomplete_results":false,"items":[]}`)
	api.json("/repos/octocat/hello/releases", http.StatusOK, `[]`)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	ctx := context.Background()
	steps := []func() error{
		func() error { _, err := fx.reader.repositoryByName(ctx, "octocat", "hello", 0); return err },
		func() error { _, err := fx.reader.pullRequest(ctx, "octocat", "hello", 42); return err },
		func() error { _, err := fx.reader.releaseByID(ctx, "octocat", "hello", 7001); return err },
		func() error { _, err := fx.reader.releaseByTag(ctx, "octocat", "hello", "release/1.0"); return err },
		func() error { _, err := fx.reader.repositoryByID(ctx, 101); return err },
		func() error { _, err := fx.reader.listRepositories(ctx, 2); return err },
		func() error { _, err := fx.reader.listPullRequests(ctx, "octocat", 3); return err },
		func() error {
			_, err := fx.reader.listReleases(ctx, candidateRepository{id: 101, fullName: "octocat/hello"}, testGitHubUserID, 1)
			return err
		},
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("read %d error = %v", i, err)
		}
	}
	wantURIs := []string{
		"/repos/octocat/hello",
		"/repos/octocat/hello/pulls/42",
		"/repos/octocat/hello/releases/7001",
		"/repos/octocat/hello/releases/tags/release%2F1.0",
		"/repositories/101",
		"/user/repos?affiliation=owner&page=2&per_page=30&sort=pushed&visibility=public",
		"/search/issues?order=desc&page=3&per_page=30&q=is%3Apr+is%3Amerged+is%3Apublic+author%3Aoctocat&sort=updated",
		"/repos/octocat/hello/releases?page=1&per_page=30",
	}
	seen := api.seen()
	if len(seen) != len(wantURIs) {
		t.Fatalf("requests = %d, want %d", len(seen), len(wantURIs))
	}
	for i, req := range seen {
		if req.method != http.MethodGet || req.uri != wantURIs[i] {
			t.Errorf("request %d = %s %s, want GET %s", i, req.method, req.uri, wantURIs[i])
		}
		for name, want := range map[string]string{
			"Authorization": "Bearer " + sentinelToken, "Accept": "application/vnd.github+json",
			"X-Github-Api-Version": "2022-11-28", "User-Agent": "aboutme.vn",
		} {
			if got := req.header.Values(name); len(got) != 1 || got[0] != want {
				t.Errorf("request %d header %s = %q, want %q", i, name, got, want)
			}
		}
	}
}

func TestReads_TransportAdmitsOnlyTheReadShapes(t *testing.T) {
	base := &countingTransport{respond: func(*http.Request) (*http.Response, error) { return jsonResponse(200, `{}`), nil }}
	c := newTestClient("", base, time.Now)
	admitted := []string{
		"/repos/octocat/hello",
		"/repos/Octo-Cat/hello.world_2",
		"/repos/octocat/hello/pulls/42",
		"/repos/octocat/hello/releases/7001",
		"/repos/octocat/hello/releases/tags/v1.0.0",
		"/repos/octocat/hello/releases/tags/release%2F1.0",
		"/repositories/101",
		"/user/repos?affiliation=owner&page=1&per_page=30&sort=pushed&visibility=public",
		"/user/repos?affiliation=owner&page=10&per_page=30&sort=pushed&visibility=public",
		"/search/issues?order=desc&page=1&per_page=30&q=is%3Apr+is%3Amerged+is%3Apublic+author%3Aoctocat&sort=updated",
		"/repos/octocat/hello/releases?page=4&per_page=30",
	}
	for _, path := range admitted {
		if _, _, _, err := c.send(context.Background(), http.MethodGet, productionAPIBaseURL+path, nil, func(http.Header) {}); err != nil {
			t.Errorf("GET %s error = %v, want admitted", path, err)
		}
	}
	if base.count() != len(admitted) {
		t.Fatalf("admitted reads sent %d requests, want %d", base.count(), len(admitted))
	}
	refused := []string{
		"/repos/octocat",
		"/repos/octocat/hello/",
		"/repos//hello",
		"/repos/-octocat/hello",
		"/repos/octo_cat/hello",
		"/repos/" + strings.Repeat("a", 40) + "/hello",
		"/repos/octocat/" + strings.Repeat("a", 101),
		"/repos/octocat/..",
		"/repos/octocat/.",
		"/repos/octocat/hel%6Co",
		"/repos/octocat/hello/pulls/042",
		"/repos/octocat/hello/pulls/0",
		"/repos/octocat/hello/pulls/x",
		"/repos/octocat/hello/pulls/99999999999999999999",
		"/repos/octocat/hello/pulls/42/files",
		"/repos/octocat/hello/issues/42",
		"/repos/octocat/hello/releases/latest",
		"/repos/octocat/hello/releases/tags/release/1.0",
		"/repos/octocat/hello/releases/tags/..",
		"/repos/octocat/hello/releases/tags/%2E%2E",
		"/repos/octocat/hello/releases/tags/v1%201",
		"/repos/octocat/hello/releases/tags/",
		"/repos/octocat/hello/contributors",
		"/repositories/0101",
		"/repositories/101/contents",
		"/repos/octocat/hello?per_page=30",
		"/repos/octocat/hello/pulls/42?page=1",
		"/repositories/101?x=1",
		"/repos/octocat/hello#x",
		"/user/repos",
		"/user/repos?affiliation=owner&page=1&per_page=30&sort=pushed",
		"/user/repos?affiliation=owner&page=1&per_page=30&sort=pushed&visibility=public&type=all",
		"/user/repos?affiliation=owner,collaborator&page=1&per_page=30&sort=pushed&visibility=public",
		"/user/repos?affiliation=owner&page=1&per_page=30&sort=pushed&visibility=all",
		"/user/repos?affiliation=owner&page=11&per_page=30&sort=pushed&visibility=public",
		"/user/repos?affiliation=owner&page=0&per_page=30&sort=pushed&visibility=public",
		"/user/repos?affiliation=owner&page=01&per_page=30&sort=pushed&visibility=public",
		"/user/repos?affiliation=owner&page=1&per_page=100&sort=pushed&visibility=public",
		"/user/repos?affiliation=owner&page=1&page=2&per_page=30&sort=pushed&visibility=public",
		"/user/repos?visibility=public&sort=pushed&per_page=30&page=1&affiliation=owner",
		"/search/issues?order=desc&page=1&per_page=30&q=is%3Apr+is%3Amerged+author%3Aoctocat&sort=updated",
		"/search/issues?order=desc&page=1&per_page=30&q=is%3Apr+is%3Amerged+is%3Apublic+author%3Aocto+cat&sort=updated",
		"/search/issues?order=desc&page=1&per_page=30&q=is%3Apr+is%3Amerged+is%3Apublic+author%3Aoctocat+repo%3Ax%2Fy&sort=updated",
		"/search/issues?order=asc&page=1&per_page=30&q=is%3Apr+is%3Amerged+is%3Apublic+author%3Aoctocat&sort=updated",
		"/search/issues?order=desc&page=1&per_page=30&q=is%3Apr+is%3Amerged+is%3Apublic+author%3Aoctocat",
		"/repos/octocat/hello/releases?page=1",
		"/repos/octocat/hello/releases?page=1&per_page=30&x=",
		"/repos/octocat/hello/releases?",
	}
	for _, path := range refused {
		_, _, _, err := c.send(context.Background(), http.MethodGet, productionAPIBaseURL+path, nil, func(http.Header) {})
		if !errors.Is(err, errDestinationRefused) {
			t.Errorf("GET %s error = %v, want refused", path, err)
		}
	}
	for _, target := range []string{
		"https://someone@api.github.com/repos/octocat/hello",
		"https://api.github.com.evil.example/repos/octocat/hello",
		"http://api.github.com/repos/octocat/hello",
		"https://github.com/repos/octocat/hello",
		"https://api.github.com:443/repos/octocat/hello",
	} {
		if _, _, _, err := c.send(context.Background(), http.MethodGet, target, nil, func(http.Header) {}); !errors.Is(err, errDestinationRefused) {
			t.Errorf("GET %s error = %v, want refused", target, err)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodHead} {
		if _, _, _, err := c.send(context.Background(), method, productionAPIBaseURL+"/repos/octocat/hello", nil, func(http.Header) {}); !errors.Is(err, errDestinationRefused) {
			t.Errorf("%s on a read shape error = %v, want refused", method, err)
		}
	}
	if base.count() != len(admitted) {
		t.Fatalf("refused reads reached the network: %d requests, want %d", base.count(), len(admitted))
	}
}

func TestReads_401EndsTheSessionAndRevokesOnce(t *testing.T) {
	api := newFakeAPI(t)
	api.json("/repos/octocat/hello", http.StatusUnauthorized, `{"message":"`+sentinelBody+`"}`)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	_, err := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 0)
	if !errors.Is(err, errSessionRejected) {
		t.Fatalf("error = %v, want the session rejected", err)
	}
	requireClean(t, err)
	if _, live := fx.reg.current(fx.account); live {
		t.Fatal("a 401 left the session live")
	}
	_, after := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 0)
	if !errors.Is(after, errSessionRejected) {
		t.Fatalf("read after the session ended error = %v, want the session rejected", after)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = fx.reg.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if fx.revoked.count(sentinelToken) != 1 || fx.revoked.total() != 1 {
		t.Fatalf("revocations = %d, want exactly one of the session token", fx.revoked.total())
	}
	if len(api.seen()) != 1 {
		t.Fatalf("requests = %d, want 1 (no read after the session ended)", len(api.seen()))
	}
	if failure := evidenceFailure(after); failure.status != http.StatusConflict || failure.code != sessionRequiredCode {
		t.Fatalf("failure = %+v, want 409 %s", failure, sessionRequiredCode)
	}
}

func TestReads_401EndsOnlyTheSessionThatUsedTheToken(t *testing.T) {
	api := newFakeAPI(t)
	api.json("/repos/octocat/hello", http.StatusUnauthorized, `{}`)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	replacement, ok := fx.reg.install(fx.account, fx.reg.hold("gho_REPLACEMENT"), 1, 1, fixedClock()())
	if !ok {
		t.Fatal("install a replacement")
	}
	if _, err := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 0); !errors.Is(err, errSessionRejected) {
		t.Fatalf("error = %v, want the session rejected", err)
	}
	if live, ok := fx.reg.current(fx.account); !ok || live != replacement {
		t.Fatal("a 401 on the replaced session ended its replacement")
	}
}

func TestReads_RateLimitVariants(t *testing.T) {
	now := fixedClock()
	for _, tc := range []struct {
		name   string
		status int
		header map[string]string
		body   string
		retry  time.Duration
	}{
		{"429 with retry-after", http.StatusTooManyRequests, map[string]string{"Retry-After": "17"}, `{}`, 17 * time.Second},
		{"429 alone", http.StatusTooManyRequests, nil, ``, 60 * time.Second},
		{"429 with a spent primary limit", http.StatusTooManyRequests, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(now().Add(95*time.Second).Unix(), 10)}, `{}`, 95 * time.Second},
		{"403 with retry-after", http.StatusForbidden, map[string]string{"Retry-After": "30"}, `{"message":"You have exceeded a secondary rate limit"}`, 30 * time.Second},
		{"403 with remaining 0", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(now().Add(10*time.Minute).Unix(), 10)}, `{"message":"x"}`, 10 * time.Minute},
		{"403 with remaining 0 and no reset", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0"}, `{}`, 60 * time.Second},
		{"403 with a rate-limit message", http.StatusForbidden, nil, `{"message":"API rate limit exceeded for user ID 1.","documentation_url":"https://docs.github.com"}`, 60 * time.Second},
		{"403 with retry-after beating the reset", http.StatusForbidden, map[string]string{"Retry-After": "5", "X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(now().Add(time.Hour).Unix(), 10)}, `{}`, 5 * time.Second},
		{"403 with a reset already past", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(now().Add(-time.Minute).Unix(), 10)}, `{}`, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newFakeAPI(t)
			api.on("/repos/octocat/hello", func(w http.ResponseWriter, _ *http.Request) {
				for name, value := range tc.header {
					w.Header().Set(name, value)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if _, err := io.WriteString(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			fx := newReaderFixture(t, api.server.URL, newTransport(), now)
			_, err := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 0)
			retry, limited := retryAfter(err)
			if !limited || retry != tc.retry {
				t.Fatalf("error = %v, retry = %v; want rate limited with %v", err, retry, tc.retry)
			}
			failure := evidenceFailure(err)
			if failure.status != http.StatusTooManyRequests || failure.code != "rate_limited" || failure.retry != tc.retry {
				t.Fatalf("failure = %+v, want 429 rate_limited", failure)
			}
			rec := httptest.NewRecorder()
			writeEvidenceFailure(rec, err)
			if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != strconv.Itoa(int(tc.retry/time.Second)) {
				t.Fatalf("response = %d Retry-After=%q", rec.Code, rec.Header().Get("Retry-After"))
			}
			if _, live := fx.reg.current(fx.account); !live {
				t.Fatal("a rate limit ended the session")
			}
		})
	}
}

func TestReads_PlainForbiddenIsUpstream(t *testing.T) {
	api := newFakeAPI(t)
	api.json("/repos/octocat/hello", http.StatusForbidden, `{"message":"Resource not accessible"}`)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	_, err := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 0)
	if !errors.Is(err, errUpstream) {
		t.Fatalf("error = %v, want upstream", err)
	}
	if failure := evidenceFailure(err); failure.status != http.StatusBadGateway || failure.code != "evidence_upstream" {
		t.Fatalf("failure = %+v, want 502 evidence_upstream", failure)
	}
}

func TestReads_SpentBudgetAnswersRateLimitedWithoutARequest(t *testing.T) {
	start := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	now := start
	clock := func() time.Time { return now }
	api := newFakeAPI(t)
	api.json("/repos/octocat/hello", http.StatusOK, repositoryJSON)
	fx := newReaderFixture(t, api.server.URL, newTransport(), clock)
	for i := range requestLimit {
		if _, err := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 0); err != nil {
			t.Fatalf("read %d error = %v", i+1, err)
		}
		now = now.Add(10 * time.Second)
	}
	sent := len(api.seen())
	_, err := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 0)
	retry, limited := retryAfter(err)
	if want := start.Add(time.Hour).Sub(now); !limited || retry != want {
		t.Fatalf("read 121 = %v retry %v, want rate limited with %v", err, retry, want)
	}
	if len(api.seen()) != sent {
		t.Fatal("a read over the budget reached GitHub")
	}
	if _, err = fx.reader.client.profile(context.Background(), fx.account, newHeldToken(sentinelToken)); !errors.Is(err, errRequestLimit) {
		t.Fatalf("profile after the reads spent the budget = %v, want the request limit", err)
	}
}

func TestReads_GoneAndUpstreamResults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		respond func(w http.ResponseWriter)
		want    error
	}{
		{"404", func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) }, errGone},
		{"410", func(w http.ResponseWriter) { w.WriteHeader(http.StatusGone) }, errGone},
		{"500", func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }, errUpstream},
		{"422", func(w http.ResponseWriter) { w.WriteHeader(http.StatusUnprocessableEntity) }, errUpstream},
		{"302", func(w http.ResponseWriter) {
			w.Header().Set("Location", "https://api.github.com/repositories/101")
			w.WriteHeader(http.StatusFound)
		}, errUpstream},
		{"200 not JSON", func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/html")
			if _, err := io.WriteString(w, repositoryJSON); err != nil {
				t.Error(err)
			}
		}, errUpstream},
		{"200 oversized", func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			if _, err := io.WriteString(w, `{"pad":"`+strings.Repeat("x", maxResponseBytes)+`"}`); err != nil {
				t.Error(err)
			}
		}, errUpstream},
		{"200 unparsable", func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			if _, err := io.WriteString(w, `{"id":101,`); err != nil {
				t.Error(err)
			}
		}, errUpstream},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newFakeAPI(t)
			api.on("/repos/octocat/hello", func(w http.ResponseWriter, _ *http.Request) { tc.respond(w) })
			fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
			_, err := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 0)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if len(api.seen()) != 1 {
				t.Fatalf("requests = %d, want 1", len(api.seen()))
			}
			requireClean(t, err)
		})
	}
	if failure := evidenceFailure(errGone); failure.status != http.StatusUnprocessableEntity || failure.code != "evidence_not_confirmed" {
		t.Fatalf("gone on attach = %+v, want 422 evidence_not_confirmed", failure)
	}
}

func TestReads_TimeoutIsUpstream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newReaderFixture(t, "", blockingTransport{}, time.Now)
		start := time.Now()
		_, err := fx.reader.pullRequest(context.Background(), "octocat", "hello", 42)
		if !errors.Is(err, errUpstream) || !errors.Is(err, errTimeout) || time.Since(start) != 5*time.Second {
			t.Fatalf("error = %v after %v, want upstream timeout at 5s", err, time.Since(start))
		}
		fx.reg.close()
	})
}

func movedTo(location string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", location)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMovedPermanently)
		if _, err := io.WriteString(w, `{"message":"Moved Permanently","url":"`+location+`"}`); err != nil {
			panic(err)
		}
	}
}

func TestReads_RenameHopAcceptsOnlyTheRepositoriesShape(t *testing.T) {
	for _, location := range []string{
		"https://api.github.com/repos/octocat/renamed",
		"https://api.github.com/repositories/101/",
		"https://api.github.com/repositories/101?x=1",
		"https://api.github.com/repositories/0101",
		"https://api.github.com/repositories/abc",
		"http://api.github.com/repositories/101",
		"https://evil.example/repositories/101",
		"https://api.github.com.evil.example/repositories/101",
		"/repositories/101",
		"https://user@api.github.com/repositories/101",
		"",
	} {
		t.Run(location, func(t *testing.T) {
			api := newFakeAPI(t)
			api.on("/repos/octocat/hello", movedTo(location))
			api.json("/repositories/101", http.StatusOK, repositoryJSON)
			fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
			_, err := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 0)
			if !errors.Is(err, errUpstream) {
				t.Fatalf("error = %v, want upstream", err)
			}
			if n := len(api.seen()); n != 1 {
				t.Fatalf("requests = %d, want exactly 1", n)
			}
		})
	}
}

func TestReads_RenameHopOnAttachMakesOneCall(t *testing.T) {
	api := newFakeAPI(t)
	api.on("/repos/octocat/old-name", movedTo("https://api.github.com/repositories/101"))
	api.json("/repositories/101", http.StatusOK, repositoryJSON)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	repo, err := fx.reader.repositoryByName(context.Background(), "octocat", "old-name", 0)
	if err != nil || repo.id != 101 || repo.fullName != "octocat/hello" {
		t.Fatalf("repository = %+v, %v; want the renamed repository", repo, err)
	}
	seen := api.seen()
	if len(seen) != 2 || seen[1].uri != "/repositories/101" || seen[1].header.Get("Authorization") != "Bearer "+sentinelToken {
		t.Fatalf("requests = %+v, want the name then /repositories/101", seen)
	}
}

func TestReads_RenameHopOnCheckAgain(t *testing.T) {
	t.Run("matching id", func(t *testing.T) {
		api := newFakeAPI(t)
		api.on("/repos/octocat/old-name", movedTo("https://api.github.com/repositories/101"))
		api.json("/repositories/101", http.StatusOK, repositoryJSON)
		fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
		repo, err := fx.reader.repositoryByName(context.Background(), "octocat", "old-name", 101)
		if err != nil || repo.id != 101 {
			t.Fatalf("repository = %+v, %v; want id 101", repo, err)
		}
		if len(api.seen()) != 2 {
			t.Fatalf("requests = %d, want 2", len(api.seen()))
		}
	})
	t.Run("mismatching id", func(t *testing.T) {
		api := newFakeAPI(t)
		api.on("/repos/octocat/old-name", movedTo("https://api.github.com/repositories/102"))
		api.json("/repositories/102", http.StatusOK, repositoryJSON)
		fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
		_, err := fx.reader.repositoryByName(context.Background(), "octocat", "old-name", 101)
		if !errors.Is(err, errUpstream) || len(api.seen()) != 1 {
			t.Fatalf("error = %v after %d requests, want upstream after exactly 1", err, len(api.seen()))
		}
	})
	t.Run("hop answers a different id", func(t *testing.T) {
		api := newFakeAPI(t)
		api.on("/repos/octocat/old-name", movedTo("https://api.github.com/repositories/101"))
		api.json("/repositories/101", http.StatusOK, strings.Replace(repositoryJSON, `"id":101`, `"id":999`, 1))
		fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
		if _, err := fx.reader.repositoryByName(context.Background(), "octocat", "old-name", 101); !errors.Is(err, errUpstream) {
			t.Fatalf("error = %v, want upstream", err)
		}
	})
	t.Run("hop moves again", func(t *testing.T) {
		api := newFakeAPI(t)
		api.on("/repos/octocat/old-name", movedTo("https://api.github.com/repositories/101"))
		api.on("/repositories/101", movedTo("https://api.github.com/repositories/101"))
		fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
		_, err := fx.reader.repositoryByName(context.Background(), "octocat", "old-name", 101)
		if !errors.Is(err, errUpstream) || len(api.seen()) != 2 {
			t.Fatalf("error = %v after %d requests, want upstream after exactly 2", err, len(api.seen()))
		}
	})
	t.Run("a different repository under the stored name", func(t *testing.T) {
		api := newFakeAPI(t)
		api.json("/repos/octocat/hello", http.StatusOK, strings.Replace(repositoryJSON, `"id":101`, `"id":555`, 1))
		fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
		if _, err := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 101); !errors.Is(err, errGone) {
			t.Fatalf("error = %v, want gone", err)
		}
	})
	t.Run("hop over the budget", func(t *testing.T) {
		now := fixedClock()
		api := newFakeAPI(t)
		api.on("/repos/octocat/old-name", movedTo("https://api.github.com/repositories/101"))
		api.json("/repositories/101", http.StatusOK, repositoryJSON)
		fx := newReaderFixture(t, api.server.URL, newTransport(), now)
		for range requestLimit - 1 {
			fx.reader.client.requests.admit(fx.account, now())
		}
		_, err := fx.reader.repositoryByName(context.Background(), "octocat", "old-name", 101)
		if _, limited := retryAfter(err); !limited || len(api.seen()) != 1 {
			t.Fatalf("error = %v after %d requests, want rate limited after the first", err, len(api.seen()))
		}
	})
}

func TestReads_ReadsWithoutALiveTokenAnswerSessionRequired(t *testing.T) {
	api := newFakeAPI(t)
	api.json("/repos/octocat/hello", http.StatusOK, repositoryJSON)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	fx.reg.end(fx.account)
	_, err := fx.reader.repositoryByName(context.Background(), "octocat", "hello", 0)
	if failure := evidenceFailure(err); failure.code != sessionRequiredCode || len(api.seen()) != 0 {
		t.Fatalf("error = %v (%+v) after %d requests, want session required and no request", err, failure, len(api.seen()))
	}
}

func TestReads_ReleaseTagIsOneEncodedSegment(t *testing.T) {
	api := newFakeAPI(t)
	var raw string
	api.on("/repos/octocat/hello/releases/tags/a%2Fb%23c%2Cd", func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, releaseJSON); err != nil {
			t.Error(err)
		}
	})
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	if _, err := fx.reader.releaseByTag(context.Background(), "octocat", "hello", "a/b#c,d"); err != nil {
		t.Fatalf("release by tag error = %v", err)
	}
	if raw != "/repos/octocat/hello/releases/tags/a%2Fb%23c%2Cd" {
		t.Fatalf("path = %q", raw)
	}
	for _, tag := range []string{"", "..", "a..b", "a b", "a?b", "a\x00b", "a~b", "a^b", "a:b", "a*b", "a[b", `a\b`, "a@{b", "/a", "a/", "a//b", ".a", "a/.b", "a.lock", "a.", "@", strings.Repeat("a", 256)} {
		if _, err := fx.reader.releaseByTag(context.Background(), "octocat", "hello", tag); !errors.Is(err, errDestinationRefused) {
			t.Errorf("release by tag %q error = %v, want refused", tag, err)
		}
	}
	if len(api.seen()) != 1 {
		t.Fatalf("requests = %d, want 1", len(api.seen()))
	}
}

func TestReads_FailureMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{errSessionRejected, http.StatusConflict, sessionRequiredCode},
		{errTokenGone, http.StatusConflict, sessionRequiredCode},
		{errGone, http.StatusUnprocessableEntity, "evidence_not_confirmed"},
		{errUnsupportedLink, http.StatusUnprocessableEntity, "evidence_unsupported_link"},
		{&rateLimitError{retry: 3 * time.Second}, http.StatusTooManyRequests, "rate_limited"},
		{errUpstream, http.StatusBadGateway, "evidence_upstream"},
		{errMalformed, http.StatusBadGateway, "evidence_upstream"},
		{errDestinationRefused, http.StatusBadGateway, "evidence_upstream"},
		{errors.New("anything"), http.StatusBadGateway, "evidence_upstream"},
	} {
		failure := evidenceFailure(tc.err)
		if failure.status != tc.status || failure.code != tc.code {
			t.Errorf("evidenceFailure(%v) = %+v, want %d %s", tc.err, failure, tc.status, tc.code)
		}
		rec := httptest.NewRecorder()
		writeEvidenceFailure(rec, tc.err)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"`+tc.code+`"`) {
			t.Errorf("writeEvidenceFailure(%v) = %d %s", tc.err, rec.Code, rec.Body.String())
		}
	}
}
