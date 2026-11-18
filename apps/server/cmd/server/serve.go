package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const shutdownTimeout = 10 * time.Second

type stopBudget struct {
	drain, join, flush, total time.Duration
}

func serverStopBudget() stopBudget {
	return stopBudget{drain: shutdownTimeout, join: 12 * time.Second, flush: 18 * time.Second, total: 20 * time.Second}
}

type stopSteps struct {
	stopRendering func(context.Context) error
	stopEvidence  func(context.Context) error
	stopWorkers   func()
	joins         []<-chan struct{}
	flushViews    func(context.Context) error
	closePool     func(context.Context)
}

func serveUntilStopped(ctx context.Context, logger *slog.Logger, budget stopBudget, publicListener net.Listener, publicHandler http.Handler,
	printListener net.Listener, printHandler http.Handler, steps stopSteps,
) error {
	start, err := servePair(ctx, logger, budget, publicListener, publicHandler, printListener, printHandler, steps.stopRendering)
	detached := context.WithoutCancel(ctx)

	evidenceCtx, cancel := context.WithDeadline(detached, start.Add(budget.join))
	if stopErr := steps.stopEvidence(evidenceCtx); stopErr != nil {
		logger.Error("github evidence shutdown did not finish revocations")
	}
	cancel()

	steps.stopWorkers()
	joinCtx, cancel := context.WithDeadline(detached, start.Add(budget.join))
	for _, done := range steps.joins {
		select {
		case <-done:
		case <-joinCtx.Done():
		}
	}
	if joinCtx.Err() != nil {
		logger.Error("background workers did not stop within the shutdown budget")
	}
	cancel()

	flushCtx, cancel := context.WithDeadline(detached, start.Add(budget.flush))
	if flushErr := steps.flushViews(flushCtx); flushErr != nil {
		logger.Warn("viewcount: final flush dropped counts")
	}
	cancel()

	poolCtx, cancel := context.WithDeadline(detached, start.Add(budget.total))
	steps.closePool(poolCtx)
	if poolCtx.Err() != nil {
		logger.Error("database pool did not close within the shutdown budget")
	}
	cancel()
	return err
}

func servePair(ctx context.Context, logger *slog.Logger, budget stopBudget, publicListener net.Listener, publicHandler http.Handler,
	printListener net.Listener, printHandler http.Handler, stopRendering func(context.Context) error,
) (time.Time, error) {
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() {
		results <- serveHTTP(serverCtx, logger, publicListener, &http.Server{
			Handler: publicHandler, ReadHeaderTimeout: 5 * time.Second,
		}, budget.drain)
	}()
	go func() {
		results <- serveHTTP(serverCtx, logger, printListener, &http.Server{
			Handler: printHandler, ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
			IdleTimeout: time.Second, MaxHeaderBytes: 4096,
		}, budget.drain)
	}()
	var first error
	remaining := 2
	select {
	case first = <-results:
		remaining--
	case <-ctx.Done():
	}
	start := time.Now()
	cancel()
	renderCtx, cancelRender := context.WithDeadline(context.WithoutCancel(ctx), start.Add(budget.drain))
	if stopErr := stopRendering(renderCtx); stopErr != nil {
		logger.Error("print queue did not stop within the shutdown budget")
	}
	cancelRender()
	for range remaining {
		first = errors.Join(first, <-results)
	}
	return start, first
}

func serve(ctx context.Context, logger *slog.Logger, ln net.Listener, handler http.Handler) error {
	return serveHTTP(ctx, logger, ln, &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}, shutdownTimeout)
}

func serveHTTP(ctx context.Context, logger *slog.Logger, ln net.Listener, srv *http.Server, drain time.Duration) error {
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", ln.Addr().String())
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drain)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return errors.Join(fmt.Errorf("graceful shutdown: %w", err), srv.Close())
	}

	logger.Info("server stopped")
	return nil
}
