package publicapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

// A resume with discovery off must keep search engines out of every public
// representation, not only its HTML. The header comes from the current state on
// each request, so toggling discovery at the same revision, a cache hit, never
// serves a stale header.

const noindex = "noindex, noarchive"

type robotsCase struct {
	name    string
	handler http.Handler
	path    string
}

// robotsHeaders returns the X-Robots-Tag of a GET, a HEAD, and a conditional
// GET (304) for path.
func robotsHeaders(t *testing.T, handler http.Handler, path string) [3]string {
	t.Helper()
	var got [3]string
	get := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	get.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, get)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %q", path, response.Code, response.Body.String())
	}
	got[0] = response.Header().Get("X-Robots-Tag")
	head := httptest.NewRequestWithContext(t.Context(), http.MethodHead, path, nil)
	head.RemoteAddr = "192.0.2.1:1234"
	headResponse := httptest.NewRecorder()
	handler.ServeHTTP(headResponse, head)
	got[1] = headResponse.Header().Get("X-Robots-Tag")
	conditional := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	conditional.RemoteAddr = "192.0.2.1:1234"
	conditional.Header.Set("If-None-Match", response.Header().Get("ETag"))
	conditionalResponse := httptest.NewRecorder()
	handler.ServeHTTP(conditionalResponse, conditional)
	if conditionalResponse.Code != http.StatusNotModified {
		t.Fatalf("conditional GET %s = %d", path, conditionalResponse.Code)
	}
	got[2] = conditionalResponse.Header().Get("X-Robots-Tag")
	return got
}

func TestPublicRepresentationsFollowDiscoveryForNoindex(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC) }
	_, _, coordinator, backing := publicServiceReaderWithStore(t)
	backing.row.DownloadEnabled = true
	var personal schema.PersonalDetails
	if err := json.Unmarshal(backing.row.PersonalDetails, &personal); err != nil {
		t.Fatal(err)
	}
	personal.Photo = &schema.Photo{Key: "private/photo.png"}
	encoded, err := json.Marshal(personal)
	if err != nil {
		t.Fatal(err)
	}
	backing.row.PersonalDetails = encoded
	var photo bytes.Buffer
	if encodeErr := png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 1, 1))); encodeErr != nil {
		t.Fatal(encodeErr)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: backing, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator,
		Media: &artifactPhotoBackend{body: photo.Bytes(), contentType: "image/png"}, Origin: mustPublicOrigin(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(16, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, prepareErr := request.Prepare(ctx)
		if prepareErr != nil {
			return renderjob.Result{}, prepareErr
		}
		body := []byte("%PDF-1.7\npublic")
		if request.Format == renderjob.PNG {
			body = []byte("\x89PNG\r\npublic")
		}
		return renderjob.Result{Bytes: body, Digest: sha256.Sum256(body), Revision: snapshot.Revision}, nil
	})
	artifacts, err := newArtifactHandlers(ArtifactDependencies{
		Reader: reader, Cache: cache, Queue: queue, AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Clock: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{}
	cases := []robotsCase{
		{"json", service.newJSONHandler(reader, cache, "sha256:app"), "/api/v1/public/resumes/ada-lovelace"},
		{"photo", service.newPhotoHandler(reader, cache, "sha256:app"), "/api/v1/public/resumes/ada-lovelace/photo"},
		{"pdf", artifacts.pdf, "/api/v1/public/resumes/ada-lovelace/pdf"},
		{"og.png", artifacts.png, "/api/v1/public/resumes/ada-lovelace/og.png"},
	}
	// The revision never changes across these toggles, so the second and
	// later rounds are cache hits on the same key.
	for _, round := range []struct {
		discoverable bool
		want         string
	}{{true, ""}, {false, noindex}, {true, ""}, {false, noindex}} {
		backing.row.SEOGeoEnabled = round.discoverable
		for _, test := range cases {
			for i, got := range robotsHeaders(t, test.handler, test.path) {
				if got != round.want {
					t.Errorf("%s discovery=%t request %d: X-Robots-Tag = %q, want %q", test.name, round.discoverable, i, got, round.want)
				}
			}
		}
	}
}
