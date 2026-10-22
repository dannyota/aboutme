package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/dannyota/aboutme/apps/server/internal/privacynotice"
)

type privacyNoticeExitError struct{ code int }

func (privacyNoticeExitError) Error() string { return "invalid_state" }

type privacyNoticeResult struct {
	report  privacynotice.Report
	initial *privacynotice.InitialResult
}

type privacyNoticeRunner func(context.Context, string, func(string) string) (privacyNoticeResult, error)

func runPrivacyNotice(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return executePrivacyNoticeCommand(ctx, args, os.Getenv, os.Stdout, os.Stderr, executePrivacyNoticeAction)
}

func executePrivacyNoticeCommand(ctx context.Context, args []string, getenv func(string) string, output, diagnostics io.Writer, run privacyNoticeRunner) error {
	invalid := func() error {
		// All failure paths hide configuration, database, and provider details.
		if _, writeErr := io.WriteString(diagnostics, "invalid_state\n"); writeErr != nil {
			return privacyNoticeExitError{code: 1}
		}
		return privacyNoticeExitError{code: 1}
	}
	action, err := parsePrivacyNoticeArgs(args)
	if err != nil {
		return invalid()
	}
	result, err := run(ctx, action, getenv)
	if err != nil {
		return invalid()
	}
	var data []byte
	code := result.report.Code
	if action == "reconcile-initial" {
		if result.initial == nil {
			return invalid()
		}
		data, err = result.initial.JSON()
		code = 0
	} else {
		if result.initial != nil {
			return invalid()
		}
		data, err = result.report.JSON(action == "activate")
	}
	if err != nil {
		return invalid()
	}
	written, writeErr := output.Write(data)
	if writeErr != nil || written != len(data) {
		return invalid()
	}
	if code != 0 {
		return privacyNoticeExitError{code: code}
	}
	return nil
}
