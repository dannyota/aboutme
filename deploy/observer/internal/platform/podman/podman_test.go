package podman

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/deploy/observer/internal/document"
	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
)

var testNow = time.Date(2026, 10, 11, 1, 15, 0, 0, time.UTC)

func observation(t *testing.T, maintenance bool) *document.Document {
	t.Helper()
	images := []platform.Image{}
	for _, name := range platform.Components {
		if name == platform.Maintenance && !maintenance {
			continue
		}
		images = append(images, platform.Image{
			Component: name, Digest: "sha256:" + strings.Repeat("a", 64),
			Replicas: 1, RunningSince: testNow.Add(-time.Minute),
		})
	}
	doc, err := document.Build(document.Input{
		Environment: "production", Site: "https://aboutme.vn", ObservedAt: testNow,
		Platform: platform.Info{Provider: "greennode", Orchestrator: "podman", Region: "HCM03"},
		Images:   images,
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func marshal(t *testing.T, doc *document.Document) []byte {
	t.Helper()
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observation.json")
	if err := os.WriteFile(path, marshal(t, observation(t, true)), 0o644); err != nil {
		t.Fatal(err)
	}
	lister := New(path)
	lister.now = func() time.Time { return testNow }
	images, err := lister.Running(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 4 {
		t.Fatalf("got %d images, want 4", len(images))
	}
	if !lister.ObservationTime().Equal(testNow) {
		t.Fatal("lost the reporter's observation timestamp")
	}
	for _, image := range images {
		if image.Replicas != 1 || image.Digest != "sha256:"+strings.Repeat("a", 64) || !image.RunningSince.Equal(testNow.Add(-time.Minute)) {
			t.Fatalf("unexpected image: %+v", image)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := lister.Running(t.Context()); err == nil {
		t.Fatal("accepted a missing observation file")
	}
}

func TestParseRejectsInvalidObservation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*document.Document)
	}{
		{"unknown component", func(d *document.Document) { d.Components[0].Name = "observer" }},
		{"bad digest", func(d *document.Document) { d.Components[0].RunningImages[0].Digest = "sha256:bad" }},
		{"zero replicas", func(d *document.Document) { d.Components[0].RunningImages[0].Replicas = 0 }},
		{"too many replicas", func(d *document.Document) { d.Components[0].RunningImages[0].Replicas = 65 }},
		{"invalid date", func(d *document.Document) { d.Components[0].RunningImages[0].RunningSince = "2026-02-30T01:00:00Z" }},
		{"stale", func(d *document.Document) { d.ObservedAt = testNow.Add(-181 * time.Second).Format(time.RFC3339) }},
		{"future", func(d *document.Document) { d.ObservedAt = testNow.Add(61 * time.Second).Format(time.RFC3339) }},
		{"wrong provider", func(d *document.Document) { d.Platform.Provider = "aws" }},
		{"wrong orchestrator", func(d *document.Document) { d.Platform.Orchestrator = "ecs" }},
		{"duplicate component", func(d *document.Document) { d.Components[1] = d.Components[0] }},
		{"missing component", func(d *document.Document) { d.Components = d.Components[1:] }},
		{"empty maintenance", func(d *document.Document) { d.Components[3].RunningImages = []document.RunningImage{} }},
		{"duplicate digest", func(d *document.Document) {
			d.Components[0].RunningImages = append(d.Components[0].RunningImages, d.Components[0].RunningImages[0])
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := observation(t, true)
			tc.mutate(doc)
			if _, _, err := parse(marshal(t, doc), testNow); err == nil {
				t.Fatal("accepted invalid observation")
			}
		})
	}
	valid := marshal(t, observation(t, false))
	for name, body := range map[string][]byte{
		"oversize":             append(valid, bytes.Repeat([]byte(" "), MaxBytes)...),
		"unknown field":        bytes.Replace(valid, []byte(`"replicas":1`), []byte(`"extra":true,"replicas":1`), 1),
		"trailing JSON":        append(valid, []byte(`{}`)...),
		"duplicate field":      bytes.Replace(valid, []byte(`"replicas":1`), []byte(`"replicas":2,"replicas":1`), 1),
		"duplicate root field": bytes.Replace(valid, []byte(`"project":"aboutme"`), []byte(`"project":"aboutme","project":"aboutme"`), 1),
		"hidden unknown field": bytes.Replace(valid, []byte(`"signature":{`), []byte(`"signature":{"extra":true},"signature":{`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parse(body, testNow); err == nil {
				t.Fatal("accepted invalid observation")
			}
		})
	}
}

func TestParseFreshnessBoundariesAndMissingContainer(t *testing.T) {
	for _, offset := range []time.Duration{-180 * time.Second, 60 * time.Second} {
		doc := observation(t, false)
		doc.ObservedAt = testNow.Add(offset).Format(time.RFC3339)
		doc.Components[1].RunningImages = []document.RunningImage{}
		images, observed, err := parse(marshal(t, doc), testNow)
		if err != nil {
			t.Fatal(err)
		}
		if len(images) != 2 {
			t.Fatalf("got %d images, want 2", len(images))
		}
		if !observed.Equal(testNow.Add(offset)) {
			t.Fatal("changed the accepted observation timestamp")
		}
	}
}

func TestParseCountsReplicas(t *testing.T) {
	doc := observation(t, false)
	doc.Components[0].RunningImages[0].Replicas = 64
	images, _, err := parse(marshal(t, doc), testNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, image := range images {
		if image.Component == platform.Server && image.Replicas != 64 {
			t.Fatalf("server replicas = %d, want 64", image.Replicas)
		}
	}
}

func TestRunningBoundsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observation.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte(" "), MaxBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path).Running(t.Context()); err == nil {
		t.Fatal("accepted an oversized file")
	}
}
