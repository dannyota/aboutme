package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

func TestCIMDMetadataURLConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	insert := func(db sqlExecer, metadataURL *string) error {
		_, err := db.Exec(ctx, `INSERT INTO oauth_clients
			(client_name, redirect_uris, created_at, last_used_at, metadata_url)
			VALUES ('Fixture', $1, $2, $2, $3)`, `["`+oauthRedirectURI+`"]`, oauthNow, metadataURL)
		return err
	}
	for _, raw := range []string{"https://client.example/" + uuid.NewString(), "https://client.example/" + strings.Repeat("a", 234)} {
		if err := insert(tx, &raw); err != nil {
			t.Fatal(err)
		}
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, &raw) })
		requirePGError(t, err, "oauth_clients_metadata_url_idx")
		upper := strings.Replace(raw, "/client.example/", "/CLIENT.example/", 1)
		if err := insert(tx, &upper); err != nil {
			t.Fatal("bytewise URL equality changed", err)
		}
	}
	for _, raw := range []string{"", "12345678", strings.Repeat("x", 257)} {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, &raw) })
		requirePGError(t, err, "oauth_clients_metadata_url_length_check")
	}
	if err := insert(tx, nil); err != nil {
		t.Fatal(err)
	}
	if err := insert(tx, nil); err != nil {
		t.Fatal("DCR null identities collided", err)
	}
}

func TestCIMDMigrationPreservesDCRAndRefusesDestructiveDown(t *testing.T) {
	t.Parallel()
	db := openTestDB(t, newTestDatabase(t))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 11); err != nil {
		t.Fatal(err)
	}
	var clientID uuid.UUID
	if err := db.QueryRowContext(ctx, `INSERT INTO oauth_clients (client_name,redirect_uris,created_at,last_used_at)
		VALUES ('Existing', $1, $2, $2) RETURNING id`, `["`+oauthRedirectURI+`"]`, oauthNow).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var unchanged bool
	if err := db.QueryRowContext(ctx, `SELECT metadata_url IS NULL AND client_name='Existing' FROM oauth_clients WHERE id=$1`, clientID).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("DCR row changed: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE oauth_clients SET metadata_url='https://client.example/metadata' WHERE id=$1`, clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err == nil {
		t.Fatal("destructive down accepted a URL client")
	}
	var present bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM oauth_clients WHERE metadata_url IS NOT NULL)`).Scan(&present); err != nil || !present {
		t.Fatalf("failed down lost identity: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM oauth_clients WHERE id=$1`, clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCIMDClientRetainsUUIDForeignKeys(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	clientID := insertOAuthClient(ctx, t, tx)
	if _, err := tx.Exec(ctx, `UPDATE oauth_clients SET metadata_url=$1 WHERE id=$2`, "https://client.example/"+uuid.NewString(), clientID); err != nil {
		t.Fatal(err)
	}
	var userID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO users (email,name) VALUES ($1,'Fixture') RETURNING id`, uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	grantID := insertOAuthGrant(ctx, t, tx, userID, clientID)
	tokenID := insertOAuthTokenReturningID(ctx, t, tx, defaultOAuthTokenRow(clientID, userID, grantID))
	var stored uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT client_id FROM oauth_tokens WHERE id=$1`, tokenID).Scan(&stored); err != nil || stored != clientID {
		t.Fatalf("UUID token binding changed: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM oauth_clients WHERE id=$1`, clientID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM oauth_tokens WHERE id=$1`, tokenID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade failed: %v", err)
	}
}
