package main

import (
	"context"
	"fmt"
	"os"
)

var linkedinProofEmails = []string{
	"li-verified@example.invalid",
	"li-unverified@example.invalid",
	"li-collision@example.invalid",
	"li-link@example.invalid",
}

const linkedinTestEmailPrefix = "lnkd-test-"

const linkedinSubjectPrefix = "lnkd-"

func runLinkedInCleanup(ctx context.Context, cfg Config) error {
	db, err := open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "password-auth-fixture: close database:", closeErr)
		}
	}()

	pattern := linkedinTestEmailPrefix + "%@example.invalid"
	if _, err := db.ExecContext(ctx,
		`DELETE FROM password_registrations WHERE email::text = ANY($1) OR email::text LIKE $2`,
		linkedinProofEmails, pattern); err != nil {
		return fmt.Errorf("delete LinkedIn proof registrations: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM identities WHERE provider = 'linkedin' AND provider_user_id LIKE $1`,
		linkedinSubjectPrefix+"%"); err != nil {
		return fmt.Errorf("delete LinkedIn proof identities: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM users WHERE email::text = ANY($1) OR email::text LIKE $2`,
		linkedinProofEmails, pattern); err != nil {
		return fmt.Errorf("delete LinkedIn proof users: %w", err)
	}
	return nil
}
