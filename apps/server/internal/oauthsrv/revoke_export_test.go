package oauthsrv

import (
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// RevokeRoutingFixture exposes the database fixture to external-package tests
// that import the MCP resource server without creating an import cycle.
type RevokeRoutingFixture struct {
	Service    *Service
	Pool       *store.Pool
	Queries    *store.Queries
	PublicID   string
	AccessRaw  string
	RefreshRaw string
	Clock      func() time.Time
	fixture    revokeFixture
}

func NewRevokeRoutingFixture(t *testing.T, mode string) RevokeRoutingFixture {
	t.Helper()
	f := newRevokeFixture(t, mode)
	return RevokeRoutingFixture{
		Service: f.s, Pool: f.pool, Queries: f.q, PublicID: f.publicID,
		AccessRaw: f.accessRaw, RefreshRaw: f.raw, Clock: f.s.clock, fixture: f,
	}
}

func (f RevokeRoutingFixture) AssertRevoked(t *testing.T) {
	t.Helper()
	assertRevokeCommittedState(t, f.fixture)
	f.fixture.assertIsolation(t)
	f.fixture.assertCannotIssue(t)
}
