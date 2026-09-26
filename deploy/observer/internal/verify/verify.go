package verify

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	sgverify "github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
)

// The signing identity every release image must carry (verification.md,
// "What verified means").
const (
	Issuer           = "https://token.actions.githubusercontent.com"
	SourceRepository = "https://github.com/dannyota/aboutme"
	RunnerHosted     = "github-hosted"

	ProvenancePredicate = "https://slsa.dev/provenance/v1"
	SPDXPredicate       = "https://spdx.dev/Document/v2.3"

	// signerPattern is the exact certificate identity: the release workflow
	// at a strict version tag.
	signerPattern = `^https://github\.com/dannyota/aboutme/\.github/workflows/release-images\.yml@refs/tags/(v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))$`
)

var (
	signerRE = regexp.MustCompile(signerPattern)
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	buildRE  = regexp.MustCompile(`^https://github\.com/dannyota/aboutme/actions/runs/[0-9]{1,20}/attempts/[0-9]{1,4}$`)
	digestRE = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)
)

// Options configure a Verifier.
type Options struct {
	HTTPClient *http.Client
	// Cache is required. A result is reused for CacheTTL.
	Cache Cache
	Now   func() time.Time
	// TUFCacheDir holds sigstore's TUF metadata; "" keeps it in memory.
	TUFCacheDir string
	// GitHubAPI and Registry default to https://api.github.com and
	// https://ghcr.io.
	GitHubAPI string
	Registry  string
	// TrustedMaterial replaces the TUF-fetched public-good trust root. Tests
	// set it to a recorded trusted root.
	TrustedMaterial root.TrustedMaterial
}

// Verifier checks digests against their attestations.
type Verifier struct {
	opts Options

	mu      sync.Mutex
	trusted root.TrustedMaterial
}

// New returns a Verifier. It does not fetch the trust root; the first Check
// does, and a failure there makes that Check's result Unchecked.
func New(_ context.Context, opts Options) (*Verifier, error) {
	if opts.Cache == nil {
		return nil, errors.New("verify: a cache is required")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.GitHubAPI == "" {
		opts.GitHubAPI = "https://api.github.com"
	}
	if opts.Registry == "" {
		opts.Registry = "https://ghcr.io"
	}
	opts.GitHubAPI = strings.TrimRight(opts.GitHubAPI, "/")
	opts.Registry = strings.TrimRight(opts.Registry, "/")
	return &Verifier{opts: opts, trusted: opts.TrustedMaterial}, nil
}

// Check returns the evidence for one digest. Provenance and SBOM are
// cached separately: a part checked less than CacheTTL ago is reused, and
// only the other part is checked again. It never fails: anything that stops
// a check from finishing makes that part Unchecked, which is never reused.
func (v *Verifier) Check(ctx context.Context, digest string) Evidence {
	ev := Evidence{Digest: digest, Provenance: Provenance{Status: Unchecked}, SBOM: SBOM{Status: Unchecked}}
	m := digestRE.FindStringSubmatch(digest)
	if m == nil {
		return ev
	}
	now := v.opts.Now().UTC()
	haveProvenance, haveSBOM := false, false
	if cached, found, err := v.opts.Cache.Get(ctx, digest); err == nil && found && cached.Digest == digest {
		if fresh(cached.Provenance.Status, cached.Provenance.CheckedAt, now) {
			ev.Provenance, haveProvenance = cached.Provenance, true
		}
		if fresh(cached.SBOM.Status, cached.SBOM.CheckedAt, now) {
			ev.SBOM, haveSBOM = cached.SBOM, true
		}
	}
	if haveProvenance && haveSBOM {
		return ev
	}
	trusted, err := v.trustedMaterial()
	if err != nil {
		return ev
	}
	sum, err := hex.DecodeString(m[1])
	if err != nil {
		return ev
	}
	if !haveProvenance {
		ev.Provenance = v.provenance(ctx, trusted, digest, sum, now)
	}
	if !haveSBOM {
		ev.SBOM = v.sbom(ctx, trusted, digest, sum, now)
	}
	// The cache holds an entry only with a final provenance; an unchecked
	// SBOM beside it is checked again next run.
	if ev.Provenance.Status != Unchecked {
		// A failed cache write only costs API calls on the next run.
		if err := v.opts.Cache.Put(ctx, ev); err != nil {
			slog.Warn("verification cache write failed", "digest", digest)
		}
	}
	return ev
}

// fresh reports whether a cached part is final and younger than CacheTTL.
func fresh(status Status, checkedAt, now time.Time) bool {
	switch status {
	case Verified, NotFound, Invalid:
	default:
		return false
	}
	age := now.Sub(checkedAt)
	return age >= 0 && age < CacheTTL
}

// trustedMaterial fetches the public-good trust root once. Its daily TUF
// refresh runs for the life of the process, not of one run, so it takes no
// run context.
func (v *Verifier) trustedMaterial() (root.TrustedMaterial, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.trusted != nil {
		return v.trusted, nil
	}
	// The default fetcher's client has no timeout; a hanging TUF mirror
	// must not hold the verifier past a run's deadline.
	f := fetcher.NewDefaultFetcher()
	f.SetHTTPClient(&http.Client{Timeout: 10 * time.Second})
	opts := tuf.DefaultOptions().WithFetcher(f)
	if v.opts.TUFCacheDir == "" {
		opts = opts.WithDisableLocalCache()
	} else {
		opts = opts.WithCachePath(v.opts.TUFCacheDir)
	}
	tr, err := root.NewLiveTrustedRoot(opts)
	if err != nil {
		return nil, fmt.Errorf("trust root: %w", err)
	}
	v.trusted = tr
	return tr, nil
}

// verified is what one bundle proves when every check passes.
type verified struct {
	subjects []string
	version  string
	commit   string
	san      string
	logIndex int64
	buildURL string
}

func (v *Verifier) provenance(ctx context.Context, trusted root.TrustedMaterial, digest string, sum []byte, now time.Time) Provenance {
	st, got := v.check(ctx, trusted, digest, sum, ProvenancePredicate, "provenance")
	p := Provenance{Status: st}
	if st != Unchecked {
		p.CheckedAt = now
	}
	if st == Verified {
		p.Subjects = got.subjects
		p.Version = got.version
		p.Commit = got.commit
		p.SignerWorkflow = got.san
		p.LogIndex = got.logIndex
		p.BuildURL = got.buildURL
	}
	return p
}

func (v *Verifier) sbom(ctx context.Context, trusted root.TrustedMaterial, digest string, sum []byte, now time.Time) SBOM {
	st, got := v.check(ctx, trusted, digest, sum, SPDXPredicate, "spdx")
	s := SBOM{Status: st}
	if st != Unchecked {
		s.CheckedAt = now
	}
	if st == Verified {
		s.Subjects = got.subjects
	}
	return s
}

// check finds the bundles for one predicate and verifies them. Any bundle
// that passes every check makes the result Verified; bundles that all fail
// make it Invalid; no bundle anywhere makes it NotFound.
func (v *Verifier) check(ctx context.Context, trusted root.TrustedMaterial, digest string, sum []byte, predicate, apiFilter string) (Status, verified) {
	bundles, err := v.fetch(ctx, digest, predicate, apiFilter)
	if err != nil {
		return Unchecked, verified{}
	}
	if len(bundles) == 0 {
		return NotFound, verified{}
	}
	for _, raw := range bundles {
		if got, err := verifyBundle(trusted, raw, sum, predicate); err == nil {
			return Verified, got
		}
	}
	return Invalid, verified{}
}

// verifyBundle applies the whole policy to one bundle: signature, Fulcio
// chain, Rekor inclusion and timestamp, issuer, exact signer identity,
// source repository, hosted runner, subject digest, and predicate type.
func verifyBundle(trusted root.TrustedMaterial, raw []byte, sum []byte, predicate string) (verified, error) {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(raw); err != nil {
		return verified{}, fmt.Errorf("bundle: %w", err)
	}
	sv, err := sgverify.NewVerifier(trusted,
		sgverify.WithSignedCertificateTimestamps(1),
		sgverify.WithTransparencyLog(1),
		sgverify.WithObserverTimestamps(1))
	if err != nil {
		return verified{}, fmt.Errorf("verifier: %w", err)
	}
	san, err := sgverify.NewSANMatcher("", signerPattern)
	if err != nil {
		return verified{}, err
	}
	iss, err := sgverify.NewIssuerMatcher(Issuer, "")
	if err != nil {
		return verified{}, err
	}
	id, err := sgverify.NewCertificateIdentity(san, iss, certificate.Extensions{
		SourceRepositoryURI: SourceRepository,
		RunnerEnvironment:   RunnerHosted,
	})
	if err != nil {
		return verified{}, err
	}
	res, err := sv.Verify(&b, sgverify.NewPolicy(
		sgverify.WithArtifactDigest("sha256", sum),
		sgverify.WithCertificateIdentity(id)))
	if err != nil {
		return verified{}, fmt.Errorf("verify: %w", err)
	}
	if res.Statement == nil || res.Statement.GetPredicateType() != predicate {
		return verified{}, errors.New("wrong predicate type")
	}
	if res.Signature == nil || res.Signature.Certificate == nil {
		return verified{}, errors.New("no certificate")
	}
	got, err := certificateFacts(*res.Signature.Certificate)
	if err != nil {
		return verified{}, err
	}
	want := hex.EncodeToString(sum)
	for _, s := range res.Statement.GetSubject() {
		if s.GetDigest()["sha256"] == want && !slices.Contains(got.subjects, s.GetName()) {
			got.subjects = append(got.subjects, s.GetName())
		}
	}
	if len(got.subjects) == 0 {
		return verified{}, errors.New("no subject names this digest")
	}
	// Exactly one entry, so the published index is the entry the verifier
	// checked, never an unverified one placed beside it.
	entries, err := b.TlogEntries()
	if err != nil || len(entries) != 1 {
		return verified{}, errors.New("want exactly one transparency log entry")
	}
	got.logIndex = entries[0].LogIndex()
	if got.logIndex < 0 {
		return verified{}, errors.New("negative log index")
	}
	return got, nil
}

// certificateFacts reads version, commit, and build link from the verified
// certificate, which Fulcio filled from GitHub's OIDC token, and requires
// them to agree with each other and with the signer identity.
func certificateFacts(c certificate.Summary) (verified, error) {
	m := signerRE.FindStringSubmatch(c.SubjectAlternativeName)
	if m == nil {
		return verified{}, errors.New("signer identity does not match")
	}
	version := m[1]
	switch {
	case c.Issuer != Issuer:
		return verified{}, errors.New("wrong issuer")
	case c.SourceRepositoryURI != SourceRepository:
		return verified{}, errors.New("wrong source repository")
	case c.RunnerEnvironment != RunnerHosted:
		return verified{}, errors.New("not a GitHub-hosted runner")
	case c.SourceRepositoryRef != "refs/tags/"+version:
		return verified{}, errors.New("source ref does not match the signer's tag")
	case c.BuildSignerURI != c.SubjectAlternativeName:
		return verified{}, errors.New("build signer does not match the signer identity")
	case !commitRE.MatchString(c.SourceRepositoryDigest):
		return verified{}, errors.New("malformed source digest")
	case !buildRE.MatchString(c.RunInvocationURI):
		return verified{}, errors.New("malformed run invocation")
	}
	return verified{
		version:  version,
		commit:   c.SourceRepositoryDigest,
		san:      c.SubjectAlternativeName,
		buildURL: c.RunInvocationURI,
	}, nil
}
