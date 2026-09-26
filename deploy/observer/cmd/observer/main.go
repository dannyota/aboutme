// Command observer publishes https://aboutme.vn/.well-known/deployment.json
// (docs/design/deployment-transparency/README.md). It asks the platform
// which image digests are running, checks each one against the signed
// GitHub build record, and writes the result to S3. It never reports the
// application's own version; every claim comes from the platform or from
// Sigstore-signed provenance.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

// version and commit are set at build time
// (deploy/observer/Dockerfile); both are empty in a build that does not set
// them.
var (
	version string
	commit  string
)

// versionString formats the observer's own build information for the
// version subcommand.
func versionString() string {
	return fmt.Sprintf("observer %s %s", version, commit)
}

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// dispatch runs the subcommand named by args. It never prints a platform
// object, an ARN, an account ID, or an environment value beyond the
// configured names (docs/design/deployment-transparency/document.md,
// "Sanitizer").
func dispatch(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: observer <lambda|run|version>")
	}
	switch args[0] {
	case "version":
		fmt.Println(versionString())
		return nil
	case "lambda":
		return runLambda()
	case "run":
		return runLoop(args[1:])
	default:
		return fmt.Errorf("unknown command %q: usage: observer <lambda|run|version>", args[0])
	}
}

// runLambda builds the observer once and starts the Lambda runtime loop,
// so a warm invocation reuses the verifier's TUF trust root and HTTP client
// (docs/design/deployment-transparency/README.md, "Where the observer runs").
func runLambda() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	ctx := context.Background()
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	obs, err := wire(ctx, cfg)
	if err != nil {
		return fmt.Errorf("build observer: %w", err)
	}
	lambda.StartWithOptions(func(ctx context.Context) error {
		return obs.run(ctx, time.Now)
	}, lambda.WithContext(ctx))
	return nil
}

// runLoop parses --interval, builds the observer once, and runs it every
// tick until the process is signaled to stop
// (docs/design/deployment-transparency/README.md, "Kubernetes").
func runLoop(args []string) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	interval := fs.Duration("interval", 60*time.Second, "how often to observe and publish")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *interval <= 0 {
		return fmt.Errorf("--interval must be positive, got %s", *interval)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	obs, err := wire(ctx, cfg)
	if err != nil {
		return fmt.Errorf("build observer: %w", err)
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		// Each run ends within one interval, as a Lambda run ends within
		// its timeout.
		runCtx, cancel := context.WithTimeout(ctx, *interval)
		if err := obs.run(runCtx, time.Now); err != nil {
			slog.Error("run failed", "error", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
