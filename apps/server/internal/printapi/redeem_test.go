package printapi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/printsnapshot"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

const (
	testResumeID   = "1abcdef0-abcd-4def-8abc-abcdefabcdef"
	testOtherID    = "1abcdef0-abcd-4def-8abc-abcdefabcdee"
	testJobID      = "2abcdef0-abcd-4def-8abc-abcdefabcdef"
	testCapability = "QkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkJCQkI"
	notFoundBody   = "{\"error\":{\"code\":\"not_found\",\"message\":\"not found\"}}\n"
)

type redeemFunc func(context.Context, renderjob.Redemption) (renderjob.Snapshot, error)

func (f redeemFunc) Redeem(ctx context.Context, redemption renderjob.Redemption) (renderjob.Snapshot, error) {
	return f(ctx, redemption)
}

type recordingRedeemer struct {
	mu       sync.Mutex
	calls    []renderjob.Redemption
	snapshot renderjob.Snapshot
	err      error
}

func (r *recordingRedeemer) Redeem(_ context.Context, redemption renderjob.Redemption) (renderjob.Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, redemption)
	return r.snapshot, r.err
}

func (r *recordingRedeemer) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *recordingRedeemer) onlyCall(t *testing.T) renderjob.Redemption {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) != 1 {
		t.Fatalf("Redeem calls = %d, want 1", len(r.calls))
	}
	return r.calls[0]
}

func TestMissingAuthorityNeverReachesRedemption(t *testing.T) {
	// This fails if an ID-only request can consume or retrieve a private snapshot.
	redeemer := &recordingRedeemer{snapshot: validSnapshot(t)}
	handler := mustHandler(t, redeemer)
	request := validRequest()
	request.Header.Del("Authorization")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	assertFailure(t, response, http.StatusNotFound, false)
	if calls := redeemer.callCount(); calls != 0 {
		t.Fatalf("Redeem calls = %d, want 0", calls)
	}
}

func TestSuccessfulRedemptionReturnsExactFrozenPayload(t *testing.T) {
	// This fails if the adapter reloads, reserializes, wraps, or changes the queue's frozen bytes.
	snapshot := validSnapshot(t)
	redeemer := &recordingRedeemer{snapshot: snapshot}
	handler := mustHandler(t, redeemer)
	request := validRequest()
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %q", response.Code, http.StatusOK, response.Body.String())
	}
	if !bytes.Equal(response.Body.Bytes(), snapshot.Payload) {
		t.Fatalf("body differs from frozen payload\n got: %s\nwant: %s", response.Body.Bytes(), snapshot.Payload)
	}
	if got, want := response.Header().Get("Content-Type"), "application/json"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Header().Get("Cache-Control"), "no-store"; got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}
	if got, want := response.Header().Get("Content-Length"), strconv.Itoa(len(snapshot.Payload)); got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}
	if got := response.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want absent", got)
	}
	call := redeemer.onlyCall(t)
	if call.ResumeID.String() != testResumeID || call.JobID.String() != testJobID || call.Audience != "nuxt-print" || call.Capability != testCapability {
		t.Fatalf("redemption = %+v", call)
	}
}

func TestRequestAuthorityAndTransportShapeFailClosed(t *testing.T) {
	// Each case catches one way a broader HTTP surface could reach the one-use authority.
	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"missing authorization", func(r *http.Request) { r.Header.Del("Authorization") }},
		{"duplicate authorization", func(r *http.Request) {
			r.Header["Authorization"] = []string{"RenderCapability " + testCapability, "RenderCapability " + testCapability}
		}},
		{"case-variant duplicate authorization", func(r *http.Request) {
			r.Header["authorization"] = []string{"RenderCapability " + testCapability}
		}},
		{"joined authorization", func(r *http.Request) {
			r.Header.Set("Authorization", "RenderCapability "+testCapability+", RenderCapability "+testCapability)
		}},
		{"wrong scheme", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+testCapability) }},
		{"lowercase scheme", func(r *http.Request) { r.Header.Set("Authorization", "rendercapability "+testCapability) }},
		{"short capability", func(r *http.Request) { r.Header.Set("Authorization", "RenderCapability short") }},
		{"padded capability", func(r *http.Request) { r.Header.Set("Authorization", "RenderCapability "+strings.Repeat("A", 42)+"=") }},
		{"noncanonical capability", func(r *http.Request) { r.Header.Set("Authorization", "RenderCapability "+strings.Repeat("A", 42)+"B") }},
		{"missing job", func(r *http.Request) { r.Header.Del("X-Render-Job-ID") }},
		{"duplicate job", func(r *http.Request) { r.Header["X-Render-Job-ID"] = []string{testJobID, testJobID} }},
		{"joined job", func(r *http.Request) { r.Header.Set("X-Render-Job-ID", testJobID+","+testJobID) }},
		{"nil job", func(r *http.Request) { r.Header.Set("X-Render-Job-ID", uuid.Nil.String()) }},
		{"uppercase job", func(r *http.Request) { r.Header.Set("X-Render-Job-ID", strings.ToUpper(testJobID)) }},
		{"unhyphenated job", func(r *http.Request) { r.Header.Set("X-Render-Job-ID", strings.ReplaceAll(testJobID, "-", "")) }},
		{"missing media type", func(r *http.Request) { r.Header.Del("Content-Type") }},
		{"duplicate media type", func(r *http.Request) { r.Header["Content-Type"] = []string{"application/json", "application/json"} }},
		{"media type parameter", func(r *http.Request) { r.Header.Set("Content-Type", "application/json; charset=utf-8") }},
		{"wrong media type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{"compressed body", func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }},
		{"transfer encoded body", func(r *http.Request) { r.TransferEncoding = []string{"chunked"} }},
		{"trailer", func(r *http.Request) {
			r.Trailer = make(http.Header)
			r.Trailer.Set("X-Extra", "value")
		}},
		{"cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "session", Value: "secret"}) }},
		{"forwarded viewer header", func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.1") }},
		{"extra header", func(r *http.Request) { r.Header.Set("X-Extra", "value") }},
		{"bad connection", func(r *http.Request) { r.Header.Set("Connection", "keep-alive") }},
		{"duplicate connection", func(r *http.Request) { r.Header["Connection"] = []string{"close", "close"} }},
		{"bad content length header", func(r *http.Request) { r.Header.Set("Content-Length", "01") }},
		{"duplicate content length header", func(r *http.Request) {
			r.Header["Content-Length"] = []string{strconv.FormatInt(r.ContentLength, 10), strconv.FormatInt(r.ContentLength, 10)}
		}},
		{"missing content length", func(r *http.Request) { r.ContentLength = -1 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			redeemer := &recordingRedeemer{snapshot: validSnapshot(t)}
			request := validRequest()
			test.mutate(request)
			response := httptest.NewRecorder()
			mustHandler(t, redeemer).ServeHTTP(response, request)
			assertFailure(t, response, http.StatusNotFound, false)
			if calls := redeemer.callCount(); calls != 0 {
				t.Fatalf("Redeem calls = %d, want 0", calls)
			}
		})
	}
}

func TestRequestPathMethodAndQueryAreExact(t *testing.T) {
	// This fails if the private handler grows aliases or parses an encoded near-match as authority.
	for _, target := range []string{
		"/internal-render/print/redeem/",
		"/internal-render/print/redeem/extra",
		"/internal-render/print/%72edeem",
		"//internal-render/print/redeem",
		"/internal-render/print/redeem?value=1",
		"/internal-render/print/redeem?",
	} {
		t.Run(target, func(t *testing.T) {
			redeemer := &recordingRedeemer{snapshot: validSnapshot(t)}
			request := validRequest()
			request.URL, _ = request.URL.Parse(target)
			response := httptest.NewRecorder()
			mustHandler(t, redeemer).ServeHTTP(response, request)
			assertFailure(t, response, http.StatusNotFound, false)
			if calls := redeemer.callCount(); calls != 0 {
				t.Fatalf("Redeem calls = %d, want 0", calls)
			}
		})
	}

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			redeemer := &recordingRedeemer{snapshot: validSnapshot(t)}
			request := validRequest()
			request.Method = method
			response := httptest.NewRecorder()
			mustHandler(t, redeemer).ServeHTTP(response, request)
			assertFailure(t, response, http.StatusMethodNotAllowed, true)
			if calls := redeemer.callCount(); calls != 0 {
				t.Fatalf("Redeem calls = %d, want 0", calls)
			}
		})
	}
}

func TestRequestBodyIsClosedAndBoundedBeforeRedemption(t *testing.T) {
	// These cases catch permissive decoding, body smuggling, and reads above the 128-byte limit.
	valid := `{"resumeId":"` + testResumeID + `","audience":"nuxt-print"}`
	t.Run("exact limit", func(t *testing.T) {
		request := requestWithBody(valid + strings.Repeat(" ", 128-len(valid)))
		response := httptest.NewRecorder()
		mustHandler(t, &recordingRedeemer{snapshot: validSnapshot(t)}).ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
	})

	tests := []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"null", "null"},
		{"array", "[]"},
		{"missing resume", `{"audience":"nuxt-print"}`},
		{"missing audience", `{"resumeId":"` + testResumeID + `"}`},
		{"extra field", valid[:len(valid)-1] + `,"extra":true}`},
		{"duplicate resume", valid[:len(valid)-1] + `,"resumeId":"` + testResumeID + `"}`},
		{"escaped duplicate resume", valid[:len(valid)-1] + `,"resume\u0049d":"` + testResumeID + `"}`},
		{"duplicate audience", valid[:len(valid)-1] + `,"audience":"nuxt-print"}`},
		{"wrong audience", strings.Replace(valid, "nuxt-print", "other", 1)},
		{"nil resume", strings.Replace(valid, testResumeID, uuid.Nil.String(), 1)},
		{"uppercase resume", strings.Replace(valid, testResumeID, strings.ToUpper(testResumeID), 1)},
		{"number resume", `{"resumeId":1,"audience":"nuxt-print"}`},
		{"trailing object", valid + `{}`},
		{"trailing scalar", valid + ` true`},
		{"malformed", valid[:len(valid)-1]},
		{"invalid utf8", valid[:1] + string([]byte{0xff}) + valid[2:]},
		{"129 bytes", valid + strings.Repeat(" ", 129-len(valid))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			redeemer := &recordingRedeemer{snapshot: validSnapshot(t)}
			request := requestWithBody(test.body)
			response := httptest.NewRecorder()
			mustHandler(t, redeemer).ServeHTTP(response, request)
			assertFailure(t, response, http.StatusNotFound, false)
			if calls := redeemer.callCount(); calls != 0 {
				t.Fatalf("Redeem calls = %d, want 0", calls)
			}
		})
	}

	t.Run("declared too large", func(t *testing.T) {
		redeemer := &recordingRedeemer{snapshot: validSnapshot(t)}
		request := validRequest()
		request.ContentLength = 129
		response := httptest.NewRecorder()
		mustHandler(t, redeemer).ServeHTTP(response, request)
		assertFailure(t, response, http.StatusNotFound, false)
		if calls := redeemer.callCount(); calls != 0 {
			t.Fatalf("Redeem calls = %d, want 0", calls)
		}
	})

	t.Run("understated length", func(t *testing.T) {
		redeemer := &recordingRedeemer{snapshot: validSnapshot(t)}
		request := validRequest()
		request.ContentLength--
		response := httptest.NewRecorder()
		mustHandler(t, redeemer).ServeHTTP(response, request)
		assertFailure(t, response, http.StatusNotFound, false)
		if calls := redeemer.callCount(); calls != 0 {
			t.Fatalf("Redeem calls = %d, want 0", calls)
		}
	})

	t.Run("read failure", func(t *testing.T) {
		redeemer := &recordingRedeemer{snapshot: validSnapshot(t)}
		request := validRequest()
		request.Body = io.NopCloser(failingReader{})
		response := httptest.NewRecorder()
		mustHandler(t, redeemer).ServeHTTP(response, request)
		assertFailure(t, response, http.StatusNotFound, false)
		if calls := redeemer.callCount(); calls != 0 {
			t.Fatalf("Redeem calls = %d, want 0", calls)
		}
	})
}

func TestQueueFailuresReplayAndPayloadBindingAreOpaque(t *testing.T) {
	// This fails if errors or frozen binding mismatches disclose state or bytes.
	base := validSnapshot(t)
	otherID := uuid.MustParse(testOtherID)
	mutations := []struct {
		name   string
		mutate func(*renderjob.Snapshot)
	}{
		{"nil snapshot resume", func(s *renderjob.Snapshot) { s.ResumeID = uuid.Nil }},
		{"wrong snapshot resume", func(s *renderjob.Snapshot) { s.ResumeID = otherID }},
		{"zero snapshot revision", func(s *renderjob.Snapshot) { s.Revision = 0 }},
		{"wrong snapshot revision", func(s *renderjob.Snapshot) { s.Revision++ }},
		{"wrong snapshot schema", func(s *renderjob.Snapshot) { s.SchemaVersion++ }},
		{"negative public generation", func(s *renderjob.Snapshot) { s.PublicGeneration = -1 }},
		{"generation differs from revision", func(s *renderjob.Snapshot) { s.PublicGeneration = s.Revision - 1 }},
		{"unbound public generation", func(s *renderjob.Snapshot) { s.PublicGeneration = s.Revision }},
		{"empty payload", func(s *renderjob.Snapshot) { s.Payload = nil }},
		{"oversized payload", func(s *renderjob.Snapshot) { s.Payload = bytes.Repeat([]byte("x"), printsnapshot.MaxEnvelopeBytes+1) }},
		{"malformed payload", func(s *renderjob.Snapshot) { s.Payload = []byte("not json") }},
		{"extra envelope key", func(s *renderjob.Snapshot) {
			s.Payload = append(append([]byte{}, s.Payload[:len(s.Payload)-1]...), []byte(`,"extra":true}`)...)
		}},
		{"duplicate envelope key", func(s *renderjob.Snapshot) {
			s.Payload = append(append([]byte{}, s.Payload[:len(s.Payload)-1]...), []byte(`,"revision":"7"}`)...)
		}},
		{"duplicate nested key", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`"schemaVersion":2`), []byte(`"schemaVersion":2,"schemaVersion":2`), 1)
		}},
		{"payload version mismatch", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`"version":1`), []byte(`"version":2`), 1)
		}},
		{"payload language malformed", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`"lng":"en"`), []byte(`"lng":"not_a_tag"`), 1)
		}},
		{"payload resume mismatch", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(testResumeID), []byte(testOtherID), 1)
		}},
		{"payload revision mismatch", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`"revision":"7"`), []byte(`"revision":"8"`), 1)
		}},
		{"payload schema mismatch", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`"schemaVersion":2`), []byte(`"schemaVersion":3`), 1)
		}},
		{"payload owner generation mismatch", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`"publicGeneration":null`), []byte(`"publicGeneration":"7"`), 1)
		}},
	}

	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			snapshot := base
			snapshot.Payload = append([]byte{}, base.Payload...)
			test.mutate(&snapshot)
			redeemer := &recordingRedeemer{snapshot: snapshot}
			response := httptest.NewRecorder()
			mustHandler(t, redeemer).ServeHTTP(response, validRequest())
			assertFailure(t, response, http.StatusNotFound, false)
			if calls := redeemer.callCount(); calls != 1 {
				t.Fatalf("Redeem calls = %d, want 1", calls)
			}
		})
	}

	t.Run("public generation matches frozen revision", func(t *testing.T) {
		snapshot := base
		snapshot.PublicGeneration = snapshot.Revision
		snapshot.Payload = bytes.Replace(snapshot.Payload, []byte(`"publicGeneration":null`), []byte(`"publicGeneration":"7"`), 1)
		response := httptest.NewRecorder()
		mustHandler(t, &recordingRedeemer{snapshot: snapshot}).ServeHTTP(response, validRequest())
		if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), snapshot.Payload) {
			t.Fatalf("public response = %d %q", response.Code, response.Body.String())
		}
	})

	t.Run("redeemer error", func(t *testing.T) {
		redeemer := &recordingRedeemer{err: errors.New("secret token and database detail")}
		response := httptest.NewRecorder()
		mustHandler(t, redeemer).ServeHTTP(response, validRequest())
		assertFailure(t, response, http.StatusNotFound, false)
		if strings.Contains(response.Body.String(), "secret") {
			t.Fatalf("response leaked dependency error: %q", response.Body.String())
		}
	})

	t.Run("replay", func(t *testing.T) {
		var calls atomic.Int64
		redeemer := redeemFunc(func(context.Context, renderjob.Redemption) (renderjob.Snapshot, error) {
			if calls.Add(1) == 1 {
				return base, nil
			}
			return renderjob.Snapshot{}, errors.New("not active")
		})
		handler := mustHandler(t, redeemer)
		first := httptest.NewRecorder()
		handler.ServeHTTP(first, validRequest())
		if first.Code != http.StatusOK {
			t.Fatalf("first status = %d", first.Code)
		}
		second := httptest.NewRecorder()
		handler.ServeHTTP(second, validRequest())
		assertFailure(t, second, http.StatusNotFound, false)
	})
}

func TestDeadlineAndCancellationArePassedAndJoined(t *testing.T) {
	// This fails if the adapter detaches redemption or omits the five-second request and socket bounds.
	t.Run("deadline", func(t *testing.T) {
		var contextDeadline time.Time
		redeemer := redeemFunc(func(ctx context.Context, _ renderjob.Redemption) (renderjob.Snapshot, error) {
			contextDeadline, _ = ctx.Deadline()
			return validSnapshot(t), nil
		})
		writer := newDeadlineRecorder()
		before := time.Now()
		mustHandler(t, redeemer).ServeHTTP(writer, validRequest())
		after := time.Now()
		if writer.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", writer.Code, http.StatusOK)
		}
		assertFiveSecondDeadline(t, "context", contextDeadline, before, after)
		assertRecordedDeadline(t, "read", writer.readDeadlines, before, after)
		assertRecordedDeadline(t, "write", writer.writeDeadlines, before, after)
	})

	t.Run("request cancellation", func(t *testing.T) {
		entered := make(chan struct{})
		exited := make(chan struct{})
		redeemer := redeemFunc(func(ctx context.Context, _ renderjob.Redemption) (renderjob.Snapshot, error) {
			close(entered)
			<-ctx.Done()
			close(exited)
			return renderjob.Snapshot{}, ctx.Err()
		})
		ctx, cancel := context.WithCancel(context.Background())
		request := validRequest().WithContext(ctx)
		response := httptest.NewRecorder()
		handler := mustHandler(t, redeemer)
		done := make(chan struct{})
		go func() {
			handler.ServeHTTP(response, request)
			close(done)
		}()
		<-entered
		cancel()
		select {
		case <-done:
			select {
			case <-exited:
			default:
				t.Fatal("handler returned before Redeem exited")
			}
		case <-time.After(time.Second):
			t.Fatal("handler did not join canceled Redeem")
		}
		assertFailure(t, response, http.StatusNotFound, false)
	})

	t.Run("redeemer that ignores cancellation remains joined", func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		redeemer := redeemFunc(func(context.Context, renderjob.Redemption) (renderjob.Snapshot, error) {
			close(entered)
			<-release
			return renderjob.Snapshot{}, errors.New("stopped")
		})
		ctx, cancel := context.WithCancel(context.Background())
		request := validRequest().WithContext(ctx)
		handler := mustHandler(t, redeemer)
		done := make(chan struct{})
		go func() {
			handler.ServeHTTP(httptest.NewRecorder(), request)
			close(done)
		}()
		<-entered
		cancel()
		select {
		case <-done:
			t.Fatal("handler detached a Redeem call that had not exited")
		case <-time.After(20 * time.Millisecond):
		}
		close(release)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("handler did not return after Redeem exited")
		}
	})
}

func TestRealHTTPContractAndSlowBodyDeadline(t *testing.T) {
	// This catches differences between direct handler calls and net/http's wire parsing.
	snapshot := validSnapshot(t)
	redeemer := &recordingRedeemer{snapshot: snapshot}
	server := httptest.NewServer(mustHandler(t, redeemer))
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "http://")
	body := validRequestBody()

	response := rawRequest(t, address, "POST /internal-render/print/redeem HTTP/1.1\r\n"+
		"Host: "+address+"\r\n"+
		"Authorization: RenderCapability "+testCapability+"\r\n"+
		"X-Render-Job-ID: "+testJobID+"\r\n"+
		"Content-Type: application/json\r\n"+
		"Content-Length: "+strconv.Itoa(len(body))+"\r\n"+
		"Connection: close\r\n\r\n"+body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d", response.StatusCode)
	}
	gotBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBody, snapshot.Payload) {
		t.Fatalf("POST body differs: %q", gotBody)
	}
	if got, want := response.Header.Get("Content-Length"), strconv.Itoa(len(snapshot.Payload)); got != want {
		t.Fatalf("POST Content-Length = %q, want %q", got, want)
	}

	response = rawRequest(t, address, "GET /internal-render/print/redeem HTTP/1.1\r\nHost: "+address+"\r\nConnection: close\r\n\r\n")
	if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != http.MethodPost {
		t.Fatalf("GET response = %d Allow %q", response.StatusCode, response.Header.Get("Allow"))
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}

	beforeCalls := redeemer.callCount()
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	partial := "POST /internal-render/print/redeem HTTP/1.1\r\n" +
		"Host: " + address + "\r\n" +
		"Authorization: RenderCapability " + testCapability + "\r\n" +
		"X-Render-Job-ID: " + testJobID + "\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: " + strconv.Itoa(len(body)) + "\r\n" +
		"Connection: close\r\n\r\n{"
	started := time.Now()
	if _, err := io.WriteString(connection, partial); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(started.Add(7 * time.Second)); err != nil {
		t.Fatal(err)
	}
	slowResponse, readErr := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})
	elapsed := time.Since(started)
	if elapsed < 4*time.Second || elapsed > 6500*time.Millisecond {
		t.Fatalf("slow body closed after %s, want about five seconds; error = %v", elapsed, readErr)
	}
	if readErr == nil {
		defer slowResponse.Body.Close()
		got, bodyErr := io.ReadAll(slowResponse.Body)
		if bodyErr != nil {
			t.Fatal(bodyErr)
		}
		if slowResponse.StatusCode != http.StatusNotFound || string(got) != notFoundBody {
			t.Fatalf("slow body response = %d %q", slowResponse.StatusCode, got)
		}
	}
	if calls := redeemer.callCount(); calls != beforeCalls {
		t.Fatalf("Redeem calls after slow body = %d, want %d", calls, beforeCalls)
	}
}

func TestNewRedeemHandlerRequiresRedeemer(t *testing.T) {
	if handler, err := NewRedeemHandler(nil); err == nil || handler != nil {
		t.Fatalf("NewRedeemHandler(nil) = (%v, %v), want nil, error", handler, err)
	}
	var pointer *recordingRedeemer
	if handler, err := NewRedeemHandler(pointer); err == nil || handler != nil {
		t.Fatalf("NewRedeemHandler(typed nil) = (%v, %v), want nil, error", handler, err)
	}
}

func validSnapshot(t *testing.T) renderjob.Snapshot {
	t.Helper()
	payload, err := printsnapshot.Marshal(printsnapshot.Envelope{
		Version:  1,
		ResumeID: testResumeID,
		Revision: "7",
		Lng:      "en",
		Document: publicresume.PublicResumeDocument{
			SchemaVersion: schema.CurrentVersion,
			PersonalDetails: publicresume.PublicPersonalDetails{
				FullName: "Ada Lovelace",
			},
			Content:       publicresume.PublicContent{},
			Customization: schema.Customization{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return renderjob.Snapshot{
		ResumeID:      uuid.MustParse(testResumeID),
		Revision:      7,
		SchemaVersion: schema.CurrentVersion,
		Payload:       payload,
	}
}

func validRequestBody() string {
	return `{"resumeId":"` + testResumeID + `","audience":"nuxt-print"}`
}

func requestWithBody(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/internal-render/print/redeem", strings.NewReader(body))
	request.Header.Set("Authorization", "RenderCapability "+testCapability)
	request.Header.Set("X-Render-Job-ID", testJobID)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func validRequest() *http.Request { return requestWithBody(validRequestBody()) }

func mustHandler(t *testing.T, redeemer Redeemer) http.Handler {
	t.Helper()
	handler, err := NewRedeemHandler(redeemer)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func assertFailure(t *testing.T, response *httptest.ResponseRecorder, status int, allow bool) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d", response.Code, status)
	}
	if got := response.Body.String(); got != notFoundBody {
		t.Fatalf("body = %q, want generic %q", got, notFoundBody)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got, want := response.Header().Get("Content-Length"), strconv.Itoa(len(notFoundBody)); got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}
	if got := response.Header().Get("Allow"); (got == http.MethodPost) != allow {
		t.Fatalf("Allow = %q, expected present = %v", got, allow)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	readDeadlines  []time.Time
	writeDeadlines []time.Time
}

func newDeadlineRecorder() *deadlineRecorder {
	return &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (r *deadlineRecorder) SetReadDeadline(deadline time.Time) error {
	r.readDeadlines = append(r.readDeadlines, deadline)
	return nil
}

func (r *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.writeDeadlines = append(r.writeDeadlines, deadline)
	return nil
}

func assertRecordedDeadline(t *testing.T, name string, got []time.Time, before, after time.Time) {
	t.Helper()
	if len(got) != 2 || !got[1].IsZero() {
		t.Fatalf("%s deadlines = %v, want one deadline then reset", name, got)
	}
	assertFiveSecondDeadline(t, name, got[0], before, after)
}

func assertFiveSecondDeadline(t *testing.T, name string, got, before, after time.Time) {
	t.Helper()
	minimum := before.Add(4900 * time.Millisecond)
	maximum := after.Add(5100 * time.Millisecond)
	if got.Before(minimum) || got.After(maximum) {
		t.Fatalf("%s deadline = %s, want five seconds after handler start", name, got)
	}
}

func rawRequest(t *testing.T, address, request string) *http.Response {
	t.Helper()
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(connection, request); err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{})
	if err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	response.Body = struct {
		io.Reader
		io.Closer
	}{Reader: response.Body, Closer: closeBoth{response.Body, connection}}
	return response
}

type closeBoth struct {
	first  io.Closer
	second io.Closer
}

func (c closeBoth) Close() error { return errors.Join(c.first.Close(), c.second.Close()) }
