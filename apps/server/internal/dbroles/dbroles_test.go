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
	digest := sha256.Sum256([]byte("aboutme.cluster-role-bootstrap.v1"))
	if got := int64(binary.BigEndian.Uint64(digest[:8])); got != LockID {
		t.Fatalf("derived lock ID = %d, want %d", got, LockID)
	}
}

func TestEnsureCatalogCreatesAllRolesBeforeMembership(t *testing.T) {
	store := newFakeStore()
	result, err := ensureCatalog(context.Background(), store)
	if err != nil {
		t.Fatalf("ensureCatalog() error = %v", err)
	}
	if want := (Result{Created: 7}); result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	want := []string{"create:aboutme_runtime_owner", "create:aboutme_migrator", "create:aboutme_app", "create:aboutme_restore_verify", "create:aboutme_lifecycle_command", "create:aboutme_fencing_proof", "create:aboutme_maintenance", "grant:aboutme_runtime_owner:aboutme_migrator:false:true:false"}
	if !reflect.DeepEqual(store.mutations, want) {
		t.Fatalf("mutations = %#v, want %#v", store.mutations, want)
	}
}

func TestEnsureCatalogVerifiesExactExistingSetup(t *testing.T) {
	store := exactFakeStore()
	result, err := ensureCatalog(context.Background(), store)
	if err != nil {
		t.Fatalf("ensureCatalog() error = %v", err)
	}
	if want := (Result{Verified: 7}); result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	if len(store.mutations) != 0 {
		t.Fatalf("mutations = %v, want none", store.mutations)
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
		{"missing", func(s *fakeStore) { s.memberships = nil }},
		{"wrong options", func(s *fakeStore) { s.memberships[0].inherit = true }},
		{"extra fixed member", func(s *fakeStore) {
			s.memberships = append(s.memberships, membership{role: "aboutme_runtime_owner", member: "aboutme_app", set: true})
		}},
		{"ordinary role member", func(s *fakeStore) {
			s.memberships = append(s.memberships, membership{role: "aboutme_runtime_owner", member: "intruder", set: true})
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

func TestEnsureCatalogRejectsWrongDatabaseAndPropagatesFailures(t *testing.T) {
	store := newFakeStore()
	store.database = "aboutme"
	if _, err := ensureCatalog(context.Background(), store); !errors.Is(err, ErrWrongDatabase) {
		t.Fatalf("wrong database error = %v", err)
	}
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
	if len(store.mutations) != 8 {
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
	s.memberships = []membership{{role: "aboutme_runtime_owner", member: "aboutme_migrator", grantor: "aboutme", set: true}}
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
func (s *fakeStore) grant(_ context.Context, m membership) error {
	if s.failAt == "grant" {
		return errors.New("grant failed")
	}
	s.mutations = append(s.mutations, "grant:"+m.role+":"+m.member+":"+boolText(m.inherit)+":"+boolText(m.set)+":"+boolText(m.admin))
	return nil
}
func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
func (s *fakeStore) Commit() error { s.commits++; return s.commitErr }
func (s *fakeStore) Rollback() error {
	s.rollbacks++
	if s.rollbackBlock != nil {
		<-s.rollbackBlock
	}
	return nil
}
