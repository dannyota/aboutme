package document

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
	"github.com/dannyota/aboutme/deploy/observer/internal/verify"
)

var observed = time.Date(2026, 10, 2, 3, 14, 5, 0, time.UTC)

func digest(n int) string { return fmt.Sprintf("sha256:%064x", n) }

func commitOf(n int) string { return fmt.Sprintf("%040x", n) }

func img(component string, n int, since time.Time) platform.Image {
	return platform.Image{Component: component, Digest: digest(n), Replicas: 1, RunningSince: since}
}

func verified(repo, version string, commit int) verify.Evidence {
	return verify.Evidence{
		Provenance: verify.Provenance{
			Status:         verify.Verified,
			CheckedAt:      observed.Add(-time.Minute),
			Subjects:       []string{repo},
			Version:        version,
			Commit:         commitOf(commit),
			SignerWorkflow: "https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/" + version,
			LogIndex:       2965228543,
			BuildURL:       "https://github.com/dannyota/aboutme/actions/runs/36218940587/attempts/1",
		},
		SBOM: verify.SBOM{Status: verify.Verified, CheckedAt: observed.Add(-time.Minute), Subjects: []string{repo}},
	}
}

const (
	serverRepo = "ghcr.io/dannyota/aboutme-server"
	webRepo    = "ghcr.io/dannyota/aboutme-web"
	caddyRepo  = "ghcr.io/dannyota/aboutme-caddy"
)

// release065 is a whole release: server 1, web 2, caddy 3, all v0.6.5.
func release065() Input {
	t0 := observed.Add(-10 * time.Minute)
	return Input{
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    platform.Info{Provider: "aws", Orchestrator: "ecs", Region: "ap-southeast-1"},
		ObservedAt:  observed,
		Images: []platform.Image{
			img(platform.Server, 1, t0.Add(2*time.Minute)),
			img(platform.Web, 2, t0),
			img(platform.Caddy, 3, t0.Add(2*time.Minute)),
		},
		Evidence: map[string]verify.Evidence{
			digest(1): verified(serverRepo, "v0.6.5", 7),
			digest(2): verified(webRepo, "v0.6.5", 7),
			digest(3): verified(caddyRepo, "v0.6.5", 7),
		},
		Observer: BuildInfo{Version: "v0.6.4", Commit: commitOf(9)},
	}
}

func build(t *testing.T, in Input) *Document {
	t.Helper()
	doc, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Encode(doc); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return doc
}

func TestVerifiedRelease(t *testing.T) {
	doc := build(t, release065())
	if doc.Summary != SummaryVerified {
		t.Fatalf("summary = %q", doc.Summary)
	}
	want := Release{Version: "v0.6.5", Commit: commitOf(7), DeployedAt: "2026-10-02T03:06:05Z"}
	if doc.Release == nil || *doc.Release != want {
		t.Errorf("release = %+v, want %+v", doc.Release, want)
	}
	if doc.ObservedAt != "2026-10-02T03:14:05Z" || doc.StaleAfter != "2026-10-02T03:17:05Z" {
		t.Errorf("observed_at %s, stale_after %s", doc.ObservedAt, doc.StaleAfter)
	}
	var names []string
	for _, c := range doc.Components {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "server,web,caddy" {
		t.Errorf("components = %v, want server,web,caddy with maintenance absent", names)
	}
	ri := doc.Components[0].RunningImages[0]
	if *ri.Links.SBOM != "https://github.com/dannyota/aboutme/releases/download/v0.6.5/aboutme-server.spdx.json" ||
		*ri.Links.Release != "https://github.com/dannyota/aboutme/releases/tag/v0.6.5" ||
		*ri.Links.TransparencyLog != "https://search.sigstore.dev/?logIndex=2965228543" ||
		*ri.Links.Commit != "https://github.com/dannyota/aboutme/commit/"+commitOf(7) {
		t.Errorf("links = %+v", ri.Links)
	}
}

func TestMaintenanceUsesTheCaddyImageAndSkipsRelease(t *testing.T) {
	in := release065()
	in.Images = append(in.Images, img(platform.Maintenance, 4, observed.Add(-time.Hour)))
	in.Evidence[digest(4)] = verified(caddyRepo, "v0.6.4", 6)
	doc := build(t, in)
	m := doc.Components[3]
	if m.Name != platform.Maintenance || m.Image != caddyRepo || m.RunningImages[0].Signature.Status != "verified" {
		t.Fatalf("maintenance = %+v", m)
	}
	// An older maintenance Caddy does not affect the release.
	if doc.Summary != SummaryVerified || doc.Release == nil || doc.Release.Version != "v0.6.5" {
		t.Errorf("summary %q release %+v", doc.Summary, doc.Release)
	}
}

func TestSummaryRules(t *testing.T) {
	cases := map[string]struct {
		mutate  func(*Input)
		summary string
	}{
		"rolling out": {func(in *Input) {
			in.Images = append(in.Images, img(platform.Web, 5, observed.Add(-time.Minute)))
			in.Evidence[digest(5)] = verified(webRepo, "v0.6.4", 6)
		}, SummaryRollingOut},
		"unchecked": {func(in *Input) { delete(in.Evidence, digest(2)) }, SummaryUnverified},
		"not found": {func(in *Input) {
			in.Evidence[digest(2)] = verify.Evidence{Provenance: verify.Provenance{Status: verify.NotFound, CheckedAt: observed}}
		}, SummaryMismatch},
		"invalid": {func(in *Input) {
			in.Evidence[digest(2)] = verify.Evidence{Provenance: verify.Provenance{Status: verify.Invalid, CheckedAt: observed}}
		}, SummaryMismatch},
		"web image running as server": {func(in *Input) { in.Evidence[digest(1)] = verified(webRepo, "v0.6.5", 7) }, SummaryMismatch},
		"no web": {func(in *Input) {
			in.Images = in.Images[:1:1]
			in.Images = append(in.Images, img(platform.Caddy, 3, observed))
		}, SummaryMismatch},
		"mismatch beats unchecked": {func(in *Input) {
			delete(in.Evidence, digest(1))
			in.Evidence[digest(2)] = verify.Evidence{Provenance: verify.Provenance{Status: verify.Invalid, CheckedAt: observed}}
		}, SummaryMismatch},
		"unchecked beats rolling out": {func(in *Input) {
			in.Images = append(in.Images, img(platform.Web, 5, observed.Add(-time.Minute)))
			in.Evidence[digest(5)] = verified(webRepo, "v0.6.4", 6)
			delete(in.Evidence, digest(3))
		}, SummaryUnverified},
		"sbom not found stays verified": {func(in *Input) {
			for _, n := range []int{1, 2, 3} {
				ev := in.Evidence[digest(n)]
				ev.SBOM = verify.SBOM{Status: verify.NotFound, CheckedAt: observed}
				in.Evidence[digest(n)] = ev
			}
		}, SummaryVerified},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := release065()
			tc.mutate(&in)
			doc := build(t, in)
			if doc.Summary != tc.summary {
				t.Errorf("summary = %q, want %q", doc.Summary, tc.summary)
			}
			if (doc.Release != nil) != (tc.summary == SummaryVerified) {
				t.Errorf("release = %+v with summary %q", doc.Release, doc.Summary)
			}
		})
	}
}

func TestVersionsDifferNoRelease(t *testing.T) {
	in := release065()
	in.Evidence[digest(3)] = verified(caddyRepo, "v0.6.4", 6)
	doc := build(t, in)
	if doc.Summary != SummaryVerified || doc.Release != nil {
		t.Errorf("summary %q release %+v", doc.Summary, doc.Release)
	}
}

func TestUnverifiedImageCarriesNoClaims(t *testing.T) {
	for _, st := range []verify.Status{verify.NotFound, verify.Invalid, verify.Unchecked} {
		in := release065()
		ev := verified(serverRepo, "v0.6.5", 7)
		ev.Provenance.Status = st
		in.Evidence[digest(1)] = ev
		doc := build(t, in)
		ri := doc.Components[0].RunningImages[0]
		if ri.Version != nil || ri.Commit != nil || ri.Signature.SignerWorkflow != nil ||
			ri.Signature.TransparencyLogIndex != nil || ri.Links.Commit != nil || ri.Links.Release != nil ||
			ri.Links.Build != nil || ri.Links.SBOM != nil || ri.Links.TransparencyLog != nil {
			t.Errorf("%s: claims leaked: %+v", st, ri)
		}
		if st == verify.Unchecked && ri.Signature.CheckedAt != nil {
			t.Errorf("unchecked carries checked_at %s", *ri.Signature.CheckedAt)
		}
	}
}

func TestSBOMLinksNeedAVerifiedSBOM(t *testing.T) {
	in := release065()
	ev := in.Evidence[digest(1)]
	ev.SBOM = verify.SBOM{Status: verify.NotFound, CheckedAt: observed}
	in.Evidence[digest(1)] = ev
	ri := build(t, in).Components[0].RunningImages[0]
	if ri.SBOM.Format != nil || ri.Links.SBOM != nil || ri.Links.Release != nil {
		t.Errorf("sbom %+v links %+v", ri.SBOM, ri.Links)
	}
	if ri.Version == nil || ri.Links.Build == nil {
		t.Error("a verified signature lost its claims")
	}
}

func TestRunningImagesOrderedByStart(t *testing.T) {
	in := release065()
	in.Images = append(in.Images, img(platform.Web, 5, observed.Add(-time.Hour)))
	in.Evidence[digest(5)] = verified(webRepo, "v0.6.4", 6)
	web := build(t, in).Components[1].RunningImages
	if web[0].Digest != digest(5) || web[1].Digest != digest(2) {
		t.Errorf("order = %s, %s", web[0].Digest, web[1].Digest)
	}
}

func TestPatternFailureDropsTheWrite(t *testing.T) {
	cases := map[string]func(*Input){
		"bad version": func(in *Input) {
			ev := in.Evidence[digest(1)]
			ev.Provenance.Version = "v0.6.5 "
			in.Evidence[digest(1)] = ev
		},
		"bad build": func(in *Input) {
			ev := in.Evidence[digest(1)]
			ev.Provenance.BuildURL = "https://evil.test/x"
			in.Evidence[digest(1)] = ev
		},
		"bad digest":    func(in *Input) { in.Images[0].Digest = "sha256:ABC" },
		"zero replicas": func(in *Input) { in.Images[0].Replicas = 0 },
		"65 replicas":   func(in *Input) { in.Images[0].Replicas = 65 },
		"bad region":    func(in *Input) { in.Platform.Region = "ap-southeast-1b" },
		"bad site":      func(in *Input) { in.Site = "https://evil.test" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := release065()
			mutate(&in)
			doc, err := Build(in)
			if err != nil {
				return
			}
			if b, err := Encode(doc); err == nil {
				t.Errorf("encoded %d bytes", len(b))
			}
		})
	}
}

func TestTooManyDigestsRefused(t *testing.T) {
	in := release065()
	for n := 10; n < 10+MaxImages; n++ {
		in.Images = append(in.Images, img(platform.Web, n, observed.Add(-time.Duration(n)*time.Minute)))
	}
	if _, err := Build(in); err == nil {
		t.Fatal("built a component with more than MaxImages digests")
	}
}

func TestSizeCap(t *testing.T) {
	in := release065()
	for _, c := range []string{platform.Server, platform.Web, platform.Caddy, platform.Maintenance} {
		for n := 0; n < MaxImages; n++ {
			d := 100*len(c) + n + 1000
			in.Images = append(in.Images, img(c, d, observed.Add(-time.Duration(n)*time.Minute)))
			in.Evidence[digest(d)] = verified(platform.ImageRepository(c), "v0.6.5", 7)
		}
	}
	in.Images = in.Images[3:]
	doc, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(doc)
	if err != nil {
		t.Fatalf("the largest valid document does not encode: %v", err)
	}
	if len(b) > MaxBytes {
		t.Fatalf("%d bytes over the cap", len(b))
	}
	var round Document
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownComponentRefused(t *testing.T) {
	in := release065()
	in.Images = append(in.Images, img("jobs", 9, observed))
	if _, err := Build(in); err == nil {
		t.Fatal("built a document with an unknown component")
	}
}

func TestSelfReportedBuildInfoOutsidePatternIsDropped(t *testing.T) {
	in := release065()
	in.Observer = BuildInfo{Version: "v0.6.5-rc1", Commit: "dirty"}
	doc := build(t, in)
	if doc.Observer.Version != nil || doc.Observer.Commit != nil {
		t.Errorf("observer = %+v, want null version and commit", doc.Observer)
	}
}
