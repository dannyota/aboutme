package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(execute(os.Args[1:], os.Stderr))
}

func execute(args []string, out io.Writer) int {
	config, err := parseRunnerArgs(args)
	if err != nil {
		return report(out, privateArtifacts{}, err, 2)
	}
	deps, err := newRuntimeDeps(config)
	if err != nil {
		return report(out, config.Run, err, 2)
	}
	ctx, stop := newRunContext()
	defer stop()
	if _, runErr := runWorkflow(ctx, config, deps); runErr != nil {
		return report(out, config.Run, runErr, 1)
	}
	return 0
}

func report(out io.Writer, run privateArtifacts, err error, status int) int {
	if reportErr := reportFailure(out, run, err); reportErr != nil {
		return 3
	}
	return status
}

func newRunContext() (context.Context, context.CancelFunc) {
	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(signalContext, workflowTimeout)
	return ctx, func() {
		cancel()
		stopSignals()
	}
}
