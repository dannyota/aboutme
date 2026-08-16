// Phase PA task 1's constraint, preflight, and down/up tests for the four
// tables added by migration 00008 (password_credentials,
// password_registrations, password_reset_tokens, auth_email_jobs). Like
// resume_schema_test.go, every insert here is raw parameterized SQL against a
// live goose-migrated database — no internal/store layer — because the point
// is proving the database itself enforces every invariant (D3), not that a Go
// pass happens to agree with it. Byte boundaries are exercised at limit and
// limit+1.
package migrations_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// passwordAuthRow helpers build exact-size byte values so boundary cases name
// their lengths instead of hand-counting.
func bytesOf(n int, fill byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = fill
	}
	return b
}

const (
	passwordAuthValidKeyID = "k-2026-08-16-a"
	passwordAuthNow        = "2026-08-16T00:00:00Z"
)

// requirePGError fails t unless err wraps a *pgconn.PgError with the given
// ConstraintName.
func requirePGError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want a %q violation", want)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("error = %v (%T), want a *pgconn.PgError", err, err)
	}
	if pgErr.ConstraintName != want {
		t.Errorf("violated constraint = %q, want %q", pgErr.ConstraintName, want)
	}
}

// insertTestUser returns a fresh users row id through the same raw-SQL path the
// other migration tests use.
func insertPasswordAuthUser(ctx context.Context, t *testing.T, db sqlExecer, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		email, "Password Auth Test User",
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	return id
}

func validPasswordAuthHash() []byte { return bytesOf(60, 'h') }

// validTokenDigest returns a fresh random 32-byte digest each call: the digest
// is globally unique in the schema, so a shared constant would collide across
// the parallel package tests that share the one migrated database.
func validTokenDigest() []byte { return []byte(uuid.NewString() + uuid.NewString())[:32] }
func validCiphertext() []byte  { return bytesOf(64, 'c') }
func validNonce() []byte       { return bytesOf(12, 'n') }

// ---------------------------------------------------------------------------
// Down/up restores the exact prior schema.
// ---------------------------------------------------------------------------

func TestPasswordAuthMigrationDownUp(t *testing.T) {
	t.Parallel()
	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("Up() error: %v", err)
	}
	if _, err := provider.DownTo(ctx, 7); err != nil {
		t.Fatalf("DownTo(7) error: %v", err)
	}

	for _, table := range []string{"password_credentials", "password_registrations", "password_reset_tokens", "auth_email_jobs"} {
		var relation *string
		if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.`+table+`')::text`).Scan(&relation); err != nil {
			t.Fatalf("probe %s after down: %v", table, err)
		}
		if relation != nil {
			t.Fatalf("%s relation after down = %q, want absent", table, *relation)
		}
	}

	if _, err := provider.UpTo(ctx, 8); err != nil {
		t.Fatalf("UpTo(8) error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Preflight: a noncanonical existing email aborts 00008 before any table or
// row change.
// ---------------------------------------------------------------------------

func TestPasswordAuthPreflightRejectsNoncanonicalEmail(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		email string
	}{
		{"uppercase", "Someone@Example.com"},
		{"non_ascii", "người@example.com"},
		{"overlong", string(bytesOf(255, 'a')) + "@example.com"},
		{"no_at", "not-an-email.example.com"},
		{"space", "someone @example.com"},
		{"empty_domain", "someone@"},
		{"no_domain_dot", "someone@localhost"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dsn := newTestDatabase(t)
			db := openTestDB(t, dsn)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			t.Cleanup(cancel)

			provider, err := migrations.NewProvider(db, migrations.FS)
			if err != nil {
				t.Fatalf("NewProvider() error: %v", err)
			}
			if _, err := provider.UpTo(ctx, 7); err != nil {
				t.Fatalf("UpTo(7) error: %v", err)
			}
			var userID uuid.UUID
			if err := db.QueryRowContext(ctx,
				`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
				tc.email, "Password Auth Test User",
			).Scan(&userID); err != nil {
				t.Fatalf("insert seeded user: %v", err)
			}

			if _, err := provider.Up(ctx); err == nil {
				t.Fatal("Up() error = nil, want preflight failure on a noncanonical email")
			}

			for _, table := range []string{"password_credentials", "password_registrations", "password_reset_tokens", "auth_email_jobs"} {
				var relation *string
				if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.`+table+`')::text`).Scan(&relation); err != nil {
					t.Fatalf("probe %s: %v", table, err)
				}
				if relation != nil {
					t.Fatalf("%s relation exists after failed preflight, want absent", table)
				}
			}

			var email string
			if err := db.QueryRowContext(ctx, `SELECT email::text FROM users WHERE id = $1`, userID).Scan(&email); err != nil {
				t.Fatalf("read seeded user: %v", err)
			}
			if email != tc.email {
				t.Fatalf("seeded user email = %q, want unchanged %q", email, tc.email)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// password_credentials constraint matrix.
// ---------------------------------------------------------------------------

func TestPasswordCredentialConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := insertPasswordAuthUser(ctx, t, tx, "cred-"+uuid.NewString()+"@example.com")

	insert := func(sp pgx.Tx, encodedHash []byte, changedAt, createdAt string) error {
		_, err := sp.Exec(ctx, `
			INSERT INTO password_credentials (user_id, encoded_hash, created_at, changed_at)
			VALUES ($1, $2, $3, $4)
		`, userID, encodedHash, createdAt, changedAt)
		return err
	}

	t.Run("empty hash", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, []byte{}, passwordAuthNow, passwordAuthNow)
		})
		requirePGError(t, err, "password_credentials_encoded_hash_length_check")
	})
	t.Run("hash over 192", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, bytesOf(193, 'h'), passwordAuthNow, passwordAuthNow)
		})
		requirePGError(t, err, "password_credentials_encoded_hash_length_check")
	})
	t.Run("changed before created", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, validPasswordAuthHash(), "2026-08-15T00:00:00Z", passwordAuthNow)
		})
		requirePGError(t, err, "password_credentials_changed_after_created_check")
	})
	t.Run("valid row", func(t *testing.T) {
		if err := insert(tx, validPasswordAuthHash(), passwordAuthNow, passwordAuthNow); err != nil {
			t.Fatalf("valid credential insert: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// password_registrations constraint matrix.
// ---------------------------------------------------------------------------

func TestPasswordRegistrationConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	now := time.Now().UTC()
	expires := now.Add(24 * time.Hour)
	insert := func(sp pgx.Tx, email string, name string, hash []byte, digest []byte, exp time.Time) error {
		_, err := sp.Exec(ctx, `
			INSERT INTO password_registrations (email, name, encoded_hash, token_digest, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, email, name, hash, digest, now, exp)
		return err
	}

	t.Run("name over 400", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, "reg-"+uuid.NewString()+"@example.com", string(bytesOf(401, 'n')), validPasswordAuthHash(), validTokenDigest(), expires)
		})
		requirePGError(t, err, "password_registrations_name_length_check")
	})
	t.Run("token digest not 32", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, "reg-"+uuid.NewString()+"@example.com", "Name", validPasswordAuthHash(), bytesOf(31, 'd'), expires)
		})
		requirePGError(t, err, "password_registrations_token_digest_length_check")
	})
	t.Run("expiry not exactly 24h", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, "reg-"+uuid.NewString()+"@example.com", "Name", validPasswordAuthHash(), validTokenDigest(), now.Add(23*time.Hour))
		})
		requirePGError(t, err, "password_registrations_expiry_check")
	})
	t.Run("duplicate email", func(t *testing.T) {
		email := "reg-dup-" + uuid.NewString() + "@example.com"
		if err := insert(tx, email, "Name", validPasswordAuthHash(), validTokenDigest(), expires); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, email, "Name2", validPasswordAuthHash(), validTokenDigest(), expires)
		})
		requirePGError(t, err, "password_registrations_email_key")
	})
}

// ---------------------------------------------------------------------------
// password_reset_tokens constraint matrix.
// ---------------------------------------------------------------------------

func TestPasswordResetTokenConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := insertPasswordAuthUser(ctx, t, tx, "reset-"+uuid.NewString()+"@example.com")

	now := time.Now().UTC()
	expires := now.Add(30 * time.Minute)
	insert := func(sp pgx.Tx, uid uuid.UUID, digest []byte, exp time.Time) error {
		_, err := sp.Exec(ctx, `
			INSERT INTO password_reset_tokens (user_id, token_digest, created_at, expires_at)
			VALUES ($1, $2, $3, $4)
		`, uid, digest, now, exp)
		return err
	}

	t.Run("token digest not 32", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, userID, bytesOf(31, 'd'), expires)
		})
		requirePGError(t, err, "password_reset_tokens_token_digest_length_check")
	})
	t.Run("expiry not exactly 30m", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, userID, validTokenDigest(), now.Add(29*time.Minute))
		})
		requirePGError(t, err, "password_reset_tokens_expiry_check")
	})
	t.Run("second token for same user", func(t *testing.T) {
		if err := insert(tx, userID, validTokenDigest(), expires); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, userID, validTokenDigest(), expires)
		})
		requirePGError(t, err, "password_reset_tokens_user_id_key")
	})
}

// ---------------------------------------------------------------------------
// auth_email_jobs: closed kind/state, scope, token requirement, and the exact
// state matrix.
// ---------------------------------------------------------------------------

func TestAuthEmailJobConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := insertPasswordAuthUser(ctx, t, tx, "job-"+uuid.NewString()+"@example.com")

	now := time.Now().UTC()
	expires := now.Add(24 * time.Hour)
	next := now.Add(time.Minute)

	var regID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO password_registrations (email, name, encoded_hash, token_digest, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, "reg-"+uuid.NewString()+"@example.com", "Name", validPasswordAuthHash(), validTokenDigest(), now, expires).Scan(&regID); err != nil {
		t.Fatalf("insert registration: %v", err)
	}

	// base builds a valid pending verify job; each case mutates one field.
	base := func(sp pgx.Tx, mutate func(m map[string]any)) error {
		cols := map[string]any{
			"kind": "verify", "state": "pending",
			"registration_id": regID, "reset_token_id": nil, "user_id": nil,
			"token_digest": validTokenDigest(), "key_id": passwordAuthValidKeyID,
			"nonce": validNonce(), "ciphertext": validCiphertext(), "attempts": 0,
			"created_at": now, "expires_at": expires, "next_attempt_at": next,
		}
		mutate(cols)
		_, err := sp.Exec(ctx, `
			INSERT INTO auth_email_jobs (
				kind, state, registration_id, reset_token_id, user_id, token_digest,
				key_id, nonce, ciphertext, attempts, created_at, expires_at, next_attempt_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		`,
			cols["kind"], cols["state"], cols["registration_id"], cols["reset_token_id"],
			cols["user_id"], cols["token_digest"], cols["key_id"], cols["nonce"],
			cols["ciphertext"], cols["attempts"], cols["created_at"], cols["expires_at"],
			cols["next_attempt_at"],
		)
		return err
	}

	t.Run("unknown kind", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return base(sp, func(m map[string]any) { m["kind"] = "bogus" })
		})
		requirePGError(t, err, "auth_email_jobs_kind_check")
	})
	t.Run("pending missing ciphertext", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return base(sp, func(m map[string]any) { m["ciphertext"] = nil })
		})
		requirePGError(t, err, "auth_email_jobs_state_matrix_check")
	})
	t.Run("verify without token digest", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return base(sp, func(m map[string]any) { m["token_digest"] = nil })
		})
		requirePGError(t, err, "auth_email_jobs_token_digest_required_check")
	})
	t.Run("verify with user scope", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return base(sp, func(m map[string]any) { m["user_id"] = userID })
		})
		requirePGError(t, err, "auth_email_jobs_scope_check")
	})
	t.Run("key_id too long", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return base(sp, func(m map[string]any) { m["key_id"] = string(bytesOf(65, 'k')) })
		})
		requirePGError(t, err, "auth_email_jobs_key_id_check")
	})
	t.Run("nonce not 12", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return base(sp, func(m map[string]any) { m["nonce"] = bytesOf(13, 'n') })
		})
		requirePGError(t, err, "auth_email_jobs_nonce_check")
	})
	t.Run("ciphertext over 4112", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return base(sp, func(m map[string]any) { m["ciphertext"] = bytesOf(4113, 'c') })
		})
		requirePGError(t, err, "auth_email_jobs_ciphertext_check")
	})
	t.Run("pending attempts 8", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return base(sp, func(m map[string]any) { m["attempts"] = 8 })
		})
		requirePGError(t, err, "auth_email_jobs_attempts_check")
	})
	t.Run("expiry over 24h", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return base(sp, func(m map[string]any) { m["expires_at"] = now.Add(25 * time.Hour) })
		})
		requirePGError(t, err, "auth_email_jobs_expiry_check")
	})
}
