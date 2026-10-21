package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"danny.vn/vngcloud"
	"danny.vn/vngcloud/storage"
)

type captureTransport struct {
	base      http.RoundTripper
	active    atomic.Bool
	mu        sync.Mutex
	remaining int64
	project   string
	region    string
	quota     string
	usage     string
}

func (capture *captureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if !capture.active.Load() {
		return capture.base.RoundTrip(request)
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if request.Method != http.MethodGet || request.URL.Scheme != "https" || request.URL.Host != "vstorage.console.greennode.ai" || request.URL.RawQuery != "" || request.URL.User != nil {
		return nil, invalid
	}
	path := request.URL.Path
	if path != "/internal/v1/regions" && path != "/internal/v1/projects" {
		return nil, invalid
	}
	if path == "/internal/v1/projects" && (capture.region == "" || request.Header.Get("region") != capture.region || request.Header.Get("region_id") != capture.region) {
		return nil, invalid
	}
	response, err := capture.base.RoundTrip(request)
	if err != nil {
		return nil, invalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, capture.remaining+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || int64(len(raw)) > capture.remaining {
		return nil, invalid
	}
	capture.remaining -= int64(len(raw))
	if path == "/internal/v1/regions" {
		capture.region, err = regionIdentity(raw)
	} else {
		capture.quota, capture.usage, err = projectNumbers(raw, capture.project, capture.region)
	}
	if err != nil {
		return nil, invalid
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, nil
}

func privateRead(path string) ([]byte, error) {
	descriptor, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, invalid
	}
	file := os.NewFile(uintptr(descriptor), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 65536 {
		return nil, invalid
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) || owner.Nlink != 1 {
		return nil, invalid
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 {
		return nil, invalid
	}
	return data, nil
}

func privateWrite(root, name string, data []byte) error {
	path := filepath.Join(root, name)
	if _, err := os.Lstat(path); err == nil {
		if _, err := privateRead(path); err != nil {
			return invalid
		}
	} else if !os.IsNotExist(err) {
		return invalid
	}
	file, err := os.CreateTemp(root, ".allowance-*")
	if err != nil {
		return invalid
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return invalid
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return invalid
	}
	if err := file.Close(); err != nil {
		return invalid
	}
	if err := os.Rename(temporary, path); err != nil {
		return invalid
	}
	directory, err := os.Open(root)
	if err != nil {
		return invalid
	}
	defer directory.Close()
	return directory.Sync()
}

func run() error {
	if len(os.Args) != 1 {
		return invalid
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 65537))
	if err != nil || len(raw) > 65536 {
		return invalid
	}
	value, err := strictJSON(raw)
	if err != nil {
		return err
	}
	input, ok := value.(map[string]any)
	if !ok || len(input) != 3 {
		return invalid
	}
	runID, runOK := input["run_id"].(string)
	project, projectOK := input["project_id"].(string)
	plan, planOK := input["plan_sha256"].(string)
	if !runOK || !projectOK || !planOK || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(runID) || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`).MatchString(project) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(plan) {
		return invalid
	}
	root := os.Getenv("ABOUTME_REHEARSAL_SCRATCH")
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || root == "" || !filepath.IsAbs(root) || root != resolved {
		return invalid
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return invalid
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) {
		return invalid
	}
	manifestBytes, err := privateRead(filepath.Join(root, "active.json"))
	if err != nil {
		return err
	}
	manifestValue, err := strictJSON(manifestBytes)
	if err != nil {
		return err
	}
	manifest, ok := manifestValue.(map[string]any)
	if !ok || manifest["run_id"] != runID || manifest["plan_sha256"] != plan {
		return invalid
	}
	configuration, ok := manifest["config"].(map[string]any)
	if !ok || configuration["storage_project_id"] != project {
		return invalid
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	transport := &captureTransport{base: http.DefaultTransport, remaining: 1 << 20, project: project}
	options := []vngcloud.LoadOption{vngcloud.WithRegion("hcm-3"), vngcloud.WithTransport(transport), vngcloud.WithTimeout(30 * time.Second), vngcloud.WithRetry(0, 0), vngcloud.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))}
	if profile := os.Getenv("VNG_PROFILE"); profile != "" {
		options = append(options, vngcloud.WithProfile(profile))
	}
	cfg, err := vngcloud.LoadConfig(ctx, options...)
	if err != nil {
		return invalid
	}
	if err := cfg.Authenticate(ctx); err != nil {
		return invalid
	}
	transport.active.Store(true)
	if _, err := storage.New(cfg).ListProjects(ctx, &storage.ListProjectsInput{Region: "HCM04"}); err != nil {
		return invalid
	}
	lower, err := gigabytes(transport.quota, true)
	if err != nil {
		return err
	}
	upper, err := gigabytes(transport.usage, false)
	if err != nil || lower < upper || lower-upper < 2147483648 {
		return invalid
	}
	quota, err := displayed(transport.quota)
	if err != nil {
		return err
	}
	usage, err := displayed(transport.usage)
	if err != nil {
		return err
	}
	observed := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	evidence := map[string]any{"project_id": project, "region": "HCM04", "quota_lexeme": transport.quota, "usage_lexeme": transport.usage, "observed_at": observed, "units": "GB"}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return invalid
	}
	encoded = append(encoded, '\n')
	hash := sha256.Sum256(encoded)
	allowance := map[string]any{"version": 1, "run_id": runID, "project_id": project, "region": "HCM04", "source_kind": "api", "observed_at": observed, "plan_sha256": plan, "quota": quota, "usage": usage, "quota_lower_bytes": lower, "usage_upper_bytes": upper, "evidence_sha256": hex.EncodeToString(hash[:]), "limits": []string{"provider_usage_refresh_lag_undocumented", "multipart_accounting_undocumented"}}
	allowanceBytes, err := json.Marshal(allowance)
	if err != nil {
		return invalid
	}
	if err := privateWrite(root, "storage-allowance-api.json", encoded); err != nil {
		return err
	}
	if err := privateWrite(root, "storage-allowance.json", append(allowanceBytes, '\n')); err != nil {
		return err
	}
	fmt.Printf("VALID allowance\nUNUSED %d\n", lower-upper)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "INVALID allowance")
		os.Exit(1)
	}
}
