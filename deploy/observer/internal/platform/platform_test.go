package platform

import (
	"fmt"
	"testing"
	"time"
)

var started = time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)

func digest(n int) string { return fmt.Sprintf("sha256:%064x", n) }

func TestRepository(t *testing.T) {
	want := map[string]string{
		Server: "aboutme-server", Web: "aboutme-web", Caddy: "aboutme-caddy", Maintenance: "aboutme-caddy",
	}
	for _, owner := range []string{"dannyota", "dntmakers"} {
		for component, name := range want {
			repo := Repository(owner, component)
			if repo != "ghcr.io/"+owner+"/"+name {
				t.Errorf("Repository(%s, %s) = %q", owner, component, repo)
			}
			if got, ok := RepositoryOwner(component, repo); !ok || got != owner {
				t.Errorf("RepositoryOwner(%s, %s) = %q, %v", component, repo, got, ok)
			}
		}
	}
	for _, c := range [][2]string{{"someone", Server}, {"", Web}, {"dannyota", "jobs"}, {"dntmakers", ""}} {
		if repo := Repository(c[0], c[1]); repo != "" {
			t.Errorf("Repository(%q, %q) = %q", c[0], c[1], repo)
		}
	}
	for _, c := range [][2]string{
		{Server, "ghcr.io/dntmakers/aboutme-web"},
		{Maintenance, "ghcr.io/dannyota/aboutme-maintenance"},
		{Server, "ghcr.io/someone/aboutme-server"},
		{Server, "docker.io/dannyota/aboutme-server"},
		{Server, ""},
		{"jobs", "ghcr.io/dannyota/aboutme-jobs"},
	} {
		if owner, ok := RepositoryOwner(c[0], c[1]); ok {
			t.Errorf("RepositoryOwner(%q, %q) accepted %q", c[0], c[1], owner)
		}
	}
}

func TestAggregateCarriesEachRepository(t *testing.T) {
	images, err := Aggregate([]Observation{
		{Component: Server, Repository: "ghcr.io/dntmakers/aboutme-server", Digest: digest(1), StartedAt: started},
		{Component: Web, Repository: "ghcr.io/dannyota/aboutme-web", Digest: digest(2), StartedAt: started},
		{Component: Maintenance, Repository: "ghcr.io/dntmakers/aboutme-caddy", Digest: digest(3), StartedAt: started},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, img := range images {
		got[img.Component] = img.Repository
	}
	want := map[string]string{
		Server: "ghcr.io/dntmakers/aboutme-server", Web: "ghcr.io/dannyota/aboutme-web", Maintenance: "ghcr.io/dntmakers/aboutme-caddy",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("repositories = %v, want %v", got, want)
	}
}

func TestAggregateRefusesRepositories(t *testing.T) {
	cases := map[string][]Observation{
		"no repository": {{Component: Server, Digest: digest(1), StartedAt: started}},
		"another owner": {{Component: Server, Repository: "ghcr.io/someone/aboutme-server", Digest: digest(1), StartedAt: started}},
		"another image": {{Component: Server, Repository: "ghcr.io/dntmakers/aboutme-web", Digest: digest(1), StartedAt: started}},
		"tagged image":  {{Component: Web, Repository: "ghcr.io/dannyota/aboutme-web:v0.9.31", Digest: digest(1), StartedAt: started}},
		"two namespaces": {
			{Component: Web, Repository: "ghcr.io/dannyota/aboutme-web", Digest: digest(1), StartedAt: started},
			{Component: Web, Repository: "ghcr.io/dntmakers/aboutme-web", Digest: digest(2), StartedAt: started},
		},
	}
	for name, obs := range cases {
		t.Run(name, func(t *testing.T) {
			if images, err := Aggregate(obs); err == nil {
				t.Fatalf("accepted %+v", images)
			}
		})
	}
}
