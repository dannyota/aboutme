package main

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/printapi"
	"github.com/dannyota/aboutme/apps/server/internal/printrender"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

func run(ctx context.Context, settings config) (resultErr error) {
	if err := prepareOutputDirectory(settings.RepositoryRoot, settings.OutputDirectory); err != nil {
		return err
	}
	fixtures, err := buildFixtureCorpus(settings.RepositoryRoot)
	if err != nil {
		return err
	}
	cgroup, cgroupErr := readCgroupEvidence()
	browserVersion, versionErr := readBrowserVersion(ctx, settings.BrowserExecutable)
	mode := modeFull
	if settings.Probe {
		mode = modeProbe
	}
	evidence := newEvidence(mode, runtime.Version(), browserVersion, cgroup, fixtures)
	if cgroupErr != nil || versionErr != nil {
		evidence.Gate = gateEvidence{Passed: false, FailureCodes: []string{"environment_invalid"}}
		if evidenceErr := writeEvidence(settings.OutputDirectory, evidence); evidenceErr != nil {
			return evidenceErr
		}
		return errors.New("environment_invalid")
	}
	origin, err := directrender.ParseRenderOrigin(fixedRenderOrigin, "development")
	if err != nil {
		return errors.New("renderer_initialization_failed")
	}
	//nolint:contextcheck // New owns a fixed constructor-validation deadline independent of measurement cancellation.
	renderer, err := printrender.New(printrender.Config{BrowserExecutable: settings.BrowserExecutable, RenderOrigin: origin})
	if err != nil {
		return errors.New("renderer_initialization_failed")
	}
	queue, err := renderjob.New(renderjob.Config{Renderer: renderer})
	if err != nil {
		return errors.New("queue_initialization_failed")
	}

	server, done, err := startPrivateServer(ctx, queue)
	if err != nil {
		if closeErr := queue.Close(); closeErr != nil {
			return errors.New("shutdown_failed")
		}
		return errors.New("private_server_failed")
	}
	stopped := false
	stop := func(parent context.Context) error {
		if stopped {
			return nil
		}
		stopped = true
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		serveErr := <-done
		queueErr := queue.Close()
		if shutdownErr != nil || serveErr != nil || queueErr != nil {
			return errors.New("shutdown_failed")
		}
		return nil
	}
	defer func() {
		if stopErr := stop(ctx); stopErr != nil {
			resultErr = errors.Join(resultErr, stopErr)
		}
	}()

	artifacts := map[string]bool{}
	for _, item := range fixtures {
		for _, format := range []renderjob.Format{renderjob.PDF, renderjob.PNG} {
			for round := 1; round <= evidence.Protocol.SeriesRepeats; round++ {
				series := seriesEvidence{Fixture: item.Name, Format: format, Round: round}
				cold := measureSample(ctx, queue, item, format, round, sampleCold, 1, false)
				evidence.SerialSamples = append(evidence.SerialSamples, cold.row)
				if err := saveFirstArtifact(settings.OutputDirectory, artifacts, item.Name, format, cold); err != nil {
					return err
				}
				series.ColdDurationNanoseconds = cold.row.DurationNanoseconds
				for index := 1; index <= evidence.Protocol.WarmupsPerSeries; index++ {
					warmup := measureSample(ctx, queue, item, format, round, sampleWarmup, index, true)
					evidence.SerialSamples = append(evidence.SerialSamples, warmup.row)
					if err := saveFirstArtifact(settings.OutputDirectory, artifacts, item.Name, format, warmup); err != nil {
						return err
					}
				}
				measured := make([]sampleEvidence, 0, evidence.Protocol.MeasuredSamplesPerSeries)
				for index := 1; index <= evidence.Protocol.MeasuredSamplesPerSeries; index++ {
					sample := measureSample(ctx, queue, item, format, round, sampleMeasured, index, false)
					evidence.SerialSamples = append(evidence.SerialSamples, sample.row)
					measured = append(measured, sample.row)
					if err := saveFirstArtifact(settings.OutputDirectory, artifacts, item.Name, format, sample); err != nil {
						return err
					}
				}
				if len(measured) > 0 {
					evidence.Series = append(evidence.Series, summarizeSeries(series, measured, evidence.Protocol.MeasuredSamplesPerSeries, time.Duration(evidence.Protocol.P95ObjectiveNanoseconds)))
				}
			}
		}
	}
	if evidence.Protocol.QueuedWaveCalls > 0 {
		evidence.QueuedWave = measureQueuedWave(ctx, queue, fixtures[len(fixtures)-1], evidence.Protocol.QueuedWaveCalls)
	}
	if err := stop(ctx); err != nil {
		return err
	}
	if err := finishCgroupEvidence(&evidence.Cgroup); err != nil {
		return err
	}
	evidence.Gate = evaluateGate(evidence)
	if err := writeEvidence(settings.OutputDirectory, evidence); err != nil {
		return err
	}
	if !evidence.Gate.Passed {
		return errors.New("measurement_gate_failed")
	}
	return nil
}

type measuredSample struct {
	row    sampleEvidence
	output []byte
}

func measureSample(ctx context.Context, queue *renderjob.Queue, item fixture, format renderjob.Format, round int, kind sampleKind, index int, discarded bool) measuredSample {
	started := time.Now()
	result, err := queue.Render(ctx, renderjob.Request{Format: format, Prepare: func(context.Context) (renderjob.Snapshot, error) { return item.Snapshot, nil }})
	row := sampleEvidence{Fixture: item.Name, Format: format, Round: round, Kind: kind, Index: index, Discarded: discarded, DurationNanoseconds: int64(time.Since(started))}
	if err != nil {
		row.Outcome = outcomeFailure
		row.FailureCode = classifyFailure(err)
		row.JoinedCleanupOvershootNanoseconds = joinedCleanupOvershoot(time.Duration(row.DurationNanoseconds), row.FailureCode)
		return measuredSample{row: row}
	}
	row.Outcome = outcomeSuccess
	row.OutputBytes = len(result.Bytes)
	row.SHA256 = hex.EncodeToString(result.Digest[:])
	return measuredSample{row: row, output: result.Bytes}
}

func measureQueuedWave(ctx context.Context, queue *renderjob.Queue, item fixture, count int) []sampleEvidence {
	arrived := make(chan struct{}, count)
	release := make(chan struct{})
	results := make(chan sampleEvidence, count)
	var start sync.WaitGroup
	start.Add(count)
	for index := 1; index <= count; index++ {
		go func(index int) {
			start.Done()
			start.Wait()
			started := time.Now()
			result, err := queue.Render(ctx, renderjob.Request{Format: renderjob.PDF, Prepare: func(prepareCtx context.Context) (renderjob.Snapshot, error) {
				arrived <- struct{}{}
				select {
				case <-release:
					return item.Snapshot, nil
				case <-prepareCtx.Done():
					return renderjob.Snapshot{}, prepareCtx.Err()
				}
			}})
			row := sampleEvidence{Fixture: item.Name, Format: renderjob.PDF, Kind: sampleQueued, Index: index, DurationNanoseconds: int64(time.Since(started))}
			if err != nil {
				row.Outcome = outcomeFailure
				row.FailureCode = classifyFailure(err)
				row.JoinedCleanupOvershootNanoseconds = joinedCleanupOvershoot(time.Duration(row.DurationNanoseconds), row.FailureCode)
			} else {
				row.Outcome = outcomeSuccess
				row.OutputBytes = len(result.Bytes)
				row.SHA256 = hex.EncodeToString(result.Digest[:])
			}
			results <- row
		}(index)
	}
	admissionTimer := time.NewTimer(renderjob.MaxJobTimeout)
	admitted := 0
	for admitted < count {
		select {
		case <-arrived:
			admitted++
		case <-admissionTimer.C:
			admitted = count
		case <-ctx.Done():
			admitted = count
		}
	}
	if !admissionTimer.Stop() {
		select {
		case <-admissionTimer.C:
		default:
		}
	}
	close(release)
	rows := make([]sampleEvidence, count)
	for range count {
		row := <-results
		rows[row.Index-1] = row
	}
	return rows
}

func saveFirstArtifact(outputDirectory string, artifacts map[string]bool, fixtureName string, format renderjob.Format, sample measuredSample) error {
	key := fixtureName + string(format)
	if sample.row.Outcome != outcomeSuccess || artifacts[key] {
		return nil
	}
	if err := saveArtifact(outputDirectory, fixtureName, format, sample.output); err != nil {
		return err
	}
	artifacts[key] = true
	return nil
}

func classifyFailure(err error) failureCode {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return failureTimeout
	case errors.Is(err, context.Canceled):
		return failureCanceled
	case errors.Is(err, renderjob.ErrSaturated):
		return failureSaturated
	case errors.Is(err, renderjob.ErrOutputTooLarge), errors.Is(err, printrender.ErrOutputTooLarge):
		return failureOutputLimit
	case errors.Is(err, renderjob.ErrRendering), errors.Is(err, printrender.ErrRenderFailed):
		return failureRender
	default:
		return failureInternal
	}
}

func startPrivateServer(ctx context.Context, queue *renderjob.Queue) (*http.Server, <-chan error, error) {
	handler, err := printapi.NewRedeemHandler(queue)
	if err != nil {
		return nil, nil, err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", fixedPrintAddress)
	if err != nil {
		return nil, nil, err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 4096}
	done := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()
	return server, done, nil
}

func readBrowserVersion(ctx context.Context, executable string) (string, error) {
	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(versionCtx, executable, "--version")
	command.Env = []string{"TZ=UTC", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	output, err := command.Output()
	value := strings.TrimSpace(string(output))
	if err != nil || value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("browser_version_failed")
	}
	return value, nil
}

func saveArtifact(outputDirectory, fixtureName string, format renderjob.Format, data []byte) error {
	if len(data) == 0 {
		return errors.New("artifact_write_failed")
	}
	path := filepath.Join(outputDirectory, fixtureName+"."+string(format))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return errors.New("artifact_write_failed")
	}
	return nil
}

func writeEvidence(outputDirectory string, evidence evidenceDocument) error {
	encoded, err := marshalEvidence(evidence)
	if err != nil {
		return errors.New("evidence_write_failed")
	}
	path := filepath.Join(outputDirectory, "evidence.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return errors.New("evidence_write_failed")
	}
	return nil
}
