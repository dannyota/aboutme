package privacynotice

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func noticeTestService(t *testing.T) (context.Context, *Service) {
	t.Helper()
	dsn, _ := testutil.NewMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal("test pool failed")
	}
	t.Cleanup(func() {
		cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelCleanup()
		pool.Close(cleanup)
	})
	return ctx, &Service{pool: pool, now: func() time.Time { return time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC) }}
}

func TestPrepareCapturesExactCohortAndResumes(t *testing.T) {
	ctx, s := noticeTestService(t)
	if _, err := s.pool.Exec(ctx, `INSERT INTO users(email,name) SELECT 'fixture-'||n||'@example.com','Fixture' FROM generate_series(1,205) AS n`); err != nil {
		t.Fatal("fixture users failed")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO password_registrations(email,name,encoded_hash,token_digest,created_at,expires_at) VALUES ('pending@example.com','Fixture',$1,$2,now(),now()+interval '24 hours')`, []byte("fixture"), make([]byte, 32)); err != nil {
		t.Fatal("pending fixture failed")
	}
	for batch, want := range []int64{100, 200, 205} {
		if err := s.Prepare(ctx); err != nil {
			t.Fatal("prepare failed")
		}
		var actual int64
		if s.pool.QueryRow(ctx, `SELECT count(*) FROM privacy_notice_recipients`).Scan(&actual) != nil || actual != want {
			t.Fatal("capture batch bound or membership differs")
		}
		var state string
		if s.pool.QueryRow(ctx, `SELECT state FROM privacy_notice_campaigns`).Scan(&state) != nil {
			t.Fatal("campaign read failed")
		}
		if batch < 2 && state != "capturing" {
			t.Fatal("partial cohort sealed")
		}
	}
	var exact bool
	if s.pool.QueryRow(ctx, `SELECT (SELECT expected_count=205 AND state='sealed' FROM privacy_notice_campaigns) AND NOT EXISTS(SELECT id FROM users EXCEPT SELECT user_id FROM privacy_notice_recipients) AND NOT EXISTS(SELECT user_id FROM privacy_notice_recipients EXCEPT SELECT id FROM users)`).Scan(&exact) != nil || !exact {
		t.Fatal("cohort proof differs")
	}
	var cutoff time.Time
	if s.pool.QueryRow(ctx, `SELECT cutoff_at FROM privacy_notice_campaigns`).Scan(&cutoff) != nil {
		t.Fatal("cutoff read failed")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO users(email,name) VALUES ('later@example.com','Fixture')`); err != nil {
		t.Fatal("later user failed")
	}
	if s.Prepare(ctx) != nil {
		t.Fatal("sealed campaign retry failed")
	}
	var unchanged bool
	if s.pool.QueryRow(ctx, `SELECT cutoff_at=$1 AND expected_count=205 FROM privacy_notice_campaigns`, cutoff).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("capture cutoff changed")
	}
}

func TestPrepareZeroCohortSeals(t *testing.T) {
	ctx, s := noticeTestService(t)
	if err := s.Prepare(ctx); err != nil {
		t.Fatal("empty capture failed")
	}
	var sealed bool
	if s.pool.QueryRow(ctx, `SELECT state='sealed' AND expected_count=0 AND sealed_at IS NOT NULL FROM privacy_notice_campaigns`).Scan(&sealed) != nil || !sealed {
		t.Fatal("empty cohort unproven")
	}
}

func TestPrepareRefusesChangedMembershipWithSameCount(t *testing.T) {
	ctx, s := noticeTestService(t)
	if _, err := s.pool.Exec(ctx, `INSERT INTO users(email,name) SELECT 'fixture-'||n||'@example.com','Fixture' FROM generate_series(1,101) AS n`); err != nil {
		t.Fatal("fixture users failed")
	}
	if s.Prepare(ctx) != nil {
		t.Fatal("first batch failed")
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id=(SELECT user_id FROM privacy_notice_recipients LIMIT 1)`); err != nil {
		t.Fatal("fixture removal failed")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO users(id,email,name) VALUES ($1,'replacement@example.com','Fixture')`, uuid.New()); err != nil {
		t.Fatal("replacement fixture failed")
	}
	if s.Prepare(ctx) == nil {
		t.Fatal("changed membership accepted")
	}
	var capturing bool
	if s.pool.QueryRow(ctx, `SELECT state='capturing' AND expected_count=101 FROM privacy_notice_campaigns`).Scan(&capturing) != nil || !capturing {
		t.Fatal("invalid cohort sealed")
	}
}

func TestNoticeServiceRequiresPoolAndClock(t *testing.T) {
	if _, err := New(nil, nil, time.Now); err == nil {
		t.Fatal("nil pool accepted")
	}
	if _, err := New(&store.Pool{}, nil, nil); err == nil {
		t.Fatal("nil clock accepted")
	}
	if _, err := New(&store.Pool{}, nil, time.Now); err != nil {
		t.Fatal("keyless prepare service rejected")
	}
}

func TestPrepareRefusesCursorWithoutProgress(t *testing.T) {
	ctx, s := noticeTestService(t)
	if _, err := s.pool.Exec(ctx, `INSERT INTO users(email,name) SELECT 'fixture-'||n||'@example.com','Fixture' FROM generate_series(1,101) AS n`); err != nil {
		t.Fatal("cursor fixture users failed")
	}
	if s.Prepare(ctx) != nil {
		t.Fatal("cursor fixture capture failed")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE privacy_notice_campaigns SET capture_cursor='ffffffff-ffff-ffff-ffff-ffffffffffff'`); err != nil {
		t.Fatal("cursor fixture update failed")
	}
	if s.Prepare(ctx) == nil {
		t.Fatal("capture accepted a cursor that cannot reach the expected cohort")
	}
}
