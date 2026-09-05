package main

import (
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

const evidenceVersion = 2

type measurementMode string
type sampleKind string
type sampleOutcome string
type failureCode string

const (
	modeFull           measurementMode = "full"
	modeProbe          measurementMode = "probe"
	sampleCold         sampleKind      = "cold"
	sampleWarmup       sampleKind      = "warmup"
	sampleMeasured     sampleKind      = "measured"
	sampleQueued       sampleKind      = "queued"
	outcomeSuccess     sampleOutcome   = "success"
	outcomeFailure     sampleOutcome   = "failure"
	failureCanceled    failureCode     = "canceled"
	failureTimeout     failureCode     = "deadline_canceled"
	failureSaturated   failureCode     = "saturated"
	failureRender      failureCode     = "render_failed"
	failureOutputLimit failureCode     = "output_limit"
	failureInternal    failureCode     = "internal_failure"
)

type protocolEvidence struct {
	Mode                            measurementMode  `json:"mode"`
	ColdSamplesPerSeries            int              `json:"coldSamplesPerSeries"`
	WarmupsPerSeries                int              `json:"warmupsPerSeries"`
	MeasuredSamplesPerSeries        int              `json:"measuredSamplesPerSeries"`
	SeriesRepeats                   int              `json:"seriesRepeats"`
	QueuedWaveCalls                 int              `json:"queuedWaveCalls"`
	CancellationDeadlineNanoseconds int64            `json:"cancellationDeadlineNanoseconds"`
	P95ObjectiveNanoseconds         int64            `json:"p95ObjectiveNanoseconds"`
	P95Method                       string           `json:"p95Method"`
	RenderOrigin                    string           `json:"renderOrigin"`
	PrivateRedemptionAddress        string           `json:"privateRedemptionAddress"`
	QueuedWaveFixture               string           `json:"queuedWaveFixture"`
	QueuedWaveFormat                renderjob.Format `json:"queuedWaveFormat"`
}

type toolchainEvidence struct {
	GoVersion      string `json:"goVersion"`
	OS             string `json:"os"`
	Architecture   string `json:"architecture"`
	BrowserVersion string `json:"browserVersion"`
}

type cgroupEvidence struct {
	Version            int               `json:"version"`
	Path               string            `json:"path"`
	MemoryMax          string            `json:"memoryMax"`
	MemorySwapMax      string            `json:"memorySwapMax"`
	CPUMax             string            `json:"cpuMax"`
	MemoryPeak         string            `json:"memoryPeak,omitempty"`
	MemoryEventsBefore map[string]uint64 `json:"memoryEventsBefore,omitempty"`
	MemoryEventsAfter  map[string]uint64 `json:"memoryEventsAfter,omitempty"`
}

type fixtureEvidence struct {
	Name                 string `json:"name"`
	SHA256               string `json:"sha256"`
	CanonicalBytes       int    `json:"canonicalBytes"`
	SectionCount         int    `json:"sectionCount"`
	EntryCount           int    `json:"entryCount"`
	LargestRichTextBytes int    `json:"largestRichTextBytes"`
	HiddenEntryCount     int    `json:"hiddenEntryCount"`
	LayoutSectionCount   int    `json:"layoutSectionCount"`
	PageFormat           string `json:"pageFormat"`
	FontFamily           string `json:"fontFamily"`
	InlinePhotoBytes     int    `json:"inlinePhotoBytes"`
}

type sampleEvidence struct {
	Fixture                           string           `json:"fixture"`
	Format                            renderjob.Format `json:"format"`
	Round                             int              `json:"round"`
	Kind                              sampleKind       `json:"kind"`
	Index                             int              `json:"index"`
	Discarded                         bool             `json:"discarded"`
	DurationNanoseconds               int64            `json:"durationNanoseconds"`
	JoinedCleanupOvershootNanoseconds int64            `json:"joinedCleanupOvershootNanoseconds"`
	OutputBytes                       int              `json:"outputBytes"`
	SHA256                            string           `json:"sha256,omitempty"`
	Outcome                           sampleOutcome    `json:"outcome"`
	FailureCode                       failureCode      `json:"failureCode,omitempty"`
}

type seriesEvidence struct {
	Fixture                 string           `json:"fixture"`
	Format                  renderjob.Format `json:"format"`
	Round                   int              `json:"round"`
	ColdDurationNanoseconds int64            `json:"coldDurationNanoseconds"`
	MeasuredSamples         int              `json:"measuredSamples"`
	MeasuredSuccesses       int              `json:"measuredSuccesses"`
	MeasuredFailures        int              `json:"measuredFailures"`
	AttemptP95Nanoseconds   int64            `json:"attemptP95Nanoseconds"`
	SLOMet                  bool             `json:"sloMet"`
}

type gateEvidence struct {
	Passed       bool     `json:"passed"`
	FailureCodes []string `json:"failureCodes"`
}

type evidenceDocument struct {
	Version       int               `json:"version"`
	GeneratedAt   string            `json:"generatedAt"`
	Protocol      protocolEvidence  `json:"protocol"`
	Toolchain     toolchainEvidence `json:"toolchain"`
	Cgroup        cgroupEvidence    `json:"cgroup"`
	Fixtures      []fixtureEvidence `json:"fixtures"`
	SerialSamples []sampleEvidence  `json:"serialSamples"`
	Series        []seriesEvidence  `json:"series"`
	QueuedWave    []sampleEvidence  `json:"queuedWave"`
	Gate          gateEvidence      `json:"gate"`
}

func newEvidence(mode measurementMode, goVersion, browserVersion string, cgroup cgroupEvidence, fixtures []fixture) evidenceDocument {
	fixtureRows := make([]fixtureEvidence, 0, len(fixtures))
	for _, item := range fixtures {
		digest := fixtureDigest(item)
		fixtureRows = append(fixtureRows, fixtureEvidence{
			Name: item.Name, SHA256: hex.EncodeToString(digest[:]), CanonicalBytes: len(item.Canonical),
			SectionCount: item.Stats.SectionCount, EntryCount: item.Stats.EntryCount,
			LargestRichTextBytes: item.Stats.LargestRichTextBytes, HiddenEntryCount: item.Stats.HiddenEntryCount,
			LayoutSectionCount: item.Stats.LayoutSectionCount, PageFormat: item.Stats.PageFormat,
			FontFamily: item.Stats.FontFamily, InlinePhotoBytes: item.Stats.InlinePhotoBytes,
		})
	}
	result := evidenceDocument{
		Version: evidenceVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Protocol: protocolEvidence{
			Mode:                 mode,
			ColdSamplesPerSeries: 1, WarmupsPerSeries: 2, MeasuredSamplesPerSeries: 10, SeriesRepeats: 2,
			QueuedWaveCalls: 9, CancellationDeadlineNanoseconds: int64(renderjob.MaxJobTimeout), P95ObjectiveNanoseconds: int64(8 * time.Second),
			P95Method: "nearest-rank", RenderOrigin: fixedRenderOrigin, PrivateRedemptionAddress: fixedPrintAddress,
			QueuedWaveFixture: "maximum", QueuedWaveFormat: renderjob.PDF,
		},
		Toolchain: toolchainEvidence{GoVersion: goVersion, OS: runtimeOS(), Architecture: runtimeArchitecture(), BrowserVersion: browserVersion},
		Cgroup:    cgroup, Fixtures: fixtureRows, SerialSamples: []sampleEvidence{}, Series: []seriesEvidence{}, QueuedWave: []sampleEvidence{},
	}
	if mode == modeProbe {
		result.Protocol.WarmupsPerSeries = 0
		result.Protocol.MeasuredSamplesPerSeries = 0
		result.Protocol.SeriesRepeats = 1
		result.Protocol.QueuedWaveCalls = 0
	}
	return result
}

func marshalEvidence(evidence evidenceDocument) ([]byte, error) {
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func nearestRankP95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	rank := (95*len(ordered) + 99) / 100
	return ordered[rank-1]
}

func joinedCleanupOvershoot(duration time.Duration, code failureCode) int64 {
	if code != failureTimeout || duration <= renderjob.MaxJobTimeout {
		return 0
	}
	return int64(duration - renderjob.MaxJobTimeout)
}

func summarizeSeries(series seriesEvidence, samples []sampleEvidence, plannedSamples int, objective time.Duration) seriesEvidence {
	attempts := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		if sample.Kind != sampleMeasured {
			continue
		}
		attempts = append(attempts, time.Duration(sample.DurationNanoseconds))
		if sample.Outcome == outcomeSuccess {
			series.MeasuredSuccesses++
		} else {
			series.MeasuredFailures++
		}
	}
	series.MeasuredSamples = len(attempts)
	series.AttemptP95Nanoseconds = int64(nearestRankP95(attempts))
	series.SLOMet = plannedSamples > 0 && len(attempts) == plannedSamples && series.MeasuredSuccesses == plannedSamples && series.MeasuredFailures == 0 && time.Duration(series.AttemptP95Nanoseconds) <= objective
	return series
}

func evaluateGate(evidence evidenceDocument) gateEvidence {
	failures := map[string]struct{}{}
	if evidence.Protocol.Mode != modeFull {
		failures["measurement_incomplete"] = struct{}{}
	}
	for _, sample := range evidence.SerialSamples {
		if sample.Outcome != outcomeSuccess {
			failures["sample_failure"] = struct{}{}
		}
		if sample.Outcome == outcomeSuccess && sample.DurationNanoseconds > evidence.Protocol.CancellationDeadlineNanoseconds {
			failures["hard_bound_exceeded"] = struct{}{}
		}
	}
	for _, sample := range evidence.QueuedWave {
		if sample.Outcome != outcomeSuccess && sample.FailureCode != failureTimeout {
			failures["queued_wave_failure"] = struct{}{}
		}
		if sample.Outcome == outcomeSuccess && sample.DurationNanoseconds > evidence.Protocol.CancellationDeadlineNanoseconds {
			failures["hard_bound_exceeded"] = struct{}{}
		}
	}
	for _, series := range evidence.Series {
		if !series.SLOMet {
			failures["p95_objective_failed"] = struct{}{}
		}
	}
	if oomDelta(evidence.Cgroup.MemoryEventsBefore, evidence.Cgroup.MemoryEventsAfter) {
		failures["cgroup_oom"] = struct{}{}
	}
	codes := make([]string, 0, len(failures))
	for code := range failures {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return gateEvidence{Passed: len(codes) == 0, FailureCodes: codes}
}

func oomDelta(before, after map[string]uint64) bool {
	for _, key := range []string{"oom", "oom_kill", "oom_group_kill"} {
		if after[key] > before[key] {
			return true
		}
	}
	return false
}
