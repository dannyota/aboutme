package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestGetPublicRealtimeResumeTracksLiveSlugState(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	owner := createPublicStoreUser(ctx, t, tx)
	slug := "realtime-" + uuid.NewString()[:8]
	resumeID := createPublicStoreResume(ctx, t, tx, owner, &slug, false, false)

	if _, err := queries.GetPublicRealtimeResume(ctx, slug); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("private resume lookup error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE resumes SET live = true, revision = revision + 1 WHERE id = $1`, resumeID); err != nil {
		t.Fatal(err)
	}
	row, err := queries.GetPublicRealtimeResume(ctx, slug)
	if err != nil {
		t.Fatal(err)
	}
	if row != (store.GetPublicRealtimeResumeRow{ID: resumeID, Revision: 2}) {
		t.Fatalf("live lookup = %#v", row)
	}

	renamed := slug + "-new"
	if _, err := tx.Exec(ctx, `UPDATE resumes SET slug = $2, revision = revision + 1 WHERE id = $1`, resumeID, renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.GetPublicRealtimeResume(ctx, slug); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old slug after rename error = %v, want pgx.ErrNoRows", err)
	}
	if row, err := queries.GetPublicRealtimeResume(ctx, renamed); err != nil || row.Revision != 3 {
		t.Fatalf("renamed slug lookup = %#v, %v", row, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM resumes WHERE id = $1`, resumeID); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.GetPublicRealtimeResume(ctx, renamed); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted resume lookup error = %v, want pgx.ErrNoRows", err)
	}
}

func TestResumeNotificationsAreCommittedMetadataOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	listener, listenerErr := pgx.Connect(ctx, dsn)
	if listenerErr != nil {
		t.Fatal(listenerErr)
	}
	t.Cleanup(func() {
		if closeErr := listener.Close(context.Background()); closeErr != nil {
			t.Errorf("close listener connection: %v", closeErr)
		}
	})
	writer, writerErr := pgx.Connect(ctx, dsn)
	if writerErr != nil {
		t.Fatal(writerErr)
	}
	t.Cleanup(func() {
		if closeErr := writer.Close(context.Background()); closeErr != nil {
			t.Errorf("close writer connection: %v", closeErr)
		}
	})
	if _, err := listener.Exec(ctx, "LISTEN aboutme_resume_revision"); err != nil {
		t.Fatal(err)
	}
	tx, beginErr := writer.Begin(ctx)
	if beginErr != nil {
		t.Fatal(beginErr)
	}
	t.Cleanup(func() {
		if rollbackErr := tx.Rollback(context.Background()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback fixture transaction: %v", rollbackErr)
		}
	})
	owner := createPublicStoreUser(ctx, t, tx)
	resumeID := createPublicStoreResume(ctx, t, tx, owner, nil, false, false)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, cleanupErr := writer.Exec(context.Background(), "DELETE FROM users WHERE id = $1", owner)
		if cleanupErr != nil {
			t.Errorf("delete fixture: %v", cleanupErr)
		}
	}()
	assertRevisionNotification(ctx, t, listener, owner, resumeID, 1, false)

	if _, err := writer.Exec(ctx, "UPDATE resumes SET title = 'Changed private title', revision = revision + 1 WHERE id = $1", resumeID); err != nil {
		t.Fatal(err)
	}
	assertRevisionNotification(ctx, t, listener, owner, resumeID, 2, false)

	rollbackTx, rollbackBeginErr := writer.Begin(ctx)
	if rollbackBeginErr != nil {
		t.Fatal(rollbackBeginErr)
	}
	if _, err := rollbackTx.Exec(ctx, "UPDATE resumes SET revision = revision + 1 WHERE id = $1", resumeID); err != nil {
		t.Fatal(err)
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// A later committed marker proves the rolled-back transaction emitted no
	// notification without relying on a quiet timeout to establish absence.
	if _, err := writer.Exec(ctx, "SELECT pg_notify('aboutme_resume_revision', 'barrier')"); err != nil {
		t.Fatal(err)
	}
	notification, err := listener.WaitForNotification(ctx)
	if err != nil || notification.Payload != "barrier" {
		t.Fatalf("rollback emitted a notification before barrier: %v, %v", notification, err)
	}
	if _, err := writer.Exec(ctx, "DELETE FROM resumes WHERE id = $1", resumeID); err != nil {
		t.Fatal(err)
	}
	assertRevisionNotification(ctx, t, listener, owner, resumeID, 3, true)

	var ceilingResumeID uuid.UUID
	if err := writer.QueryRow(ctx, `
		INSERT INTO resumes (
			user_id, title, schema_version, personal_details, content, customization
		) VALUES ($1, 'Revision ceiling', 2, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb)
		RETURNING id
	`, owner).Scan(&ceilingResumeID); err != nil {
		t.Fatal(err)
	}
	assertRevisionNotification(ctx, t, listener, owner, ceilingResumeID, 1, false)
	const maxRevision int64 = 1<<63 - 1
	if _, err := writer.Exec(ctx, "UPDATE resumes SET revision = $2 WHERE id = $1", ceilingResumeID, maxRevision); err != nil {
		t.Fatal(err)
	}
	assertRevisionNotification(ctx, t, listener, owner, ceilingResumeID, maxRevision, false)
	if _, err := writer.Exec(ctx, "DELETE FROM resumes WHERE id = $1", ceilingResumeID); err != nil {
		t.Fatalf("delete resume at revision ceiling: %v", err)
	}
	assertRevisionNotification(ctx, t, listener, owner, ceilingResumeID, maxRevision, true)
}

func assertRevisionNotification(ctx context.Context, t *testing.T, conn *pgx.Conn, owner, resumeID uuid.UUID, revision int64, deleted bool) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	notification, err := conn.WaitForNotification(readCtx)
	if err != nil {
		t.Fatalf("missing committed resume notification: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(notification.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 4 {
		t.Fatalf("notification has non-metadata fields: %s", notification.Payload)
	}
	want := map[string]any{"account_id": owner, "resume_id": resumeID, "revision": revision, "deleted": deleted}
	for field, value := range want {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if string(payload[field]) != string(encoded) {
			t.Errorf("%s = %s, want %s", field, payload[field], encoded)
		}
	}
}
