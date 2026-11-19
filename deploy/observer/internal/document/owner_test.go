package document

import (
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/deploy/observer/internal/platform"
	"danny.vn/aboutme/deploy/observer/internal/source"
	"danny.vn/aboutme/deploy/observer/internal/verify"
)

func releaseBy(observerOwner, imageOwner string) Input {
	in := release065()
	in.Owner = observerOwner
	for i := range in.Images {
		in.Images[i].Repository = platform.Repository(imageOwner, in.Images[i].Component)
	}
	for n, c := range map[int]string{1: platform.Server, 2: platform.Web, 3: platform.Caddy} {
		in.Evidence[digest(n)] = signedBy(imageOwner, platform.Repository(imageOwner, c), "v0.6.5", 7)
	}
	return in
}

func TestOwnerIsRequired(t *testing.T) {
	for _, owner := range []string{"", "someone", "Dntmakers", "dannyota/aboutme"} {
		in := release065()
		in.Owner = owner
		if _, err := Build(in); err == nil {
			t.Errorf("built a document for owner %q", owner)
		}
	}
}

func TestDntmakersRelease(t *testing.T) {
	doc := build(t, releaseBy(source.Dntmakers, source.Dntmakers))
	if doc.Summary != SummaryVerified || doc.Release == nil || doc.SourceRepository != "https://github.com/dntmakers/aboutme" {
		t.Fatalf("summary %q release %+v source %s", doc.Summary, doc.Release, doc.SourceRepository)
	}
	c := doc.Components[0]
	ri := c.RunningImages[0]
	if c.Image != "ghcr.io/dntmakers/aboutme-server" ||
		*ri.Signature.SignerWorkflow != "https://github.com/dntmakers/aboutme/.github/workflows/release-images.yml@refs/tags/v0.6.5" ||
		*ri.Links.Build != "https://github.com/dntmakers/aboutme/actions/runs/36218940587/attempts/1" ||
		*ri.Links.Commit != "https://github.com/dntmakers/aboutme/commit/"+commitOf(7) ||
		*ri.Links.Release != "https://github.com/dntmakers/aboutme/releases/tag/v0.6.5" ||
		*ri.Links.SBOM != "https://github.com/dntmakers/aboutme/releases/download/v0.6.5/aboutme-server-amd64.spdx.json" ||
		ri.Links.Provenance != "https://api.github.com/repos/dntmakers/aboutme/attestations/"+digest(1)+"?predicate_type=provenance" {
		t.Errorf("server = %s %+v %+v", c.Image, ri.Signature, ri.Links)
	}
}

func TestBuildTimeOwnerNamesTheSourceLinks(t *testing.T) {
	doc := build(t, releaseBy(source.Dntmakers, source.Dannyota))
	if doc.Summary != SummaryVerified || doc.SourceRepository != "https://github.com/dntmakers/aboutme" {
		t.Fatalf("summary %q source %s", doc.Summary, doc.SourceRepository)
	}
	c := doc.Components[1]
	ri := c.RunningImages[0]
	if c.Image != "ghcr.io/dannyota/aboutme-web" ||
		!strings.HasPrefix(*ri.Signature.SignerWorkflow, "https://github.com/dannyota/") ||
		!strings.HasPrefix(*ri.Links.Build, "https://github.com/dannyota/") {
		t.Errorf("certificate facts were renamed: %s %+v %+v", c.Image, ri.Signature, ri.Links)
	}
	for _, link := range []string{*ri.Links.Commit, *ri.Links.Release, *ri.Links.SBOM, ri.Links.Provenance} {
		if !strings.Contains(link, "/dntmakers/aboutme/") {
			t.Errorf("%s is not under the build-time owner", link)
		}
	}
}

func TestRollbackKeepsANewerMaintenanceImage(t *testing.T) {
	in := release065()
	in.Images = append(in.Images, ownedImg(source.Dntmakers, platform.Maintenance, 4, observed.Add(-time.Hour)))
	in.Evidence[digest(4)] = signedBy(source.Dntmakers, "ghcr.io/dntmakers/aboutme-caddy", "v0.9.40", 6)
	doc := build(t, in)
	m := doc.Components[3]
	if doc.Summary != SummaryVerified || doc.Release == nil || doc.Release.Version != "v0.6.5" ||
		m.Image != "ghcr.io/dntmakers/aboutme-caddy" || m.RunningImages[0].Signature.Status != "verified" {
		t.Fatalf("summary %q release %+v maintenance %+v", doc.Summary, doc.Release, m)
	}
	if doc.SourceRepository != "https://github.com/dannyota/aboutme" ||
		*m.RunningImages[0].Links.Commit != "https://github.com/dannyota/aboutme/commit/"+commitOf(6) {
		t.Errorf("links left the build-time owner: %s %+v", doc.SourceRepository, m.RunningImages[0].Links)
	}
}

func TestSignerOwnerMustMatchTheImageNamespace(t *testing.T) {
	cases := map[string]func(*Input){
		"dannyota signer on a dntmakers image": func(in *Input) {
			in.Images[0].Repository = "ghcr.io/dntmakers/aboutme-server"
			ev := signedBy(source.Dannyota, "ghcr.io/dntmakers/aboutme-server", "v0.6.5", 7)
			in.Evidence[digest(1)] = ev
		},
		"dntmakers signer on a dannyota image": func(in *Input) {
			ev := signedBy(source.Dntmakers, serverRepo, "v0.6.5", 7)
			in.Evidence[digest(1)] = ev
		},
		"dntmakers subject on a dannyota image": func(in *Input) {
			in.Evidence[digest(1)] = signedBy(source.Dntmakers, "ghcr.io/dntmakers/aboutme-server", "v0.6.5", 7)
		},
		"unknown signer": func(in *Input) {
			ev := verified(serverRepo, "v0.6.5", 7)
			ev.Provenance.SignerWorkflow = strings.Replace(ev.Provenance.SignerWorkflow, "dannyota", "someone", 1)
			in.Evidence[digest(1)] = ev
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := release065()
			mutate(&in)
			doc := build(t, in)
			ri := doc.Components[0].RunningImages[0]
			if ri.Signature.Status != string(verify.Invalid) || doc.Summary != SummaryMismatch || ri.Version != nil {
				t.Errorf("status %q summary %q version %v", ri.Signature.Status, doc.Summary, ri.Version)
			}
		})
	}
}

func TestImageRepositoriesAreRefused(t *testing.T) {
	cases := map[string]func(*Input){
		"no repository":     func(in *Input) { in.Images[0].Repository = "" },
		"another owner":     func(in *Input) { in.Images[0].Repository = "ghcr.io/someone/aboutme-server" },
		"another component": func(in *Input) { in.Images[0].Repository = "ghcr.io/dntmakers/aboutme-web" },
		"two namespaces in one component": func(in *Input) {
			in.Images = append(in.Images, ownedImg(source.Dntmakers, platform.Web, 5, observed.Add(-time.Minute)))
			in.Evidence[digest(5)] = signedBy(source.Dntmakers, "ghcr.io/dntmakers/aboutme-web", "v0.6.5", 7)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := release065()
			mutate(&in)
			if doc, err := Build(in); err == nil {
				t.Fatalf("built %+v", doc.Components)
			}
		})
	}
}

func TestStoppedComponentNamesTheDannyotaImage(t *testing.T) {
	in := releaseBy(source.Dntmakers, source.Dntmakers)
	in.Images = []platform.Image{in.Images[0], in.Images[2]}
	doc := build(t, in)
	if doc.Summary != SummaryMismatch || doc.Components[1].Image != "ghcr.io/dannyota/aboutme-web" ||
		len(doc.Components[1].RunningImages) != 0 || doc.Components[0].Image != "ghcr.io/dntmakers/aboutme-server" {
		t.Errorf("summary %q components %+v", doc.Summary, doc.Components)
	}
}
