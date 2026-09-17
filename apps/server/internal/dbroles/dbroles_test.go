package dbroles

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLockIDMatchesNamespaceDigest(t *testing.T) {
	digest := sha256.Sum256([]byte("aboutme.db-setup.v1"))
	if got := int64(binary.BigEndian.Uint64(digest[:8])); got != LockID {
		t.Fatalf("derived lock ID = %d, want %d", got, LockID)
	}
}

func TestEnsureCatalogCreatesRolesThenGrants(t *testing.T) {
	store := newFakeStore()
	result, err := ensureCatalog(context.Background(), store)
	if err != nil {
		t.Fatalf("ensureCatalog() error = %v", err)
	}
	if want := (Result{Created: 2}); result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	want := []string{"create:aboutme_migrator", "create:aboutme_app", "grant:postgres"}
	if !reflect.DeepEqual(store.mutations, want) {
		t.Fatalf("mutations = %#v, want %#v", store.mutations, want)
	}
}

func TestEnsureCatalogVerifiesExactExistingSetupThenGrants(t *testing.T) {
	store := exactFakeStore()
	result, err := ensureCatalog(context.Background(), store)
	if err != nil {
		t.Fatalf("ensureCatalog() error = %v", err)
	}
	if want := (Result{Verified: 2}); result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	want := []string{"grant:postgres"}
	if !reflect.DeepEqual(store.mutations, want) {
		t.Fatalf("mutations = %#v, want %#v", store.mutations, want)
	}
}

func TestEnsureCatalogGrantsAgainstTheConnectedDatabase(t *testing.T) {
	store := exactFakeStore()
	store.database = "aboutme_dev"
	if _, err := ensureCatalog(context.Background(), store); err != nil {
		t.Fatalf("ensureCatalog() error = %v", err)
	}
	if want := []string{"grant:aboutme_dev"}; !reflect.DeepEqual(store.mutations, want) {
		t.Fatalf("mutations = %#v, want %#v", store.mutations, want)
	}
}

func TestEnsureCatalogRejectsPartialSetupBeforeMutation(t *testing.T) {
	store := newFakeStore()
	store.roles[fixedRoles[0].name] = fixedRoles[0]
	_, err := ensureCatalog(context.Background(), store)
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("error = %v, want ErrDrift", err)
	}
	if len(store.mutations) != 0 {
		t.Fatalf("mutations = %v, want none", store.mutations)
	}
}

func TestEnsureCatalogRejectsEveryRoleAttributeDrift(t *testing.T) {
	tests := []struct {
		name string
		edit func(*role)
	}{
		{"login", func(r *role) { r.login = !r.login }}, {"superuser", func(r *role) { r.superuser = true }},
		{"createdb", func(r *role) { r.createdb = true }}, {"createrole", func(r *role) { r.createrole = true }},
		{"replication", func(r *role) { r.replication = true }}, {"bypassrls", func(r *role) { r.bypassRLS = true }},
		{"inherit", func(r *role) { r.inherit = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := exactFakeStore()
			r := store.roles[fixedRoles[0].name]
			tt.edit(&r)
			store.roles[r.name] = r
			_, err := ensureCatalog(context.Background(), store)
			if !errors.Is(err, ErrDrift) {
				t.Fatalf("error = %v, want ErrDrift", err)
			}
			if len(store.mutations) != 0 {
				t.Fatalf("mutations = %v, want none", store.mutations)
			}
		})
	}
}

func TestCreateStatementsNeverSetCredentials(t *testing.T) {
	for name, statement := range createStatements {
		upper := strings.ToUpper(statement)
		if strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "VALID UNTIL") {
			t.Fatalf("CREATE ROLE for %s contains credential state", name)
		}
	}
}

func TestEnsureCatalogRejectsMembershipDrift(t *testing.T) {
	tests := []struct {
		name string
		edit func(*fakeStore)
	}{
		{"wrong options", func(s *fakeStore) {
			s.memberships = []membership{{role: "aboutme_migrator", member: "aboutme", grantor: "aboutme", set: true}}
		}},
		{"non-creator member", func(s *fakeStore) {
			s.memberships = []membership{{role: "aboutme_migrator", member: "intruder", admin: true}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := exactFakeStore()
			tt.edit(store)
			_, err := ensureCatalog(context.Background(), store)
			if !errors.Is(err, ErrDrift) {
				t.Fatalf("error = %v, want ErrDrift", err)
			}
			if len(store.mutations) != 0 {
				t.Fatalf("mutations = %v, want none", store.mutations)
			}
		})
	}
}

func TestEnsureCatalogAllowsCreatorAdministrativeMembership(t *testing.T) {
	store := exactFakeStore()
	store.currentUser = "bootstrap_admin"
	for _, spec := range fixedRoles {
		store.memberships = append(store.memberships, membership{role: spec.name, member: "bootstrap_admin", grantor: "postgres", admin: true})
	}
	if _, err := ensureCatalog(context.Background(), store); err != nil {
		t.Fatalf("ensureCatalog() error = %v", err)
	}
}

func TestEnsureCatalogRejectsMembershipReferencingAbsentRole(t *testing.T) {
	store := newFakeStore()
	store.memberships = []membership{{role: "aboutme_migrator", member: "aboutme", set: true}}
	if _, err := ensureCatalog(context.Background(), store); !errors.Is(err, ErrDrift) {
		t.Fatalf("error = %v, want ErrDrift", err)
	}
}

func TestEnsureCatalogPropagatesFailures(t *testing.T) {
	for _, failAt := range []string{"database", "lock", "roles", "memberships", "create", "grant"} {
		t.Run(failAt, func(t *testing.T) {
			s := newFakeStore()
			s.failAt = failAt
			_, err := ensureCatalog(context.Background(), s)
			if err == nil {
				t.Fatal("ensureCatalog() error = nil")
			}
		})
	}
}

func TestEnsureTransactionRollsBackAfterFailureWithCancelledContext(t *testing.T) {
	store := newFakeStore()
	store.failAt = "roles"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ensureTransaction(ctx, store)
	if err == nil {
		t.Fatal("ensureTransaction() error = nil")
	}
	if store.rollbacks != 1 {
		t.Fatalf("rollbacks = %d, want 1", store.rollbacks)
	}
	if store.commits != 0 {
		t.Fatalf("commits = %d, want 0", store.commits)
	}
}

func TestEnsureTransactionDoesNotRetryUnknownCommitOutcome(t *testing.T) {
	store := newFakeStore()
	store.commitErr = errors.New("connection lost after commit")
	_, err := ensureTransaction(context.Background(), store)
	if err == nil || !strings.Contains(err.Error(), "outcome unknown") {
		t.Fatalf("error = %v, want unknown outcome", err)
	}
	if store.commits != 1 {
		t.Fatalf("commits = %d, want 1", store.commits)
	}
	if len(store.mutations) != 3 {
		t.Fatalf("mutations = %d, want one attempt", len(store.mutations))
	}
}

func TestRollbackBoundedReturnsWhenCleanupStalls(t *testing.T) {
	store := newFakeStore()
	store.rollbackBlock = make(chan struct{})
	start := time.Now()
	rollbackBounded(store, 5*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("rollbackBounded took %s", elapsed)
	}
	close(store.rollbackBlock)
}

type fakeStore struct {
	database, currentUser string
	roles                 map[string]role
	memberships           []membership
	mutations             []string
	failAt                string
	commits, rollbacks    int
	commitErr             error
	rollbackBlock         chan struct{}
}

func newFakeStore() *fakeStore {
	return &fakeStore{database: "postgres", currentUser: "aboutme", roles: make(map[string]role)}
}
func exactFakeStore() *fakeStore {
	s := newFakeStore()
	for _, r := range fixedRoles {
		s.roles[r.name] = r
	}
	return s
}
func (s *fakeStore) databaseAndUser(context.Context) (string, string, error) {
	if s.failAt == "database" {
		return "", "", errors.New("read failed")
	}
	return s.database, s.currentUser, nil
}
func (s *fakeStore) lock(context.Context) error {
	if s.failAt == "lock" {
		return errors.New("lock failed")
	}
	return nil
}
func (s *fakeStore) readRoles(context.Context) (map[string]role, error) {
	if s.failAt == "roles" {
		return nil, errors.New("roles failed")
	}
	return s.roles, nil
}
func (s *fakeStore) readMemberships(context.Context) ([]membership, error) {
	if s.failAt == "memberships" {
		return nil, errors.New("memberships failed")
	}
	return s.memberships, nil
}
func (s *fakeStore) createRole(_ context.Context, r role) error {
	if s.failAt == "create" {
		return errors.New("create failed")
	}
	s.mutations = append(s.mutations, "create:"+r.name)
	return nil
}
func (s *fakeStore) grantDatabaseAndSchema(_ context.Context, database string) error {
	if s.failAt == "grant" {
		return errors.New("grant failed")
	}
	s.mutations = append(s.mutations, "grant:"+database)
	return nil
}
func (s *fakeStore) Commit() error { s.commits++; return s.commitErr }
func (s *fakeStore) Rollback() error {
	s.rollbacks++
	if s.rollbackBlock != nil {
		<-s.rollbackBlock
	}
	return nil
}
