package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxBody bounds every response the verifier reads.
const maxBody = 4 << 20

// bundleMediaType is the Sigstore bundle layer actions/attest pushes.
const bundleMediaType = "application/vnd.dev.sigstore.bundle.v0.3+json"

// registryRepositories are searched for a bundle when the GitHub API is
// rate limited. The digest alone does not say which image it belongs to;
// the document checks the subject name against the component afterward.
var registryRepositories = []string{"dannyota/aboutme-server", "dannyota/aboutme-web", "dannyota/aboutme-caddy"}

// errRateLimited marks a GitHub API answer that sends the lookup to the
// registry.
var errRateLimited = errors.New("github api rate limited")

// fetch returns the raw bundles for one digest and predicate. It asks the
// GitHub attestations API first, then the registry when the API is rate
// limited or holds nothing. It returns an error whenever either source did
// not answer, so an empty result means both answered with nothing
// (verification.md, "not_found").
func (v *Verifier) fetch(ctx context.Context, digest, predicate, apiFilter string) ([][]byte, error) {
	bundles, apiErr := v.fetchAPI(ctx, digest, apiFilter)
	switch {
	case apiErr == nil && len(bundles) > 0:
		return bundles, nil
	case apiErr != nil && !errors.Is(apiErr, errRateLimited):
		return nil, apiErr
	}
	bundles, err := v.fetchRegistry(ctx, digest, predicate)
	if err != nil {
		return nil, err
	}
	if len(bundles) == 0 && apiErr != nil {
		// A rate-limited GitHub never answered, so absence is not proven.
		return nil, apiErr
	}
	return bundles, nil
}

func (v *Verifier) fetchAPI(ctx context.Context, digest, apiFilter string) ([][]byte, error) {
	u := fmt.Sprintf("%s/repos/dannyota/aboutme/attestations/%s?predicate_type=%s&per_page=30",
		v.opts.GitHubAPI, url.PathEscape(digest), url.QueryEscape(apiFilter))
	body, status, err := v.get(ctx, u, map[string]string{"Accept": "application/vnd.github+json"})
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, errRateLimited
	default:
		return nil, fmt.Errorf("github api: status %d", status)
	}
	var resp struct {
		Attestations []struct {
			Bundle json.RawMessage `json:"bundle"`
		} `json:"attestations"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("github api: %w", err)
	}
	var out [][]byte
	for _, a := range resp.Attestations {
		if len(a.Bundle) > 0 && string(a.Bundle) != "null" {
			out = append(out, a.Bundle)
		}
	}
	return out, nil
}

// fetchRegistry reads bundles stored beside the image under the OCI
// referrers tag schema: the index tagged sha256-<hex> lists one manifest per
// attestation, annotated with its predicate type.
func (v *Verifier) fetchRegistry(ctx context.Context, digest, predicate string) ([][]byte, error) {
	var out [][]byte
	for _, repo := range registryRepositories {
		got, err := v.fetchRepository(ctx, repo, digest, predicate)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

func (v *Verifier) fetchRepository(ctx context.Context, repo, digest, predicate string) ([][]byte, error) {
	token, err := v.registryToken(ctx, repo)
	if err != nil {
		return nil, err
	}
	auth := map[string]string{"Authorization": "Bearer " + token}
	indexURL := fmt.Sprintf("%s/v2/%s/manifests/%s", v.opts.Registry, repo, strings.Replace(digest, ":", "-", 1))
	body, status, err := v.get(ctx, indexURL, withAccept(auth, "application/vnd.oci.image.index.v1+json"))
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("registry index: status %d", status)
	}
	var index struct {
		Manifests []struct {
			Digest       string            `json:"digest"`
			ArtifactType string            `json:"artifactType"`
			Annotations  map[string]string `json:"annotations"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("registry index: %w", err)
	}
	var out [][]byte
	for _, m := range index.Manifests {
		if m.ArtifactType != bundleMediaType || m.Annotations["dev.sigstore.bundle.predicateType"] != predicate {
			continue
		}
		if digestRE.FindStringSubmatch(m.Digest) == nil {
			return nil, errors.New("registry index: malformed manifest digest")
		}
		b, err := v.fetchBundle(ctx, repo, m.Digest, auth)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func (v *Verifier) fetchBundle(ctx context.Context, repo, manifestDigest string, auth map[string]string) ([]byte, error) {
	u := fmt.Sprintf("%s/v2/%s/manifests/%s", v.opts.Registry, repo, manifestDigest)
	body, status, err := v.get(ctx, u, withAccept(auth, "application/vnd.oci.image.manifest.v1+json"))
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("registry manifest: status %d", status)
	}
	if err := matchDigest(body, manifestDigest); err != nil {
		return nil, fmt.Errorf("registry manifest: %w", err)
	}
	var manifest struct {
		Layers []struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("registry manifest: %w", err)
	}
	for _, l := range manifest.Layers {
		if l.MediaType != bundleMediaType || digestRE.FindStringSubmatch(l.Digest) == nil {
			continue
		}
		blob, status, err := v.get(ctx, fmt.Sprintf("%s/v2/%s/blobs/%s", v.opts.Registry, repo, l.Digest), auth)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("registry blob: status %d", status)
		}
		if err := matchDigest(blob, l.Digest); err != nil {
			return nil, fmt.Errorf("registry blob: %w", err)
		}
		return blob, nil
	}
	return nil, errors.New("registry manifest: no bundle layer")
}

func (v *Verifier) registryToken(ctx context.Context, repo string) (string, error) {
	host := v.opts.Registry
	if u, err := url.Parse(v.opts.Registry); err == nil && u.Host != "" {
		host = u.Host
	}
	u := fmt.Sprintf("%s/token?scope=%s&service=%s", v.opts.Registry,
		url.QueryEscape("repository:"+repo+":pull"), url.QueryEscape(host))
	body, status, err := v.get(ctx, u, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("registry token: status %d", status)
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.Token == "" {
		return "", errors.New("registry token: no token")
	}
	return tok.Token, nil
}

func (v *Verifier) get(ctx context.Context, u string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	resp, err := v.opts.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // a read-only body
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, 0, err
	}
	if len(body) > maxBody {
		return nil, 0, errors.New("response too large")
	}
	return body, resp.StatusCode, nil
}

func withAccept(h map[string]string, accept string) map[string]string {
	out := map[string]string{"Accept": accept}
	for k, val := range h {
		out[k] = val
	}
	return out
}

func matchDigest(b []byte, digest string) error {
	sum := sha256.Sum256(b)
	if "sha256:"+hex.EncodeToString(sum[:]) != digest {
		return errors.New("content does not match its digest")
	}
	return nil
}
