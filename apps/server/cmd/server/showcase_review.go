package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
	"github.com/dannyota/aboutme/apps/server/internal/showcase"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	showcaseReviewCommandName  = "showcase-review"
	showcaseReviewTimeout      = 30 * time.Second
	showcaseReviewCleanupLimit = 5 * time.Second
	// showcaseNone prints an unset value.
	showcaseNone = "-"
)

// Exit codes: 0 the command ran, 1 a failure or a usage error, 3 approve or
// decline matched nothing because the key is stale or the resume has no
// request. Every run that reaches the database ends its stdout with
// showcaseReviewEnd, so a reader can tell a complete log from a partial one.
const (
	showcaseStaleExitCode = 3
	showcaseReviewEnd     = "showcase-review: end"
)

// showcaseStaleExitError reports a stale review key through the exit code.
type showcaseStaleExitError struct{}

func (showcaseStaleExitError) Error() string { return "showcase review key is stale" }

var errShowcaseReviewUsage = errors.New("usage: server showcase-review {pending|approve <slug> <key>|decline <slug> <key>|show <slug>}; exit 3 means a stale key")

// showcaseReviewCommand is one parsed operator review command
// (docs/design/showcase.md "Review"). Its output and logs carry slugs, keys,
// versions, and dates only, never a name or headline.
type showcaseReviewCommand struct {
	action string
	slug   string
	key    string
}

func parseShowcaseReviewCommand(args []string) (showcaseReviewCommand, error) {
	if len(args) == 0 {
		return showcaseReviewCommand{}, errShowcaseReviewUsage
	}
	command := showcaseReviewCommand{action: args[0]}
	switch command.action {
	case "pending":
		if len(args) != 1 {
			return showcaseReviewCommand{}, errShowcaseReviewUsage
		}
	case "approve", "decline":
		if len(args) != 3 || !publicroots.ValidSlug(args[1]) || !showcase.ValidKey(args[2]) {
			return showcaseReviewCommand{}, errShowcaseReviewUsage
		}
		command.slug, command.key = args[1], args[2]
	case "show":
		if len(args) != 2 || !publicroots.ValidSlug(args[1]) {
			return showcaseReviewCommand{}, errShowcaseReviewUsage
		}
		command.slug = args[1]
	default:
		return showcaseReviewCommand{}, errShowcaseReviewUsage
	}
	return command, nil
}

func runShowcaseReview(args []string) error {
	command, err := parseShowcaseReviewCommand(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, showcaseReviewTimeout)
	defer cancel()
	return executeShowcaseReview(ctx, command, os.Getenv, os.Stdout)
}

func executeShowcaseReview(ctx context.Context, command showcaseReviewCommand, getenv func(string) string, output io.Writer) error {
	if err := ctx.Err(); err != nil {
		return errors.New("showcase review canceled")
	}
	cfg, err := config.LoadPrivacyJob(getenv, false)
	if err != nil {
		return errors.New("showcase review configuration is invalid")
	}
	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("showcase review database configuration is invalid")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), showcaseReviewCleanupLimit)
		defer cancel()
		pool.Close(cleanupCtx)
	}()
	return runShowcaseReviewWithEnd(ctx, showcase.NewReviewer(pool, time.Now), command, output)
}

// runShowcaseReviewWithEnd runs the command and, on success and on a stale
// key, prints the final sentinel line. A failure prints nothing more.
func runShowcaseReviewWithEnd(ctx context.Context, reviewer *showcase.Reviewer, command showcaseReviewCommand, output io.Writer) error {
	err := runShowcaseReviewCommand(ctx, reviewer, command, output)
	var stale showcaseStaleExitError
	if err != nil && !errors.As(err, &stale) {
		return err
	}
	if _, writeErr := fmt.Fprintln(output, showcaseReviewEnd); writeErr != nil {
		return writeResult(writeErr)
	}
	return err
}

// runShowcaseReviewCommand runs one parsed command against reviewer and prints
// its result. A stale key changes nothing and says so.
func runShowcaseReviewCommand(ctx context.Context, reviewer *showcase.Reviewer, command showcaseReviewCommand, output io.Writer) error {
	switch command.action {
	case "pending":
		items, err := reviewer.Pending(ctx)
		if err != nil {
			return errors.New("showcase review database is unavailable")
		}
		if len(items) == 0 {
			_, err = fmt.Fprintln(output, "no pending showcase requests")
			return writeResult(err)
		}
		for _, item := range items {
			if _, err = fmt.Fprintf(output, "%s\t%s\t%s\t%s\n", item.Slug, item.ReviewKey, item.CardVersion, item.RequestedAt.UTC().Format(time.RFC3339)); err != nil {
				return writeResult(err)
			}
		}
		return nil
	case "approve":
		applied, err := reviewer.Approve(ctx, command.slug, command.key)
		return printReviewResult(output, "approved", applied, err, command)
	case "decline":
		applied, err := reviewer.Decline(ctx, command.slug, command.key)
		return printReviewResult(output, "declined", applied, err, command)
	case "show":
		state, found, err := reviewer.Show(ctx, command.slug)
		if err != nil {
			return errors.New("showcase review database is unavailable")
		}
		if !found {
			_, err = fmt.Fprintf(output, "%s has no showcase request\n", command.slug)
			return writeResult(err)
		}
		return printShowcaseState(output, state)
	default:
		return errShowcaseReviewUsage
	}
}

func printReviewResult(output io.Writer, verb string, applied bool, err error, command showcaseReviewCommand) error {
	if err != nil {
		return errors.New("showcase review database is unavailable")
	}
	if !applied {
		_, writeErr := fmt.Fprintf(output, "stale: %s is not the current review key of %s, or it has no showcase request; nothing changed\n", command.key, command.slug)
		if writeErr != nil {
			return writeResult(writeErr)
		}
		return showcaseStaleExitError{}
	}
	_, writeErr := fmt.Fprintf(output, "%s %s %s\n", verb, command.slug, command.key)
	return writeResult(writeErr)
}

func printShowcaseState(output io.Writer, state showcase.State) error {
	_, err := fmt.Fprintf(output,
		"slug: %s\nstate: %s\nrole: %s\ntemplate: %s\nreview_key: %s\ncard_version: %s\nreviewed_key: %s\noutcome: %s\nrequested_at: %s\nreviewed_at: %s\nfirst_listed_at: %s\n",
		state.Slug, state.State, orNone(state.Role), orNone(state.TemplateID), state.ReviewKey, state.CardVersion,
		orNone(state.ReviewedKey), orNone(state.Outcome), state.RequestedAt.UTC().Format(time.RFC3339),
		formatShowcaseTime(state.ReviewedAt), formatShowcaseTime(state.FirstListedAt))
	return writeResult(err)
}

func orNone(value *string) string {
	if value == nil {
		return showcaseNone
	}
	return *value
}

func formatShowcaseTime(value *time.Time) string {
	if value == nil {
		return showcaseNone
	}
	return value.UTC().Format(time.RFC3339)
}

func writeResult(err error) error {
	if err != nil {
		return errors.New("showcase review result could not be written")
	}
	return nil
}
