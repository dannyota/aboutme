package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
)

// The recorded public attestation for ghcr.io/dannyota/aboutme-server
// v0.5.21 (verification.md, "What the release workflow produces").
const (
	recordedDigest = "sha256:d1c33a0a4c1cf1e4bdc2e868a457ae6f7dc7949886baf8b9697dafe501e6945c"
	recordedCommit = "d73f404321d81175ec786bc79289cca06ce4bb5b"
	recordedBuild  = "https://github.com/dannyota/aboutme/actions/runs/36218940587/attempts/1"
	recordedIndex  = 2965228543
	recordedSigner = "https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/v0.5.21"
	otherDigest    = "sha256:0000000000000000000000000000000000000000000000000000000000000001"
)

var fixedNow = time.Date(2026, 10, 2, 3, 14, 5, 0, time.UTC)

func trustedRoot(t *testing.T) root.TrustedMaterial {
	t.Helper()
	b, err := os.ReadFile("../../testdata/verify/trusted_root.json")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := root.NewTrustedRootFromJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func recordedBundle(t *testing.T) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("../../testdata/verify/api-server-v0.5.21-provenance.json")
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Attestations []struct {
			Bundle json.RawMessage `json:"bundle"`
		} `json:"attestations"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Attestations[0].Bundle
}

func apiBody(bundles ...json.RawMessage) string {
	var parts []string
	for _, b := range bundles {
		parts = append(parts, fmt.Sprintf(`{"bundle":%s,"repository_id":1,"initiator":"user"}`, b))
	}
	return `{"attestations":[` + strings.Join(parts, ",") + `]}`
}

// fakeGitHub serves the attestations API and a registry. api maps a
// predicate_type filter to a status and body; registry maps a request path
// to a status and body. Unlisted paths answer 404.
type fakeGitHub struct {
	api      map[string]response
	registry map[string]response
	calls    atomic.Int64
}

type response struct {
	status int
	body   string
}

func (f *fakeGitHub) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		var resp response
		var ok bool
		if strings.HasPrefix(r.URL.Path, "/repos/dannyota/aboutme/attestations/") {
			resp, ok = f.api[r.URL.Query().Get("predicate_type")]
		} else if r.URL.Path == "/token" {
			resp, ok = response{200, `{"token":"anonymous"}`}, true
		} else {
			resp, ok = f.registry[r.URL.Path]
		}
		if !ok {
			resp = response{http.StatusNotFound, `{"message":"Not Found"}`}
		}
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body)) //nolint:errcheck // test server
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newVerifier(t *testing.T, srv *httptest.Server, cache Cache) *Verifier {
	t.Helper()
	if cache == nil {
		cache = NewMemoryCache()
	}
	v, err := New(context.Background(), Options{
		HTTPClient:      srv.Client(),
		Cache:           cache,
		Now:             func() time.Time { return fixedNow },
		GitHubAPI:       srv.URL,
		Registry:        srv.URL,
		TrustedMaterial: trustedRoot(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRecordedProvenanceVerifies(t *testing.T) {
	f := &fakeGitHub{api: map[string]response{"provenance": {200, apiBody(recordedBundle(t))}}}
	cache := NewMemoryCache()
	ev := newVerifier(t, f.server(t), cache).Check(context.Background(), recordedDigest)

	p := ev.Provenance
	if p.Status != Verified {
		t.Fatalf("provenance status = %q, want verified", p.Status)
	}
	if p.Version != "v0.5.21" || p.Commit != recordedCommit || p.SignerWorkflow != recordedSigner ||
		p.BuildURL != recordedBuild || p.LogIndex != recordedIndex || !p.CheckedAt.Equal(fixedNow) {
		t.Errorf("provenance = %+v", p)
	}
	if len(p.Subjects) != 1 || p.Subjects[0] != "ghcr.io/dannyota/aboutme-server" {
		t.Errorf("subjects = %v", p.Subjects)
	}
	// v0.5.21 predates SBOM attestations: GitHub and every registry
	// repository answer, and none holds one.
	if ev.SBOM.Status != NotFound {
		t.Errorf("sbom status = %q, want not_found", ev.SBOM.Status)
	}
	if _, found, err := cache.Get(context.Background(), recordedDigest); err != nil || !found {
		t.Error("a finished result was not cached")
	}
}

func TestBundleForAnotherDigestIsInvalid(t *testing.T) {
	f := &fakeGitHub{api: map[string]response{"provenance": {200, apiBody(recordedBundle(t))}}}
	ev := newVerifier(t, f.server(t), nil).Check(context.Background(), otherDigest)
	if ev.Provenance.Status != Invalid {
		t.Fatalf("status = %q, want invalid", ev.Provenance.Status)
	}
	if ev.Provenance.Version != "" || ev.Provenance.Commit != "" || len(ev.Provenance.Subjects) != 0 {
		t.Errorf("an invalid result carries claims: %+v", ev.Provenance)
	}
}

func TestProvenanceBundleNeverPassesAsSBOM(t *testing.T) {
	b := recordedBundle(t)
	f := &fakeGitHub{api: map[string]response{"provenance": {200, apiBody(b)}, "spdx": {200, apiBody(b)}}}
	ev := newVerifier(t, f.server(t), nil).Check(context.Background(), recordedDigest)
	if ev.SBOM.Status != Invalid {
		t.Fatalf("sbom status = %q, want invalid", ev.SBOM.Status)
	}
}

func TestTamperedBundleIsInvalid(t *testing.T) {
	var b map[string]any
	if err := json.Unmarshal(recordedBundle(t), &b); err != nil {
		t.Fatal(err)
	}
	env, ok := b["dsseEnvelope"].(map[string]any)
	if !ok {
		t.Fatal("no dsseEnvelope")
	}
	sigs, ok := env["signatures"].([]any)
	if !ok || len(sigs) == 0 {
		t.Fatal("no signatures")
	}
	sig, ok := sigs[0].(map[string]any)
	if !ok {
		t.Fatal("malformed signature")
	}
	s, ok := sig["sig"].(string)
	if !ok || len(s) < 8 {
		t.Fatal("malformed signature value")
	}
	flip := "A"
	if s[4] == 'A' {
		flip = "B"
	}
	sig["sig"] = s[:4] + flip + s[5:]
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGitHub{api: map[string]response{"provenance": {200, apiBody(raw)}}}
	ev := newVerifier(t, f.server(t), nil).Check(context.Background(), recordedDigest)
	if ev.Provenance.Status != Invalid {
		t.Fatalf("status = %q, want invalid", ev.Provenance.Status)
	}
}

func TestRateLimitedAPIFallsBackToRegistry(t *testing.T) {
	bundle := []byte(recordedBundle(t))
	blobDigest := digestOf(bundle)
	manifest := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",`+
		`"artifactType":%q,"layers":[{"mediaType":%q,"digest":%q,"size":%d}]}`,
		bundleMediaType, bundleMediaType, blobDigest, len(bundle))
	manifestDigest := digestOf([]byte(manifest))
	index := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[`+
		`{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":%q,"size":%d,"artifactType":%q,`+
		`"annotations":{"dev.sigstore.bundle.predicateType":%q}}]}`,
		manifestDigest, len(manifest), bundleMediaType, ProvenancePredicate)
	repo := "/v2/dannyota/aboutme-server"
	f := &fakeGitHub{
		api: map[string]response{
			"provenance": {http.StatusForbidden, `{"message":"API rate limit exceeded"}`},
			"spdx":       {http.StatusTooManyRequests, `{}`},
		},
		registry: map[string]response{
			repo + "/manifests/sha256-" + strings.TrimPrefix(recordedDigest, "sha256:"): {200, index},
			repo + "/manifests/" + manifestDigest:                                       {200, manifest},
			repo + "/blobs/" + blobDigest:                                               {200, string(bundle)},
		},
	}
	ev := newVerifier(t, f.server(t), nil).Check(context.Background(), recordedDigest)
	if ev.Provenance.Status != Verified || ev.Provenance.Version != "v0.5.21" {
		t.Fatalf("provenance = %+v, want verified v0.5.21", ev.Provenance)
	}
	// GitHub never answered for the SBOM and the registry holds none, so its
	// absence is not proven: unchecked, not not_found.
	if ev.SBOM.Status != Unchecked {
		t.Errorf("sbom status = %q, want unchecked", ev.SBOM.Status)
	}
}

func TestRateLimitedAndAbsentFromRegistryIsUnchecked(t *testing.T) {
	f := &fakeGitHub{api: map[string]response{
		"provenance": {http.StatusForbidden, `{"message":"API rate limit exceeded"}`},
		"spdx":       {http.StatusForbidden, `{"message":"API rate limit exceeded"}`},
	}}
	cache := NewMemoryCache()
	ev := newVerifier(t, f.server(t), cache).Check(context.Background(), recordedDigest)
	if ev.Provenance.Status != Unchecked || ev.SBOM.Status != Unchecked {
		t.Fatalf("evidence = %+v, want both unchecked", ev)
	}
	if _, found, err := cache.Get(context.Background(), recordedDigest); err != nil || found {
		t.Error("an unproven absence was cached")
	}
}

func TestSecondTransparencyLogEntryIsInvalid(t *testing.T) {
	var b map[string]any
	if err := json.Unmarshal(recordedBundle(t), &b); err != nil {
		t.Fatal(err)
	}
	vm, ok := b["verificationMaterial"].(map[string]any)
	if !ok {
		t.Fatal("no verificationMaterial")
	}
	entries, ok := vm["tlogEntries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatal("want one recorded tlog entry")
	}
	// A second entry from a log the trust root does not know: sigstore-go
	// skips it and still passes on the first, so only the one-entry rule
	// stops a published index from pointing at an unchecked entry.
	first, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatal("malformed tlog entry")
	}
	extraRaw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var extra map[string]any
	if err = json.Unmarshal(extraRaw, &extra); err != nil {
		t.Fatal(err)
	}
	extra["logIndex"] = "1"
	extra["logId"] = map[string]any{"keyId": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	vm["tlogEntries"] = []any{extra, first}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGitHub{api: map[string]response{"provenance": {200, apiBody(raw)}}}
	ev := newVerifier(t, f.server(t), nil).Check(context.Background(), recordedDigest)
	if ev.Provenance.Status != Invalid {
		t.Fatalf("status = %q, want invalid", ev.Provenance.Status)
	}
}

func TestRegistryBlobMustMatchItsDigest(t *testing.T) {
	bundle := []byte(recordedBundle(t))
	blobDigest := digestOf(bundle)
	manifest := fmt.Sprintf(`{"layers":[{"mediaType":%q,"digest":%q}]}`, bundleMediaType, blobDigest)
	manifestDigest := digestOf([]byte(manifest))
	index := fmt.Sprintf(`{"manifests":[{"digest":%q,"artifactType":%q,"annotations":{"dev.sigstore.bundle.predicateType":%q}}]}`,
		manifestDigest, bundleMediaType, ProvenancePredicate)
	repo := "/v2/dannyota/aboutme-server"
	f := &fakeGitHub{
		api: map[string]response{"provenance": {http.StatusForbidden, `{}`}},
		registry: map[string]response{
			repo + "/manifests/sha256-" + strings.TrimPrefix(recordedDigest, "sha256:"): {200, index},
			repo + "/manifests/" + manifestDigest:                                       {200, manifest},
			repo + "/blobs/" + blobDigest:                                               {200, string(bundle) + " "},
		},
	}
	ev := newVerifier(t, f.server(t), nil).Check(context.Background(), recordedDigest)
	if ev.Provenance.Status != Unchecked {
		t.Fatalf("status = %q, want unchecked", ev.Provenance.Status)
	}
}

func TestUnansweredChecksAreUncheckedAndNotCached(t *testing.T) {
	cases := map[string]*fakeGitHub{
		"api error": {api: map[string]response{"provenance": {500, `{}`}, "spdx": {500, `{}`}}},
		"api empty, registry error": {
			api:      map[string]response{"provenance": {404, `{}`}, "spdx": {404, `{}`}},
			registry: map[string]response{"/v2/dannyota/aboutme-web/manifests/sha256-" + strings.TrimPrefix(recordedDigest, "sha256:"): {500, ``}},
		},
		"api malformed": {api: map[string]response{"provenance": {200, `{"attestations":`}, "spdx": {200, `nope`}}},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			cache := NewMemoryCache()
			ev := newVerifier(t, f.server(t), cache).Check(context.Background(), recordedDigest)
			if ev.Provenance.Status != Unchecked || !ev.Provenance.CheckedAt.IsZero() {
				t.Errorf("provenance = %+v, want unchecked with no checked_at", ev.Provenance)
			}
			if _, found, err := cache.Get(context.Background(), recordedDigest); err != nil || found {
				t.Error("an unchecked result was cached")
			}
		})
	}
}

func TestMalformedDigestIsUncheckedWithoutRequests(t *testing.T) {
	f := &fakeGitHub{}
	v := newVerifier(t, f.server(t), nil)
	for _, d := range []string{"", "sha256:ABC", "sha512:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("a", 63)} {
		if ev := v.Check(context.Background(), d); ev.Provenance.Status != Unchecked || ev.SBOM.Status != Unchecked {
			t.Errorf("%q: %+v", d, ev)
		}
	}
	if n := f.calls.Load(); n != 0 {
		t.Errorf("%d requests for malformed digests", n)
	}
}

func TestCacheReusesFreshResultsOnly(t *testing.T) {
	f := &fakeGitHub{api: map[string]response{"provenance": {200, apiBody(recordedBundle(t))}}}
	srv := f.server(t)
	cache := NewMemoryCache()
	cached := Evidence{
		Digest:     recordedDigest,
		Provenance: Provenance{Status: Invalid, CheckedAt: fixedNow.Add(-CacheTTL + time.Minute)},
		SBOM:       SBOM{Status: NotFound, CheckedAt: fixedNow.Add(-CacheTTL + time.Minute)},
	}
	if err := cache.Put(context.Background(), cached); err != nil {
		t.Fatal(err)
	}
	ev := newVerifier(t, srv, cache).Check(context.Background(), recordedDigest)
	if ev.Provenance.Status != Invalid || f.calls.Load() != 0 {
		t.Fatalf("a fresh cached result was not reused: %+v, %d calls", ev.Provenance, f.calls.Load())
	}

	cached.Provenance.CheckedAt = fixedNow.Add(-CacheTTL - time.Minute)
	if err := cache.Put(context.Background(), cached); err != nil {
		t.Fatal(err)
	}
	ev = newVerifier(t, srv, cache).Check(context.Background(), recordedDigest)
	if ev.Provenance.Status != Verified {
		t.Fatalf("an expired cached result was reused: %+v", ev.Provenance)
	}
}

func TestCertificatePolicy(t *testing.T) {
	good := certificate.Summary{
		SubjectAlternativeName: recordedSigner,
		Extensions: certificate.Extensions{
			Issuer:                 Issuer,
			BuildSignerURI:         recordedSigner,
			RunnerEnvironment:      "github-hosted",
			SourceRepositoryURI:    SourceRepository,
			SourceRepositoryDigest: recordedCommit,
			SourceRepositoryRef:    "refs/tags/v0.5.21",
			RunInvocationURI:       recordedBuild,
		},
	}
	got, err := certificateFacts(good)
	if err != nil {
		t.Fatalf("good certificate: %v", err)
	}
	if got.version != "v0.5.21" || got.commit != recordedCommit || got.buildURL != recordedBuild || got.san != recordedSigner {
		t.Errorf("facts = %+v", got)
	}

	bad := map[string]func(*certificate.Summary){
		"self-hosted runner": func(c *certificate.Summary) { c.RunnerEnvironment = "self-hosted" },
		"other workflow": func(c *certificate.Summary) {
			c.SubjectAlternativeName = strings.Replace(recordedSigner, "release-images", "ci", 1)
		},
		"branch ref": func(c *certificate.Summary) {
			c.SubjectAlternativeName = strings.Replace(recordedSigner, "refs/tags/v0.5.21", "refs/heads/main", 1)
		},
		"loose tag":           func(c *certificate.Summary) { c.SubjectAlternativeName = recordedSigner + "-rc1" },
		"fork":                func(c *certificate.Summary) { c.SourceRepositoryURI = "https://github.com/someone/aboutme" },
		"other issuer":        func(c *certificate.Summary) { c.Issuer = "https://accounts.google.com" },
		"source ref mismatch": func(c *certificate.Summary) { c.SourceRepositoryRef = "refs/tags/v0.5.20" },
		"reusable signer": func(c *certificate.Summary) {
			c.BuildSignerURI = "https://github.com/other/wf/.github/workflows/x.yml@refs/heads/main"
		},
		"short commit": func(c *certificate.Summary) { c.SourceRepositoryDigest = recordedCommit[:12] },
		"foreign run": func(c *certificate.Summary) {
			c.RunInvocationURI = "https://github.com/other/repo/actions/runs/1/attempts/1"
		},
	}
	for name, mutate := range bad {
		c := good
		mutate(&c)
		if _, err := certificateFacts(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestCachedProvenanceIsReusedWhileTheSBOMIsChecked(t *testing.T) {
	// The API fails for provenance; a fresh cached result must be used
	// instead of asking again.
	f := &fakeGitHub{api: map[string]response{"provenance": {500, `{}`}}}
	cache := NewMemoryCache()
	cached := Evidence{
		Digest: recordedDigest,
		Provenance: Provenance{Status: Verified, CheckedAt: fixedNow.Add(-time.Hour), Subjects: []string{"ghcr.io/dannyota/aboutme-server"},
			Version: "v0.5.21", Commit: recordedCommit, SignerWorkflow: recordedSigner, LogIndex: recordedIndex, BuildURL: recordedBuild},
		SBOM: SBOM{Status: Unchecked},
	}
	if err := cache.Put(context.Background(), cached); err != nil {
		t.Fatal(err)
	}
	ev := newVerifier(t, f.server(t), cache).Check(context.Background(), recordedDigest)
	if ev.Provenance.Status != Verified || ev.Provenance.Version != "v0.5.21" {
		t.Fatalf("provenance = %+v, want the cached verified result", ev.Provenance)
	}
	if ev.SBOM.Status != NotFound {
		t.Errorf("sbom status = %q, want not_found after checking again", ev.SBOM.Status)
	}
	if f.calls.Load() == 0 {
		t.Error("the unchecked SBOM was not checked again")
	}
}
