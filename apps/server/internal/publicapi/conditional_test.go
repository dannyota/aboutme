package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

type admissionStore struct{ row store.Resume }

func (s admissionStore) GetPublicState(context.Context) (store.PublicState, error) {
	return store.PublicState{}, errors.New("unused")
}
func (s admissionStore) GetPublicResumeBySlug(context.Context, string) (store.Resume, error) {
	return s.row, nil
}
func (s admissionStore) GetPublicResumeByOwner(context.Context, store.GetPublicResumeByOwnerParams) (store.Resume, error) {
	return store.Resume{}, errors.New("unused")
}
func (s admissionStore) ListEligiblePublicSlugs(context.Context) ([]string, error) {
	return nil, errors.New("unused")
}

func TestConditionalStrictSingleton(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []string
		valid  bool
	}{
		{"empty strong", []string{`""`}, true},
		{"strong", []string{`"abc"`}, true},
		{"weak", []string{`W/"abc"`}, false},
		{"wildcard", []string{"*"}, false},
		{"list", []string{`"abc", "def"`}, false},
		{"leading whitespace", []string{` "abc"`}, false},
		{"trailing whitespace", []string{`"abc" `}, false},
		{"unclosed", []string{`"abc`}, false},
		{"internal quote", []string{`"a"b"`}, false},
		{"control", []string{"\"a\x1fb\""}, false},
		{"repeated", []string{`"abc"`, `"def"`}, false},
	} {
		h := http.Header{"If-None-Match": test.values}
		_, present, valid := parseIfNoneMatch(h)
		if !present || valid != test.valid {
			t.Fatalf("%s: present=%v valid=%v", test.name, present, valid)
		}
	}
}

func TestCacheAndConditionalHitAcquireLeaseFirst(t *testing.T) {
	slug, lng, name := "ada", "en", "Ada"
	doc := schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{FullName: &name}, Content: map[string]schema.Section{}}
	pd, _ := json.Marshal(doc.PersonalDetails)
	content, _ := json.Marshal(doc.Content)
	customization, _ := json.Marshal(doc.Customization)
	id := uuid.New()
	row := store.Resume{ID: id, Slug: &slug, Live: true, Revision: 1, Lng: &lng, SchemaVersion: int32(schema.CurrentVersion), PersonalDetails: pd, Content: content, Customization: customization}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	origin, _ := publicresume.ParsePublicOrigin("https://resume.example", "production")
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{Store: admissionStore{row: row}, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 1, Class: publicstate.NonDraining}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(1, time.Second, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	cacheTouched, conditionalTouched := false, false
	if _, lease, err := reader.ReadResume(context.Background(), slug, publicstate.RepresentationJSON); err == nil {
		defer lease.Release()
		cacheTouched = true
		cache.Get(publiccache.Key{ResumeID: id, Generation: 1})
		conditionalTouched = true
		parseIfNoneMatch(http.Header{"If-None-Match": {`""`}})
	}
	if cacheTouched || conditionalTouched {
		t.Fatalf("cache=%v conditional=%v before admission", cacheTouched, conditionalTouched)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
	_, lease, err := reader.ReadResume(context.Background(), slug, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatal(err)
	}
	draining, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 1, Class: publicstate.Revoking}}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- draining.Close(context.Background(), time.Now().Add(time.Second)) }()
	select {
	case <-lease.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("revocation did not cancel active lease")
	}
	select {
	case err := <-done:
		t.Fatalf("drain returned before response release: %v", err)
	default:
	}
	lease.Release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := draining.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestConditionalInvalidGrammarHasExactNoValidator400(t *testing.T) {
	selected, err := NewSelectedResponse(http.StatusOK, "image/png", "no-cache, must-revalidate", []byte("body"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]string{{`W/"x"`}, {"*"}, {`"x", "y"`}, {` "x"`}, {`"x" `}, {`"x`}, {`"x"`, `"y"`}} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header["If-None-Match"] = values
		w := httptest.NewRecorder()
		selected.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest || w.Header().Get("ETag") != "" || w.Body.String() != "{\"error\":{\"code\":\"request_invalid\",\"message\":\"request is invalid\"}}\n" {
			t.Fatalf("values=%q response=%d %#v %q", values, w.Code, w.Header(), w.Body.String())
		}
	}
}
