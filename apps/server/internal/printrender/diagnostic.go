package printrender

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"golang.org/x/sys/unix"
)

var debugDiagnosticMu sync.Mutex

// DebugRenderDiagnostic writes closed fields for a disposable CI investigation.
func DebugRenderDiagnostic(stage string, err error, status int, elapsed time.Duration) {
	switch stage {
	case "readiness-entry", "readiness-exit", "renderer-entry", "renderer-exit",
		"permit-wait", "permit-acquired", "browser-entry", "browser-exit",
		"proxy-start", "proxy-close", "launch", "network-setup", "navigation",
		"document-status", "document-validation", "page-readiness", "assets-complete",
		"capture", "capture-complete", "request-policy", "request-continue",
		"response-policy", "loading-finished", "loading-failed", "target-attached",
		"target-created", "pdf-print", "pdf-stream", "pdf-metadata", "owner-entry",
		"owner-dependencies", "owner-queue", "owner-response", "owner-result",
		"owner-read", "owner-photo", "owner-snapshot", "owner-marshal":
	default:
		return
	}
	if status < 100 || status > 599 {
		status = 0
	}
	record := struct {
		Stage     string `json:"stage"`
		Result    string `json:"result"`
		Reason    string `json:"reason"`
		Status    int    `json:"status"`
		ElapsedMS int64  `json:"elapsedMs"`
	}{stage, debugDiagnosticResult(err), initializationReason(err), status,
		min(max(elapsed.Milliseconds(), 0), 60000)}
	if err == nil {
		record.Reason = "none"
	}
	encoded, encodeErr := json.Marshal(record)
	if encodeErr != nil {
		return
	}
	encoded = append(encoded, '\n')
	debugDiagnosticMu.Lock()
	defer debugDiagnosticMu.Unlock()
	file, openErr := os.OpenFile(renderDiagnosticsPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if openErr != nil {
		return
	}
	defer func() { ignoreProcessError(file.Close()) }()
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return
	}
	if lockErr := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); lockErr != nil {
		return
	}
	defer func() { ignoreProcessError(unix.Flock(int(file.Fd()), unix.LOCK_UN)) }()
	info, statErr = file.Stat()
	if statErr != nil || info.Size()+int64(len(encoded)) > 65536 {
		return
	}
	_, writeErr := file.Write(encoded)
	ignoreProcessError(writeErr)
}

func debugDiagnosticResult(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrUnavailable):
		return "unavailable"
	case errors.Is(err, ErrOutputTooLarge), errors.Is(err, renderjob.ErrOutputTooLarge):
		return "output-limit"
	case errors.Is(err, ErrRenderFailed), errors.Is(err, renderjob.ErrRendering):
		return "render-failed"
	case errors.Is(err, renderjob.ErrPreparation):
		return "preparation"
	case errors.Is(err, renderjob.ErrNotActive):
		return "not-active"
	case errors.Is(err, renderjob.ErrClosed):
		return "closed"
	case errors.Is(err, renderjob.ErrSaturated):
		return "saturated"
	case errors.Is(err, renderjob.ErrInvalidRequest):
		return "invalid-request"
	case errors.Is(err, renderjob.ErrAuthoritySource):
		return "authority-source"
	case errors.Is(err, renderjob.ErrGenerationChanged):
		return "generation-changed"
	default:
		return "other"
	}
}

func renderDiagnosticsPath() string {
	if workspace := os.Getenv("GITHUB_WORKSPACE"); workspace != "" {
		dir := workspace + "/.dev/proof-evidence/render-diagnostics"
		if err := os.MkdirAll(dir, 0o700); err == nil {
			return dir + "/render-diagnostics.jsonl"
		}
	}
	return "/tmp/aboutme-render-diagnostics.jsonl"
}
