package resume_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	schema "danny.vn/aboutme/packages/schema/gen/go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

func TestSaveDocumentCheckpointsFirstRestorableWrite(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	owner := createTestUser(t, q)
	before := validDocForTest(t)
	before.PersonalDetails.FullName = new("First name")
	created, err := s.Create(ctx, owner, "First title", before)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		var n int
		if countErr := pool.QueryRow(ctx, `SELECT count(*) FROM resume_checkpoints WHERE resume_id = $1`, created.ID).Scan(&n); countErr != nil {
			t.Fatal(countErr)
		}
		return n
	}
	if count() != 0 {
		t.Fatal("create started a checkpoint session")
	}
	revision, err := s.SaveDocument(ctx, owner, created.ID, before, created.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if count() != 0 {
		t.Fatal("identical write started a checkpoint session")
	}
	withPhoto := before
	withPhoto.PersonalDetails.Photo = &schema.Photo{Key: seamPhotoKey(created.ID)}
	revision, err = s.SaveDocument(ctx, owner, created.ID, withPhoto, revision)
	if err != nil {
		t.Fatal(err)
	}
	if count() != 0 {
		t.Fatal("photo-only write started a checkpoint session")
	}
	listener, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Release()
	if _, err = listener.Exec(ctx, "LISTEN aboutme_resume_revision"); err != nil {
		t.Fatal(err)
	}
	after := withPhoto
	after.PersonalDetails.FullName = new("Second name")
	revision, err = s.SaveDocument(ctx, owner, created.ID, after, revision)
	if err != nil {
		t.Fatal(err)
	}
	marker := uuid.NewString()
	if _, err = pool.Exec(ctx, "SELECT pg_notify('aboutme_resume_revision', $1)", marker); err != nil {
		t.Fatal(err)
	}
	var notifications int
	for {
		event, waitErr := listener.Conn().WaitForNotification(ctx)
		if waitErr != nil {
			t.Fatal(waitErr)
		}
		if event.Payload == marker {
			break
		}
		var body struct {
			ResumeID uuid.UUID `json:"resume_id"`
		}
		if json.Unmarshal([]byte(event.Payload), &body) == nil && body.ResumeID == created.ID {
			notifications++
		}
	}
	if notifications != 1 {
		t.Fatalf("first checkpoint write emitted %d resume notifications, want 1", notifications)
	}
	if _, err = listener.Exec(ctx, "UNLISTEN *"); err != nil {
		t.Fatal(err)
	}
	var title, name, cause, actor string
	var hasPhoto bool
	if err = pool.QueryRow(ctx, `SELECT c.title, c.personal_details->>'fullName', c.cause, r.session_actor, c.personal_details ? 'photo'
		FROM resume_checkpoints c JOIN resumes r ON r.id = c.resume_id WHERE c.resume_id = $1`, created.ID).
		Scan(&title, &name, &cause, &actor, &hasPhoto); err != nil {
		t.Fatalf("first restorable write did not checkpoint prior state: %v", err)
	}
	if title != "First title" || name != "First name" || cause != "edit" || actor != "owner" || hasPhoto {
		t.Fatalf("checkpoint was not the owner's prior state without the photo: title=%q name=%q cause=%q actor=%q hasPhoto=%v", title, name, cause, actor, hasPhoto)
	}
	after.PersonalDetails.FullName = new("Third name")
	if _, err = s.SaveDocument(ctx, owner, created.ID, after, revision); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("two writes in one session stored %d checkpoints, want 1", n)
	}
}

func TestCheckpointSessionBoundaries(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                                  string
		age, idle                             time.Duration
		initial, assistant, force, wantInsert bool
	}{
		{name: "first write", initial: true, wantInsert: true},
		{name: "exactly ten minutes idle", age: time.Minute * 20, idle: time.Minute * 10},
		{name: "more than ten minutes idle", age: time.Minute * 20, idle: time.Minute*10 + time.Microsecond, wantInsert: true},
		{name: "exactly one hour old", age: time.Hour, idle: time.Minute},
		{name: "more than an hour old", age: time.Hour + time.Microsecond, idle: time.Minute, wantInsert: true},
		{name: "different actor", age: time.Minute, idle: time.Minute, assistant: true, wantInsert: true},
		{name: "force despite identical state", age: time.Minute, idle: time.Minute, force: true, wantInsert: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, q, pool, ctx := newIntegrationStore(t)
			owner := createTestUser(t, q)
			doc := validDocForTest(t)
			created, err := s.Create(ctx, owner, "Before", doc)
			if err != nil {
				t.Fatal(err)
			}
			tx, qtx := beginSeamTx(ctx, t, pool)
			if _, err = qtx.LockUserForResumeWrite(ctx, owner); err != nil {
				t.Fatal(err)
			}
			before := resume.CheckpointState{Title: created.Title, SchemaVersion: int32(created.Doc.SchemaVersion), Document: resume.CheckpointDocument{
				PersonalDetails: created.Doc.PersonalDetails, Content: created.Doc.Content, Customization: created.Doc.Customization,
			}}
			after := before
			write := resume.CheckpointWrite{Force: tc.force}
			if tc.force {
				write.Cause = "restore"
			} else {
				after.Title = "After"
			}
			session := resume.CheckpointSession{}
			if !tc.initial {
				session = resume.CheckpointSession{Actor: new("owner"), StartedAt: new(now.Add(-tc.age)), LastWriteAt: new(now.Add(-tc.idle))}
			}
			expectedActor := "owner"
			if tc.assistant {
				grant := checkpointGrant(ctx, t, qtx, owner, now)
				write.GrantID = &grant
				expectedActor = "grant:" + grant.String()
			}
			got, err := resume.CheckpointTx(ctx, qtx, resume.CheckpointInput{ResumeID: created.ID, UserID: owner, Write: write, Before: before, After: after, Session: session, Now: now})
			if err != nil {
				t.Fatal(err)
			}
			wantStart := now.Add(-tc.age)
			wantCount := 0
			if tc.wantInsert {
				wantStart = now
				wantCount = 1
			}
			if got.Actor == nil || *got.Actor != expectedActor || got.StartedAt == nil || !got.StartedAt.Equal(wantStart) || got.LastWriteAt == nil || !got.LastWriteAt.Equal(now) {
				t.Fatalf("bad session: %+v", got)
			}
			var count int
			if err = tx.QueryRow(ctx, "SELECT count(*) FROM resume_checkpoints WHERE resume_id=$1", created.ID).Scan(&count); err != nil || count != wantCount {
				t.Fatalf("stored %d checkpoints, want %d: %v", count, wantCount, err)
			}
			if tc.assistant {
				if _, err = tx.Exec(ctx, "DELETE FROM oauth_grants WHERE id=$1", *write.GrantID); err != nil {
					t.Fatal(err)
				}
				var name string
				var grantGone bool
				if err = tx.QueryRow(ctx, "SELECT actor_name,actor_grant_id IS NULL FROM resume_checkpoints WHERE resume_id=$1", created.ID).Scan(&name, &grantGone); err != nil || !grantGone || name != "History fixture" {
					t.Fatalf("actor after grant deletion: name=%q grantGone=%v error=%v", name, grantGone, err)
				}
			}
		})
	}
}

func checkpointGrant(ctx context.Context, t *testing.T, q *store.Queries, owner uuid.UUID, now time.Time) uuid.UUID {
	t.Helper()
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "History fixture", RedirectURIs: []byte(`["https://example.test/callback"]`), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: owner, ClientID: client.ID, Scopes: "resumes:write", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return grant.ID
}

func TestCheckpointPrunesOldestByCountAndBytes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		seed, size, want int
	}{
		{"count", 50, 100, 50},
		{"bytes", 4, 524_288, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, q, pool, ctx := newIntegrationStore(t)
			owner := createTestUser(t, q)
			doc := validDocForTest(t)
			created, err := s.Create(ctx, owner, "Before", doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `INSERT INTO resume_checkpoints
     (resume_id, started_at, cause, title, schema_version, personal_details, content, customization, byte_size)
     SELECT id, '2026-01-01'::timestamptz + n * interval '1 second', 'edit', n::text,
       schema_version, personal_details - 'photo', content, customization, $3
     FROM resumes CROSS JOIN generate_series(1,$2::int) AS n WHERE id=$1`, created.ID, tc.seed, tc.size); err != nil {
				t.Fatal(err)
			}
			doc.PersonalDetails.FullName = new("Change")
			if _, err = s.SaveDocument(ctx, owner, created.ID, doc, created.Revision); err != nil {
				t.Fatal(err)
			}
			var count int
			var bytes int64
			var oldest string
			if err = pool.QueryRow(ctx, `SELECT count(*), sum(byte_size), (SELECT title FROM resume_checkpoints WHERE resume_id=$1 ORDER BY started_at,id LIMIT 1)
     FROM resume_checkpoints WHERE resume_id=$1`, created.ID).Scan(&count, &bytes, &oldest); err != nil || count != tc.want || bytes > 2_097_152 || oldest != "2" {
				t.Fatalf("count=%d bytes=%d oldest=%q error=%v", count, bytes, oldest, err)
			}
		})
	}
}

func TestCheckpointOwnerAndAssistantRace(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	owner := createTestUser(t, q)
	grant := checkpointGrant(ctx, t, q, owner, time.Now())
	created, err := s.Create(ctx, owner, "Before", validDocForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, write := range []resume.CheckpointWrite{{}, {GrantID: &grant}} {
		go func() {
			<-start
			doc := created.Doc
			doc.PersonalDetails.FullName = new("Racing change")
			results <- pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				_, saveErr := s.SaveDocumentTx(ctx, q.WithTx(tx), owner, created.ID, doc, created.Revision, write)
				return saveErr
			})
		}()
	}
	close(start)
	var wins int
	for range 2 {
		if saveErr := <-results; saveErr == nil {
			wins++
		} else {
			var stale *resume.RevisionMismatchError
			if !errors.As(saveErr, &stale) {
				t.Fatal(saveErr)
			}
		}
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM resume_checkpoints WHERE resume_id=$1", created.ID).Scan(&count); err != nil || count != 1 || wins != 1 {
		t.Fatalf("racing writes produced %d winners and %d checkpoints: %v", wins, count, err)
	}
}
