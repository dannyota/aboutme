package directrender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestClientPostsClosedRequestToDirectOrigin(t *testing.T) {
	// This fails if the client follows a viewer route, forwards credentials, or changes the worker envelope.
	origin, err := ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	var gotMethod, gotURL, gotContentType, gotBody string
	var gotAmbient http.Header
	client := New(origin, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		gotMethod, gotURL, gotContentType = request.Method, request.URL.String(), request.Header.Get("Content-Type")
		gotAmbient = request.Header.Clone()
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Fatalf("read request body: %v", readErr)
		}
		gotBody = string(body)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBufferString("<html></html>"))}, nil
	})})
	result, err := client.Render(context.Background(), renderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(result.HTML), "<html></html>"; got != want {
		t.Fatalf("HTML = %q, want %q", got, want)
	}
	if gotMethod != http.MethodPost || gotURL != "http://127.0.0.1:20030/internal-render/public" {
		t.Fatalf("request = %s %s", gotMethod, gotURL)
	}
	if gotContentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", gotContentType)
	}
	for _, header := range []string{"Cookie", "Authorization", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-IP"} {
		if got := gotAmbient.Get(header); got != "" {
			t.Fatalf("%s = %q", header, got)
		}
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(gotBody), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope) != 4 || string(envelope["mode"]) != `"continuous"` || string(envelope["canonicalOrigin"]) != `"https://aboutme.example"` || string(envelope["discoveryEnabled"]) != "false" || envelope["publicResume"] == nil {
		t.Fatalf("closed envelope = %s", gotBody)
	}
}

func TestClientRejectsStatusTypeAndResponseLimit(t *testing.T) {
	// This fails if a malformed worker result can become a public success.
	origin, err := ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		response *http.Response
		status   int
		tooLarge bool
	}{
		{"status", &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}, Body: io.NopCloser(bytes.NewBuffer(nil))}, http.StatusBadGateway, false},
		{"wrong content type", &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(bytes.NewBuffer(nil))}, 0, false},
		{"duplicate content type", &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8", "text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBuffer(nil))}, 0, false},
		{"too large", &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 2_097_153)))}, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := New(origin, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return test.response, nil })})
			_, got := client.Render(context.Background(), renderRequest())
			if !errors.Is(got, ErrRenderUnavailable) {
				t.Fatalf("error = %v, want unavailable", got)
			}
			var statusError *RenderStatusError
			if errors.As(got, &statusError) != (test.status != 0) || (statusError != nil && statusError.Status != test.status) {
				t.Fatalf("status cause = %#v, want %d", statusError, test.status)
			}
			var largeError *RenderResponseTooLargeError
			if errors.As(got, &largeError) != test.tooLarge || (largeError != nil && largeError.Limit != 2_097_152) {
				t.Fatalf("size cause = %#v", largeError)
			}
		})
	}
}

func TestClientCapsRequestBeforeTransportAndSetsFiveSecondDeadline(t *testing.T) {
	// This fails if a caller can allocate an unbounded envelope or the renderer has no hard deadline.
	origin, err := ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	client := New(origin, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("transport must not run")
	})})
	tooLarge := renderRequest()
	tooLarge.PublicResume.Document.PersonalDetails.FullName = string(bytes.Repeat([]byte("x"), 532_480))
	if _, err := client.Render(context.Background(), tooLarge); !errors.Is(err, ErrRenderUnavailable) {
		t.Fatalf("oversized request error = %v", err)
	}
	if called {
		t.Fatal("transport received an oversized request")
	}

	var deadline time.Time
	client = New(origin, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, _ = request.Context().Deadline()
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBufferString("ok"))}, nil
	})})
	before := time.Now()
	if _, renderErr := client.Render(context.Background(), renderRequest()); renderErr != nil {
		t.Fatal(renderErr)
	}
	duration := deadline.Sub(before)
	if duration < 4_900*time.Millisecond || duration > 5_100*time.Millisecond {
		t.Fatalf("deadline duration = %s, want five seconds", duration)
	}
}

func TestClientRequestAndResponseBoundaries(t *testing.T) {
	// This fails if either inclusive byte limit changes independently of the configured literal budget.
	buffer := &limitedBuffer{limit: 532_480}
	if _, err := buffer.Write(bytes.Repeat([]byte("x"), 532_480)); err != nil {
		t.Fatalf("exact request limit rejected: %v", err)
	}
	if _, err := buffer.Write([]byte("x")); !errors.Is(err, errRenderRequestTooLarge) {
		t.Fatalf("request limit+1 error = %v", err)
	}
	origin, err := ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	client := New(origin, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 2_097_152)))}, nil
	})})
	result, err := client.Render(context.Background(), renderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.HTML) != 2_097_152 {
		t.Fatalf("response length = %d", len(result.HTML))
	}
}

func TestClientCancellationJoin(t *testing.T) {
	// This fails if canceling a viewer lets direct render continue after Render returns.
	origin, err := ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	client := New(origin, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		close(exited)
		return nil, request.Context().Err()
	})})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, renderErr := client.Render(ctx, renderRequest()); done <- renderErr }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrRenderUnavailable) {
			t.Fatalf("error = %v", err)
		}
		select {
		case <-exited:
		default:
			t.Fatal("Render returned before transport exited")
		}
	case <-time.After(time.Second):
		t.Fatal("Render did not join canceled transport")
	}
}

func TestClientStripsJarCookiesAndRejectsRedirects(t *testing.T) {
	// This fails if injected HTTP client state can reach the renderer or redirect its request away from the direct listener.
	origin, err := ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	directURL, err := url.Parse("http://127.0.0.1:20030")
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(directURL, []*http.Cookie{{Name: "owner", Value: "secret"}})
	var cookie string
	client := New(origin, &http.Client{Jar: jar, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		cookie = request.Header.Get("Cookie")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBufferString("ok"))}, nil
	})})
	if _, renderErr := client.Render(context.Background(), renderRequest()); renderErr != nil {
		t.Fatal(renderErr)
	}
	if cookie != "" {
		t.Fatalf("ambient Cookie = %q", cookie)
	}

	calls := 0
	client = New(origin, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"http://redirect.invalid/internal-render/public"}}, Body: io.NopCloser(bytes.NewBuffer(nil))}, nil
	})})
	_, err = client.Render(context.Background(), renderRequest())
	if !errors.Is(err, ErrRenderUnavailable) {
		t.Fatalf("redirect error = %v", err)
	}
	var statusError *RenderStatusError
	if !errors.As(err, &statusError) || statusError.Status != http.StatusFound {
		t.Fatalf("redirect status cause = %#v", statusError)
	}
	if calls != 1 {
		t.Fatalf("redirect transport calls = %d, want 1", calls)
	}
}

func renderRequest() PublicRenderRequest {
	return PublicRenderRequest{PublicResume: publicresume.PublicResume{Slug: "ada"}, Mode: PublicRenderMode, CanonicalOrigin: "https://aboutme.example"}
}
