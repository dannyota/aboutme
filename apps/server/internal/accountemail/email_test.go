package accountemail_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// validEmailCorpus is the canonical-email corpus shared with the migration
// preflight fixtures: every entry must round-trip through Canonicalize to its
// own lowercase form.
var validEmailCorpus = []string{
	"a@b.co",
	"user@example.com",
	"first.last@example.com",
	"under_score@example.com",
	"tag+mail@example.com",
	"abc@sub.domain.example.com",
	"ABC@Example.COM",
	"a1!b#c$d%e&f'g*h+i-j/k=l?m^n_o`p{q|r}s~t@example.com",
	"john.doe+tag@example-domain.co.uk",
	strings.Repeat("a", 64) + "@example.com",
	"a@" + strings.Repeat("b", 63) + ".com",
	"a@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 63) + "." + strings.Repeat("e", 60),
}

func TestCanonicalizeValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"minimum", "a@b.co", "a@b.co"},
		{"simple", "user@example.com", "user@example.com"},
		{"local dots", "first.last@example.com", "first.last@example.com"},
		{"local underscore", "under_score@example.com", "under_score@example.com"},
		{"local plus", "tag+mail@example.com", "tag+mail@example.com"},
		{"multi label domain", "abc@sub.domain.example.com", "abc@sub.domain.example.com"},
		{"lowercase output", "ABC@Example.COM", "abc@example.com"},
		{"all local atoms", "a1!b#c$d%e&f'g*h+i-j/k=l?m^n_o`p{q|r}s~t@example.com", "a1!b#c$d%e&f'g*h+i-j/k=l?m^n_o`p{q|r}s~t@example.com"},
		{"domain hyphen", "john.doe+tag@example-domain.co.uk", "john.doe+tag@example-domain.co.uk"},
		{"local 64 bytes", strings.Repeat("a", 64) + "@example.com", strings.Repeat("a", 64) + "@example.com"},
		{"domain label 63 bytes", "a@" + strings.Repeat("b", 63) + ".com", "a@" + strings.Repeat("b", 63) + ".com"},
		{"total 254 bytes", "a@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 63) + "." + strings.Repeat("e", 60), "a@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 63) + "." + strings.Repeat("e", 60)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := accountemail.Canonicalize(tc.raw)
			if err != nil {
				t.Fatalf("Canonicalize(%q) error = %v, want nil", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("Canonicalize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestCanonicalizeInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"too short", "a@b."},
		{"too long", strings.Repeat("a", 255) + "@example.com"},
		{"non ascii", "người@example.com"},
		{"control", "a@b.c\n"},
		{"leading space", " user@example.com"},
		{"trailing space", "user@example.com "},
		{"interior space", "us er@example.com"},
		{"quoted local", "\"user\"@example.com"},
		{"comment", "user(comment)@example.com"},
		{"domain literal", "user@[127.0.0.1]"},
		{"empty local", "@example.com"},
		{"empty domain", "user@"},
		{"empty domain label", "user@example..com"},
		{"leading domain dot", "user@.example.com"},
		{"trailing domain dot", "user@example.com."},
		{"leading local dot", ".user@example.com"},
		{"trailing local dot", "user.@example.com"},
		{"consecutive local dots", "user..name@example.com"},
		{"leading domain hyphen", "user@-example.com"},
		{"trailing domain hyphen", "user@example-.com"},
		{"domain label 64 bytes", "a@" + strings.Repeat("b", 64) + ".com"},
		{"local 65 bytes", strings.Repeat("a", 65) + "@example.com"},
		{"domain no dot", "user@localhost"},
		{"domain underscore", "user@exa_mple.com"},
		{"domain punctuation", "user@example!.com"},
		{"two at signs", "user@name@example.com"},
		{"local comma", "user,name@example.com"},
		{"local colon", "user:name@example.com"},
		{"local backslash", "user\\name@example.com"},
		{"del char", "user@example.com\x7f"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := accountemail.Canonicalize(tc.raw)
			if err == nil {
				t.Fatalf("Canonicalize(%q) = %q, want ErrEmailInvalid", tc.raw, got)
			}
			if !errors.Is(err, accountemail.ErrEmailInvalid) {
				t.Errorf("Canonicalize(%q) error = %v, want ErrEmailInvalid", tc.raw, err)
			}
		})
	}
}

// TestCanonicalizeLiveEmails reads every stored email through the exact Go
// parser (D1's last paragraph). It is skipped unless TEST_DATABASE_URL is set;
// the corpus above is seeded so the check is non-vacuous even on a cold
// database.
func TestCanonicalizeLiveEmails(t *testing.T) {
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Logf("close database: %v", closeErr)
		}
	})

	ctx := context.Background()
	for _, email := range validEmailCorpus {
		if _, err = db.ExecContext(
			ctx,
			`INSERT INTO users (email, name) VALUES ($1, $2) ON CONFLICT (email) DO NOTHING`,
			email, "live email corpus",
		); err != nil {
			t.Fatalf("seed corpus email %q: %v", email, err)
		}
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT email::text FROM users UNION SELECT email::text FROM password_registrations`,
	)
	if err != nil {
		t.Fatalf("query stored emails: %v", err)
	}
	defer rows.Close() //nolint:errcheck // nothing meaningful to do with a rows close error in a test

	count := 0
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			t.Fatalf("scan stored email: %v", err)
		}
		count++
		canon, err := accountemail.Canonicalize(email)
		if err != nil {
			t.Errorf("stored email %q fails Canonicalize: %v", email, err)
			continue
		}
		if canon != strings.ToLower(email) {
			t.Errorf("stored email %q canonicalized to %q, want %q", email, canon, strings.ToLower(email))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stored emails: %v", err)
	}
	if count < len(validEmailCorpus) {
		t.Errorf("read %d stored emails, want at least %d", count, len(validEmailCorpus))
	}
}
