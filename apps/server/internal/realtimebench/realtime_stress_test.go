package realtimebench_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/realtime"
	"github.com/dannyota/aboutme/apps/server/internal/realtimeapi"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	stressConnections = 2000
	stressChurn       = 200
	stressSamples     = 5
	stressSampleEvery = time.Second
)

var stressResumeID = uuid.MustParse("22222222-2222-4222-8222-222222222222")

type stressStore struct{}

func (stressStore) GetPublicRealtimeResume(_ context.Context, slug string) (store.GetPublicRealtimeResumeRow, error) {
	if slug != "stress-resume" {
		return store.GetPublicRealtimeResumeRow{}, pgx.ErrNoRows
	}
	return store.GetPublicRealtimeResumeRow{ID: stressResumeID, Revision: 1}, nil
}

func (stressStore) GetSessionByID(context.Context, uuid.UUID) (store.Session, error) {
	return store.Session{}, pgx.ErrNoRows
}

type stream struct {
	body   io.ReadCloser
	reader *bufio.Reader
}

type resourceSample struct {
	OpenStreams    int    `json:"open_streams"`
	FDs            int    `json:"fds"`
	SocketFDs      int    `json:"socket_fds"`
	TCPConnections int    `json:"tcp_connections"`
	Goroutines     int    `json:"goroutines"`
	HeapAlloc      uint64 `json:"heap_alloc_bytes"`
	HeapInuse      uint64 `json:"heap_inuse_bytes"`
}

type roundMeasurement struct {
	Round             int              `json:"round"`
	SustainDurationMS int64            `json:"sustain_duration_ms"`
	SampleCount       int              `json:"sample_count"`
	ChurnDepartures   int              `json:"churn_departures"`
	ChurnAdmissions   int              `json:"churn_admissions"`
	OpenElapsedMS     int64            `json:"open_elapsed_ms"`
	DispatchElapsedMS int64            `json:"dispatch_elapsed_ms"`
	Samples           []resourceSample `json:"samples"`
}

type stressMeasurement struct {
	Kind                    string             `json:"kind"`
	Transport               string             `json:"transport"`
	Network                 string             `json:"network"`
	OS                      string             `json:"os"`
	Arch                    string             `json:"arch"`
	Kernel                  string             `json:"kernel"`
	CPU                     string             `json:"cpu"`
	LogicalCPUs             int                `json:"logical_cpus"`
	GoVersion               string             `json:"go_version"`
	SoftNoFile              uint64             `json:"soft_nofile"`
	HardNoFile              uint64             `json:"hard_nofile"`
	ConnectionCap           int                `json:"connection_cap"`
	CanonicalIPCount        int                `json:"canonical_ip_count"`
	PerIPCap                int                `json:"per_ip_cap"`
	ChurnPerRound           int                `json:"churn_per_round"`
	Baseline                resourceSample     `json:"baseline"`
	FDLimitProbe            resourceSample     `json:"fd_limit_probe"`
	FDLimitProbePerformed   bool               `json:"fd_limit_probe_performed"`
	Rounds                  []roundMeasurement `json:"rounds"`
	AfterCleanup            resourceSample     `json:"after_cleanup"`
	PeakHeapDelta           int64              `json:"peak_heap_delta_bytes"`
	CleanupHeapDelta        int64              `json:"cleanup_heap_delta_bytes"`
	PeakFDHeadroom          int64              `json:"minimum_fd_headroom"`
	TotalElapsedMS          int64              `json:"total_elapsed_ms"`
	HTTP1Caveat             string             `json:"http1_caveat"`
	ProductionEvidenceScope string             `json:"production_evidence_scope"`
}

// TestPublicSSEConnectionChurn is an opt-in local capacity measurement. A
// production change that bypasses PublicHandler, its real hub admission
// limits, the public lease, or HTTP response streaming makes this test fail.
func TestPublicSSEConnectionChurn(t *testing.T) {
	if os.Getenv("ABOUTME_REALTIME_STRESS") != "1" {
		t.Skip("set ABOUTME_REALTIME_STRESS=1 to run the local realtime measurement")
	}
	if runtime.GOOS != "linux" {
		t.Skip("the real process FD guard and /proc measurements require Linux")
	}

	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 58*time.Second)
	defer cancel()
	baseline := sampleResources(t, 0)
	limit := nofileLimit(t)
	measurement := stressMeasurement{
		Kind:                    "aboutme_phase_6_local_realtime_stress",
		Transport:               "2000 concurrent SSE request streams over HTTP/1.1",
		Network:                 "2000 real loopback TCP connections",
		OS:                      runtime.GOOS,
		Arch:                    runtime.GOARCH,
		Kernel:                  kernelRelease(),
		CPU:                     cpuModel(),
		LogicalCPUs:             runtime.NumCPU(),
		GoVersion:               runtime.Version(),
		SoftNoFile:              limit.Cur,
		HardNoFile:              limit.Max,
		ConnectionCap:           stressConnections,
		CanonicalIPCount:        77,
		PerIPCap:                100,
		ChurnPerRound:           stressChurn,
		Baseline:                baseline,
		HTTP1Caveat:             "the client and server run in one local Go process, so each TCP connection consumes both endpoint descriptors",
		ProductionEvidenceScope: "local HTTP/1.1 capacity only; AWS 512 MiB and production-shaped resource use remain unproven",
	}

	hub, err := realtime.NewHub(realtime.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	hub.SetAvailable(true)
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	service, err := realtimeapi.New(realtimeapi.Dependencies{
		Hub: hub, Store: stressStore{}, Sessions: auth.NewSessionManager(nil),
		Coordinator: coordinator, TrustedProxies: api.LoopbackTrustedProxies(),
	})
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(service.PublicHandler())
	transport := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{Transport: transport}
	defer func() {
		transport.CloseIdleConnections()
		server.Close()
	}()

	probeFDAdmission(ctx, t, client, server.URL, limit, &measurement)
	probePerIPLimit(ctx, t, client, server.URL)

	var open []*stream
	defer func() { closeStreams(t, open) }()
	for round := 1; round <= 2; round++ {
		openedAt := time.Now()
		open = openMany(ctx, t, client, server.URL, stressConnections, (round-1)*100)
		roundResult := roundMeasurement{Round: round, OpenElapsedMS: time.Since(openedAt).Milliseconds()}
		if status := requestStatus(ctx, t, client, server.URL, 5002); status != http.StatusTooManyRequests {
			t.Fatalf("connection 2001 status = %d, want 429", status)
		}

		sustainAt := time.Now()
		for cycle := 0; cycle < stressSamples; cycle++ {
			departing := append([]*stream(nil), open[:stressChurn/stressSamples]...)
			closeStreams(t, departing)
			open = open[stressChurn/stressSamples:]
			waitForTCPConnections(ctx, t, len(open))
			open = append(open, openManyForIP(ctx, t, client, server.URL, stressChurn/stressSamples, 1000+(round-1)*10+cycle)...)
			roundResult.ChurnDepartures += stressChurn / stressSamples
			roundResult.ChurnAdmissions += stressChurn / stressSamples

			revision := int64(round*100 + cycle + 2)
			dispatchedAt := time.Now()
			hub.Publish(realtime.Change{ResumeID: stressResumeID, Revision: revision})
			readRevisionFrames(ctx, t, open, revision)
			roundResult.DispatchElapsedMS += time.Since(dispatchedAt).Milliseconds()
			waitUntil(ctx, t, sustainAt.Add(time.Duration(cycle+1)*stressSampleEvery))
			sample := sampleResources(t, len(open))
			if sample.TCPConnections != len(open) {
				t.Fatalf("round %d cycle %d TCP connections = %d, want %d", round, cycle+1, sample.TCPConnections, len(open))
			}
			roundResult.Samples = append(roundResult.Samples, sample)
		}
		roundResult.SampleCount = len(roundResult.Samples)
		roundResult.SustainDurationMS = time.Since(sustainAt).Milliseconds()
		measurement.Rounds = append(measurement.Rounds, roundResult)

		closeStreams(t, open)
		waitForResources(ctx, t, baseline, 24, 96, 48<<20)
	}

	open = openMany(ctx, t, client, server.URL, stressConnections, 200)
	hub.SetAvailable(false)
	waitForEOF(ctx, t, open)
	closeStreams(t, open)
	if status := requestStatus(ctx, t, client, server.URL, 5003); status != http.StatusServiceUnavailable {
		t.Fatalf("listener unavailable status = %d, want 503", status)
	}
	hub.SetAvailable(true)
	restored := openOne(ctx, t, client, server.URL, 5003)
	if restored.StatusCode != http.StatusOK {
		closeBody(t, restored.Body)
		t.Fatalf("listener restored status = %d, want 200", restored.StatusCode)
	}
	consumeHeartbeat(t, restored)
	if closeErr := restored.Body.Close(); closeErr != nil {
		t.Fatalf("close restored response body: %v", closeErr)
	}

	transport.CloseIdleConnections()
	server.CloseClientConnections()
	server.Close()
	debug.FreeOSMemory()
	measurement.AfterCleanup = waitForResources(ctx, t, baseline, 2, 8, 48<<20)
	if measurement.AfterCleanup.SocketFDs != baseline.SocketFDs || measurement.AfterCleanup.TCPConnections != 0 {
		t.Fatalf("network resources leaked: baseline=%+v after=%+v", baseline, measurement.AfterCleanup)
	}
	measurement.PeakHeapDelta, measurement.PeakFDHeadroom = measurementPeaks(measurement)
	measurement.CleanupHeapDelta = int64(measurement.AfterCleanup.HeapAlloc) - int64(baseline.HeapAlloc)
	measurement.TotalElapsedMS = time.Since(startedAt).Milliseconds()
	encoded, err := json.Marshal(measurement)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(encoded))
}

func probePerIPLimit(ctx context.Context, t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	streams := openManyForIP(ctx, t, client, baseURL, 100, 5000)
	defer closeStreams(t, streams)
	response := openOne(ctx, t, client, baseURL, 5000)
	defer closeBody(t, response.Body)
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("connection 101 for one canonical IP status = %d, want 429", response.StatusCode)
	}
}

func probeFDAdmission(ctx context.Context, t *testing.T, client *http.Client, baseURL string, limit syscall.Rlimit, measurement *stressMeasurement) {
	t.Helper()
	probeWarmConnection(ctx, t, client, baseURL)

	target := int(limit.Cur-limit.Cur/4) + 2
	if needed := target - countFDs(t); needed > 800 {
		return
	}
	var files []*os.File
	defer func() { closeFiles(t, files) }()
	for countFDs(t) < target {
		readEnd, writeEnd, err := os.Pipe()
		if err != nil {
			t.Fatalf("open FD probe pipe: %v", err)
		}
		files = append(files, readEnd, writeEnd)
	}
	measurement.FDLimitProbe = sampleResources(t, 0)
	measurement.FDLimitProbePerformed = true
	response := openOne(ctx, t, client, baseURL, 5001)
	defer closeBody(t, response.Body)
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("FD-limited admission status = %d, want 429", response.StatusCode)
	}
}

func openMany(ctx context.Context, t *testing.T, client *http.Client, baseURL string, count, firstIP int) []*stream {
	t.Helper()
	streams := make([]*stream, 0, count)
	remaining := count
	for ip := firstIP; remaining > 0; ip++ {
		batch := 95
		if ip%21 == 20 {
			batch = 100
		}
		if batch > remaining {
			batch = remaining
		}
		streams = append(streams, openManyForIP(ctx, t, client, baseURL, batch, ip+1)...)
		remaining -= batch
	}
	return streams
}

func openManyForIP(ctx context.Context, t *testing.T, client *http.Client, baseURL string, count, ip int) []*stream {
	t.Helper()
	result := make([]*stream, count)
	errs := make([]error, count)
	const workers = 64
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				//nolint:bodyclose // The successful response body moves into result and closeStreams owns it.
				response, err := doOpen(ctx, client, baseURL, ip)
				if err != nil {
					errs[index] = err
					continue
				}
				if response.StatusCode != http.StatusOK {
					errs[index] = closeWithError(fmt.Errorf("status %d", response.StatusCode), response.Body)
					continue
				}
				if response.ProtoMajor != 1 {
					errs[index] = closeWithError(fmt.Errorf("protocol %s, want HTTP/1.1", response.Proto), response.Body)
					continue
				}
				reader := bufio.NewReader(response.Body)
				if err := expectFrame(reader, heartbeatFrame()); err != nil {
					errs[index] = closeWithError(err, response.Body)
					continue
				}
				result[index] = &stream{body: response.Body, reader: reader}
			}
		}()
	}
	for index := range count {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	for index, err := range errs {
		if err != nil {
			closeStreams(t, result)
			t.Fatalf("open stream %d for IP %d: %v", index, ip, err)
		}
	}
	return result
}

func openOne(ctx context.Context, t *testing.T, client *http.Client, baseURL string, ip int) *http.Response {
	t.Helper()
	response, err := doOpen(ctx, client, baseURL, ip)
	if err != nil {
		t.Fatalf("open stream for IP %d: %v", ip, err)
	}
	return response
}

func requestStatus(ctx context.Context, t *testing.T, client *http.Client, baseURL string, ip int) int {
	t.Helper()
	response := openOne(ctx, t, client, baseURL, ip)
	status := response.StatusCode
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close status response body: %v", err)
	}
	return status
}

func probeWarmConnection(ctx context.Context, t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	response := openOne(ctx, t, client, baseURL, 5001)
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close FD probe warmup body: %v", closeErr)
		}
	}()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("FD probe warmup status = %d", response.StatusCode)
	}
	consumeHeartbeat(t, response)
}

func doOpen(ctx context.Context, client *http.Client, baseURL string, ip int) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/live/stress-resume", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set(api.TrustedClientIPHeader, fmt.Sprintf("2001:db8::%x", ip))
	request.Header.Set("Cookie", "__Host-session=invalid-ignored-cookie")
	return client.Do(request)
}

func consumeHeartbeat(t *testing.T, response *http.Response) {
	t.Helper()
	if response.ProtoMajor != 1 {
		t.Fatalf("stream protocol = %s, want HTTP/1.1", response.Proto)
	}
	if err := expectFrame(bufio.NewReader(response.Body), heartbeatFrame()); err != nil {
		t.Fatal(err)
	}
}

func readRevisionFrames(ctx context.Context, t *testing.T, streams []*stream, revision int64) {
	t.Helper()
	want := "event: revision\nid: " + strconv.FormatInt(revision, 10) + "\ndata: {\"version\":1,\"revision\":\"" + strconv.FormatInt(revision, 10) + "\"}\n\n"
	errs := make(chan error, len(streams))
	const workers = 64
	jobs := make(chan *stream)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for stream := range jobs {
				errs <- expectFrame(stream.reader, want)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, stream := range streams {
			select {
			case jobs <- stream:
			case <-ctx.Done():
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("read dispatched revision: %v", err)
		}
	}
}

func waitForEOF(ctx context.Context, t *testing.T, streams []*stream) {
	t.Helper()
	errs := make(chan error, len(streams))
	jobs := make(chan *stream)
	const workers = 64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for stream := range jobs {
				_, err := stream.reader.ReadByte()
				if !errors.Is(err, io.EOF) {
					errs <- fmt.Errorf("stream remained open: %w", err)
					continue
				}
				errs <- nil
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, stream := range streams {
			select {
			case jobs <- stream:
			case <-ctx.Done():
				return
			}
		}
	}()
	for range streams {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatalf("wait for listener-down EOF: %v", ctx.Err())
		}
	}
	wg.Wait()
}

func expectFrame(reader *bufio.Reader, want string) error {
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		frame.WriteString(line)
		if line == "\n" {
			if got := frame.String(); got != want {
				return fmt.Errorf("frame = %q, want %q", got, want)
			}
			return nil
		}
	}
}

func heartbeatFrame() string {
	return "event: heartbeat\ndata: {\"version\":1}\n\n"
}

func closeStreams(t *testing.T, streams []*stream) {
	t.Helper()
	for _, stream := range streams {
		if stream != nil && stream.body != nil {
			closeBody(t, stream.body)
		}
	}
}

func closeBody(t *testing.T, closer io.Closer) {
	t.Helper()
	if err := closer.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
}

func closeFiles(t *testing.T, files []*os.File) {
	t.Helper()
	for _, file := range files {
		if err := file.Close(); err != nil {
			t.Errorf("close FD probe file: %v", err)
		}
	}
}

func closeWithError(primary error, closer io.Closer) error {
	if err := closer.Close(); err != nil {
		return errors.Join(primary, fmt.Errorf("close response body: %w", err))
	}
	return primary
}

func waitUntil(ctx context.Context, t *testing.T, deadline time.Time) {
	t.Helper()
	delay := time.Until(deadline)
	if delay <= 0 {
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatalf("sustained sampling: %v", ctx.Err())
	}
}

func waitForTCPConnections(ctx context.Context, t *testing.T, maximum int) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		socketFDs := countSocketFDs(t)
		if socketFDs > 0 && (socketFDs-1)/2 <= maximum {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("TCP connections did not fall to %d after departures", maximum)
		case <-ctx.Done():
			t.Fatalf("wait for TCP departures: %v", ctx.Err())
		}
	}
}

func waitForResources(ctx context.Context, t *testing.T, baseline resourceSample, fdSlack, goroutineSlack int, heapSlack uint64) resourceSample {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		debug.FreeOSMemory()
		current := sampleResources(t, 0)
		if current.FDs <= baseline.FDs+fdSlack && current.Goroutines <= baseline.Goroutines+goroutineSlack && current.HeapAlloc <= baseline.HeapAlloc+heapSlack {
			return current
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("resources did not restore within bounds: baseline=%+v current=%+v", baseline, current)
		case <-ctx.Done():
			t.Fatalf("resource restoration: %v", ctx.Err())
		}
	}
}

func sampleResources(t *testing.T, openStreams int) resourceSample {
	t.Helper()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	socketFDs := countSocketFDs(t)
	tcpConnections := 0
	if socketFDs > 0 {
		tcpConnections = (socketFDs - 1) / 2
	}
	return resourceSample{
		OpenStreams: openStreams,
		FDs:         countFDs(t), SocketFDs: socketFDs, TCPConnections: tcpConnections,
		Goroutines: runtime.NumGoroutine(), HeapAlloc: memory.HeapAlloc, HeapInuse: memory.HeapInuse,
	}
}

func countFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("read /proc/self/fd: %v", err)
	}
	return len(entries)
}

func countSocketFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("read /proc/self/fd: %v", err)
	}
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink("/proc/self/fd/" + entry.Name())
		if err == nil && strings.HasPrefix(target, "socket:[") {
			count++
		}
	}
	return count
}

func nofileLimit(t *testing.T) syscall.Rlimit {
	t.Helper()
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatalf("get RLIMIT_NOFILE: %v", err)
	}
	return limit
}

func kernelRelease() string {
	var value syscall.Utsname
	if err := syscall.Uname(&value); err != nil {
		return "unknown"
	}
	return charsToString(value.Release[:])
}

func charsToString(value []int8) string {
	bytes := make([]byte, 0, len(value))
	for _, character := range value {
		if character == 0 {
			break
		}
		bytes = append(bytes, byte(character))
	}
	return string(bytes)
}

func cpuModel() string {
	content, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "model name" {
			return strings.TrimSpace(value)
		}
	}
	return "unknown"
}

func measurementPeaks(measurement stressMeasurement) (int64, int64) {
	peakHeap := measurement.Baseline.HeapAlloc
	peakFDs := measurement.Baseline.FDs
	for _, round := range measurement.Rounds {
		for _, sample := range round.Samples {
			if sample.HeapAlloc > peakHeap {
				peakHeap = sample.HeapAlloc
			}
			if sample.FDs > peakFDs {
				peakFDs = sample.FDs
			}
		}
	}
	return int64(peakHeap) - int64(measurement.Baseline.HeapAlloc), int64(measurement.SoftNoFile) - int64(peakFDs)
}
