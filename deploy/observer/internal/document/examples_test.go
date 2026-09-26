package document

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
	"github.com/dannyota/aboutme/deploy/observer/internal/verify"
)

// updateExamples rewrites testdata/examples/*.json from the current
// Build/Encode output, for an intentional change to the document shape.
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
		Subjects:       []string{platform.ImageRepository(component)},
		Version:        version,
		Commit:         commit,
		SignerWorkflow: exampleSignerWorkflow(version),
		LogIndex:       exampleLogIndex,
		BuildURL:       exampleBuildURL,
	}
}

func exampleVerifiedSBOM(checkedAt time.Time, component string) verify.SBOM {
	return verify.SBOM{Status: verify.Verified, CheckedAt: checkedAt, Subjects: []string{platform.ImageRepository(component)}}
}

func exampleNotFoundEvidence(digest string, checkedAt time.Time) verify.Evidence {
	return verify.Evidence{
		Digest:     digest,
		Provenance: verify.Provenance{Status: verify.NotFound, CheckedAt: checkedAt},
		SBOM:       verify.SBOM{Status: verify.NotFound, CheckedAt: checkedAt},
	}
}

var examplePlatformAWS = platform.Info{Provider: "aws", Orchestrator: "ecs", Region: "ap-southeast-1"}

func exampleImage(component, digest, since string, t *testing.T) platform.Image {
	return platform.Image{Component: component, Digest: digest, Replicas: 1, RunningSince: exampleMustParse(t, since)}
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
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatformAWS,
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
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatformAWS,
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
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatformAWS,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestServer, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Web, exampleDigestWebNew, "2026-10-02T03:11:00Z", t),
			exampleImage(platform.Caddy, exampleDigestUnchecked, "2026-10-02T03:12:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestServer: verifiedEvidence(exampleDigestServer, "v0.6.5", exampleCommit65, platform.Server),
			exampleDigestWebNew: verifiedEvidence(exampleDigestWebNew, "v0.6.5", exampleCommit65, platform.Web),
			// exampleDigestUnchecked has no entry: the observer has not
			// finished checking it yet.
		},
		Observer: BuildInfo{Version: "v0.6.5", Commit: exampleCommit65},
	}

	scenarios["mismatch-not-found"] = Input{
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatformAWS,
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
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatformAWS,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestServer, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Web, exampleDigestWebNew, "2026-10-02T03:11:00Z", t),
			exampleImage(platform.Caddy, exampleDigestInvalid, "2026-10-02T03:12:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestServer: verifiedEvidence(exampleDigestServer, "v0.6.5", exampleCommit65, platform.Server),
			exampleDigestWebNew: verifiedEvidence(exampleDigestWebNew, "v0.6.5", exampleCommit65, platform.Web),
			// The signed statement for this digest names the web image, not
			// caddy's own: a build for a different component running under
			// this component's name.
			exampleDigestInvalid: {
				Digest:     exampleDigestInvalid,
				Provenance: exampleVerifiedProvenance("v0.6.5", exampleCommit65, checked, platform.Web),
				SBOM:       verify.SBOM{Status: verify.NotFound, CheckedAt: checked},
			},
		},
		Observer: BuildInfo{Version: "v0.6.5", Commit: exampleCommit65},
	}

	scenarios["mismatch-missing-component"] = Input{
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatformAWS,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestServer, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Caddy, exampleDigestCaddy, "2026-10-02T03:12:00Z", t),
			// No web task or pod is running at all.
		},
		Evidence: map[string]verify.Evidence{
			exampleDigestServer: verifiedEvidence(exampleDigestServer, "v0.6.5", exampleCommit65, platform.Server),
			exampleDigestCaddy:  verifiedEvidence(exampleDigestCaddy, "v0.6.5", exampleCommit65, platform.Caddy),
		},
		Observer: BuildInfo{Version: "v0.6.5", Commit: exampleCommit65},
	}

	scenarios["sbom-not-found"] = Input{
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatformAWS,
		ObservedAt:  observedAt,
		Images: []platform.Image{
			exampleImage(platform.Server, exampleDigestSBOMMiss, "2026-10-02T03:10:00Z", t),
			exampleImage(platform.Web, exampleDigestWebNew, "2026-10-02T03:11:00Z", t),
			exampleImage(platform.Caddy, exampleDigestCaddy, "2026-10-02T03:12:00Z", t),
		},
		Evidence: map[string]verify.Evidence{
			// server predates the SBOM-attestation release
			// (docs/design/deployment-transparency/README.md, "Release plan
			// and compatibility"): it has signed provenance but no SBOM.
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

	// A stale document was checked before it was observed, long ago.
	staleChecked := exampleMustParse(t, "2026-01-01T00:10:00Z")
	staleEvidence := func(digest, component string) verify.Evidence {
		return verify.Evidence{
			Digest:     digest,
			Provenance: exampleVerifiedProvenance("v0.6.5", exampleCommit65, staleChecked, component),
			SBOM:       exampleVerifiedSBOM(staleChecked, component),
		}
	}
	scenarios["stale"] = Input{
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    examplePlatformAWS,
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

	return scenarios
}

// TestExamples builds every state the /verify page renders from fixed
// inputs and compares the encoded bytes against testdata/examples/*.json.
// Run with -update to regenerate a golden file after an intentional change
// (deploy/observer/README.md, "Example documents").
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

// TestFutureVersionIsNeverValidated asserts the hand-written
// future-version.json is not schema_version 1 and is only used by the
// frontend, never by this package's own validation.
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
