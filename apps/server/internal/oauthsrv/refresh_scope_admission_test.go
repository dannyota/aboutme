package oauthsrv

import (
	"testing"

	"danny.vn/aboutme/apps/server/internal/testutil"
)

func TestTokenGrantRefusalHidesUnparseableStoredGrantScopes(t *testing.T) {
	for _, scopes := range []string{"resumes:admin", "resumes:write resumes:read"} {
		t.Run(scopes, func(t *testing.T) {
			dsn, _ := testutil.NewMigratedTestDatabase(t)
			t.Setenv("TEST_DATABASE_URL", dsn)
			f := newTokenRefusalFixture(t, "dcr", "refresh", false)
			if _, err := f.pool.Exec(t.Context(), `ALTER TABLE oauth_grants DROP CONSTRAINT oauth_grants_scopes_check`); err != nil {
				t.Fatalf("drop the grant scope check in this test's database: %v", err)
			}
			if _, err := f.pool.Exec(t.Context(), `UPDATE oauth_grants SET scopes = $1 WHERE id = $2`, scopes, f.grantID); err != nil {
				t.Fatalf("store unparseable grant scopes: %v", err)
			}
			before := f.snapshot(t)

			admitted := f.post(t, f.form)
			assertClosedInvalidGrant(t, admitted)
			if f.snapshot(t) != before {
				t.Fatal("admitted refresh with unparseable stored scopes rotated or revoked tokens")
			}

			f.exhaust(t)
			refused := f.post(t, f.form)
			if refused.Code != admitted.Code || refused.Body.String() != admitted.Body.String() {
				t.Fatalf("refused response = %d %q, admitted response = %d %q; want identical", refused.Code, refused.Body.String(), admitted.Code, admitted.Body.String())
			}
			assertSameTokenResponse(t, refused, admitted)
			assertClosedInvalidGrant(t, refused)
			assertSameTokenResponse(t, refused, f.post(t, f.unknownForm(f.form)))
			if f.snapshot(t) != before {
				t.Fatal("refused refresh with unparseable stored scopes changed token state")
			}
		})
	}
}
