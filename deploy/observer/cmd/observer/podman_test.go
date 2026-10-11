package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dannyota/aboutme/deploy/observer/internal/document"
	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
	podmanplatform "github.com/dannyota/aboutme/deploy/observer/internal/platform/podman"
	"github.com/dannyota/aboutme/deploy/observer/internal/publish"
)

func TestWirePodmanWithoutAWS(t *testing.T) {
	work := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(work, "missing-aws-config"))
	t.Setenv("AWS_PROFILE", "unconfigured")
	cfg := config{Platform: platformPodman, Region: "HCM03", ObservationFile: filepath.Join(work, "observation.json"), OutputDir: work}
	obs, err := wire(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := obs.lister.(*podmanplatform.Lister); !ok {
		t.Fatal("podman did not use the file adapter")
	}
	if _, ok := obs.publisher.(*publish.FilePublisher); !ok {
		t.Fatal("podman did not use the file publisher")
	}
	if obs.info != (platform.Info{Provider: "greennode", Orchestrator: "podman", Region: "HCM03"}) {
		t.Fatalf("unexpected platform: %+v", obs.info)
	}
	path := filepath.Join(work, "deployment.json")
	if err = os.WriteFile(path, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := document.Build(document.Input{
		Environment: environment, Site: site, Platform: obs.info,
		ObservedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := document.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg.ObservationFile, body, 0o644); err != nil {
		t.Fatal(err)
	}
	checker := &fakeChecker{}
	obs.verifier = checker
	if err = obs.run(t.Context(), fixedNow); err == nil {
		t.Fatal("stale podman observation did not fail the run")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "previous" || checker.calls != 0 {
		t.Fatal("stale observation changed public evidence")
	}
}
