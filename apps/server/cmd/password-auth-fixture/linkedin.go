package main

import (
	"context"
	"fmt"
	"os"
)

// linkedinProofEmails are the fixed addresses the local LinkedIn mock issues
// (internal/uatmock linkedinAccounts). Only the LinkedIn browser proof creates
// accounts with them.
var linkedinProofEmails = []string{
	"li-verified@example.invalid",
	"li-unverified@example.invalid",
	"li-collision@example.invalid",
	"li-link@example.invalid",
}

// linkedinTestEmailPrefix reserves the runtime-random password accounts the
// LinkedIn browser proof creates for linking.
const linkedinTestEmailPrefix = "lnkd-test-"

// linkedinSubjectPrefix matches every subject the local LinkedIn mock issues.
const linkedinSubjectPrefix = "lnkd-"

// runLinkedInCleanup removes every row the LinkedIn browser proof can leave
// behind, so each run starts from the same state: the fixed mock emails, the
// reserved runtime-random accounts, their pending registrations, and any
// identity that holds a mock subject. It is idempotent.
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
