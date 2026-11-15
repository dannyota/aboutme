package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"danny.vn/aboutme/deploy/observer/internal/document"
	"danny.vn/aboutme/deploy/observer/internal/platform"
	"danny.vn/aboutme/deploy/observer/internal/verify"
)

type evidenceChecker map[string]verify.Evidence

func (e evidenceChecker) Check(_ context.Context, digest string) verify.Evidence { return e[digest] }

func TestRunPublishesPodmanGolden(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Date(2026, 10, 11, 1, 15, 0, 0, time.UTC)
		time.Sleep(time.Until(now))
		const (
			releaseVersion = "v0.7.3"
			releaseCommit  = "4c2e81d04c2e81d04c2e81d04c2e81d04c2e81d0"
		)
		running := now.Add(-5 * time.Minute)
		images := []platform.Image{
			{Component: platform.Server, Digest: "sha256:" + strings.Repeat("1a", 32), Replicas: 1, RunningSince: running},
			{Component: platform.Web, Digest: "sha256:" + strings.Repeat("2b", 32), Replicas: 1, RunningSince: running},
			{Component: platform.Caddy, Digest: "sha256:" + strings.Repeat("3c", 32), Replicas: 1, RunningSince: running},
		}
		reported, err := document.Build(document.Input{
			Environment: environment, Site: site,
			Platform:   platform.Info{Provider: "greennode", Orchestrator: "podman", Region: "HCM03"},
			ObservedAt: now.Add(-30 * time.Second),
			Images:     images,
		})
		if err != nil {
			t.Fatal(err)
		}
		observation, err := json.Marshal(reported)
		if err != nil {
			t.Fatal(err)
		}
		work := t.TempDir()
		cfg := config{
			Platform: platformPodman, Region: "HCM03",
			ObservationFile: filepath.Join(work, "observation.json"),
			OutputDir:       filepath.Join(work, "public"),
		}
		if err = os.WriteFile(cfg.ObservationFile, observation, 0o644); err != nil {
			t.Fatal(err)
		}
		if err = os.Mkdir(cfg.OutputDir, 0o755); err != nil {
			t.Fatal(err)
		}
		obs, err := wire(t.Context(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		checkedAt := now.Add(-20 * time.Second)
		evidence := evidenceChecker{}
		for _, image := range images {
			repo := platform.ImageRepository(image.Component)
			evidence[image.Digest] = verify.Evidence{
				Digest: image.Digest,
				Provenance: verify.Provenance{
					Status: verify.Verified, CheckedAt: checkedAt, Subjects: []string{repo},
					Version: releaseVersion, Commit: releaseCommit,
					SignerWorkflow: "https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/" + releaseVersion,
					LogIndex:       2965228543,
					BuildURL:       "https://github.com/dannyota/aboutme/actions/runs/36218940587/attempts/1",
				},
				SBOM: verify.SBOM{Status: verify.Verified, CheckedAt: checkedAt, Subjects: []string{repo}},
			}
		}
		obs.verifier = evidence
		obs.build = document.BuildInfo{Version: releaseVersion, Commit: releaseCommit}

		if err = obs.run(t.Context(), time.Now); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(cfg.OutputDir, "deployment.json"))
		if err != nil {
			t.Fatal(err)
		}
		golden := filepath.Join("..", "..", "testdata", "podman", "deployment.json")
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("observer run published a podman document that does not match %s\ngot:\n%s\nwant:\n%s", golden, got, want)
		}
	})
}
