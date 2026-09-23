package secondfactor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// totpHealthCapturingLogger returns a JSON logger and the buffer that
// receives its records, matching the pattern in
// internal/auth/handlers_test.go's newCapturingLogger.
func totpHealthCapturingLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}

// ---- TOTPUnavailableSignal: pure logic, no database -----------------------

// TestTOTPUnavailableSignal_RateLimitsPerReasonPerProcess proves the fixed
// totp_unavailable line logs at most once per minute per reason: a repeat
// within the window is silent, a repeat after the window logs again, and a
// different reason is tracked independently (docs/design/totp-key-management.md
// "Key failures"; docs/design/budgets.md "totp_unavailable log line").
func TestTOTPUnavailableSignal_RateLimitsPerReasonPerProcess(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	logger, buf := totpHealthCapturingLogger()
	signal := NewTOTPUnavailableSignal(clock.Now, logger)

	signal.Emit(TOTPUnavailableReasonUnknownKeyID)
	if !strings.Contains(buf.String(), `"reason":"unknown_key_id"`) {
		t.Fatalf("first Emit did not log; buf = %q", buf.String())
	}
	buf.Reset()

	signal.Emit(TOTPUnavailableReasonUnknownKeyID)
	if buf.Len() != 0 {
		t.Fatalf("repeat Emit within the window logged; buf = %q, want silent", buf.String())
	}

	signal.Emit(TOTPUnavailableReasonKeyIDOverflow)
	if !strings.Contains(buf.String(), `"reason":"key_id_overflow"`) {
		t.Fatalf("a different reason within the same window did not log; buf = %q", buf.String())
	}
	buf.Reset()

	clock.Advance(totpUnavailableInterval + time.Second)
	signal.Emit(TOTPUnavailableReasonUnknownKeyID)
	if !strings.Contains(buf.String(), `"reason":"unknown_key_id"`) {
		t.Fatalf("Emit after the window elapsed did not log; buf = %q", buf.String())
	}
}

// TestTOTPUnavailableSignal_EmitDecryptFailureIncludesKindAndID proves only
// decrypt_failed carries the record kind and internal row ID
// (docs/design/totp-key-management.md "Key failures").
func TestTOTPUnavailableSignal_EmitDecryptFailureIncludesKindAndID(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	logger, buf := totpHealthCapturingLogger()
	signal := NewTOTPUnavailableSignal(clock.Now, logger)

	id := uuid.New()
	signal.EmitDecryptFailure(TOTPRecordKindEnrollment, id)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("decode log record: %v; buf = %q", err, buf.String())
	}
	if record["msg"] != totpUnavailableMessage {
		t.Errorf("msg = %v, want %q", record["msg"], totpUnavailableMessage)
	}
	if record["reason"] != TOTPUnavailableReasonDecryptFailed {
		t.Errorf("reason = %v, want %q", record["reason"], TOTPUnavailableReasonDecryptFailed)
	}
	if record["kind"] != string(TOTPRecordKindEnrollment) {
		t.Errorf("kind = %v, want %q", record["kind"], TOTPRecordKindEnrollment)
	}
	if record["id"] != id.String() {
		t.Errorf("id = %v, want %q", record["id"], id.String())
	}
}

// TestTOTPUnavailableSignal_NilLoggerIsSilent proves a nil logger makes
// Emit and EmitDecryptFailure no-ops instead of panicking, matching Logger
// may be nil in Options elsewhere in this package.
func TestTOTPUnavailableSignal_NilLoggerIsSilent(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	signal := NewTOTPUnavailableSignal(clock.Now, nil)
	signal.Emit(TOTPUnavailableReasonKeyIDOverflow)
	signal.EmitDecryptFailure(TOTPRecordKindCredential, uuid.New())
}

// ---- TOTPHealthChecker.Check: pure branching logic, no database -----------

// fakeTOTPKeyIDLister returns a fixed ID list or error, letting
// TestTOTPHealthChecker_Check exercise Check's branching deterministically.
// The real query is unscoped across every account's rows
// (docs/design/totp-key-management.md "Key failures"), so it cannot be
// pinned to one test's fixtures against the shared test database; this fake
// is the reliable way to test the branching itself.
type fakeTOTPKeyIDLister struct {
	ids []string
	err error
}

func (f fakeTOTPKeyIDLister) ListTOTPActiveKeyIDs(context.Context, time.Time) ([]string, error) {
	return f.ids, f.err
}

func newTOTPHealthCheckerForTest(t *testing.T, lister totpKeyIDLister, ring *TOTPKeyRing, signal *TOTPUnavailableSignal, now func() time.Time) *TOTPHealthChecker {
	t.Helper()
	return &TOTPHealthChecker{q: lister, ring: ring, now: now, signal: signal}
}

func TestTOTPHealthChecker_Check(t *testing.T) {
	ring, err := NewTOTPKeyRing(testTOTPKeyZeroText, testTOTPKeySeqText, rand.Reader)
	if err != nil {
		t.Fatalf("NewTOTPKeyRing() error = %v", err)
	}
	unrelated := "tk1_" + strings.Repeat("z", 22)

	cases := []struct {
		name         string
		ids          []string
		listErr      error
		wantErr      bool
		wantOverflow bool
		wantUnknown  bool
	}{
		{name: "no rows", ids: nil},
		{name: "only the active key", ids: []string{testTOTPKeyZeroID}},
		{name: "active and previous", ids: []string{testTOTPKeyZeroID, testTOTPKeySeqID}},
		{name: "an id outside the ring", ids: []string{testTOTPKeyZeroID, unrelated}, wantUnknown: true},
		{name: "a third distinct id", ids: []string{testTOTPKeyZeroID, testTOTPKeySeqID, unrelated}, wantOverflow: true, wantUnknown: true},
		{name: "query failure", listErr: errors.New("boom"), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := testutil.NewClockAtEpoch()
			logger, buf := totpHealthCapturingLogger()
			signal := NewTOTPUnavailableSignal(clock.Now, logger)
			checker := newTOTPHealthCheckerForTest(t, fakeTOTPKeyIDLister{ids: tc.ids, err: tc.listErr}, ring, signal, clock.Now)

			err := checker.Check(t.Context())
			if tc.wantErr {
				if err == nil {
					t.Fatal("Check() error = nil, want the query error wrapped")
				}
				if strings.Contains(buf.String(), totpUnavailableMessage) {
					t.Errorf("Check() logged %q on a query failure, want nothing (not a closed reason)", buf.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("Check() error = %v, want nil", err)
			}
			logged := buf.String()
			if got := strings.Contains(logged, TOTPUnavailableReasonKeyIDOverflow); got != tc.wantOverflow {
				t.Errorf("key_id_overflow logged = %v, want %v; log = %q", got, tc.wantOverflow, logged)
			}
			if got := strings.Contains(logged, TOTPUnavailableReasonUnknownKeyID); got != tc.wantUnknown {
				t.Errorf("unknown_key_id logged = %v, want %v; log = %q", got, tc.wantUnknown, logged)
			}
		})
	}
}

// ---- TOTPHealthChecker: live-database wiring -------------------------------

// newTOTPHealthPool opens a real pool against the migrated test database.
// ListTOTPActiveKeyIDs scans every account's rows with no per-test scoping
// (docs/design/totp-key-management.md "Key failures"), so this test only
// proves the real store.Queries wiring succeeds; TestTOTPHealthChecker_Check
// above is the reliable source for Check's branching behavior.
func newTOTPHealthPool(t *testing.T) *store.Pool {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	return pool
}

// totpHealthTestContext returns a bounded context tied to t's lifetime,
// matching the pattern in service_test.go's testContext.
func totpHealthTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestTOTPHealthChecker_LiveCheckSucceedsAgainstAnInstalledCredential proves
// NewTOTPHealthChecker and Check run the real bounded query end to end
// without decrypting or erroring, regardless of what else the shared test
// database holds.
func TestTOTPHealthChecker_LiveCheckSucceedsAgainstAnInstalledCredential(t *testing.T) {
	pool := newTOTPHealthPool(t)
	ctx := totpHealthTestContext(t)
	q := store.New(pool)
	clock := testutil.NewClockAtEpoch()

	ring, err := NewTOTPKeyRing(testTOTPKeyZeroText, "", rand.Reader)
	if err != nil {
		t.Fatalf("NewTOTPKeyRing() error = %v", err)
	}
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.com", Name: "Test User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	rowID := uuid.Must(uuid.NewV7())
	secret, err := GenerateTOTPSecret(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}
	sealed, err := ring.Seal(TOTPRecordKindCredential, user.ID, rowID, secret)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if _, err = q.InstallTOTPCredential(ctx, store.InstallTOTPCredentialParams{
		ID: rowID, UserID: user.ID, KeyID: sealed.KeyID, Nonce: sealed.Nonce[:], Ciphertext: sealed.Ciphertext,
		LastUsedStep: 0, CreatedAt: clock.Now(),
	}); err != nil {
		t.Fatalf("InstallTOTPCredential: %v", err)
	}

	logger, _ := totpHealthCapturingLogger()
	signal := NewTOTPUnavailableSignal(clock.Now, logger)
	checker, err := NewTOTPHealthChecker(TOTPHealthConfig{Pool: pool, Ring: ring, Now: clock.Now, Signal: signal})
	if err != nil {
		t.Fatalf("NewTOTPHealthChecker() error = %v", err)
	}
	if err = checker.Check(ctx); err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
}
