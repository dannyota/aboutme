package main

import (
	"context"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/privacynotice"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const privacyNoticeTimeout = 5 * time.Minute

func executePrivacyNoticeAction(ctx context.Context, action string, getenv func(string) string) (privacyNoticeResult, error) {
	failed := privacyNoticeResult{report: privacynotice.Report{Code: 1}}
	if ctx.Err() != nil {
		return failed, privacynotice.ErrState
	}
	if _, err := parsePrivacyNoticeArgs([]string{action}); err != nil {
		return failed, privacynotice.ErrState
	}
	ctx, cancel := context.WithTimeout(ctx, privacyNoticeTimeout)
	defer cancel()
	cfg, err := config.LoadPrivacyJob(getenv, false)
	if err != nil {
		return failed, privacynotice.ErrState
	}
	var outbox *authmail.Outbox
	if action == "enqueue" {
		ring, keyErr := privacyNoticeKeyRing(getenv)
		if keyErr != nil {
			return failed, privacynotice.ErrState
		}
		outbox, err = authmail.NewOutbox(ring, time.Now)
		if err != nil {
			return failed, privacynotice.ErrState
		}
	}
	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return failed, privacynotice.ErrState
	}
	defer func() {
		cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelCleanup()
		pool.Close(cleanup)
	}()
	service, err := privacynotice.New(pool, outbox, time.Now)
	if err != nil {
		return failed, privacynotice.ErrState
	}
	if action == "reconcile-initial" {
		initial, initialErr := service.ReconcileInitial(ctx)
		if initialErr != nil {
			return failed, initialErr
		}
		return privacyNoticeResult{initial: &initial}, nil
	}
	report, err := service.Run(ctx, action)
	return privacyNoticeResult{report: report}, err
}
