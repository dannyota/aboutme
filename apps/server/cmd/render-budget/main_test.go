package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

func TestParseConfigRequiresClosedLocalPaths(t *testing.T) {
	t.Parallel()
	repositoryRoot := testRepositoryRoot(t)
	chromium := filepath.Join(t.TempDir(), "chromium")
	if err := os.WriteFile(chromium, []byte("browser"), 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(repositoryRoot, ".dev", "phase-7", "render-budget-test")

	config, err := parseConfig([]string{
		"--repository-root", repositoryRoot,
		"--chromium-executable", chromium,
		"--output-directory", output,
	})
	if err != nil {
		t.Fatalf("parseConfig(valid) error = %v", err)
	}
	if config.RepositoryRoot != repositoryRoot || config.BrowserExecutable != chromium || config.OutputDirectory != output || config.Probe {
		t.Fatalf("parseConfig(valid) = %+v", config)
	}
	probeConfig, err := parseConfig([]string{
		"--repository-root", repositoryRoot,
		"--chromium-executable", chromium,
		"--output-directory", output,
		"--probe",
	})
	if err != nil || !probeConfig.Probe {
		t.Fatalf("parseConfig(probe) = (%+v, %v)", probeConfig, err)
	}

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "missing repository", args: []string{"--chromium-executable", chromium, "--output-directory", output}},
		{name: "missing browser", args: []string{"--repository-root", repositoryRoot, "--output-directory", output}},
		{name: "missing output", args: []string{"--repository-root", repositoryRoot, "--chromium-executable", chromium}},
		{name: "unknown network flag", args: []string{"--repository-root", repositoryRoot, "--chromium-executable", chromium, "--output-directory", output, "--render-origin", "https://example.com"}},
		{name: "relative repository", args: []string{"--repository-root", ".", "--chromium-executable", chromium, "--output-directory", output}},
		{name: "relative browser", args: []string{"--repository-root", repositoryRoot, "--chromium-executable", "chromium", "--output-directory", output}},
		{name: "output outside phase directory", args: []string{"--repository-root", repositoryRoot, "--chromium-executable", chromium, "--output-directory", t.TempDir()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseConfig(test.args); err == nil {
				t.Fatal("parseConfig() error = nil")
			}
		})
	}
}

func TestBrowserVersionProbeDoesNotInheritParentEnvironment(t *testing.T) {
	t.Setenv("ABOUTME_PARENT_SECRET", "synthetic-sentinel")
	executable := filepath.Join(t.TempDir(), "version-probe")
	program := `#!/bin/sh
if [ "${ABOUTME_PARENT_SECRET+x}" = x ]; then
  printf 'parent-environment-inherited'
elif [ "$TZ" != UTC ] || [ "$LANG" != C.UTF-8 ] || [ "$LC_ALL" != C.UTF-8 ]; then
  printf 'unexpected-locale'
else
  printf 'test-browser'
fi
`
	if err := os.WriteFile(executable, []byte(program), 0o755); err != nil {
		t.Fatal(err)
	}
	version, err := readBrowserVersion(t.Context(), executable)
	if err != nil || version != "test-browser" {
		t.Fatalf("browser version probe = %q, %v", version, err)
	}
}

func TestPrepareOutputDirectoryRefusesExistingContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := filepath.Join(root, ".dev", "phase-7")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(base, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareOutputDirectory(root, empty); err != nil {
		t.Fatalf("prepareOutputDirectory(empty) error = %v", err)
	}
	nonempty := filepath.Join(base, "nonempty")
	if err := os.Mkdir(nonempty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonempty, "evidence.json"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepareOutputDirectory(root, nonempty); err == nil {
		t.Fatal("prepareOutputDirectory(nonempty) error = nil")
	}
}

func TestBuildFixtureCorpusMatchesMeasuredContract(t *testing.T) {
	t.Parallel()
	fixtures, err := buildFixtureCorpus(testRepositoryRoot(t))
	if err != nil {
		t.Fatalf("buildFixtureCorpus() error = %v", err)
	}
	if len(fixtures) != 3 {
		t.Fatalf("fixtures = %d, want 3", len(fixtures))
	}
	byName := make(map[string]fixture, len(fixtures))
	for _, item := range fixtures {
		byName[item.Name] = item
		if err := resume.ValidateForStore(item.Document); err != nil {
			t.Fatalf("fixture %q is not store-valid: %v", item.Name, err)
		}
		canonical, err := resume.AssembleCanonical(item.Document)
		if err != nil {
			t.Fatalf("fixture %q canonical encoding: %v", item.Name, err)
		}
		if string(canonical) != string(item.Canonical) {
			t.Fatalf("fixture %q retained noncanonical bytes", item.Name)
		}
		if len(item.Snapshot.Payload) == 0 {
			t.Fatalf("fixture %q has no print snapshot", item.Name)
		}
	}

	minimal := byName["minimal"]
	if minimal.Document.Content == nil || len(minimal.Document.Content) != 0 {
		t.Fatalf("minimal content = %#v, want preserved empty object", minimal.Document.Content)
	}
	full := byName["full"]
	if full.Stats.InlinePhotoBytes == 0 || full.PhotoContentType != "image/png" {
		t.Fatalf("full photo = %d/%q, want normalized PNG", full.Stats.InlinePhotoBytes, full.PhotoContentType)
	}
	maximum := byName["maximum"]
	if len(maximum.Canonical) != resume.MaxDocumentBytes {
		t.Fatalf("maximum canonical bytes = %d, want %d", len(maximum.Canonical), resume.MaxDocumentBytes)
	}
	if maximum.Stats.SectionCount != 24 || maximum.Stats.EntryCount != 24*64 {
		t.Fatalf("maximum counts = %d sections/%d entries", maximum.Stats.SectionCount, maximum.Stats.EntryCount)
	}
	if maximum.Stats.LargestRichTextBytes != 16_384 {
		t.Fatalf("maximum rich text = %d, want 16384", maximum.Stats.LargestRichTextBytes)
	}
	if maximum.Stats.HiddenEntryCount != 0 || maximum.Stats.LayoutSectionCount != 24 {
		t.Fatalf("maximum visibility = %d hidden/%d layout sections", maximum.Stats.HiddenEntryCount, maximum.Stats.LayoutSectionCount)
	}
}

func TestEvidenceJSONHasStableProtocolAndSafeFailures(t *testing.T) {
	t.Parallel()
	samples := []sampleEvidence{
		{Fixture: "maximum", Format: renderjob.PDF, Round: 1, Kind: sampleMeasured, Index: 1, DurationNanoseconds: int64(7 * time.Second), Outcome: outcomeSuccess},
		{Fixture: "maximum", Format: renderjob.PDF, Round: 1, Kind: sampleMeasured, Index: 2, DurationNanoseconds: int64(9 * time.Second), Outcome: outcomeFailure, FailureCode: failureTimeout},
	}
	evidence := newEvidence(modeFull, "go-test", "Chromium 151.0.7922.34", cgroupEvidence{
		Version: 2, Path: "/test.scope", MemoryMax: "536870912", MemorySwapMax: "0", CPUMax: "50000 100000",
	}, nil)
	evidence.SerialSamples = samples
	evidence.Series = []seriesEvidence{{
		Fixture: "maximum", Format: renderjob.PDF, Round: 1, MeasuredSamples: 10,
		MeasuredSuccesses: 9, MeasuredFailures: 1, AttemptP95Nanoseconds: int64(9 * time.Second), SLOMet: false,
	}}
	evidence.Gate = evaluateGate(evidence)

	encoded, err := marshalEvidence(evidence)
	if err != nil {
		t.Fatalf("marshalEvidence() error = %v", err)
	}
	if !strings.HasSuffix(string(encoded), "\n") || strings.Contains(string(encoded), "raw dependency") {
		t.Fatalf("unsafe or unstable evidence: %q", encoded)
	}
	var decoded evidenceDocument
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if decoded.Version != evidenceVersion || decoded.Protocol.Mode != modeFull || decoded.Protocol.WarmupsPerSeries != 2 || decoded.Protocol.MeasuredSamplesPerSeries != 10 || decoded.Protocol.SeriesRepeats != 2 || decoded.Protocol.QueuedWaveCalls != 9 || decoded.Protocol.CancellationDeadlineNanoseconds != int64(renderjob.MaxJobTimeout) {
		t.Fatalf("protocol drifted: %+v", decoded.Protocol)
	}
	if decoded.Gate.Passed || decoded.SerialSamples[1].FailureCode != failureTimeout {
		t.Fatalf("failure was not represented safely: %+v", decoded.Gate)
	}
}

func TestNearestRankP95(t *testing.T) {
	t.Parallel()
	values := []time.Duration{9, 1, 5, 3, 7, 2, 4, 6, 8, 10}
	if got := nearestRankP95(values); got != 10 {
		t.Fatalf("nearestRankP95() = %v, want 10", got)
	}
}

func TestExpectedQueuedDeadlineSheddingDoesNotFailSerialGate(t *testing.T) {
	t.Parallel()
	evidence := newEvidence(modeFull, "go-test", "Chromium 151.0.7922.34", cgroupEvidence{}, nil)
	duration := renderjob.MaxJobTimeout + 84*time.Millisecond
	evidence.QueuedWave = []sampleEvidence{{
		Fixture: "maximum", Format: renderjob.PDF, Kind: sampleQueued, Index: 9,
		DurationNanoseconds: int64(duration), JoinedCleanupOvershootNanoseconds: joinedCleanupOvershoot(duration, failureTimeout),
		Outcome: outcomeFailure, FailureCode: failureTimeout,
	}}
	gate := evaluateGate(evidence)
	if !gate.Passed {
		t.Fatalf("expected queue deadline shedding failed the serial gate: %+v", gate)
	}
	if got := evidence.QueuedWave[0].JoinedCleanupOvershootNanoseconds; got != int64(84*time.Millisecond) {
		t.Fatalf("joined cleanup overshoot = %d, want %d", got, 84*time.Millisecond)
	}
}

func TestSuccessfulReturnPastCancellationDeadlineFailsGate(t *testing.T) {
	t.Parallel()
	evidence := newEvidence(modeFull, "go-test", "Chromium 151.0.7922.34", cgroupEvidence{}, nil)
	evidence.QueuedWave = []sampleEvidence{{
		Fixture: "maximum", Format: renderjob.PDF, Kind: sampleQueued, Index: 1,
		DurationNanoseconds: int64(renderjob.MaxJobTimeout + time.Nanosecond), Outcome: outcomeSuccess,
	}}
	gate := evaluateGate(evidence)
	if gate.Passed || !containsString(gate.FailureCodes, "hard_bound_exceeded") {
		t.Fatalf("successful late return gate = %+v", gate)
	}
}

func TestSeriesSLORequiresEveryMeasuredSampleToSucceed(t *testing.T) {
	t.Parallel()
	samples := make([]sampleEvidence, 10)
	for index := range samples {
		samples[index] = sampleEvidence{Kind: sampleMeasured, DurationNanoseconds: int64(time.Second), Outcome: outcomeSuccess}
	}
	samples[4].Outcome = outcomeFailure
	samples[4].FailureCode = failureRender
	series := summarizeSeries(seriesEvidence{Fixture: "full", Format: renderjob.PDF, Round: 1}, samples, 10, 8*time.Second)
	if series.MeasuredSuccesses != 9 || series.MeasuredFailures != 1 || series.AttemptP95Nanoseconds != int64(time.Second) || series.SLOMet {
		t.Fatalf("failure-aware series = %+v", series)
	}
}

func TestSeriesSLORequiresExactPlannedSuccessCount(t *testing.T) {
	t.Parallel()
	nineSuccesses := make([]sampleEvidence, 9)
	for index := range nineSuccesses {
		nineSuccesses[index] = sampleEvidence{Kind: sampleMeasured, DurationNanoseconds: int64(time.Second), Outcome: outcomeSuccess}
	}
	series := summarizeSeries(seriesEvidence{Fixture: "full", Format: renderjob.PDF, Round: 1}, nineSuccesses, 10, 8*time.Second)
	if series.MeasuredSuccesses != 9 || series.MeasuredFailures != 0 || series.SLOMet {
		t.Fatalf("incomplete series = %+v", series)
	}

	tenSuccesses := append(nineSuccesses, sampleEvidence{Kind: sampleMeasured, DurationNanoseconds: int64(time.Second), Outcome: outcomeSuccess})
	series = summarizeSeries(seriesEvidence{Fixture: "full", Format: renderjob.PDF, Round: 1}, tenSuccesses, 10, 8*time.Second)
	if series.MeasuredSuccesses != 10 || series.MeasuredFailures != 0 || !series.SLOMet {
		t.Fatalf("complete series = %+v", series)
	}
}

func TestProbeEvidenceCannotPassFullMeasurementGate(t *testing.T) {
	t.Parallel()
	evidence := newEvidence(modeProbe, "go-test", "Chromium 151.0.7922.34", cgroupEvidence{}, nil)
	if evidence.Protocol.Mode != modeProbe || evidence.Protocol.SeriesRepeats != 1 || evidence.Protocol.WarmupsPerSeries != 0 || evidence.Protocol.MeasuredSamplesPerSeries != 0 || evidence.Protocol.QueuedWaveCalls != 0 {
		t.Fatalf("probe protocol = %+v", evidence.Protocol)
	}
	gate := evaluateGate(evidence)
	if gate.Passed || !containsString(gate.FailureCodes, "measurement_incomplete") {
		t.Fatalf("probe gate = %+v", gate)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}
