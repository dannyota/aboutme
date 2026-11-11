package migrations_test

import "testing"

// unindexedForeignKeys names each foreign key that no index leads, with the
// reason a scan of the referencing table is acceptable.
var unindexedForeignKeys = map[string]string{
	"oauth_authorization_codes_grant_id_user_client_fkey": "grants are deleted only by the user or client cascade, " +
		"which also removes these codes through their indexed user_id and client_id keys; " +
		"the grant cascade can use the user_id index too, and codes expire 60 seconds after creation",
	"pending_authentications_session_id_fkey": "sessions are deleted only by the account cascade; " +
		"rows expire 5 minutes after creation and are swept before each new pending sign-in or ceremony",
	"totp_enrollments_session_id_fkey": "sessions are deleted only by the account cascade; " +
		"at most one row per user, expiring 10 minutes after creation and swept before each enrollment start",
	"webauthn_ceremonies_pending_authentication_id_fkey": "the table holds minutes of rows: " +
		"they expire 5 minutes after creation and are swept before each new pending sign-in or ceremony",
	"webauthn_ceremonies_session_id_fkey": "sessions are deleted only by the account cascade; " +
		"rows expire 5 minutes after creation and are swept before each new pending sign-in or ceremony",
}

// uncoveredForeignKeysQuery lists foreign keys in public whose referencing
// columns, in any order, are not the leading key columns of a valid index. A
// partial index counts only when its predicate is "column IS NOT NULL" on a
// single-column key, because a cascade or lookup never matches a NULL.
const uncoveredForeignKeysQuery = `
SELECT c.conname, c.conrelid::regclass::text
FROM pg_constraint AS c
WHERE c.contype = 'f'
  AND c.connamespace = 'public'::regnamespace
  AND NOT EXISTS (
      SELECT 1
      FROM pg_index AS i
      CROSS JOIN LATERAL (
          SELECT (string_to_array(i.indkey::text, ' ')::int2[])[1:cardinality(c.conkey)] AS lead_keys
      ) AS k
      WHERE i.indrelid = c.conrelid
        AND i.indisvalid
        AND i.indnkeyatts >= cardinality(c.conkey)
        AND k.lead_keys @> c.conkey
        AND k.lead_keys <@ c.conkey
        AND (
            i.indpred IS NULL
            OR (cardinality(c.conkey) = 1
                AND pg_get_expr(i.indpred, i.indrelid) = format('(%I IS NOT NULL)',
                    (SELECT a.attname FROM pg_attribute AS a
                     WHERE a.attrelid = c.conrelid AND a.attnum = c.conkey[1])))
        )
  )
ORDER BY c.conname`

// TestForeignKeys_LeadingIndexCoversReferencingColumns fails for a foreign key
// that no index leads and that has no exemption: ON DELETE CASCADE, SET NULL,
// and lookups by its referencing columns would scan the whole child table.
func TestForeignKeys_LeadingIndexCoversReferencingColumns(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	rows, err := tx.Query(ctx, uncoveredForeignKeysQuery)
	if err != nil {
		t.Fatalf("query uncovered foreign keys: %v", err)
	}
	defer rows.Close()
	uncovered := map[string]bool{}
	for rows.Next() {
		var name, table string
		if err := rows.Scan(&name, &table); err != nil {
			t.Fatalf("scan foreign key: %v", err)
		}
		uncovered[name] = true
		if _, exempt := unindexedForeignKeys[name]; !exempt {
			t.Errorf("foreign key %s on %s has no index led by its referencing columns", name, table)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign keys: %v", err)
	}
	for name, reason := range unindexedForeignKeys {
		if !uncovered[name] {
			t.Errorf("exemption %s (%s) is stale: the key is indexed or gone; remove the exemption", name, reason)
		}
	}
}
