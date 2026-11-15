package podman

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/deploy/observer/internal/document"
	"danny.vn/aboutme/deploy/observer/internal/platform"
	"danny.vn/aboutme/deploy/observer/internal/publish"
	"danny.vn/aboutme/deploy/observer/internal/verify"
)

var updatePublished = flag.Bool("update", false, "update testdata/podman/deployment.json")

const (
	publishedVersion = "v0.7.3"
	publishedCommit  = "4c2e81d04c2e81d04c2e81d04c2e81d04c2e81d0"
	publishedBuild   = "https://github.com/dannyota/aboutme/actions/runs/36218940587/attempts/1"
)

var publishedInfo = platform.Info{Provider: "greennode", Orchestrator: "podman", Region: "HCM03"}

func publishedEvidence(images []platform.Image, checkedAt time.Time) map[string]verify.Evidence {
	evidence := map[string]verify.Evidence{}
	for _, image := range images {
		repo := platform.ImageRepository(image.Component)
		evidence[image.Digest] = verify.Evidence{
			Digest: image.Digest,
			Provenance: verify.Provenance{
				Status: verify.Verified, CheckedAt: checkedAt, Subjects: []string{repo},
				Version: publishedVersion, Commit: publishedCommit,
				SignerWorkflow: "https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/" + publishedVersion,
				LogIndex:       2965228543,
				BuildURL:       publishedBuild,
			},
			SBOM: verify.SBOM{Status: verify.Verified, CheckedAt: checkedAt, Subjects: []string{repo}},
		}
	}
	return evidence
}

func TestPublishedDocument_MatchesGolden(t *testing.T) {
	running := testNow.Add(-5 * time.Minute)
	reported, err := document.Build(document.Input{
		Environment: "production", Site: "https://aboutme.vn", Platform: publishedInfo,
		ObservedAt: testNow.Add(-30 * time.Second),
		Images: []platform.Image{
			{Component: platform.Server, Digest: "sha256:" + strings.Repeat("1a", 32), Replicas: 1, RunningSince: running},
			{Component: platform.Web, Digest: "sha256:" + strings.Repeat("2b", 32), Replicas: 1, RunningSince: running},
			{Component: platform.Caddy, Digest: "sha256:" + strings.Repeat("3c", 32), Replicas: 1, RunningSince: running},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	path := filepath.Join(work, "observation.json")
	if err = os.WriteFile(path, marshal(t, reported), 0o644); err != nil {
		t.Fatal(err)
	}
	lister := New(path)
	lister.now = func() time.Time { return testNow }
	images, err := lister.Running(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.Build(document.Input{
		Environment: "production", Site: "https://aboutme.vn", Platform: publishedInfo,
		ObservedAt: lister.ObservationTime(),
		Images:     images,
		Evidence:   publishedEvidence(images, testNow.Add(-20*time.Second)),
		Observer:   document.BuildInfo{Version: publishedVersion, Commit: publishedCommit},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := document.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	public := filepath.Join(work, "public")
	if err = os.Mkdir(public, 0o755); err != nil {
		t.Fatal(err)
	}
	if err = publish.NewFilePublisher(public).Put(t.Context(), body); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(public, "deployment.json"))
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("..", "..", "..", "testdata", "podman", "deployment.json")
	if *updatePublished {
		if err = os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("published podman document does not match %s\ngot:\n%s\nwant:\n%s", golden, got, want)
	}
}
