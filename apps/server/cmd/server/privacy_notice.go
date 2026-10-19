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

type privacyNoticeRunner func(context.Context, string, func(string) string) (privacynotice.Report, error)

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
	report, err := run(ctx, action, getenv)
	if err != nil {
		return invalid()
	}
	data, err := report.JSON(action == "activate")
	if err != nil {
		return invalid()
	}
	written, writeErr := output.Write(data)
	if writeErr != nil || written != len(data) {
		return invalid()
	}
	if report.Code != 0 {
		return privacyNoticeExitError{code: report.Code}
	}
	return nil
}
