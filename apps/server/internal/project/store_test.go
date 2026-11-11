package project

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func projectDB(t *testing.T) (context.Context, *store.Pool, *store.Queries, uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	pool, err := store.NewPool(ctx, testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	u, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Project test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", u.ID); err != nil {
			t.Error(err)
		}
	})
	return ctx, pool, q, u.ID
}

func TestStore_SlugsAndHolds(t *testing.T) {
	ctx, _, q, owner := projectDB(t)
	s := NewStore()
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	a, err := s.CreateTx(ctx, q, owner, "", draft(), now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Slug != "du-an" || a.ID.Version() != 7 || a.Revision != 1 {
		t.Fatalf("created=%+v", a)
	}
	b, err := s.CreateTx(ctx, q, owner, "", draft(), now)
	if err != nil {
		t.Fatal(err)
	}
	if b.Slug == a.Slug {
		t.Fatal("proposal was not deduplicated")
	}
	if _, err = s.CreateTx(ctx, q, owner, a.Slug, draft(), now); !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("duplicate error=%v", err)
	}
	renamed, err := s.ReplaceTx(ctx, q, owner, a.ID, a.Revision, "new-slug", draft(), now)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Revision != 2 {
		t.Fatalf("revision=%d", renamed.Revision)
	}
	if _, err = s.CreateTx(ctx, q, owner, a.Slug, draft(), now.Add(180*24*time.Hour-time.Microsecond)); !errors.Is(err, ErrSlugHeld) {
		t.Fatalf("held rename error=%v", err)
	}
	if err = s.DeleteTx(ctx, q, owner, b.ID, b.Revision, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateTx(ctx, q, owner, b.Slug, draft(), now); !errors.Is(err, ErrSlugHeld) {
		t.Fatalf("held delete error=%v", err)
	}
	if _, err = s.CreateTx(ctx, q, owner, a.Slug, draft(), now.Add(180*24*time.Hour)); err != nil {
		t.Fatalf("hold expiry error=%v", err)
	}
	_, _, q2, owner2 := projectDB(t)
	if _, err = s.CreateTx(ctx, q2, owner2, "new-slug", draft(), now); err != nil {
		t.Fatalf("same slug in another account: %v", err)
	}
}

func TestStore_ProposalWhenSuffixedCandidateEqualsBase(t *testing.T) {
	ctx, _, q, owner := projectDB(t)
	s := NewStore()
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	d := draft()
	d.Text["vi"] = Text{Name: strings.Repeat("a", 38) + " 2"}
	base := strings.Repeat("a", 38) + "-2"
	want := []string{base, strings.Repeat("a", 38) + "-3", strings.Repeat("a", 38) + "-4"}
	for i, slug := range want {
		p, err := s.CreateTx(ctx, q, owner, "", d, now)
		if err != nil {
			t.Fatalf("project %d: %v", i+1, err)
		}
		if p.Slug != slug {
			t.Fatalf("project %d slug=%q want %q", i+1, p.Slug, slug)
		}
	}
}

func TestStore_AccountProjectBound(t *testing.T) {
	ctx, _, q, owner := projectDB(t)
	s := NewStore()
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		if _, err := s.CreateTx(ctx, q, owner, "", draft(), now); err != nil {
			t.Fatalf("project %d: %v", i+1, err)
		}
	}
	if _, err := s.CreateTx(ctx, q, owner, "", draft(), now); !errors.Is(err, ErrLimit) {
		t.Fatalf("31st error=%v", err)
	}
}

func TestStore_StoredByteBoundary(t *testing.T) {
	ctx, pool, q, owner := projectDB(t)
	s := NewStore()
	now := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	d := draft()
	d.RepositoryURL = "https://example.test/"
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var base int
	if err = pool.QueryRow(ctx, "SELECT octet_length($1::jsonb::text)", raw).Scan(&base); err != nil {
		t.Fatal(err)
	}
	d.RepositoryURL += strings.Repeat("a", 98304-base)
	a, err := s.CreateTx(ctx, q, owner, "at-bound", d, now)
	if err != nil {
		t.Fatalf("at stored byte bound: %v", err)
	}
	d.RepositoryURL += "a"
	if _, err = s.ReplaceTx(ctx, q, owner, a.ID, a.Revision, a.Slug, d, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("over stored byte bound error=%v", err)
	}
}

func TestStore_DatabaseConstraints(t *testing.T) {
	ctx, pool, _, owner := projectDB(t)
	for _, tc := range []struct {
		name, slug, doc string
		revision        int64
	}{
		{"short slug", "a", `{}`, 1}, {"long slug", strings.Repeat("a", 41), `{}`, 1}, {"slug grammar", "a--b", `{}`, 1}, {"revision", "valid", `{}`, 0}, {"size", "valid", `{"value":"` + strings.Repeat("a", 98304) + `"}`, 1}, {"non object", "valid", `[]`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "INSERT INTO projects(user_id,slug,revision,document) VALUES($1,$2,$3,$4)", owner, tc.slug, tc.revision, tc.doc); err == nil {
				t.Fatal("invalid row accepted")
			}
		})
	}
	for _, table := range []string{"projects", "project_slug_holds"} {
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var allowed bool
			if err := pool.QueryRow(ctx, "SELECT has_table_privilege('aboutme_app',$1,$2)", table, privilege).Scan(&allowed); err != nil || !allowed {
				t.Fatalf("table=%s privilege=%s allowed=%t error=%v", table, privilege, allowed, err)
			}
		}
	}
}
