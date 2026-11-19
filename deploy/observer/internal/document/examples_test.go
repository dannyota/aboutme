package document

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/deploy/observer/internal/platform"
	"danny.vn/aboutme/deploy/observer/internal/source"
	"danny.vn/aboutme/deploy/observer/internal/verify"
)

var updateExamples = flag.Bool("update", false, "update golden files in testdata/examples")

const (
	exampleCommit65  = "3b1f9e073b1f9e073b1f9e073b1f9e073b1f9e07"
	exampleCommit64  = "77e0c2d177e0c2d177e0c2d177e0c2d177e0c2d1"
	exampleCommit521 = "d73f40430000000000000000000000000000000d"
	exampleBuildURL  = "https://github.com/dannyota/aboutme/actions/runs/36218940587/attempts/1"
	exampleLogIndex  = 2965228543
)

func exampleRepeatHex(pair string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(pair)
	}
	return b.String()
}

var (
	exampleDigestServer    = "sha256:" + exampleRepeatHex("9f2c", 16)
	exampleDigestWebNew    = "sha256:" + exampleRepeatHex("a41b", 16)
	exampleDigestWebOld    = "sha256:" + exampleRepeatHex("77e0", 16)
	exampleDigestCaddy     = "sha256:" + exampleRepeatHex("ca44", 16)
	exampleDigestMaint     = "sha256:" + exampleRepeatHex("ca55", 16)
	exampleDigestUnchecked = "sha256:" + exampleRepeatHex("ab01", 16)
	exampleDigestNotFound  = "sha256:" + exampleRepeatHex("1234", 16)
	exampleDigestInvalid   = "sha256:" + exampleRepeatHex("5678", 16)
	exampleDigestSBOMMiss  = "sha256:" + exampleRepeatHex("9e0d", 16)
)

func exampleMustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return tm
}

func exampleSignerWorkflow(version string) string {
	return "https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/" + version
}

func exampleVerifiedProvenance(version, commit string, checkedAt time.Time, component string) verify.Provenance {
	return verify.Provenance{
		Status:         verify.Verified,
		CheckedAt:      checkedAt,
		Subjects:       []string{platform.Repository(source.Dannyota, component)},
		Version:        version,
		Commit:         commit,
		SignerWorkflow: exampleSignerWorkflow(version),
		LogIndex:       exampleLogIndex,
		BuildURL:       exampleBuildURL,
	}
}

func exampleVerifiedSBOM(checkedAt time.Time, component string) verify.SBOM {
	return verify.SBOM{Status: verify.Verified, CheckedAt: checkedAt, Subjects: []string{platform.Repository(source.Dannyota, component)}}
}

func exampleNotFoundEvidence(digest string, checkedAt time.Time) verify.Evidence {
	return verify.Evidence{
		Digest:     digest,
		Provenance: verify.Provenance{Status: verify.NotFound, CheckedAt: checkedAt},
		SBOM:       verify.SBOM{Status: verify.NotFound, CheckedAt: checkedAt},
	}
}

var examplePlatform = platform.Info{Provider: "greennode", Orchestrator: "podman", Region: "HCM03"}

func exampleImage(component, digest, since string, t *testing.T) platform.Image {
	return platform.Image{
		Component: component, Repository: platform.Repository(source.Dannyota, component),
		Digest: digest, Replicas: 1, RunningSince: exampleMustParse(t, since),
	}
}

func exampleMovedTo(in Input, owner string) Input {
	moved := func(s string) string { return strings.Replace(s, "/"+source.Dannyota+"/", "/"+owner+"/", 1) }
	movedAll := func(names []string) []string {
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = moved(n)
		}
		return out
	}
	in.Owner = owner
	in.Images = append([]platform.Image{}, in.Images...)
	for i := range in.Images {
		in.Images[i].Repository = moved(in.Images[i].Repository)
	}
	evidence := map[string]verify.Evidence{}
	for d, ev := range in.Evidence {
		ev.Provenance.Subjects = movedAll(ev.Provenance.Subjects)
		ev.Provenance.SignerWorkflow = moved(ev.Provenance.SignerWorkflow)
		ev.Provenance.BuildURL = moved(ev.Provenance.BuildURL)
		ev.SBOM.Subjects = movedAll(ev.SBOM.Subjects)
		evidence[d] = ev
	}
	in.Evidence = evidence
	return in
}

// exampleScenarios builds every state the /verify page renders
// (docs/design/deployment-transparency/document.md, "Summary and release").
// Each Input mirrors a fixed, realistic deployment, never invented data.
func exampleScenarios(t *testing.T) map[string]Input {
	t.Helper()
	checked := exampleMustParse(t, "2026-10-02T03:15:00Z")
	observedAt := exampleMustParse(t, "2026-10-02T03:20:00Z")

	verifiedEvidence := func(digest, version, commit, component string) verify.Evidence {
		return verify.Evidence{
			Digest:     digest,
			Provenance: exampleVerifiedProvenance(version, commit, checked, component),
			SBOM:       exampleVerifiedSBOM(checked, component),
		}
	}

	scenarios := map[string]Input{}

	scenarios["verified"] = Input{
		Owner:       source.Dannyota,
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatform,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestServer, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Web, exampleDigestWebNew, "2026-10-02T03:11:00Z", t),
			exampleImage(platform.Caddy, exampleDigestCaddy, "2026-10-02T03:12:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestServer: verifiedEvidence(exampleDigestServer, "v0.6.5", exampleCommit65, platform.Server),
			exampleDigestWebNew: verifiedEvidence(exampleDigestWebNew, "v0.6.5", exampleCommit65, platform.Web),
			exampleDigestCaddy:  verifiedEvidence(exampleDigestCaddy, "v0.6.5", exampleCommit65, platform.Caddy),
		},
		Observer: BuildInfo{Version: "v0.6.5", Commit: exampleCommit65},
	}

	withMaintenance := scenarios["verified"]
	withMaintenance.Images = append(append([]platform.Image{}, withMaintenance.Images...),
		exampleImage(platform.Maintenance, exampleDigestMaint, "2026-10-02T03:05:00Z", t))
	withMaintenance.Evidence = map[string]verify.Evidence{
		exampleDigestServer: verifiedEvidence(exampleDigestServer, "v0.6.5", exampleCommit65, platform.Server),
		exampleDigestWebNew: verifiedEvidence(exampleDigestWebNew, "v0.6.5", exampleCommit65, platform.Web),
		exampleDigestCaddy:  verifiedEvidence(exampleDigestCaddy, "v0.6.5", exampleCommit65, platform.Caddy),
		exampleDigestMaint:  verifiedEvidence(exampleDigestMaint, "v0.6.5", exampleCommit65, platform.Caddy),
	}
	scenarios["verified-with-maintenance"] = withMaintenance

	scenarios["rolling-out"] = Input{
		Owner:       source.Dannyota,
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatform,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestServer, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Caddy, exampleDigestCaddy, "2026-10-02T03:12:00Z", t),
			exampleImage(platform.Web, exampleDigestWebOld, "2026-10-02T02:00:00Z", t),
			exampleImage(platform.Web, exampleDigestWebNew, "2026-10-02T03:11:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestServer: verifiedEvidence(exampleDigestServer, "v0.6.5", exampleCommit65, platform.Server),
			exampleDigestCaddy:  verifiedEvidence(exampleDigestCaddy, "v0.6.5", exampleCommit65, platform.Caddy),
			exampleDigestWebOld: verifiedEvidence(exampleDigestWebOld, "v0.6.4", exampleCommit64, platform.Web),
			exampleDigestWebNew: verifiedEvidence(exampleDigestWebNew, "v0.6.5", exampleCommit65, platform.Web),
		},
		Observer: BuildInfo{Version: "v0.6.4", Commit: exampleCommit64},
	}

	scenarios["unverified"] = Input{
		Owner:       source.Dannyota,
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatform,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestServer, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Web, exampleDigestWebNew, "2026-10-02T03:11:00Z", t),
			exampleImage(platform.Caddy, exampleDigestUnchecked, "2026-10-02T03:12:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestServer: verifiedEvidence(exampleDigestServer, "v0.6.5", exampleCommit65, platform.Server),
			exampleDigestWebNew: verifiedEvidence(exampleDigestWebNew, "v0.6.5", exampleCommit65, platform.Web),
		},
		Observer: BuildInfo{Version: "v0.6.5", Commit: exampleCommit65},
	}

	scenarios["mismatch-not-found"] = Input{
		Owner:       source.Dannyota,
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatform,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestServer, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Web, exampleDigestWebNew, "2026-10-02T03:11:00Z", t),
			exampleImage(platform.Caddy, exampleDigestNotFound, "2026-10-02T03:12:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestServer:   verifiedEvidence(exampleDigestServer, "v0.6.5", exampleCommit65, platform.Server),
			exampleDigestWebNew:   verifiedEvidence(exampleDigestWebNew, "v0.6.5", exampleCommit65, platform.Web),
			exampleDigestNotFound: exampleNotFoundEvidence(exampleDigestNotFound, checked),
		},
		Observer: BuildInfo{Version: "v0.6.5", Commit: exampleCommit65},
	}

	scenarios["mismatch-invalid"] = Input{
		Owner:       source.Dannyota,
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatform,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestServer, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Web, exampleDigestWebNew, "2026-10-02T03:11:00Z", t),
			exampleImage(platform.Caddy, exampleDigestInvalid, "2026-10-02T03:12:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestServer: verifiedEvidence(exampleDigestServer, "v0.6.5", exampleCommit65, platform.Server),
			exampleDigestWebNew: verifiedEvidence(exampleDigestWebNew, "v0.6.5", exampleCommit65, platform.Web),
			exampleDigestInvalid: {
				Digest:     exampleDigestInvalid,
				Provenance: exampleVerifiedProvenance("v0.6.5", exampleCommit65, checked, platform.Web),
				SBOM:       verify.SBOM{Status: verify.NotFound, CheckedAt: checked},
			},
		},
		Observer: BuildInfo{Version: "v0.6.5", Commit: exampleCommit65},
	}

	scenarios["mismatch-missing-component"] = Input{
		Owner:       source.Dannyota,
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatform,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestServer, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Caddy, exampleDigestCaddy, "2026-10-02T03:12:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestServer: verifiedEvidence(exampleDigestServer, "v0.6.5", exampleCommit65, platform.Server),
			exampleDigestCaddy:  verifiedEvidence(exampleDigestCaddy, "v0.6.5", exampleCommit65, platform.Caddy),
		},
		Observer: BuildInfo{Version: "v0.6.5", Commit: exampleCommit65},
	}

	scenarios["sbom-not-found"] = Input{
		Owner:       source.Dannyota,
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatform,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestSBOMMiss, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Web, exampleDigestWebNew, "2026-10-02T03:11:00Z", t),
			exampleImage(platform.Caddy, exampleDigestCaddy, "2026-10-02T03:12:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestSBOMMiss: {
				Digest:     exampleDigestSBOMMiss,
				Provenance: exampleVerifiedProvenance("v0.5.21", exampleCommit521, checked, platform.Server),
				SBOM:       verify.SBOM{Status: verify.NotFound, CheckedAt: checked},
			},
			exampleDigestWebNew: verifiedEvidence(exampleDigestWebNew, "v0.5.21", exampleCommit521, platform.Web),
			exampleDigestCaddy:  verifiedEvidence(exampleDigestCaddy, "v0.5.21", exampleCommit521, platform.Caddy),
		},
		Observer: BuildInfo{Version: "v0.5.21", Commit: exampleCommit521},
	}

	staleChecked := exampleMustParse(t, "2026-01-01T00:10:00Z")
	staleEvidence := func(digest, component string) verify.Evidence {
		return verify.Evidence{
			Digest:     digest,
			Provenance: exampleVerifiedProvenance("v0.6.5", exampleCommit65, staleChecked, component),
			SBOM:       exampleVerifiedSBOM(staleChecked, component),
		}
	}
	scenarios["stale"] = Input{
		Owner:       source.Dannyota,
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatform,
		ObservedAt:  exampleMustParse(t, "2026-01-01T00:15:00Z"),
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestServer, "2026-01-01T00:00:00Z", t),
			exampleImage(platform.Web, exampleDigestWebNew, "2026-01-01T00:01:00Z", t),
			exampleImage(platform.Caddy, exampleDigestCaddy, "2026-01-01T00:02:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestServer: staleEvidence(exampleDigestServer, platform.Server),
			exampleDigestWebNew: staleEvidence(exampleDigestWebNew, platform.Web),
			exampleDigestCaddy:  staleEvidence(exampleDigestCaddy, platform.Caddy),
		},
		Observer: BuildInfo{Version: "v0.6.5", Commit: exampleCommit65},
	}

	scenarios["verified-dntmakers"] = exampleMovedTo(scenarios["verified"], source.Dntmakers)

	return scenarios
}

func TestExamples(t *testing.T) {
	scenarios := exampleScenarios(t)
	for name, in := range scenarios {
		name, in := name, in
		t.Run(name, func(t *testing.T) {
			doc, err := Build(in)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			got, err := Encode(doc)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			path := filepath.Join("..", "..", "testdata", "examples", name+".json")
			if *updateExamples {
				if err = os.WriteFile(path, got, 0o644); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if string(got) != string(want) {
				t.Errorf("%s: encoded document does not match the golden file (run with -update after an intentional change)\ngot:\n%s\nwant:\n%s", path, got, want)
			}
		})
	}
}

func TestFutureVersionIsNeverValidated(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "examples", "future-version.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(b), `"schema_version": 2`) {
		t.Errorf("%s: does not declare schema_version 2", path)
	}
}
