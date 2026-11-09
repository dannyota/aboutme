// Command server runs the aboutme Go API server. This file is wiring only:
// load config, build the store and router, run the HTTP server, and shut
// down gracefully on SIGINT/SIGTERM. Construction lives in internal/app and
// all behavior in internal/.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	platformruntime "runtime"
	"strconv"
	"syscall"
	"time"

	"danny.vn/aboutme/apps/server/internal/app"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/printrender"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/realtime"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		var noticeExit privacyNoticeExitError
		if errors.As(err, &noticeExit) {
			os.Exit(noticeExit.code)
		}
		var occupancyExit rootOccupancyExitError
		if errors.As(err, &occupancyExit) {
			os.Exit(occupancyExit.code)
		}
		var deleteExit accountDeleteExitError
		if errors.As(err, &deleteExit) {
			if deleteExit.message != "" {
				fmt.Fprintln(os.Stderr, deleteExit.message)
			}
			os.Exit(deleteExit.code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// dispatch handles operator commands before the default server and privacy jobs.
func dispatch(args []string) error {
	if len(args) > 0 && args[0] == privacyNoticeCommandName {
		return runPrivacyNotice(args[1:])
	}
	if len(args) > 0 && args[0] == checkPublicRootCommandName {
		return runCheckPublicRoot(args[1:])
	}
	if len(args) > 0 && args[0] == totpKeyReencryptCommandName {
		return runTOTPKeyReencrypt(args[1:])
	}
	if len(args) > 0 && args[0] == accountDeleteCommandName {
		return runAccountDelete(args[1:])
	}
	if len(args) > 0 && args[0] == mediaDeletionStatusCommandName {
		return runMediaDeletionStatus(args[1:])
	}
	return runCommand(args)
}

func run() error {
	cfg, err := config.LoadEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := app.NewLogger(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// NewPool does not dial eagerly. The durable public-state read below is the
	// intentional startup gate: the server must not admit public traffic with a
	// guessed discovery generation.
	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close(context.Background())
	queries := store.New(pool)
	coordinator, err := app.NewPublicState(ctx, queries)
	if err != nil {
		return err
	}
	runtime, err := app.ParsePublicRuntime(cfg.PublicOrigin, cfg.PublicRenderOrigin, cfg.Env, cfg.AppBuildDigest, cfg.PublicRendererBuildDigest)
	if err != nil {
		return fmt.Errorf("parse public runtime: %w", err)
	}

	authService, err := auth.NewService(logger, cfg, pool)
	if err != nil {
		return fmt.Errorf("create auth service: %w", err)
	}
	passwordAuth, err := app.NewPasswordAuth(ctx, logger, cfg, pool, queries)
	if err != nil {
		return fmt.Errorf("create password auth: %w", err)
	}
	blobs, err := app.NewMedia(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create media backend: %w", err)
	}
	projector := docmigrate.NewIdentityProjector()
	reader, err := app.NewReader(queries, projector, coordinator, blobs, runtime)
	if err != nil {
		return fmt.Errorf("create public reader: %w", err)
	}
	showcaseService, err := app.NewShowcase(pool, reader, projector, logger)
	if err != nil {
		return fmt.Errorf("create showcase service: %w", err)
	}
	// Showcase rows follow the current scrub rules, presets, and card layout
	// from the first request on (docs/design/showcase.md "Derived values and
	// reports").
	recomputed, skipped, err := showcaseService.RecomputeAll(ctx)
	if err != nil {
		return fmt.Errorf("recompute showcase rows: %w", err)
	}
	logger.Info("showcase rows recomputed", "recomputed", recomputed, "skipped", skipped)
	cache, err := publiccache.New(128, time.Minute, time.Now)
	if err != nil {
		return fmt.Errorf("create public cache: %w", err)
	}
	renderer := directrender.New(runtime.RenderOrigin, nil)
	printRenderer, printQueue, err := app.NewPrint(platformruntime.GOOS, printrender.Config{
		BrowserExecutable: cfg.ChromiumPath, RenderOrigin: runtime.RenderOrigin, Logger: logger,
	}, printrender.New)
	if err != nil {
		return err
	}
	if printRenderer == nil {
		logger.Info("print and PDF rendering is unavailable on this platform", "platform", platformruntime.GOOS)
	}
	closePrintQueue := func() {
		if closeErr := printQueue.Close(); closeErr != nil {
			logger.Error("close print queue failed")
		}
	}
	defer closePrintQueue()
	printHandler, err := app.PrivatePrintHandler(printQueue, printRenderer != nil)
	if err != nil {
		return errors.New("initialize private print handler")
	}
	sessionManager := auth.NewSessionManagerWithPool(pool)
	accountService, err := app.NewAccount(app.AccountDependencies{
		Config: cfg, Pool: pool, Sessions: sessionManager, Coordinator: coordinator,
		Media: blobs, Projector: projector, Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("create account service: %w", err)
	}
	authService.SetAccountDeleteHandler(accountService.DeleteHandler())
	cards, err := app.NewPreviewCards(cfg.PreviewCards, pool, reader, printQueue, logger)
	if err != nil {
		return err
	}
	hub, err := realtime.NewHub(realtime.Config{Observe: app.EvictDiscoveryOnDelete(cache, cards.Observe())})
	if err != nil {
		return fmt.Errorf("create realtime hub: %w", err)
	}
	defer hub.Close()
	viewPassKey, err := app.DecodeViewPassKey(cfg)
	if err != nil {
		return fmt.Errorf("decode VIEW_PASS_KEY: %w", err)
	}
	streams, err := app.NewStreams(cfg, hub, queries, sessionManager, coordinator, viewPassKey)
	if err != nil {
		return fmt.Errorf("create realtime service: %w", err)
	}
	views, err := app.NewViewCounts(cfg, logger, queries, sessionManager, viewPassKey)
	if err != nil {
		return err
	}
	publicService, err := app.NewPublic(app.PublicDependencies{
		Config: cfg, Runtime: runtime, Logger: logger, Reader: reader, Queries: queries,
		Coordinator: coordinator, Cache: cache, Renderer: renderer, Streams: streams, PrintQueue: printQueue,
		Cards: cards.Public, Views: views.Counter, Showcase: showcaseService, ViewPassKey: viewPassKey,
	})
	if err != nil {
		return fmt.Errorf("create public service: %w", err)
	}
	readiness := app.NewReadiness(app.ReadinessDependencies{
		Coordinator: coordinator, PingDatabase: pool.Ping, Renderer: renderer,
		PrintRenderer: printRenderer, PrintQueue: printQueue, PublicOrigin: runtime.PublicOrigin,
	})
	resumeService := app.NewResumes(app.ResumeDependencies{
		Config: cfg, Pool: pool, Projector: projector, Media: blobs, Logger: logger,
		Sessions: sessionManager, Coordinator: coordinator, PrintQueue: printQueue, Showcase: showcaseService,
	})
	agentRoutes, err := app.NewAgents(ctx, cfg, pool, queries, resumeService, sessionManager)
	if err != nil {
		return fmt.Errorf("create agent access: %w", err)
	}
	secondFactor, err := app.NewSecondFactor(logger, cfg, pool)
	if err != nil {
		return fmt.Errorf("create second factor routes: %w", err)
	}

	handler := app.NewRouter(app.RouterDependencies{
		Config: cfg, Logger: logger, Readiness: readiness, Public: publicService,
		Providers: authService, Account: accountService, Resumes: resumeService, Passwords: passwordAuth.Service,
		Agents: agentRoutes, SecondFactor: secondFactor.RegisterRoutes, Streams: streams, Views: views.Service,
	})

	var lc net.ListenConfig
	addr := net.JoinHostPort(cfg.ListenHost, strconv.Itoa(cfg.Port))
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	defer func() {
		if closeErr := ln.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			logger.Error("close public listener failed")
		}
	}()
	printListener, err := lc.Listen(ctx, "tcp", cfg.PrintListenAddr)
	if err != nil {
		return errors.New("listen on private print address")
	}
	defer func() {
		if closeErr := printListener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			logger.Error("close private print listener failed")
		}
	}()

	// The mail worker runs for the life of the server: SIGINT/SIGTERM cancels
	// workerCtx (via ctx), and Run joins its in-flight sends before returning.
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	workerDone := make(chan struct{})
	listenerDone := make(chan struct{})
	go func() {
		defer close(listenerDone)
		if listenErr := realtime.RunListener(workerCtx, pool, hub); listenErr != nil {
			logger.Error("realtime listener stopped unexpectedly")
		}
	}()
	go func() {
		defer close(workerDone)
		if runErr := passwordAuth.Worker.Run(workerCtx); runErr != nil {
			// Run returns nil on cancellation; a non-nil return is unexpected.
			logger.Error("authmail worker stopped unexpectedly", "err", "worker stopped")
		}
	}()
	// A startup query failure only means the bounded three-ID key-ring read
	// itself could not run; it is not one of the closed totp_unavailable
	// reasons, so it is logged and never fails startup
	// (docs/design/totp-key-management.md "Key failures").
	if healthErr := secondFactor.TOTPHealth.Check(ctx); healthErr != nil {
		logger.Error("totp key health check failed at startup", "err", "query failed")
	}
	cardsDone := cards.Run(workerCtx, logger)
	// The view flusher outlives the signal so counts from requests that
	// finish while the server drains still reach the database.
	viewsCtx, stopViews := context.WithCancel(context.WithoutCancel(ctx))
	defer stopViews()
	viewsDone := views.Run(viewsCtx)
	totpHealthDone := make(chan struct{})
	go func() {
		defer close(totpHealthDone)
		runTOTPHealthTicker(workerCtx, logger, secondFactor.TOTPHealth)
	}()

	logger.Info("starting", "env", cfg.Env)
	err = servePair(ctx, logger, ln, handler, printListener, printHandler, closePrintQueue)
	cancelWorker()
	stopViews()
	<-workerDone
	<-listenerDone
	<-totpHealthDone
	<-cardsDone
	<-viewsDone
	return err
}
