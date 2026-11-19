package platform

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"

	"danny.vn/aboutme/deploy/observer/internal/source"
)

const (
	Server      = "server"
	Web         = "web"
	Caddy       = "caddy"
	Maintenance = "maintenance"
)

var Components = []string{Server, Web, Caddy, Maintenance}

var Required = []string{Server, Web, Caddy}

func imageName(component string) string {
	switch component {
	case Server:
		return "aboutme-server"
	case Web:
		return "aboutme-web"
	case Caddy, Maintenance:
		return "aboutme-caddy"
	}
	return ""
}

func Repository(owner, component string) string {
	if !source.Valid(owner) || imageName(component) == "" {
		return ""
	}
	return "ghcr.io/" + owner + "/" + imageName(component)
}

func RepositoryOwner(component, repository string) (string, bool) {
	for _, owner := range source.Owners() {
		if r := Repository(owner, component); r != "" && r == repository {
			return owner, true
		}
	}
	return "", false
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func ValidDigest(d string) bool { return digestPattern.MatchString(d) }

type Image struct {
	Component    string
	Repository   string
	Digest       string
	Replicas     int
	RunningSince time.Time
}

type Observation struct {
	Component  string
	Repository string
	Digest     string
	StartedAt  time.Time
}

type Info struct {
	Provider     string
	Orchestrator string
	Region       string
}

type Lister interface {
	Running(ctx context.Context) ([]Image, error)
}

type SnapshotLister interface {
	Lister
	ObservationTime() time.Time
}

func Aggregate(obs []Observation) ([]Image, error) {
	type key struct{ component, digest string }
	byKey := map[key]*Image{}
	repositories := map[string]string{}
	for _, o := range obs {
		if imageName(o.Component) == "" {
			return nil, fmt.Errorf("unknown component %q", o.Component)
		}
		if _, ok := RepositoryOwner(o.Component, o.Repository); !ok {
			return nil, fmt.Errorf("a running %s container has no accepted image repository", o.Component)
		}
		if r, seen := repositories[o.Component]; seen && r != o.Repository {
			return nil, fmt.Errorf("running %s containers name two image repositories", o.Component)
		}
		repositories[o.Component] = o.Repository
		if !ValidDigest(o.Digest) {
			return nil, fmt.Errorf("a running %s container has no valid digest", o.Component)
		}
		if o.StartedAt.IsZero() {
			return nil, fmt.Errorf("a running %s container has no start time", o.Component)
		}
		k := key{o.Component, o.Digest}
		img := byKey[k]
		if img == nil {
			img = &Image{Component: o.Component, Repository: o.Repository, Digest: o.Digest, RunningSince: o.StartedAt.UTC()}
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
