// Package main runs the MCP owner workflow.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

var (
	errBrowserTimeout = errors.New("browser_timeout")
	errBrowserFailed  = errors.New("browser_failed")
	// errSecondFactorRequired is the browser helper's own outcome for a
	// password login that answers 202 secondFactorRequired. Passkeys are live
	// in production and the owner workflow signs in with password only, so
	// this is a distinct closed reason rather than the generic browser
	// failure a login rejection or a stuck consent page produces.
	errSecondFactorRequired = errors.New("second_factor_required")
)

// browserResultSecondFactor is the exact byte content the browser helper
// writes to browser-result.json when the password login response carries
// secondFactorRequired. See docs/design/mcp-owner-workflow.md#browser-helper-interface.
const browserResultSecondFactor = "second_factor_required"

const browserTimeout = 5 * time.Minute

type browserRequest struct {
	Version          int    `json:"version"`
	AuthorizationURL string `json:"authorization_url"`
	Mode             string `json:"mode"`
	ExpectedOrigin   string `json:"expected_origin"`
	ExpectedLogin    string `json:"expected_login_path"`
	CredentialFile   string `json:"credential_file"`
	ResultFile       string `json:"result_file"`
}

type browserCoordinator struct {
	directory string
	mode      string
	origin    string
	wait      time.Duration
}

func (b browserCoordinator) Open(ctx context.Context, authorizationURL string) error {
	deadline := time.NewTimer(b.wait)
	defer deadline.Stop()
	for {
		ready, err := readPrivateFile(b.directory, "browser-ready")
		if err == nil && string(ready) == "ready" {
			break
		}
		select {
		case <-ctx.Done():
			return errBrowserTimeout
		case <-deadline.C:
			return errBrowserTimeout
		case <-time.After(10 * time.Millisecond):
		}
	}
	request, err := json.Marshal(browserRequest{
		Version:          1,
		AuthorizationURL: authorizationURL,
		Mode:             b.mode,
		ExpectedOrigin:   b.origin,
		ExpectedLogin:    "/login",
		CredentialFile:   "/mcp-credentials/login.env",
		ResultFile:       "/mcp-browser/browser-result.json",
	})
	if err != nil || atomicPrivateWrite(b.directory, "browser-request.json", request) != nil {
		return errBrowserFailed
	}
	for {
		result, err := readPrivateFile(b.directory, "browser-result.json")
		if err == nil {
			switch string(result) {
			case "completed":
				return nil
			case browserResultSecondFactor:
				return errSecondFactorRequired
			default:
				return errBrowserFailed
			}
		}
		select {
		case <-ctx.Done():
			return errBrowserTimeout
		case <-deadline.C:
			return errBrowserTimeout
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func newBrowserCoordinator(directory, mode, origin string) browserHandoff {
	return browserCoordinator{directory: directory, mode: mode, origin: origin, wait: browserTimeout}
}

func removeBrowserFiles(directory string) error {
	for _, name := range []string{"browser-ready", "browser-request.json", "browser-result.json"} {
		if err := os.Remove(filepath.Join(directory, name)); err != nil && !errors.Is(err, os.ErrNotExist) { //nolint:gosec // G703: directory is the validated browser root and name is one of three fixed constants.
			return errUnsafeFile
		}
	}
	return nil
}
