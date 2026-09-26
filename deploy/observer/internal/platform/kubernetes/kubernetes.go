// Package kubernetes adapts the in-cluster Kubernetes API to
// platform.Lister (docs/design/deployment-transparency/README.md,
// "Kubernetes"). It calls only GET on the pods resource, with a Role that
// grants "get" and "list" on pods in its own namespace, and never uses
// client-go.
package kubernetes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
)

// componentLabel is the label the observer reads to map a pod to a
// component (docs/design/deployment-transparency/document.md, "Component
// mapping"). The observer never writes it.
const componentLabel = "app.kubernetes.io/component"

// Default in-cluster paths, overridable in Config for tests.
const (
	defaultTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// requestTimeout bounds one call to the API server.
const requestTimeout = 10 * time.Second

// maxPodListBytes bounds the pod list the observer decodes.
const maxPodListBytes = 8 << 20

// Config names the in-cluster API server and the paths the observer reads
// its credentials from. Host, Port, TokenFile, and CAFile default to the
// standard in-cluster values and are overridable for tests.
type Config struct {
	Namespace string
	Host      string
	Port      string
	TokenFile string
	CAFile    string
}

// Lister lists the running images of the pods labeled with a known
// component in one namespace. It implements platform.Lister.
type Lister struct {
	cfg     Config
	client  *http.Client
	podsURL string
}

// New returns a Lister for cfg, reading the API server's CA certificate
// once. It defaults Host and Port from KUBERNETES_SERVICE_HOST and
// KUBERNETES_SERVICE_PORT, and Namespace to "aboutme".
func New(cfg Config) (*Lister, error) {
	if cfg.Namespace == "" {
		cfg.Namespace = "aboutme"
	}
	if cfg.Host == "" {
		cfg.Host = os.Getenv("KUBERNETES_SERVICE_HOST")
	}
	if cfg.Port == "" {
		cfg.Port = os.Getenv("KUBERNETES_SERVICE_PORT")
	}
	if cfg.TokenFile == "" {
		cfg.TokenFile = defaultTokenFile
	}
	if cfg.CAFile == "" {
		cfg.CAFile = defaultCAFile
	}
	if cfg.Host == "" || cfg.Port == "" {
		return nil, fmt.Errorf("kubernetes: no in-cluster API host or port configured")
	}

	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("kubernetes: CA file %s has no certificates", cfg.CAFile)
	}
	client := &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}

	u := url.URL{
		Scheme: "https",
		Host:   net.JoinHostPort(cfg.Host, cfg.Port),
		Path:   fmt.Sprintf("/api/v1/namespaces/%s/pods", cfg.Namespace),
	}
	q := url.Values{}
	q.Set("labelSelector", componentLabel+" in ("+strings.Join(platform.Components, ",")+")")
	u.RawQuery = q.Encode()

	return &Lister{cfg: cfg, client: client, podsURL: u.String()}, nil
}

// podList is the minimal shape the observer reads from a PodList
// (docs/design/deployment-transparency/README.md, "Kubernetes"). Every other
// field, including names, labels outside componentLabel, and annotations,
// is never decoded, so it cannot reach the document
// (docs/design/deployment-transparency/document.md, "Sanitizer").
type podList struct {
	Items []struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Status struct {
			ContainerStatuses []struct {
				Name    string `json:"name"`
				ImageID string `json:"imageID"`
				State   struct {
					Running *struct {
						StartedAt time.Time `json:"startedAt"`
					} `json:"running"`
				} `json:"state"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

// Running implements platform.Lister. A pod counts only for the container
// named like its component, and only while that container's state is
// running.
func (l *Lister) Running(ctx context.Context) ([]platform.Image, error) {
	token, err := os.ReadFile(l.cfg.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: read token file: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.podsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Accept", "application/json")

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: list pods: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() //nolint:errcheck // best-effort close once the body below is fully read
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kubernetes: list pods: unexpected status %s", resp.Status)
	}

	var list podList
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPodListBytes)).Decode(&list); err != nil {
		return nil, fmt.Errorf("kubernetes: decode pod list: %w", err)
	}

	var obs []platform.Observation
	for _, pod := range list.Items {
		component := pod.Metadata.Labels[componentLabel]
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name != component || cs.State.Running == nil {
				continue
			}
			digest, ok := digestFromImageID(cs.ImageID)
			if !ok {
				return nil, fmt.Errorf("kubernetes: a running %s container has no valid image digest", component)
			}
			obs = append(obs, platform.Observation{
				Component: component,
				Digest:    digest,
				StartedAt: cs.State.Running.StartedAt,
			})
		}
	}
	return platform.Aggregate(obs)
}

// imageIDDigest matches the "@sha256:<hex>" suffix of a container's
// imageID, discarding any prefix such as "docker-pullable://" or
// "containerd://" and the repository part
// (docs/design/deployment-transparency/README.md, "Kubernetes").
var imageIDDigest = regexp.MustCompile(`@(sha256:[0-9a-f]{64})$`)

// digestFromImageID is the pure form of the imageID digest extraction rule.
func digestFromImageID(imageID string) (string, bool) {
	m := imageIDDigest.FindStringSubmatch(imageID)
	if m == nil {
		return "", false
	}
	return m[1], true
}
