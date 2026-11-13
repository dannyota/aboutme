package routetable

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/publicapi"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

const stalledPhotoPath = "/api/v1/public/resumes/ada-lovelace/photo"

// TestRouteTable_RevocationEndsAPublicWriteStalledBehindCaddy drives the
// real Go public photo route through a real Caddy with an HTTP/2 viewer that
// set its stream window to 0 and never updates it. Caddy stops reading the
// origin once its own write to that stream blocks, so the Go handler's write
// stalls mid-body while it holds the resume's lease. A revoking transition
// must still drain within the five-second bound of
// docs/adr/0010-public-artifact-revocation.md, and the viewer must never
// receive the whole old photo: its stream ends with a reset or a closed
// connection, either while its window is still 0 or, once it opens the
// window, after what was in flight.
func TestRouteTable_RevocationEndsAPublicWriteStalledBehindCaddy(t *testing.T) {
	bin := caddyBinary(t)
	origin := newStalledPhotoOrigin(t)
	webStub := newStubBackend(t, "web")
	sitePort := findFreePort(t)
	content := withH2C(t, adaptedCaddyfile(t, sitePort, tcpPort(t, origin.server.Listener.Addr()), tcpPort(t, webStub.Listener.Addr())))
	configPath := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("writing adapted Caddyfile to %s: %v", configPath, err)
	}
	startCaddy(t, bin, configPath)
	address := fmt.Sprintf("127.0.0.1:%d", sitePort)
	waitForCaddyReady(t, "http://"+address, 10*time.Second)

	client := dialH2C(t, address)
	client.get(t, address, stalledPhotoPath)
	if headers := client.awaitHeaders(t); headers.status != "200" || headers.endStream {
		t.Fatalf("photo response headers = %s, want a 200 with a body to follow", headers)
	}
	stalledAt := origin.awaitStall(t)

	transition, err := origin.coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{
		{ID: origin.resumeID, ExpectedRevision: 1, Class: publicstate.Revoking},
	}})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := transition.Close(context.Background(), started.Add(5*time.Second)); err != nil {
		t.Fatalf("revocation did not drain the stalled photo write within five seconds: %v", err)
	}
	drained := time.Since(started)
	select {
	case <-origin.done:
	case <-time.After(time.Second):
		t.Fatal("the photo handler still runs after its lease drained")
	}
	if written := origin.written.Load(); written >= int64(len(origin.photo)) {
		t.Fatalf("the origin wrote the whole photo (%d bytes): the write never stalled", written)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}

	// Check for a reset while the window is still 0 first. Caddy's copy loop
	// is blocked writing to this stream, so it may not see the origin's
	// failure until the viewer reads; then open the window and read on.
	received := 0
	end, ended := client.endWhileWindowZero(t, time.Second)
	if ended {
		t.Logf("stream ended while the window was 0: %s", end)
	} else {
		t.Log("no reset within 1s while the window was 0: Caddy still holds the stream; opening the window")
		client.openWindow(t)
		received, end = client.readStream(t)
	}
	if received >= len(origin.photo) || (end.kind == h2Data && end.endStream) {
		t.Fatalf("the viewer received %d of %d bytes ending with %s, want a truncated stream that never ends normally", received, len(origin.photo), end)
	}
	t.Logf("origin stalled at %d of %d bytes; revocation drained in %s; viewer then got %d bytes and %s", stalledAt, len(origin.photo), drained, received, end)
}

// withH2C turns on cleartext HTTP/2 beside HTTP/1.1 in the adapted
// Caddyfile's global options, so the hand-written client can speak HTTP/2
// without TLS. Production serves HTTP/2 over TLS
// (deploy/caddy/production/edges/direct.global); stream flow control, which
// this test exercises, is the same in both.
func withH2C(t *testing.T, content string) string {
	t.Helper()
	const globalOption = "\tadmin off\n"
	if got := strings.Count(content, globalOption); got != 1 {
		t.Fatalf("adapted Caddyfile: found %d occurrence(s) of %q, want exactly 1", got, globalOption)
	}
	return strings.Replace(content, globalOption, globalOption+"\tservers {\n\t\tprotocols h1 h2c\n\t}\n", 1)
}

// stalledPhotoOrigin is a real Go public route handler over one live resume
// whose photo is at its 2 MiB limit. It counts the body bytes the handler
// has handed to its connection and closes done when the photo handler
// returns.
type stalledPhotoOrigin struct {
	server      *httptest.Server
	coordinator *publicstate.Coordinator
	resumeID    uuid.UUID
	photo       []byte
	written     atomic.Int64
	done        chan struct{}
	doneOnce    sync.Once
}

func newStalledPhotoOrigin(t *testing.T) *stalledPhotoOrigin {
	t.Helper()
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	publicOrigin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	slug, lng, name := "ada-lovelace", "en", "Ada"
	document := schema.Resume{
		SchemaVersion: schema.CurrentVersion, Content: map[string]schema.Section{},
		PersonalDetails: schema.PersonalDetails{FullName: &name, Photo: &schema.Photo{Key: "private/photo.png"}},
	}
	row := store.Resume{
		ID: uuid.New(), Slug: &slug, Live: true, SEOGeoEnabled: true, Revision: 1, Lng: &lng, SchemaVersion: int32(schema.CurrentVersion),
		PersonalDetails: mustJSON(t, document.PersonalDetails), Content: mustJSON(t, document.Content), Customization: mustJSON(t, document.Customization),
	}
	origin := &stalledPhotoOrigin{coordinator: coordinator, resumeID: row.ID, photo: bytes.Repeat([]byte{0x89}, media.MaxObjectBytes), done: make(chan struct{})}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: photoResumeStore{row: row}, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator,
		Media: photoBackend{body: origin.photo}, Origin: publicOrigin,
	})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	service, err := publicapi.NewService(publicapi.ServiceDependencies{
		Reader: reader, DiscoveryStore: unusedDiscovery{}, Coordinator: coordinator, Cache: cache,
		Renderer: directrender.New(renderOrigin, nil), PublicOrigin: publicOrigin, AppDigest: "sha256:app", RendererDigest: "sha256:renderer",
	})
	if err != nil {
		t.Fatal(err)
	}
	origin.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == stalledPhotoPath {
			defer origin.doneOnce.Do(func() { close(origin.done) })
		}
		service.ServeHTTP(&progressWriter{ResponseWriter: w, written: &origin.written}, request)
	}))
	origin.server.Listener = smallSendBufferListener{Listener: origin.server.Listener}
	origin.server.Start()
	t.Cleanup(origin.server.Close)
	return origin
}

// awaitStall waits until the photo handler has stopped handing bytes to its
// connection short of the whole body, and returns how many it handed over.
// It fails if the handler returns first: the body then fit in the buffers
// between the origin and the viewer, and nothing stalled.
func (o *stalledPhotoOrigin) awaitStall(t *testing.T) int64 {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	last, steady := int64(-1), 0
	for time.Now().Before(deadline) {
		select {
		case <-o.done:
			t.Fatalf("the photo handler returned after %d of %d bytes, before its write stalled", o.written.Load(), len(o.photo))
		case <-time.After(50 * time.Millisecond):
		}
		written := o.written.Load()
		if written != last || written == 0 || written >= int64(len(o.photo)) {
			last, steady = written, 0
			continue
		}
		if steady++; steady == 6 {
			return written
		}
	}
	t.Fatalf("the photo write never stalled; it stands at %d of %d bytes", o.written.Load(), len(o.photo))
	return 0
}

// progressWriter hands a body to the connection in 16 KiB pieces and counts
// the bytes accepted, so the test sees where a write stalls. Unwrap lets
// http.ResponseController reach the connection's write deadline.
type progressWriter struct {
	http.ResponseWriter
	written *atomic.Int64
}

func (w *progressWriter) Write(body []byte) (int, error) {
	total := 0
	for len(body) > 0 {
		piece := body[:min(len(body), 16<<10)]
		n, err := w.ResponseWriter.Write(piece)
		total += n
		w.written.Add(int64(n))
		if err != nil {
			return total, err
		}
		body = body[len(piece):]
	}
	return total, nil
}

func (w *progressWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// smallSendBufferListener gives each accepted connection a 16 KiB send
// buffer. Linux grows loopback send buffers to megabytes, enough to hold the
// whole 2 MiB photo; a small one makes the stall depend only on the viewer's
// window, as a real network path does.
type smallSendBufferListener struct{ net.Listener }

func (l smallSendBufferListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return conn, nil
	}
	if bufferErr := tcp.SetWriteBuffer(16 << 10); bufferErr != nil {
		if closeErr := conn.Close(); closeErr != nil {
			return nil, fmt.Errorf("%w; closing: %w", bufferErr, closeErr)
		}
		return nil, bufferErr
	}
	return conn, nil
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// photoResumeStore reads one live resume. The embedded nil interface makes
// every other query panic, so an unexpected read fails the test loudly.
type photoResumeStore struct {
	store.PublicReadQueries
	row store.Resume
}

func (s photoResumeStore) GetPublicResumeBySlug(_ context.Context, slug string) (store.GetPublicResumeBySlugRow, error) {
	if s.row.Slug == nil || *s.row.Slug != slug {
		return store.GetPublicResumeBySlugRow{}, fmt.Errorf("unexpected slug %q", slug)
	}
	return store.GetPublicResumeBySlugRow{Resume: s.row, ProjectLinks: json.RawMessage(`[]`)}, nil
}

// photoBackend returns body as a PNG for every key.
type photoBackend struct {
	media.Backend
	body []byte
}

func (b photoBackend) Get(context.Context, string) (io.ReadCloser, string, error) {
	return io.NopCloser(bytes.NewReader(b.body)), "image/png", nil
}

// unusedDiscovery satisfies the service's discovery dependency; this test
// never requests a discovery document.
type unusedDiscovery struct{ store.PublicDiscoveryQueries }
