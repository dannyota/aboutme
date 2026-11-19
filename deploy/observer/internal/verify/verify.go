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

	"danny.vn/aboutme/deploy/observer/internal/source"
)

const (
	Issuer       = "https://token.actions.githubusercontent.com"
	RunnerHosted = "github-hosted"

	ProvenancePredicate = "https://slsa.dev/provenance/v1"
	SPDXPredicate       = "https://spdx.dev/Document/v2.3"

	signerPattern = `^https://github\.com/(` + source.OwnerPattern + `)/aboutme/\.github/workflows/release-images\.yml@refs/tags/(v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))$`
)

var (
	signerRE = regexp.MustCompile(signerPattern)
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	buildRE  = regexp.MustCompile(`^https://github\.com/(` + source.OwnerPattern + `)/aboutme/actions/runs/[0-9]{1,20}/attempts/[0-9]{1,4}$`)
	digestRE = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)
)

func SignerOwner(signerWorkflow string) string {
	if m := signerRE.FindStringSubmatch(signerWorkflow); m != nil {
		return m[1]
	}
	return ""
}

type Options struct {
	HTTPClient      *http.Client
	Cache           Cache
	Now             func() time.Time
	TUFCacheDir     string
	GitHubAPI       string
	Registry        string
	TrustedMaterial root.TrustedMaterial
}

type Verifier struct {
	opts Options

	mu      sync.Mutex
	trusted root.TrustedMaterial
}

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
	if ev.Provenance.Status != Unchecked {
		if err := v.opts.Cache.Put(ctx, ev); err != nil {
			slog.Warn("verification cache write failed", "digest", digest)
		}
	}
	return ev
}

func fresh(status Status, checkedAt, now time.Time) bool {
	switch status {
	case Verified, NotFound, Invalid:
	default:
		return false
	}
	age := now.Sub(checkedAt)
	return age >= 0 && age < CacheTTL
}

// sigstore-go's NewLiveTrustedRoot refreshes TUF daily for the life of the process, so it takes no run context (sigstore-go root).
func (v *Verifier) trustedMaterial() (root.TrustedMaterial, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.trusted != nil {
		return v.trusted, nil
	}
	// The default TUF fetcher's HTTP client has no timeout, so a hanging mirror could outlast the run (go-tuf fetcher).
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

type verified struct {
	owner    string
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

func (v *Verifier) check(ctx context.Context, trusted root.TrustedMaterial, digest string, sum []byte, predicate, apiFilter string) (Status, verified) {
	bundles, fetchErr := v.fetch(ctx, digest, predicate, apiFilter)
	for _, raw := range bundles {
		if got, err := verifyBundle(trusted, raw, sum, predicate); err == nil {
			return Verified, got
		}
	}
	switch {
	case fetchErr != nil:
		return Unchecked, verified{}
	case len(bundles) == 0:
		return NotFound, verified{}
	}
	return Invalid, verified{}
}

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
		SourceRepositoryIdentifier: source.RepositoryID,
		RunnerEnvironment:          RunnerHosted,
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
	if err = inNamespace(got.subjects, got.owner); err != nil {
		return verified{}, err
	}
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

func inNamespace(subjects []string, owner string) error {
	if len(subjects) == 0 {
		return errors.New("no subject names this digest")
	}
	for _, s := range subjects {
		if !source.Valid(owner) || !strings.HasPrefix(s, "ghcr.io/"+owner+"/") {
			return errors.New("a subject is outside the signer's namespace")
		}
	}
	return nil
}

func certificateFacts(c certificate.Summary) (verified, error) {
	m := signerRE.FindStringSubmatch(c.SubjectAlternativeName)
	if m == nil {
		return verified{}, errors.New("signer identity does not match")
	}
	owner, version := m[1], m[2]
	run := buildRE.FindStringSubmatch(c.RunInvocationURI)
	switch {
	case c.Issuer != Issuer:
		return verified{}, errors.New("wrong issuer")
	case c.SourceRepositoryIdentifier != source.RepositoryID:
		return verified{}, errors.New("wrong source repository identifier")
	case c.SourceRepositoryURI != source.Repository(owner):
		return verified{}, errors.New("source repository owner differs from the signer's")
	case c.SourceRepositoryOwnerIdentifier != source.OwnerID(owner):
		return verified{}, errors.New("source repository owner identifier does not match the signer's owner")
	case c.RunnerEnvironment != RunnerHosted:
		return verified{}, errors.New("not a GitHub-hosted runner")
	case c.SourceRepositoryRef != "refs/tags/"+version:
		return verified{}, errors.New("source ref does not match the signer's tag")
	case c.BuildSignerURI != c.SubjectAlternativeName:
		return verified{}, errors.New("build signer does not match the signer identity")
	case !commitRE.MatchString(c.SourceRepositoryDigest):
		return verified{}, errors.New("malformed source digest")
	case run == nil || run[1] != owner:
		return verified{}, errors.New("malformed run invocation")
	}
	return verified{
		owner:    owner,
		version:  version,
		commit:   c.SourceRepositoryDigest,
		san:      c.SubjectAlternativeName,
		buildURL: c.RunInvocationURI,
	}, nil
}
