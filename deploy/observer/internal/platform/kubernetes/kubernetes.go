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

	"danny.vn/aboutme/deploy/observer/internal/platform"
)

const componentLabel = "app.kubernetes.io/component"

const (
	defaultTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

const requestTimeout = 10 * time.Second

const maxPodListBytes = 8 << 20

type Config struct {
	Namespace string
	Host      string
	Port      string
	TokenFile string
	CAFile    string
}

type Lister struct {
	cfg     Config
	client  *http.Client
	podsURL string
}

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

var imageIDDigest = regexp.MustCompile(`@(sha256:[0-9a-f]{64})$`)

func digestFromImageID(imageID string) (string, bool) {
	m := imageIDDigest.FindStringSubmatch(imageID)
	if m == nil {
		return "", false
	}
	return m[1], true
}
