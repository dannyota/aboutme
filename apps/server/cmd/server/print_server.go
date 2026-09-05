package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

func servePair(ctx context.Context, logger *slog.Logger, publicListener net.Listener, publicHandler http.Handler,
	printListener net.Listener, printHandler http.Handler, stopRendering func(),
) error {
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- serve(serverCtx, logger, publicListener, publicHandler) }()
	go func() {
		results <- serveHTTP(serverCtx, logger, printListener, &http.Server{
			Handler: printHandler, ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
			IdleTimeout: time.Second, MaxHeaderBytes: 4096,
		})
	}()
	var first error
	remaining := 2
	select {
	case first = <-results:
		remaining--
	case <-ctx.Done():
	}
	cancel()
	stopRendering()
	for range remaining {
		first = errors.Join(first, <-results)
	}
	return first
}
