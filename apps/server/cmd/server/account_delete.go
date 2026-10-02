package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/accountapi"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// The account-delete command answers a report against a showcase listing: the
// operator deletes the account that owns the reported slug, through the same
// deletion path as a self-delete from Settings (docs/design/showcase.md
// "Report and account deletion", ADR 0003). Output and logs carry the user ID,
// slugs, action, outcome, and time only.

const (
	accountDeleteCommandName = "account-delete"
	accountDeleteTimeout     = 2 * time.Minute
	accountDeleteEndLine     = "account-delete: end"

	accountDeleteUsageExitCode    = 2
	accountDeleteUnknownExitCode  = 3
	accountDeleteNotFoundExitCode = 4
	accountDeleteMismatchExitCode = 5
)

const accountDeleteUsage = "usage: server account-delete {show <slug> | confirm <slug> <user-id>}"

// accountDeleteExitError carries a fixed exit status. Status 4 and 5 have
// already written their result line; status 2 carries the usage text.
type accountDeleteExitError struct {
	code    int
	message string
}

func (err accountDeleteExitError) Error() string { return err.message }

type accountDeleteArgs struct {
	confirm bool
	slug    string
	userID  uuid.UUID
}

func parseAccountDeleteArgs(args []string) (accountDeleteArgs, error) {
	usage := accountDeleteExitError{code: accountDeleteUsageExitCode, message: accountDeleteUsage}
	if len(args) == 0 {
		return accountDeleteArgs{}, usage
	}
	var parsed accountDeleteArgs
	switch {
	case args[0] == "show" && len(args) == 2:
	case args[0] == "confirm" && len(args) == 3:
		parsed.confirm = true
	default:
		return accountDeleteArgs{}, usage
	}
	parsed.slug = args[1]
	if !publicroots.ValidSlug(parsed.slug) {
		return accountDeleteArgs{}, usage
	}
	if parsed.confirm {
		id, err := uuid.Parse(args[2])
		if err != nil || id.String() != args[2] {
			return accountDeleteArgs{}, usage
		}
		parsed.userID = id
	}
	return parsed, nil
}

func runAccountDelete(args []string) error {
	parsed, err := parseAccountDeleteArgs(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, accountDeleteTimeout)
	defer cancel()
	return executeAccountDelete(ctx, parsed, os.Getenv, os.Stdout, os.Stderr)
}

func executeAccountDelete(ctx context.Context, args accountDeleteArgs, getenv func(string) string, output, diagnostics io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(diagnostics, nil))
	action := "show"
	if args.confirm {
		action = "confirm"
	}
	failed := func(err error) error {
		attrs := []any{"action", action, "outcome", "failed"}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			attrs = append(attrs, "sqlstate", pgErr.Code)
		}
		logger.Error(accountDeleteCommandName, attrs...)
		return errors.New("account-delete failed; nothing was deleted")
	}
	cfg, err := config.LoadPrivacyJob(getenv, false)
	if err != nil {
		return failed(errors.New("configuration is invalid"))
	}
	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return failed(errors.New("database configuration is invalid"))
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), privacyJobCleanupTimeout)
		defer cancel()
		pool.Close(cleanupCtx)
	}()
	service, err := accountapi.NewOperator(accountapi.OperatorDependencies{
		Pool: pool, Projector: docmigrate.NewIdentityProjector(), Logger: logger, Now: time.Now,
	})
	if err != nil {
		return failed(err)
	}

	var account accountapi.Account
	if args.confirm {
		account, err = service.DeleteAccountBySlug(ctx, args.slug, args.userID)
	} else {
		account, err = service.AccountBySlug(ctx, args.slug)
	}
	if err != nil && !errors.Is(err, accountapi.ErrSlugNotFound) && !errors.Is(err, accountapi.ErrSlugMismatch) && !errors.Is(err, accountapi.ErrOutcomeUnknown) {
		return failed(err)
	}
	return reportAccountDelete(output, logger, args, account, err)
}

// reportAccountDelete writes the result of one run and returns its exit
// status. A lost commit answer exits 3 with no end line: the account may be
// deleted, and show tells.
func reportAccountDelete(output io.Writer, logger *slog.Logger, args accountDeleteArgs, account accountapi.Account, err error) error {
	action := "show"
	if args.confirm {
		action = "confirm"
	}
	switch {
	case errors.Is(err, accountapi.ErrSlugNotFound):
		logger.Info(accountDeleteCommandName, "action", action, "outcome", "not_found", "slugs", []string{args.slug})
		return accountDeleteResult(output, accountDeleteNotFoundExitCode, "not found: "+args.slug)
	case errors.Is(err, accountapi.ErrSlugMismatch):
		logger.Info(accountDeleteCommandName, "action", action, "outcome", "mismatch", "user", args.userID.String(), "slugs", []string{args.slug})
		return accountDeleteResult(output, accountDeleteMismatchExitCode,
			fmt.Sprintf("mismatch: %s is not owned by %s; nothing deleted", args.slug, args.userID))
	case errors.Is(err, accountapi.ErrOutcomeUnknown):
		logger.Error(accountDeleteCommandName, "action", action, "outcome", "unknown", "user", args.userID.String(), "slugs", []string{args.slug})
		if _, writeErr := fmt.Fprintf(output, "result unknown: run show %s\n", args.slug); writeErr != nil {
			return errors.New("account-delete result could not be written")
		}
		return accountDeleteExitError{code: accountDeleteUnknownExitCode}
	}
	line := fmt.Sprintf("user=%s slugs=%s", account.UserID, strings.Join(account.Slugs, ","))
	outcome := "shown"
	if args.confirm {
		line, outcome = "deleted "+line, "deleted"
	} else {
		line = "account " + line
	}
	logger.Info(accountDeleteCommandName, "action", action, "outcome", outcome, "user", account.UserID.String(), "slugs", account.Slugs)
	return accountDeleteResult(output, 0, line)
}

// accountDeleteResult writes the result line and the end marker that the
// deploy script reads for, then returns the exit status as an error when it
// is not zero.
func accountDeleteResult(output io.Writer, code int, line string) error {
	if _, err := fmt.Fprintf(output, "%s\n%s\n", line, accountDeleteEndLine); err != nil {
		return errors.New("account-delete result could not be written")
	}
	if code != 0 {
		return accountDeleteExitError{code: code}
	}
	return nil
}
