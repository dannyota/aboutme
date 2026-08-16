package authmail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

var testNow = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

// fakeDBTX implements store.DBTX just enough for EnqueueTx: only QueryRow is
// used (CreateAuthEmailJob), and each call records the exact SQL args so tests
// can prove scope mapping and encrypted-only inserts without a database.
type fakeDBTX struct {
	inserts []insertCall
	err     error
}

type insertCall struct {
	sql  string
	args []any
}

func (f *fakeDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}

func (f *fakeDBTX) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.inserts = append(f.inserts, insertCall{sql: sql, args: append([]any(nil), args...)})
	return &fakeRow{err: f.err}
}

type fakeRow struct{ err error }

func (r *fakeRow) Scan(...any) error { return r.err }

func newTestOutbox(t *testing.T, ring *KeyRing, clock func() time.Time) *Outbox {
	t.Helper()
	o, err := NewOutbox(ring, clock)
	if err != nil {
		t.Fatalf("NewOutbox: %v", err)
	}
	return o
}

func testDigest() [32]byte {
	var d [32]byte
	copy(d[:], bytes.Repeat([]byte{0xab}, 32))
	return d
}

// isNilAny reports whether a captured query arg is nil, including a typed nil
// pointer/slice (pgx passes *uuid.UUID(nil) and []byte(nil) as typed nils).
func isNilAny(a any) bool {
	if a == nil {
		return true
	}
	v := reflect.ValueOf(a)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

// dumpArgs renders captured query args with pointer/string/slice contents so a
// plaintext leak is visible as a substring rather than a pointer address.
func dumpArgs(args []any) string {
	parts := make([]string, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case nil:
			parts[i] = "<nil>"
		case string:
			parts[i] = fmt.Sprintf("%q", v)
		case []byte:
			if v == nil {
				parts[i] = "<nil>"
			} else {
				parts[i] = fmt.Sprintf("%q", v)
			}
		case *string:
			if v == nil {
				parts[i] = "<nil>"
			} else {
				parts[i] = fmt.Sprintf("%q", *v)
			}
		case *time.Time:
			if v == nil {
				parts[i] = "<nil>"
			} else {
				parts[i] = v.String()
			}
		case time.Time:
			parts[i] = v.String()
		case *uuid.UUID:
			if v == nil {
				parts[i] = "<nil>"
			} else {
				parts[i] = v.String()
			}
		default:
			parts[i] = fmt.Sprintf("%#v", a)
		}
	}
	return strings.Join(parts, "|")
}

func TestOutboxNewRejectsNil(t *testing.T) {
	if _, err := NewOutbox(nil, func() time.Time { return testNow }); !errors.Is(err, ErrOutbox) {
		t.Fatalf("err = %v, want ErrOutbox", err)
	}
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	if _, err := NewOutbox(ring, nil); !errors.Is(err, ErrOutbox) {
		t.Fatalf("err = %v, want ErrOutbox", err)
	}
}

func TestOutboxEnqueueRejectsNilQueries(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	o := newTestOutbox(t, ring, func() time.Time { return testNow })
	if err := o.EnqueueTx(context.Background(), nil, EnqueueRequest{}); !errors.Is(err, ErrOutbox) {
		t.Fatalf("err = %v, want ErrOutbox", err)
	}
}

func TestOutboxEnqueueScopeMapping(t *testing.T) {
	regID := uuid.New()
	tokID := uuid.New()
	userID := uuid.New()
	digest := testDigest()

	cases := []struct {
		name         string
		kind         Kind
		reg, tok     *uuid.UUID
		usr          *uuid.UUID
		digest       *[32]byte
		wantKind     string
		wantScopeIdx int // 3=registration, 4=reset token, 5=user
	}{
		{"verify", KindVerify, &regID, nil, nil, &digest, "verify", 3},
		{"reset", KindReset, nil, &tokID, nil, &digest, "reset", 4},
		{"password_changed", KindPasswordChanged, nil, nil, &userID, nil, "password_changed", 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh ring per case: Seal consumes nonce entropy.
			ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
			o := newTestOutbox(t, ring, func() time.Time { return testNow })

			f := &fakeDBTX{}
			req := EnqueueRequest{
				JobID:          uuid.New(),
				Kind:           tc.kind,
				RegistrationID: tc.reg,
				ResetTokenID:   tc.tok,
				UserID:         tc.usr,
				TokenDigest:    tc.digest,
				Payload:        validPayloadForKind(tc.kind),
				ExpiresAt:      testNow.Add(time.Hour),
			}
			if err := o.EnqueueTx(context.Background(), store.New(f), req); err != nil {
				t.Fatalf("EnqueueTx: %v", err)
			}
			if len(f.inserts) != 1 {
				t.Fatalf("inserts = %d, want 1", len(f.inserts))
			}
			args := f.inserts[0].args
			if len(args) != 13 {
				t.Fatalf("arg count = %d, want 13", len(args))
			}
			if got := args[0].(uuid.UUID); got != req.JobID {
				t.Errorf("id arg = %s, want %s", got, req.JobID)
			}
			if got := args[1].(string); got != tc.wantKind {
				t.Errorf("kind arg = %q, want %q", got, tc.wantKind)
			}
			if got := args[2].(string); got != "pending" {
				t.Errorf("state arg = %q, want pending", got)
			}

			regGot, _ := args[3].(*uuid.UUID)
			tokGot, _ := args[4].(*uuid.UUID)
			usrGot, _ := args[5].(*uuid.UUID)
			switch tc.wantScopeIdx {
			case 3:
				if regGot == nil || *regGot != regID {
					t.Errorf("registration arg = %v, want %s", args[3], regID)
				}
				if !isNilAny(args[4]) {
					t.Errorf("reset token arg = %v, want nil", args[4])
				}
				if !isNilAny(args[5]) {
					t.Errorf("user arg = %v, want nil", args[5])
				}
			case 4:
				if tokGot == nil || *tokGot != tokID {
					t.Errorf("reset token arg = %v, want %s", args[4], tokID)
				}
				if !isNilAny(args[3]) {
					t.Errorf("registration arg = %v, want nil", args[3])
				}
				if !isNilAny(args[5]) {
					t.Errorf("user arg = %v, want nil", args[5])
				}
			case 5:
				if usrGot == nil || *usrGot != userID {
					t.Errorf("user arg = %v, want %s", args[5], userID)
				}
				if !isNilAny(args[3]) {
					t.Errorf("registration arg = %v, want nil", args[3])
				}
				if !isNilAny(args[4]) {
					t.Errorf("reset token arg = %v, want nil", args[4])
				}
			}

			if tc.digest != nil {
				if got := args[6].([]byte); !bytes.Equal(got, tc.digest[:]) {
					t.Errorf("token digest arg = %x, want %x", got, tc.digest[:])
				}
			} else if !isNilAny(args[6]) {
				t.Errorf("token digest arg = %v, want nil", args[6])
			}
			if got := args[7].(*string); got == nil || *got != "k-active" {
				t.Errorf("key id arg = %v, want k-active", args[7])
			}
			if got := args[8].([]byte); len(got) != 12 {
				t.Errorf("nonce arg len = %d, want 12", len(got))
			}
			if got := args[9].([]byte); len(got) == 0 {
				t.Error("ciphertext arg empty")
			}
			if got := args[10].(time.Time); !got.Equal(testNow) {
				t.Errorf("created_at arg = %v, want %v", got, testNow)
			}
			if got := args[11].(time.Time); !got.Equal(req.ExpiresAt) {
				t.Errorf("expires_at arg = %v, want %v", got, req.ExpiresAt)
			}
			if got := args[12].(*time.Time); got == nil || !got.Equal(testNow) {
				t.Errorf("next_attempt_at arg = %v, want %v", args[12], testNow)
			}
		})
	}
}

func TestOutboxEnqueueEncryptedOnlyInsert(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	o := newTestOutbox(t, ring, func() time.Time { return testNow })

	f := &fakeDBTX{}
	regID := uuid.New()
	digest := testDigest()
	payload := validVerifyPayload() // destination + link token live only here

	req := EnqueueRequest{
		JobID:          uuid.New(),
		Kind:           KindVerify,
		RegistrationID: &regID,
		TokenDigest:    &digest,
		Payload:        payload,
		ExpiresAt:      testNow.Add(time.Hour),
	}
	if err := o.EnqueueTx(context.Background(), store.New(f), req); err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}
	if len(f.inserts) != 1 {
		t.Fatalf("inserts = %d, want 1", len(f.inserts))
	}
	args := f.inserts[0].args

	ct := args[9].([]byte)
	if bytes.Contains(ct, []byte(payload.To)) {
		t.Error("ciphertext contains destination plaintext")
	}
	if bytes.Contains(ct, []byte("TESTTOKEN")) {
		t.Error("ciphertext contains link token plaintext")
	}
	if bytes.Contains(ct, []byte(verifyLinkPrefix)) {
		t.Error("ciphertext contains link prefix plaintext")
	}

	// The digest column holds only the 32-byte digest; the API has no raw-token
	// field, so no raw token can ever reach the capture.
	if !bytes.Equal(args[6].([]byte), digest[:]) {
		t.Errorf("token digest arg = %x, want %x", args[5], digest[:])
	}

	dump := dumpArgs(args)
	for _, secret := range []string{payload.To, "TESTTOKEN", verifyLinkPrefix} {
		if strings.Contains(dump, secret) {
			t.Errorf("captured args leak %q: %s", secret, dump)
		}
	}
}

func TestOutboxEnqueuePropagatesInsertError(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	o := newTestOutbox(t, ring, func() time.Time { return testNow })

	boom := errors.New("boom")
	f := &fakeDBTX{err: boom}
	regID := uuid.New()
	digest := testDigest()
	req := EnqueueRequest{
		JobID:          uuid.New(),
		Kind:           KindVerify,
		RegistrationID: &regID,
		TokenDigest:    &digest,
		Payload:        validVerifyPayload(),
		ExpiresAt:      testNow.Add(time.Hour),
	}
	err := o.EnqueueTx(context.Background(), store.New(f), req)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want the insert error propagated", err)
	}
	if len(f.inserts) != 1 {
		t.Fatalf("inserts = %d, want 1 (insert attempted before failing)", len(f.inserts))
	}
}

func TestOutboxNoInsertOnValidationFailure(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	o := newTestOutbox(t, ring, func() time.Time { return testNow })

	regID := uuid.New()
	userID := uuid.New()
	digest := testDigest()

	cases := []struct {
		name string
		req  EnqueueRequest
	}{
		{
			"invalid kind",
			EnqueueRequest{JobID: uuid.New(), Kind: Kind("bogus"), RegistrationID: &regID, TokenDigest: &digest, Payload: validVerifyPayload(), ExpiresAt: testNow.Add(time.Hour)},
		},
		{
			"nil job id",
			EnqueueRequest{JobID: uuid.Nil, Kind: KindVerify, RegistrationID: &regID, TokenDigest: &digest, Payload: validVerifyPayload(), ExpiresAt: testNow.Add(time.Hour)},
		},
		{
			"missing registration",
			EnqueueRequest{JobID: uuid.New(), Kind: KindVerify, TokenDigest: &digest, Payload: validVerifyPayload(), ExpiresAt: testNow.Add(time.Hour)},
		},
		{
			"stray reset token on verify",
			EnqueueRequest{JobID: uuid.New(), Kind: KindVerify, RegistrationID: &regID, ResetTokenID: &uuid.Nil, TokenDigest: &digest, Payload: validVerifyPayload(), ExpiresAt: testNow.Add(time.Hour)},
		},
		{
			"password_changed with digest",
			EnqueueRequest{JobID: uuid.New(), Kind: KindPasswordChanged, UserID: &userID, TokenDigest: &digest, Payload: validPayloadForKind(KindPasswordChanged), ExpiresAt: testNow.Add(time.Hour)},
		},
		{
			"verify missing digest",
			EnqueueRequest{JobID: uuid.New(), Kind: KindVerify, RegistrationID: &regID, Payload: validVerifyPayload(), ExpiresAt: testNow.Add(time.Hour)},
		},
		{
			"invalid email",
			EnqueueRequest{JobID: uuid.New(), Kind: KindVerify, RegistrationID: &regID, TokenDigest: &digest, Payload: Payload{Version: 1, To: "nope", Link: verifyLinkPrefix + "t"}, ExpiresAt: testNow.Add(time.Hour)},
		},
		{
			"expired",
			EnqueueRequest{JobID: uuid.New(), Kind: KindVerify, RegistrationID: &regID, TokenDigest: &digest, Payload: validVerifyPayload(), ExpiresAt: testNow.Add(-time.Hour)},
		},
		{
			"beyond 24 hours",
			EnqueueRequest{JobID: uuid.New(), Kind: KindVerify, RegistrationID: &regID, TokenDigest: &digest, Payload: validVerifyPayload(), ExpiresAt: testNow.Add(25 * time.Hour)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeDBTX{}
			if err := o.EnqueueTx(context.Background(), store.New(f), tc.req); err == nil {
				t.Fatal("expected error, got nil")
			}
			if len(f.inserts) != 0 {
				t.Fatalf("inserts = %d, want 0 on validation failure", len(f.inserts))
			}
		})
	}
}

func TestOutboxNoInsertOnEncryptionFailure(t *testing.T) {
	ring, err := NewKeyRing("k-active", map[string][32]byte{"k-active": fixedKey()}, errReader{})
	if err != nil {
		t.Fatal(err)
	}
	o := newTestOutbox(t, ring, func() time.Time { return testNow })

	f := &fakeDBTX{}
	regID := uuid.New()
	digest := testDigest()
	req := EnqueueRequest{
		JobID:          uuid.New(),
		Kind:           KindVerify,
		RegistrationID: &regID,
		TokenDigest:    &digest,
		Payload:        validVerifyPayload(),
		ExpiresAt:      testNow.Add(time.Hour),
	}
	if err := o.EnqueueTx(context.Background(), store.New(f), req); !errors.Is(err, ErrNonce) {
		t.Fatalf("err = %v, want ErrNonce", err)
	}
	if len(f.inserts) != 0 {
		t.Fatalf("inserts = %d, want 0 on encryption failure", len(f.inserts))
	}
}

// --- Live-database tests (skipped unless TEST_DATABASE_URL is set) ---

func newAuthmailPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

func newAuthmailRegistration(ctx context.Context, t *testing.T, q *store.Queries) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	reg, err := q.CreatePasswordRegistration(ctx, store.CreatePasswordRegistrationParams{
		Email:       uuid.NewString() + "@example.com",
		Name:        "Authmail Test",
		EncodedHash: []byte("hash"),
		TokenDigest: []byte(uuid.NewString() + uuid.NewString())[:32],
		CreatedAt:   now,
		ExpiresAt:   now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePasswordRegistration: %v", err)
	}
	return reg.ID
}

func TestOutboxEnqueueRollsBackWithCallerTransaction(t *testing.T) {
	ctx, pool := newAuthmailPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	qtx := store.New(tx)
	regID := newAuthmailRegistration(ctx, t, qtx)

	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	now := time.Now().UTC()
	o := newTestOutbox(t, ring, func() time.Time { return now })

	digest := testDigest()
	jobID := uuid.New()
	req := EnqueueRequest{
		JobID:          jobID,
		Kind:           KindVerify,
		RegistrationID: &regID,
		TokenDigest:    &digest,
		Payload:        validVerifyPayload(),
		ExpiresAt:      now.Add(time.Hour),
	}
	if err := o.EnqueueTx(ctx, qtx, req); err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}

	// EnqueueTx must not open or commit its own transaction: rolling back the
	// caller's transaction removes the row entirely.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth_email_jobs WHERE id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if n != 0 {
		t.Fatalf("committed rows after caller rollback = %d, want 0", n)
	}
}

func TestOutboxEnqueueCommitsEncryptedJob(t *testing.T) {
	ctx, pool := newAuthmailPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	qtx := store.New(tx)
	regID := newAuthmailRegistration(ctx, t, qtx)

	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	now := time.Now().UTC()
	o := newTestOutbox(t, ring, func() time.Time { return now })

	digest := testDigest()
	jobID := uuid.New()
	req := EnqueueRequest{
		JobID:          jobID,
		Kind:           KindVerify,
		RegistrationID: &regID,
		TokenDigest:    &digest,
		Payload:        validVerifyPayload(),
		ExpiresAt:      now.Add(time.Hour),
	}
	if err := o.EnqueueTx(ctx, qtx, req); err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// Deleting the registration cascades the job, keeping the shared DB clean.
	t.Cleanup(func() {
		if _, err := store.New(pool).DeletePasswordRegistration(context.Background(), regID); err != nil {
			t.Errorf("cleanup delete registration: %v", err)
		}
	})

	var kind, state, keyID string
	var nonce, ciphertext, tokenDigest []byte
	var reg uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT kind, state, key_id, nonce, ciphertext, token_digest, registration_id
		 FROM auth_email_jobs WHERE id = $1`, jobID,
	).Scan(&kind, &state, &keyID, &nonce, &ciphertext, &tokenDigest, &reg); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if kind != "verify" || state != "pending" {
		t.Fatalf("row = (kind=%s, state=%s), want (verify, pending)", kind, state)
	}
	if keyID != "k-active" {
		t.Fatalf("key_id = %q, want k-active", keyID)
	}
	if len(nonce) != 12 {
		t.Fatalf("nonce len = %d, want 12", len(nonce))
	}
	if !bytes.Equal(tokenDigest, digest[:]) {
		t.Fatalf("token_digest = %x, want %x", tokenDigest, digest[:])
	}
	if reg != regID {
		t.Fatalf("registration_id = %s, want %s", reg, regID)
	}

	var nonceArr [12]byte
	copy(nonceArr[:], nonce)
	got, err := ring.Open(jobID, KindVerify, Sealed{KeyID: keyID, Nonce: nonceArr, Ciphertext: ciphertext})
	if err != nil {
		t.Fatalf("Open stored row: %v", err)
	}
	if got != validVerifyPayload() {
		t.Fatalf("opened payload = %+v, want %+v", got, validVerifyPayload())
	}
}
