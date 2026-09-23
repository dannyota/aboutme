package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// scriptedTransport answers a fixed list of responses and records requests.
// It fails once the script is exhausted, so no test can loop.
type scriptedTransport struct {
	requests []*http.Request
	bodies   []string
	answers  []scriptedAnswer
}

type scriptedAnswer struct {
	status int
	body   string
	date   string
	err    error
}

func (s *scriptedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if len(s.requests) >= len(s.answers) {
		return nil, errFakeBudget
	}
	body := ""
	if request.Body != nil {
		data, err := io.ReadAll(io.LimitReader(request.Body, 4096))
		if err != nil {
			return nil, err
		}
		body = string(data)
	}
	s.requests = append(s.requests, request)
	s.bodies = append(s.bodies, body)
	answer := s.answers[len(s.requests)-1]
	if answer.err != nil {
		return nil, answer.err
	}
	header := make(http.Header)
	if answer.date != "" {
		header.Set("Date", answer.date)
	}
	return &http.Response{StatusCode: answer.status, Header: header, Body: io.NopCloser(strings.NewReader(answer.body)), Request: request}, nil
}

func testRevocationClient(t *testing.T, answers ...scriptedAnswer) (httpRevocation, *scriptedTransport) {
	t.Helper()
	origin, err := parseOrigin(productionOrigin)
	if err != nil {
		t.Fatal(err)
	}
	transport := &scriptedTransport{answers: answers}
	return httpRevocation{client: newRestrictedHTTPClient(origin, transport), origin: productionOrigin}, transport
}

var fakeServerDate = fakeEpoch.Format(http.TimeFormat)

func TestRevocationRequestHasExactFormWithoutAmbientAuthority(t *testing.T) {
	client, transport := testRevocationClient(t, scriptedAnswer{status: http.StatusOK, date: fakeServerDate})
	if err := client.Revoke(context.Background(), "refresh+token/value"); err != nil {
		t.Fatal(err)
	}
	request := transport.requests[0]
	if request.Method != http.MethodPost || request.URL.String() != productionOrigin+"/oauth/revoke" ||
		request.Header.Get("Content-Type") != formContentType || request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
		t.Fatalf("request = %s %s %v", request.Method, request.URL, request.Header)
	}
	want := url.Values{"token": {"refresh+token/value"}, "token_type_hint": {"refresh_token"}}.Encode()
	if transport.bodies[0] != want {
		t.Fatalf("form = %q, want %q", transport.bodies[0], want)
	}
}

func TestRevocationRetriesOnceOnlyForTransportOr5xx(t *testing.T) {
	for name, test := range map[string]struct {
		answers  []scriptedAnswer
		want     bool
		requests int
	}{
		"transport then ok":  {[]scriptedAnswer{{err: errFakeLost}, {status: http.StatusOK}}, true, 2},
		"502 then ok":        {[]scriptedAnswer{{status: http.StatusBadGateway}, {status: http.StatusOK}}, true, 2},
		"two failures":       {[]scriptedAnswer{{status: http.StatusBadGateway}, {err: errFakeLost}, {status: http.StatusOK}}, false, 2},
		"400 is not retried": {[]scriptedAnswer{{status: http.StatusBadRequest}, {status: http.StatusOK}}, false, 1},
	} {
		t.Run(name, func(t *testing.T) {
			client, transport := testRevocationClient(t, test.answers...)
			err := client.Revoke(context.Background(), "refresh-a")
			if (err == nil) != test.want || len(transport.requests) != test.requests {
				t.Fatalf("err = %v requests = %d", err, len(transport.requests))
			}
			for index := 1; index < len(transport.bodies); index++ {
				if transport.bodies[index] != transport.bodies[0] {
					t.Fatal("retry was not byte-equivalent")
				}
			}
			if err != nil && strings.Contains(err.Error(), "refresh-a") {
				t.Fatal("error exposed the token")
			}
		})
	}
}

func TestRevocationProbeReturnsStatusAndServerDate(t *testing.T) {
	client, transport := testRevocationClient(t, scriptedAnswer{status: http.StatusUnauthorized, date: fakeServerDate})
	status, date, err := client.Probe(context.Background(), "access-a")
	if err != nil || status != http.StatusUnauthorized || !date.Equal(fakeEpoch) {
		t.Fatalf("probe = %d %v %v", status, date, err)
	}
	request := transport.requests[0]
	if request.Method != http.MethodGet || request.URL.Path != "/mcp" || request.Header.Get("Authorization") != "Bearer access-a" {
		t.Fatalf("probe request = %s %s", request.Method, request.URL)
	}
	if _, _, invalidErr := client.Probe(context.Background(), "bad\ntoken"); !errors.Is(invalidErr, errRevocation) {
		t.Fatal("probe sent an unsafe header value")
	}
}

func TestRevocationRefreshClassifiesInvalidGrantRotationAndUnknown(t *testing.T) {
	rotation := `{"access_token":"access-b","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-b","scope":"resumes:read resumes:write"}`
	for name, test := range map[string]struct {
		answer scriptedAnswer
		want   refreshOutcome
		ok     bool
	}{
		"invalid grant": {scriptedAnswer{status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`, date: fakeServerDate}, refreshOutcome{Dead: true, ServerDate: fakeEpoch}, true},
		"rotation":      {scriptedAnswer{status: http.StatusOK, body: rotation, date: fakeServerDate}, refreshOutcome{AccessToken: "access-b", RefreshToken: "refresh-b", ServerDate: fakeEpoch}, true},
		"missing date":  {scriptedAnswer{status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`}, refreshOutcome{}, false},
		"other error":   {scriptedAnswer{status: http.StatusBadRequest, body: `{"error":"invalid_request"}`, date: fakeServerDate}, refreshOutcome{}, false},
		"server error":  {scriptedAnswer{status: http.StatusBadGateway, date: fakeServerDate}, refreshOutcome{}, false},
		"extra field":   {scriptedAnswer{status: http.StatusOK, body: strings.Replace(rotation, "}", `,"id_token":"x"}`, 1), date: fakeServerDate}, refreshOutcome{}, false},
		"reused token":  {scriptedAnswer{status: http.StatusOK, body: strings.Replace(rotation, "refresh-b", "refresh-a", 1), date: fakeServerDate}, refreshOutcome{}, false},
		"oversized":     {scriptedAnswer{status: http.StatusOK, body: strings.Repeat(" ", maxOAuthResponseBytes+1), date: fakeServerDate}, refreshOutcome{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			client, transport := testRevocationClient(t, test.answer)
			got, err := client.Refresh(context.Background(), "refresh-a")
			if (err == nil) != test.ok || got.Dead != test.want.Dead || got.AccessToken != test.want.AccessToken ||
				got.RefreshToken != test.want.RefreshToken || !got.ServerDate.Equal(test.want.ServerDate) {
				t.Fatalf("refresh = %+v, %v", got, err)
			}
			if transport.bodies[0] != "grant_type=refresh_token&refresh_token=refresh-a" || transport.requests[0].URL.Path != "/oauth/token" {
				t.Fatalf("refresh form = %q", transport.bodies[0])
			}
		})
	}
}

func TestRevocationObserverRecordsDatesAndDenialWithoutTokens(t *testing.T) {
	later := fakeEpoch.Add(time.Minute).Format(http.TimeFormat)
	base := &scriptedTransport{answers: []scriptedAnswer{
		{status: http.StatusOK, date: fakeServerDate},
		{status: http.StatusOK, date: fakeServerDate},
		{status: http.StatusOK, date: later},
		{status: http.StatusUnauthorized, date: later},
	}}
	observer := &serverObserver{base: base}
	send := func(method, path, bearer string) {
		t.Helper()
		request, err := http.NewRequestWithContext(context.Background(), method, productionOrigin+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if bearer != "" {
			request.Header.Set("Authorization", "Bearer "+bearer)
		}
		response, err := observer.RoundTrip(request)
		if err != nil {
			t.Fatal(err)
		}
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	mark := observer.mark()
	send(http.MethodPost, tokenEndpointPath, "")
	send(http.MethodPost, tokenEndpointPath, "")
	send(http.MethodPost, mcpEndpointPath, "access-a")
	if date, ok := observer.authenticatedDateSince(mark); !ok || !date.Equal(fakeEpoch.Add(time.Minute)) {
		t.Fatalf("authenticated date = %v %t", date, ok)
	}
	if first, latest, ok := observer.tokenDates(); !ok || !first.Equal(fakeEpoch) || !latest.Equal(fakeEpoch) {
		t.Fatalf("token dates = %v %v %t", first, latest, ok)
	}
	denialMark := observer.mark()
	send(http.MethodPost, mcpEndpointPath, "access-a")
	if _, ok := observer.deniedSince(denialMark, digest([]byte("access-b"))); ok {
		t.Fatal("denial matched another token")
	}
	if date, ok := observer.deniedSince(denialMark, digest([]byte("access-a"))); !ok || !date.Equal(fakeEpoch.Add(time.Minute)) {
		t.Fatal("denial not observed")
	}
	if observer.reauthorizationRefusedSince(denialMark) {
		t.Fatal("refusal reported without a gate refusal")
	}
	if strings.Contains(observer.deniedDigest, "access-a") {
		t.Fatal("observer stored a token")
	}
}

type stubOAuth struct{ authorizations int }

func (s *stubOAuth) TokenSource(context.Context) (oauth2.TokenSource, error) { return nil, nil }

func (s *stubOAuth) Authorize(context.Context, *http.Request, *http.Response) error {
	s.authorizations++
	return nil
}

func TestRevocationBlocksSDKReauthorization(t *testing.T) {
	delegate := &stubOAuth{}
	observer := &serverObserver{base: &scriptedTransport{}}
	handler := observedOAuth{inner: &oauthGate{delegate: delegate}, observer: observer}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, productionOrigin+mcpEndpointPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := func() *http.Response {
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}
	}
	first := response()
	firstErr := handler.Authorize(context.Background(), request, first)
	if closeErr := first.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	mark := observer.mark()
	for range 2 {
		refused := response()
		authErr := handler.Authorize(context.Background(), request, refused)
		if closeErr := refused.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if !errors.Is(authErr, errReauthorizationDisabled) {
			t.Fatalf("reauthorization err = %v", authErr)
		}
	}
	if delegate.authorizations != 1 || !observer.reauthorizationRefusedSince(mark) {
		t.Fatalf("authorizations = %d refused = %t", delegate.authorizations, observer.reauthorizationRefusedSince(mark))
	}
}
