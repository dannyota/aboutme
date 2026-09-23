package migrations_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

func totpMigrationContents(t *testing.T) string {
	t.Helper()
	contents, err := migrations.FS.ReadFile("00005_totp_second_factor.sql")
	if err != nil {
		t.Fatalf("read TOTP migration: %v", err)
	}
	return string(contents)
}

func TestTOTPMigrationDefinesRequiredRelations(t *testing.T) {
	t.Parallel()
	contents := totpMigrationContents(t)

	for _, fragment := range []string{
		"CREATE TABLE totp_credentials",
		"CREATE TABLE totp_enrollments",
		"ALTER TABLE second_factor_policies ADD COLUMN attempt_mail_at timestamptz NULL",
		"CREATE INDEX totp_credentials_key_id_idx ON totp_credentials (key_id)",
		"CREATE INDEX totp_enrollments_key_id_idx ON totp_enrollments (key_id)",
		"CREATE INDEX totp_enrollments_expires_id_idx ON totp_enrollments (expires_at, id)",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE totp_credentials, totp_enrollments TO aboutme_app",
	} {
		if !strings.Contains(contents, fragment) {
			t.Errorf("migration does not contain %q", fragment)
		}
	}
}

// TestTOTPMigrationEnforcesOneRowPerAccount pins the accepted contract's
// plain unique totp_enrollments.user_id (no partial index conditioned on
// expiry or a consumed marker) and confirms the table has no consumed_at
// column, since every enrollment exit deletes the row instead
// (docs/design/totp-second-factor-contract.md#postgresql-shape-and-bounds).
func TestTOTPMigrationEnforcesOneRowPerAccount(t *testing.T) {
	t.Parallel()
	contents := totpMigrationContents(t)

	for _, fragment := range []string{
		"user_id uuid NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE",
	} {
		if strings.Count(contents, fragment) != 2 {
			t.Errorf("migration contains %q %d times, want exactly 2 (credentials and enrollments)",
				fragment, strings.Count(contents, fragment))
		}
	}
	if strings.Contains(contents, "consumed_at") {
		t.Error("TOTP migration contains consumed_at; every enrollment exit deletes the row instead")
	}
	if strings.Contains(contents, "UNIQUE (user_id) WHERE") || strings.Contains(contents, "WHERE expires_at") {
		t.Error("TOTP migration contains a partial index; totp_enrollments.user_id must be a plain unique")
	}
}

func TestTOTPMigrationBindsKeyIdentifierShape(t *testing.T) {
	t.Parallel()
	contents := totpMigrationContents(t)

	for _, fragment := range []string{
		`octet_length(key_id) = 26 AND key_id ~ '^tk1_[A-Za-z0-9_-]{22}$'`,
		"octet_length(nonce) = 12",
		"octet_length(ciphertext) = 36",
		"format_version = 1",
	} {
		if strings.Count(contents, fragment) != 2 {
			t.Errorf("migration contains %q %d times, want exactly 2 (credentials and enrollments)",
				fragment, strings.Count(contents, fragment))
		}
	}
	if !strings.Contains(contents, "failed_attempts BETWEEN 0 AND 1000") {
		t.Error("migration does not bound failed_attempts to 0 through 1,000")
	}
	if !strings.Contains(contents, "expires_at = created_at + interval '10 minutes'") {
		t.Error("migration does not bind the ten-minute enrollment lifetime")
	}
}

func TestTOTPMigrationAdmitsOnlyUserScopedTOTPMail(t *testing.T) {
	t.Parallel()
	contents := totpMigrationContents(t)

	for _, kind := range []string{"totp_added", "totp_replaced", "totp_removed"} {
		if !strings.Contains(contents, kind) {
			t.Errorf("migration does not admit security mail kind %q", kind)
		}
	}
	for _, fragment := range []string{
		"DROP CONSTRAINT auth_email_jobs_kind_check",
		"DROP CONSTRAINT auth_email_jobs_scope_check",
		"'totp_added'::text, 'totp_replaced'::text, 'totp_removed'::text\n    ]) AND registration_id IS NULL AND reset_token_id IS NULL AND user_id IS NOT NULL",
	} {
		if !strings.Contains(contents, fragment) {
			t.Errorf("migration does not contain %q", fragment)
		}
	}
	// The token-digest rule from 00004 is untouched: TOTP kinds are not in
	// the verify/reset array, so it already requires token_digest IS NULL
	// for them without any change to that constraint.
	if strings.Contains(contents, "auth_email_jobs_token_digest_required_check") {
		t.Error("migration touches auth_email_jobs_token_digest_required_check, which the TOTP contract leaves unchanged")
	}
}

// TestTOTPMigrationDownRefusesWhileCredentialsExist pins the down section's
// exact order: the guard raises before either table is dropped, so a
// database holding a TOTP credential cannot lose it to a rollback
// (docs/design/totp-second-factor-contract.md#postgresql-shape-and-bounds).
func TestTOTPMigrationDownRefusesWhileCredentialsExist(t *testing.T) {
	t.Parallel()
	contents := totpMigrationContents(t)

	downIdx := strings.Index(contents, "-- +goose Down")
	if downIdx < 0 {
		t.Fatal("migration has no down section")
	}
	down := contents[downIdx:]

	guardIdx := strings.Index(down, "RAISE EXCEPTION")
	if guardIdx < 0 {
		t.Fatal("down section does not raise an exception")
	}
	if !strings.Contains(down, "SELECT 1 FROM totp_credentials") {
		t.Error("down guard does not check totp_credentials for existing rows")
	}
	dropEnrollmentsIdx := strings.Index(down, "DROP TABLE totp_enrollments")
	dropCredentialsIdx := strings.Index(down, "DROP TABLE totp_credentials")
	if dropEnrollmentsIdx < 0 || dropCredentialsIdx < 0 {
		t.Fatal("down section does not drop both TOTP tables")
	}
	if guardIdx > dropEnrollmentsIdx || guardIdx > dropCredentialsIdx {
		t.Error("down section drops a TOTP table before raising its existence guard")
	}

	deleteMailIdx := strings.Index(down, "DELETE FROM auth_email_jobs")
	if deleteMailIdx < 0 || deleteMailIdx < guardIdx {
		t.Error("down section does not delete the three TOTP mail kinds after the guard")
	}

	if !strings.Contains(down, "ALTER TABLE second_factor_policies DROP COLUMN attempt_mail_at") {
		t.Error("down section does not drop attempt_mail_at")
	}
	// Down restores the exact v0.4.2 (00004) constraint bodies: the TOTP
	// kinds must not survive into the restored kind_check.
	kindCheckIdx := strings.LastIndex(down, "auth_email_jobs_kind_check CHECK")
	if kindCheckIdx < 0 {
		t.Fatal("down section does not restore auth_email_jobs_kind_check")
	}
	restoredKindCheck := down[kindCheckIdx:]
	if strings.Contains(restoredKindCheck, "totp_added") {
		t.Error("down section's restored kind_check still admits a TOTP mail kind")
	}
}

// totpMigrationV042JobKinds are the ten mail kinds migration 00004 already
// admits, each scoped as the existing auth_email_jobs_scope_check requires:
// verify to a registration, reset to a reset token, and every other kind to
// a user.
var totpMigrationV042JobKinds = []string{
	"verify", "reset", "password_changed",
	"second_factor_enabled", "passkey_added", "passkey_removed",
	"second_factor_disabled", "recovery_codes_regenerated", "recovery_code_used",
	"second_factor_attempts_exhausted",
}

// totpMigrationTOTPJobKinds are the three mail kinds migration 00005 adds,
// each requiring exactly the same user-only scope as the second group of
// totpMigrationV042JobKinds
// (docs/design/totp-second-factor-contract.md#security-mail-and-locales).
var totpMigrationTOTPJobKinds = []string{"totp_added", "totp_replaced", "totp_removed"}

// totpMigrationValidKeyID returns a key identifier shaped exactly like the
// migration's tk1_ CHECK constraint requires: 26 bytes total.
func totpMigrationValidKeyID() string { return "tk1_" + string(bytesOf(22, 'k')) }

// insertTOTPMigrationJob inserts one pending auth_email_jobs row of kind,
// scoped by whichever of registrationID, resetTokenID, or userID is
// non-nil (verify to a registration, reset to a reset token, every other
// kind including the three TOTP additions to a user). Passing nil for the
// kind's required scope column, or a value for the wrong one, is how the
// scope_check and kind_check violation cases below are built.
func insertTOTPMigrationJob(ctx context.Context, t *testing.T, db *sql.DB, kind string, registrationID, resetTokenID, userID any, now, expires time.Time) error {
	t.Helper()
	var tokenDigest any
	if kind == "verify" || kind == "reset" {
		tokenDigest = validTokenDigest()
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO auth_email_jobs (
			kind, state, registration_id, reset_token_id, user_id, token_digest,
			key_id, nonce, ciphertext, attempts, created_at, expires_at, next_attempt_at
		) VALUES ($1, 'pending', $2, $3, $4, $5, $6, $7, $8, 0, $9, $10, $11)
	`, kind, registrationID, resetTokenID, userID, tokenDigest,
		passwordAuthValidKeyID, validNonce(), validCiphertext(), now, expires, now.Add(time.Minute))
	return err
}

func countAuthEmailJobsOfKind(ctx context.Context, t *testing.T, db *sql.DB, kind string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM auth_email_jobs WHERE kind = $1`, kind).Scan(&n); err != nil {
		t.Fatalf("count auth_email_jobs kind %q: %v", kind, err)
	}
	return n
}

func tableExists(ctx context.Context, t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1
	`, name).Scan(&n); err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return n == 1
}

func columnExists(ctx context.Context, t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
	`, table, column).Scan(&n); err != nil {
		t.Fatalf("check column %s.%s: %v", table, column, err)
	}
	return n == 1
}

// TestTOTPMigrationLiveUpDownRoundTrip proves the migration's up/down
// behavior against a real, freshly migrated database, not just the source
// text: version 5 admits every v0.4.2 mail kind and the three TOTP
// additions under the exact scope_check the contract requires; a live
// totp_credentials row refuses the down migration and leaves version 5
// untouched; removing that row lets the down migration delete only the
// TOTP jobs, restore the v0.4.2-only kind_check, and drop both TOTP tables
// and attempt_mail_at; and the schema then upgrades cleanly back to
// version 5
// (docs/design/totp-second-factor-contract.md#postgresql-shape-and-bounds).
func TestTOTPMigrationLiveUpDownRoundTrip(t *testing.T) {
	t.Parallel()

	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()

	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if _, err := provider.UpTo(ctx, 5); err != nil {
		t.Fatalf("UpTo(5) error: %v", err)
	}

	now := time.Now().UTC()
	expires := now.Add(24 * time.Hour)

	var userID uuid.UUID
	if err := db.QueryRowContext(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		"totp-migration-"+uuid.NewString()+"@example.com", "TOTP Migration Test User",
	).Scan(&userID); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	var registrationID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO password_registrations (email, name, encoded_hash, token_digest, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, "totp-migration-reg-"+uuid.NewString()+"@example.com", "Name", bytesOf(60, 'h'), validTokenDigest(), now, expires,
	).Scan(&registrationID); err != nil {
		t.Fatalf("insert test registration: %v", err)
	}
	var resetTokenID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO password_reset_tokens (user_id, token_digest, created_at, expires_at)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, userID, validTokenDigest(), now, now.Add(30*time.Minute)).Scan(&resetTokenID); err != nil {
		t.Fatalf("insert test reset token: %v", err)
	}

	for _, kind := range totpMigrationV042JobKinds {
		var regArg, resetArg, userArg any
		switch kind {
		case "verify":
			regArg = registrationID
		case "reset":
			resetArg = resetTokenID
		default:
			userArg = userID
		}
		if err := insertTOTPMigrationJob(ctx, t, db, kind, regArg, resetArg, userArg, now, expires); err != nil {
			t.Fatalf("insert v0.4.2 kind %q at version 5: %v", kind, err)
		}
	}
	for _, kind := range totpMigrationTOTPJobKinds {
		if err := insertTOTPMigrationJob(ctx, t, db, kind, nil, nil, userID, now, expires); err != nil {
			t.Fatalf("insert TOTP kind %q at version 5: %v", kind, err)
		}
	}

	if err := insertTOTPMigrationJob(ctx, t, db, "totp_added", registrationID, nil, nil, now, expires); err == nil {
		t.Error("totp_added with registration scope: want an error, got nil")
	} else {
		requirePGError(t, err, "auth_email_jobs_scope_check")
	}
	if err := insertTOTPMigrationJob(ctx, t, db, "totp_replaced", nil, resetTokenID, nil, now, expires); err == nil {
		t.Error("totp_replaced with reset scope: want an error, got nil")
	} else {
		requirePGError(t, err, "auth_email_jobs_scope_check")
	}
	if err := insertTOTPMigrationJob(ctx, t, db, "totp_removed", nil, nil, nil, now, expires); err == nil {
		t.Error("totp_removed with no user_id: want an error, got nil")
	} else {
		requirePGError(t, err, "auth_email_jobs_scope_check")
	}

	credentialID := uuid.Must(uuid.NewV7())
	if _, err := db.ExecContext(ctx, `
		INSERT INTO totp_credentials (id, user_id, key_id, nonce, ciphertext, format_version, last_used_step, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 1, 0, $6, $6)
	`, credentialID, userID, totpMigrationValidKeyID(), bytesOf(12, 'n'), bytesOf(36, 'c'), now); err != nil {
		t.Fatalf("insert totp_credentials row: %v", err)
	}

	if _, err := provider.DownTo(ctx, 4); err == nil {
		t.Fatal("DownTo(4) with a totp_credentials row present: want an error, got nil")
	}
	if version, versionErr := provider.GetDBVersion(ctx); versionErr != nil {
		t.Fatalf("GetDBVersion() after failed DownTo(4): %v", versionErr)
	} else if version != 5 {
		t.Fatalf("version after failed DownTo(4) = %d, want 5", version)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM totp_credentials WHERE id = $1`, credentialID); err != nil {
		t.Fatalf("delete totp_credentials row: %v", err)
	}
	if _, err := provider.DownTo(ctx, 4); err != nil {
		t.Fatalf("DownTo(4) after removing the credential row: %v", err)
	}
	if version, versionErr := provider.GetDBVersion(ctx); versionErr != nil {
		t.Fatalf("GetDBVersion() after DownTo(4): %v", versionErr)
	} else if version != 4 {
		t.Fatalf("version after DownTo(4) = %d, want 4", version)
	}

	for _, kind := range totpMigrationTOTPJobKinds {
		if n := countAuthEmailJobsOfKind(ctx, t, db, kind); n != 0 {
			t.Errorf("auth_email_jobs kind %q count after DownTo(4) = %d, want 0", kind, n)
		}
	}
	for _, kind := range totpMigrationV042JobKinds {
		if n := countAuthEmailJobsOfKind(ctx, t, db, kind); n != 1 {
			t.Errorf("auth_email_jobs kind %q count after DownTo(4) = %d, want 1 (v0.4.2 jobs survive)", kind, n)
		}
	}
	if tableExists(ctx, t, db, "totp_credentials") {
		t.Error("totp_credentials table survives DownTo(4)")
	}
	if tableExists(ctx, t, db, "totp_enrollments") {
		t.Error("totp_enrollments table survives DownTo(4)")
	}
	if columnExists(ctx, t, db, "second_factor_policies", "attempt_mail_at") {
		t.Error("second_factor_policies.attempt_mail_at survives DownTo(4)")
	}

	if err := insertTOTPMigrationJob(ctx, t, db, "totp_added", nil, nil, userID, now, expires); err == nil {
		t.Error("insert TOTP kind at version 4: want an error, got nil")
	} else {
		requirePGError(t, err, "auth_email_jobs_kind_check")
	}

	if _, err := provider.UpTo(ctx, 5); err != nil {
		t.Fatalf("UpTo(5) after DownTo(4): %v", err)
	}
	if version, versionErr := provider.GetDBVersion(ctx); versionErr != nil {
		t.Fatalf("GetDBVersion() after re-UpTo(5): %v", versionErr)
	} else if version != 5 {
		t.Fatalf("version after re-UpTo(5) = %d, want 5", version)
	}
}
