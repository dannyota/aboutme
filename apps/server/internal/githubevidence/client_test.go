package githubevidence

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"danny.vn/aboutme/apps/server/internal/config"
)

const (
	testClientID     = "Ov23liEvidenceTest"
	testClientSecret = "client-secret-SENTINEL-7f3a"
	sentinelToken    = "gho_TOKEN_SENTINEL_9b1e"
	sentinelRefresh  = "ghr_REFRESH_SENTINEL_4c2d"
	sentinelBody     = "BODY_SENTINEL_e81f"
)

type countingTransport struct {
	mu       sync.Mutex
	requests []*http.Request
	respond  func(*http.Request) (*http.Response, error)
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	respond := c.respond
	c.mu.Unlock()
	if respond == nil {
		return nil, errors.New("no response configured")
	}
	return respond(req)
}

func (c *countingTransport) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newTestClient(endpoint string, transport http.RoundTripper, now func() time.Time) *client {
	return newClient(testClientID, config.Secret(testClientSecret), endpoint, transport, newWindowLimiter(requestLimit, limitWindow), now)
}

func requireClean(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		return
	}
	for _, secret := range append([]string{sentinelToken, sentinelRefresh, testClientSecret, sentinelBody}, secrets...) {
		if strings.Contains(err.Error(), secret) || strings.Contains(fmt.Sprintf("%+v %#v", err, err), secret) {
			t.Fatalf("error %q carries a secret or raw upstream text", err)
		}
	}
}

func TestClient_ProductionDestinationsAndTransportAreFixed(t *testing.T) {
	c := newTestClient("", newTransport(), time.Now)
	if c.tokenURL != "https://github.com/login/oauth/access_token" || c.userURL != "https://api.github.com/user" ||
		c.revokeURL != "https://api.github.com/applications/"+testClientID+"/token" {
		t.Fatalf("destinations = %q %q %q", c.tokenURL, c.userURL, c.revokeURL)
	}
	link, err := url.Parse(c.authorizeLink("state", oauth2.GenerateVerifier(), "https://aboutme.example"+CallbackPath))
	if err != nil || link.Scheme != "https" || link.Host != "github.com" || link.Path != "/login/oauth/authorize" {
		t.Fatalf("authorize link = %v, %v", link, err)
	}
	if link.Query().Has("scope") {
		t.Fatal("authorize link asks for a scope")
	}
	transport := newTransport()
	if transport.Proxy != nil {
		t.Fatal("transport takes a proxy from the environment")
	}
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("transport turns certificate validation off")
	}
	if c.http.Timeout != 5*time.Second || c.http.CheckRedirect == nil {
		t.Fatalf("client timeout = %v, redirect check set = %v", c.http.Timeout, c.http.CheckRedirect != nil)
	}
}

func TestClient_RefusesOtherDestinationsWithoutSending(t *testing.T) {
	base := &countingTransport{respond: func(*http.Request) (*http.Response, error) { return jsonResponse(200, `{}`), nil }}
	c := newTestClient("", base, time.Now)
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "https://evil.example/user"},
		{http.MethodGet, "https://api.github.com.evil.example/user"},
		{http.MethodGet, "http://api.github.com/user"},
		{http.MethodGet, "https://api.github.com:443/user"},
		{http.MethodGet, "https://api.github.com/user/emails"},
		{http.MethodGet, "https://api.github.com/user/"},
		{http.MethodGet, "https://api.github.com/user?per_page=1"},
		{http.MethodGet, "https://someone@api.github.com/user"},
		{http.MethodPost, "https://api.github.com/user"},
		{http.MethodGet, "https://github.com/login/oauth/access_token"},
		{http.MethodPost, "https://github.com/login/oauth/access_token/"},
		{http.MethodDelete, "https://api.github.com/applications/other-client/token"},
		{http.MethodDelete, "https://api.github.com/applications/" + testClientID + "/grant"},
	} {
		_, _, _, err := c.send(context.Background(), tc.method, tc.target, nil, func(http.Header) {})
		if !errors.Is(err, errDestinationRefused) {
			t.Errorf("%s %s error = %v, want refused", tc.method, tc.target, err)
		}
	}
	if n := base.count(); n != 0 {
		t.Fatalf("refused destinations sent %d requests, want 0", n)
	}
}

func TestClient_RefusesRedirectsWithoutASecondRequest(t *testing.T) {
	for _, sameDestination := range []bool{false, true} {
		t.Run(fmt.Sprintf("same destination %v", sameDestination), func(t *testing.T) {
			var targetHits, sourceHits int
			var mu sync.Mutex
			profile := func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				if _, err := io.WriteString(w, `{"id":1,"login":"octocat"}`); err != nil {
					t.Error(err)
				}
			}
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				targetHits++
				mu.Unlock()
				profile(w)
			}))
			t.Cleanup(target.Close)
			var source *httptest.Server
			source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				sourceHits++
				first := sourceHits == 1
				mu.Unlock()
				switch {
				case !first:
					profile(w)
				case sameDestination:
					http.Redirect(w, r, source.URL+"/user", http.StatusFound)
				default:
					http.Redirect(w, r, target.URL+"/user", http.StatusFound)
				}
			}))
			t.Cleanup(source.Close)
			c := newTestClient(source.URL, newTransport(), time.Now)
			_, err := c.profile(context.Background(), uuid.New(), newHeldToken(sentinelToken))
			mu.Lock()
			defer mu.Unlock()
			if sourceHits != 1 || targetHits != 0 {
				t.Fatalf("requests after a redirect: source = %d, other host = %d; want 1 and 0", sourceHits, targetHits)
			}
			if !errors.Is(err, errRedirectRefused) {
				t.Fatalf("profile error = %v, want redirect refused", err)
			}
			requireClean(t, err)
		})
	}
}

func TestClient_ResponseBound(t *testing.T) {
	const bound = 512 * 1024
	exact := `{"id":1,"login":"octocat","pad":"`
	exact += strings.Repeat("x", bound-len(exact)-2) + `"}`
	if len(exact) != bound {
		t.Fatalf("fixture length = %d", len(exact))
	}
	for _, tc := range []struct {
		name string
		body string
		want error
	}{
		{"exactly 512 KiB", exact, nil},
		{"one byte over", exact + " ", errResponseTooLarge},
		{"far over", strings.Repeat(" ", 4*bound), errResponseTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &countingTransport{respond: func(*http.Request) (*http.Response, error) { return jsonResponse(200, tc.body), nil }}
			c := newTestClient("", base, time.Now)
			profile, err := c.profile(context.Background(), uuid.New(), newHeldToken(sentinelToken))
			if !errors.Is(err, tc.want) || (tc.want == nil && profile.Login != "octocat") {
				t.Fatalf("profile = %+v, %v; want %v", profile, err, tc.want)
			}
		})
	}
}

type blockingTransport struct {
	inBody bool
}

type blockingBody struct {
	ctx context.Context
}

func (b blockingBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (blockingBody) Close() error { return nil }

func (b blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.inBody {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: blockingBody{ctx: req.Context()}}, nil
	}
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestClient_EachRequestStopsAtFiveSeconds(t *testing.T) {
	for _, inBody := range []bool{false, true} {
		t.Run(fmt.Sprintf("stalled body %v", inBody), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := newTestClient("", blockingTransport{inBody: inBody}, time.Now)
				start := time.Now()
				_, err := c.profile(context.Background(), uuid.New(), newHeldToken(sentinelToken))
				if !errors.Is(err, errTimeout) {
					t.Fatalf("profile error = %v, want timeout", err)
				}
				if elapsed := time.Since(start); elapsed != 5*time.Second {
					t.Fatalf("request stopped after %v, want exactly 5s", elapsed)
				}
				start = time.Now()
				if err = c.revoke(context.Background(), sentinelToken); !errors.Is(err, errTimeout) || time.Since(start) != 5*time.Second {
					t.Fatalf("revoke error = %v after %v, want timeout at 5s", err, time.Since(start))
				}
				requireClean(t, err)
			})
		})
	}
}

func tokenTransport(body string, seen *url.Values) *countingTransport {
	return &countingTransport{respond: func(req *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if *seen, err = url.ParseQuery(string(raw)); err != nil {
			return nil, err
		}
		return jsonResponse(200, body), nil
	}}
}

func TestClient_ExchangeRequiresAnEmptyScopeAndDropsTheRefreshToken(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		want      error
		wantToken bool
	}{
		{"empty scope", `{"access_token":"` + sentinelToken + `","token_type":"bearer","scope":"","expires_in":28800,"refresh_token":"` + sentinelRefresh + `","refresh_token_expires_in":15897600}`, nil, true},
		{"any scope", `{"access_token":"` + sentinelToken + `","token_type":"bearer","scope":"read:user"}`, errScopeRejected, true},
		{"missing scope", `{"access_token":"` + sentinelToken + `","token_type":"bearer"}`, errScopeRejected, true},
		{"null scope", `{"access_token":"` + sentinelToken + `","token_type":"bearer","scope":null}`, errScopeRejected, true},
		{"array scope", `{"access_token":"` + sentinelToken + `","token_type":"bearer","scope":[]}`, errScopeRejected, true},
		{"wrong token type", `{"access_token":"` + sentinelToken + `","token_type":"mac","scope":""}`, errMalformed, true},
		{"error beside a token", `{"access_token":"` + sentinelToken + `","token_type":"bearer","scope":"","error":"x"}`, errUpstreamRejected, true},
		{"error only", `{"error":"bad_verification_code","error_description":"` + sentinelBody + `"}`, errUpstreamRejected, false},
		{"duplicate scope", `{"access_token":"` + sentinelToken + `","token_type":"bearer","scope":"","scope":"repo"}`, errMalformed, true},
		{"trailing data", `{"access_token":"` + sentinelToken + `","token_type":"bearer","scope":""} {}`, errMalformed, true},
		{"the same token twice", `{"access_token":"` + sentinelToken + `","access_token":"` + sentinelToken + `","token_type":"bearer","scope":""}`, errMalformed, true},
		{"two different tokens", `{"access_token":"` + sentinelToken + `","access_token":"other-token","token_type":"bearer","scope":""}`, errAmbiguousToken, false},
		{"truncated after the token", `{"access_token":"` + sentinelToken + `","token_type":"bea`, errMalformed, true},
		{"not an object", `["` + sentinelToken + `"]`, errMalformed, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var form url.Values
			c := newTestClient("", tokenTransport(tc.body, &form), time.Now)
			token, err := c.exchange(context.Background(), uuid.New(), "the-code", "https://aboutme.example"+CallbackPath, "the-verifier")
			if !errors.Is(err, tc.want) {
				t.Fatalf("exchange error = %v, want %v", err, tc.want)
			}
			requireClean(t, err)
			if (token != nil) != tc.wantToken {
				t.Fatalf("exchange token held = %v, want %v", token != nil, tc.wantToken)
			}
			if token != nil {
				if value, ok := token.peek(); !ok || value != sentinelToken {
					t.Fatalf("held token = %q, want the access token only", value)
				}
			}
			want := url.Values{"client_id": {testClientID}, "client_secret": {testClientSecret}, "code": {"the-code"},
				"redirect_uri": {"https://aboutme.example" + CallbackPath}, "code_verifier": {"the-verifier"}}
			if form.Encode() != want.Encode() {
				t.Fatalf("exchange form = %v, want %v", form, want)
			}
		})
	}
}

func TestClient_ExchangeRejectsNonJSONAndErrorStatus(t *testing.T) {
	for _, tc := range []struct {
		respond   func(*http.Request) (*http.Response, error)
		wantToken bool
	}{
		{func(*http.Request) (*http.Response, error) {
			return jsonResponse(500, `{"access_token":"`+sentinelToken+`","message":"`+sentinelBody+`"}`), nil
		}, true},
		{func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}},
				Body: io.NopCloser(strings.NewReader("access_token=" + sentinelToken + "&scope="))}, nil
		}, false},
	} {
		c := newTestClient("", &countingTransport{respond: tc.respond}, time.Now)
		token, err := c.exchange(context.Background(), uuid.New(), "code", "https://aboutme.example"+CallbackPath, "verifier")
		if !errors.Is(err, errUpstreamRejected) || (token != nil) != tc.wantToken {
			t.Fatalf("exchange = %v, %v; want rejected, token held = %v", token, err, tc.wantToken)
		}
		requireClean(t, err)
	}
}

func TestClient_ProfileParsesStrictly(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"id":583231,"login":"octocat","name":"The Octocat","email":null}`, true},
		{`{"id":"583231","login":"octocat"}`, false},
		{`{"id":5.8e5,"login":"octocat"}`, false},
		{`{"id":0,"login":"octocat"}`, false},
		{`{"id":-4,"login":"octocat"}`, false},
		{`{"id":583231}`, false},
		{`{"id":583231,"login":""}`, false},
		{`{"id":583231,"login":"octo cat"}`, false},
		{`{"id":583231,"login":"` + strings.Repeat("a", 101) + `"}`, false},
		{`{"id":583231,"login":"octocat","login":"other"}`, false},
		{`{"id":583231,"login":"octocat"}x`, false},
	} {
		var authorization string
		c := newTestClient("", &countingTransport{respond: func(req *http.Request) (*http.Response, error) {
			authorization = req.Header.Get("Authorization")
			return jsonResponse(200, tc.body), nil
		}}, time.Now)
		profile, err := c.profile(context.Background(), uuid.New(), newHeldToken(sentinelToken))
		if (err == nil) != tc.ok {
			t.Errorf("profile(%s) = %+v, %v; want ok=%v", tc.body, profile, err, tc.ok)
		}
		if tc.ok && (profile.ID != 583231 || profile.Login != "octocat") {
			t.Errorf("profile = %+v", profile)
		}
		if authorization != "Bearer "+sentinelToken {
			t.Errorf("profile Authorization = %q", authorization)
		}
		requireClean(t, err)
	}
}

func TestClient_RevocationRequestShape(t *testing.T) {
	var method, path, authorization, contentType string
	var body []byte
	base := &countingTransport{respond: func(req *http.Request) (*http.Response, error) {
		method, path = req.Method, req.URL.String()
		authorization, contentType = req.Header.Get("Authorization"), req.Header.Get("Content-Type")
		if req.Body == nil {
			body = nil
			return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody}, nil
		}
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody}, nil
	}}
	c := newTestClient("", base, time.Now)
	if err := c.revoke(context.Background(), sentinelToken); err != nil {
		t.Fatalf("revoke error = %v", err)
	}
	if method != http.MethodDelete || path != "https://api.github.com/applications/"+testClientID+"/token" {
		t.Fatalf("revocation = %s %s", method, path)
	}
	if authorization != "Basic "+base64.StdEncoding.EncodeToString([]byte(testClientID+":"+testClientSecret)) {
		t.Fatalf("revocation Authorization = %q, want Basic client id and secret", authorization)
	}
	var decoded map[string]any
	if contentType != "application/json" || json.Unmarshal(body, &decoded) != nil || len(decoded) != 1 || decoded["access_token"] != sentinelToken {
		t.Fatalf("revocation body = %s (%s), want exactly {\"access_token\": token}", body, contentType)
	}
	if base.count() != 1 {
		t.Fatalf("revocation sent %d requests, want 1", base.count())
	}

	failing := &countingTransport{respond: func(*http.Request) (*http.Response, error) {
		return jsonResponse(500, `{"message":"`+sentinelBody+`"}`), nil
	}}
	c = newTestClient("", failing, time.Now)
	err := c.revoke(context.Background(), sentinelToken)
	if !errors.Is(err, errUpstreamRejected) || failing.count() != 1 {
		t.Fatalf("failed revoke = %v after %d requests, want rejected after exactly 1 (no retry)", err, failing.count())
	}
	requireClean(t, err)
}

func TestClient_PerAccountRequestBudget(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	base := &countingTransport{respond: func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodDelete {
			return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody}, nil
		}
		return jsonResponse(200, `{"id":1,"login":"octocat"}`), nil
	}}
	c := newTestClient("", base, func() time.Time { return now })
	account, other := uuid.New(), uuid.New()
	for i := range requestLimit {
		if _, err := c.profile(context.Background(), account, newHeldToken(sentinelToken)); err != nil {
			t.Fatalf("request %d error = %v", i+1, err)
		}
		now = now.Add(29 * time.Second)
	}
	sent := base.count()
	if _, err := c.profile(context.Background(), account, newHeldToken(sentinelToken)); !errors.Is(err, errRequestLimit) {
		t.Fatalf("request 121 error = %v, want the request limit", err)
	}
	if _, err := c.exchange(context.Background(), account, "code", "https://aboutme.example"+CallbackPath, "verifier"); !errors.Is(err, errRequestLimit) {
		t.Fatalf("exchange after the limit error = %v, want the request limit", err)
	}
	if base.count() != sent {
		t.Fatalf("refused requests reached the network: %d sent, want %d", base.count(), sent)
	}
	if err := c.revoke(context.Background(), sentinelToken); err != nil || base.count() != sent+1 {
		t.Fatalf("revoke after the limit = %v with %d requests, want sent", err, base.count())
	}
	if _, err := c.profile(context.Background(), other, newHeldToken(sentinelToken)); err != nil {
		t.Fatalf("another account's request error = %v, want admitted", err)
	}
	now = now.Add(time.Hour - 120*29*time.Second + time.Second)
	if _, err := c.profile(context.Background(), account, newHeldToken(sentinelToken)); err != nil {
		t.Fatalf("request after the oldest left the hour error = %v", err)
	}
}

func TestWindowLimiter_TenStartsPerHour(t *testing.T) {
	limiter := newWindowLimiter(startLimit, limitWindow)
	account := uuid.New()
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for i := range startLimit {
		if ok, _ := limiter.admit(account, start.Add(time.Duration(i)*time.Minute)); !ok {
			t.Fatalf("start %d refused", i+1)
		}
	}
	ok, retry := limiter.admit(account, start.Add(59*time.Minute))
	if ok || retry != time.Minute {
		t.Fatalf("11th start = %v, retry %v; want refused with 1m to wait", ok, retry)
	}
	if ok, _ = limiter.admit(account, start.Add(time.Hour)); !ok {
		t.Fatal("start after the first left the hour refused")
	}
}
