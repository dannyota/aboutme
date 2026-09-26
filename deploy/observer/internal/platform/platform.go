// Package platform holds the one type platform adapters return: a running
// image per component, with nothing else from the platform. The document
// builder reads only this type, so no platform field can reach the published
// document (docs/design/deployment-transparency/document.md, "Sanitizer").
package platform

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"
)

// Component names, in document order.
const (
	Server      = "server"
	Web         = "web"
	Caddy       = "caddy"
	Maintenance = "maintenance"
)

// Components lists every component in document order.
var Components = []string{Server, Web, Caddy, Maintenance}

// Required lists the components that must run for a healthy deployment.
var Required = []string{Server, Web, Caddy}

// imageRepository is the fixed image each component runs. The platform's own
// image reference is never used.
var imageRepository = map[string]string{
	Server:      "ghcr.io/dannyota/aboutme-server",
	Web:         "ghcr.io/dannyota/aboutme-web",
	Caddy:       "ghcr.io/dannyota/aboutme-caddy",
	Maintenance: "ghcr.io/dannyota/aboutme-caddy",
}

// ImageRepository returns the fixed image name for a component, or "" for a
// name outside Components.
func ImageRepository(component string) string { return imageRepository[component] }

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ValidDigest reports whether d is a lowercase sha256 image digest.
func ValidDigest(d string) bool { return digestPattern.MatchString(d) }

// Image is one distinct digest running for one component.
type Image struct {
	Component    string
	Digest       string
	Replicas     int
	RunningSince time.Time
}

// Observation is one running container of a known component, as an adapter
// saw it.
type Observation struct {
	Component string
	Digest    string
	StartedAt time.Time
}

// Info names the platform in the document.
type Info struct {
	Provider     string // aws or greennode
	Orchestrator string // ecs or kubernetes
	Region       string
}

// Lister reads the running images from a platform.
type Lister interface {
	Running(ctx context.Context) ([]Image, error)
}

// Aggregate counts observations by component and digest. It fails on an
// unknown component or a malformed digest rather than dropping the container,
// so a running container the document cannot describe stops the write.
func Aggregate(obs []Observation) ([]Image, error) {
	type key struct{ component, digest string }
	byKey := map[key]*Image{}
	for _, o := range obs {
		if ImageRepository(o.Component) == "" {
			return nil, fmt.Errorf("unknown component %q", o.Component)
		}
		if !ValidDigest(o.Digest) {
			return nil, fmt.Errorf("a running %s container has no valid digest", o.Component)
		}
		if o.StartedAt.IsZero() {
			return nil, fmt.Errorf("a running %s container has no start time", o.Component)
		}
		k := key{o.Component, o.Digest}
		img := byKey[k]
		if img == nil {
			img = &Image{Component: o.Component, Digest: o.Digest, RunningSince: o.StartedAt.UTC()}
			byKey[k] = img
		}
		img.Replicas++
		if o.StartedAt.Before(img.RunningSince) {
			img.RunningSince = o.StartedAt.UTC()
		}
	}
	out := make([]Image, 0, len(byKey))
	for _, img := range byKey {
		out = append(out, *img)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Component != out[j].Component {
			return out[i].Component < out[j].Component
		}
		if !out[i].RunningSince.Equal(out[j].RunningSince) {
			return out[i].RunningSince.Before(out[j].RunningSince)
		}
		return out[i].Digest < out[j].Digest
	})
	return out, nil
}
