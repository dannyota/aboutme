package password

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

// tinyPolicy is a fast Argon2id parameter set for tests; it stays inside the
// global parse budget so encode/parse/verify all exercise the real code paths
// without the production 64 MiB cost.
func tinyPolicy(memory uint32) HashPolicy {
	return HashPolicy{
		Version:     argon2Version,
		MemoryKiB:   memory,
		Iterations:  1,
		Parallelism: 1,
		SaltLen:     argon2SaltLen,
		KeyLen:      argon2KeyLen,
	}
}

// seqReader is a deterministic, error-free entropy source.
type seqReader struct{ n byte }

func (r *seqReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.n
		r.n++
	}
	return len(p), nil
}

// limitedFailReader returns n bytes successfully and then the given error.
type limitedFailReader struct {
	n   int
	err error
}

func (r *limitedFailReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, r.err
	}
	if len(p) > r.n {
		p = p[:r.n]
	}
	for i := range p {
		p[i] = 0x01
	}
	r.n -= len(p)
	return len(p), nil
}

func testSaltB64() string {
	return base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x5a}, argon2SaltLen))
}
func testKeyB64() string {
	return base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0xa5}, argon2KeyLen))
}

func TestEncodeParseRoundTrip(t *testing.T) {
	t.Parallel()
	policy := tinyPolicy(64)
	salt := bytes.Repeat([]byte{0x5a}, argon2SaltLen)
	key := bytes.Repeat([]byte{0xa5}, argon2KeyLen)
	encoded := encodePHC(policy, salt, key)
	if len(encoded) > phcMaxLen {
		t.Fatalf("encoded length %d exceeds %d", len(encoded), phcMaxLen)
	}

	parsed, gotSalt, gotKey, err := parsePHC(encoded)
	if err != nil {
		t.Fatalf("parsePHC error = %v", err)
	}
	if !parsed.equal(policy) {
		t.Errorf("parsed policy = %+v, want %+v", parsed, policy)
	}
	if !bytes.Equal(gotSalt, salt) || !bytes.Equal(gotKey, key) {
		t.Errorf("parsed salt/key mismatch")
	}
}

func TestParsePHCRejects(t *testing.T) {
	t.Parallel()
	salt := testSaltB64()
	key := testKeyB64()

	// valid assembles a canonical string from the m/t/p decimal spellings so
	// malformed values (leading zeros, over-budget) can be injected.
	valid := func(m, t, p string) string {
		return "$argon2id$v=19$m=" + m + ",t=" + t + ",p=" + p + "$" + salt + "$" + key
	}

	cases := []struct {
		name string
		s    string
	}{
		{"empty", ""},
		{"not phc", "plaintext"},
		{"wrong algorithm", "$argon2i$v=19$m=64,t=1,p=1$" + salt + "$" + key},
		{"missing version", "$argon2id$m=64,t=1,p=1$" + salt + "$" + key},
		{"unknown param", "$argon2id$v=19$m=64,t=1,p=1,q=2$" + salt + "$" + key},
		{"duplicate t", "$argon2id$v=19$m=64,t=1,t=1$" + salt + "$" + key},
		{"param order", "$argon2id$v=19$t=1,m=64,p=1$" + salt + "$" + key},
		{"leading zero", valid("064", "1", "1")},
		{"memory zero", valid("0", "1", "1")},
		{"iterations zero", valid("64", "0", "1")},
		{"parallelism zero", valid("64", "1", "0")},
		{"memory over budget", valid("65537", "3", "1")},
		{"iterations over budget", valid("65536", "4", "1")},
		{"parallelism over budget", valid("65536", "3", "2")},
		{"wrong version", "$argon2id$v=18$m=64,t=1,p=1$" + salt + "$" + key},
		{"version leading zero", "$argon2id$v=019$m=64,t=1,p=1$" + salt + "$" + key},
		{"salt padding", "$argon2id$v=19$m=64,t=1,p=1$" + strings.Repeat("A", 22) + "=$" + key},
		{"salt wrong length", "$argon2id$v=19$m=64,t=1,p=1$" + base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x01}, 15)) + "$" + key},
		{"key wrong length", "$argon2id$v=19$m=64,t=1,p=1$" + salt + "$" + base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x01}, 31))},
		{"oversize", "$argon2id$v=19$m=64,t=1,p=1$" + salt + "$" + key + strings.Repeat("A", 200)},
		{"non ascii byte", "$argon2id$v=19$m=64,t=1,p=1$" + salt + "$" + key + "\x00"},
		{"missing t", "$argon2id$v=19$m=64,p=1$" + salt + "$" + key},
		{"missing p", "$argon2id$v=19$m=64,t=1$" + salt + "$" + key},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, _, err := parsePHC(tc.s); !errors.Is(err, ErrHashInvalid) {
				t.Errorf("parsePHC(%q) error = %v, want ErrHashInvalid", tc.s, err)
			}
		})
	}
}

func TestHashVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	hasher, err := NewHasher(tinyPolicy(64), &seqReader{}, NewAdmission())
	if err != nil {
		t.Fatalf("NewHasher error = %v", err)
	}
	const password = "correct horse battery staple"
	encoded, err := hasher.Hash(context.Background(), password)
	if err != nil {
		t.Fatalf("Hash error = %v", err)
	}

	// Independent Argon2 result: parse the encoding and recompute with the
	// same salt and parameters, proving the PHC stores exactly what IDKey
	// produced.
	parsed, salt, want, err := parsePHC(encoded)
	if err != nil {
		t.Fatalf("parsePHC error = %v", err)
	}
	if got := argon2.IDKey([]byte(password), salt, parsed.Iterations, parsed.MemoryKiB, parsed.Parallelism, parsed.KeyLen); !bytes.Equal(got, want) {
		t.Error("independent argon2 result does not match the stored key")
	}

	res, err := hasher.Verify(context.Background(), encoded, password)
	if err != nil {
		t.Fatalf("Verify error = %v", err)
	}
	if !res.Match {
		t.Error("Verify Match = false, want true")
	}
	if res.NeedsRehash {
		t.Error("Verify NeedsRehash = true, want false for a current-parameter hash")
	}
}

func TestVerifyMismatch(t *testing.T) {
	t.Parallel()
	hasher, err := NewHasher(tinyPolicy(64), &seqReader{}, NewAdmission())
	if err != nil {
		t.Fatalf("NewHasher error = %v", err)
	}
	encoded, err := hasher.Hash(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash error = %v", err)
	}
	res, err := hasher.Verify(context.Background(), encoded, "wrong password here")
	if err != nil {
		t.Fatalf("Verify error = %v", err)
	}
	if res.Match {
		t.Error("Verify Match = true, want false")
	}
}

func TestVerifyNeedsRehash(t *testing.T) {
	t.Parallel()
	// Generate under a weaker policy, then verify under the current policy.
	weak, err := NewHasher(tinyPolicy(32), &seqReader{}, NewAdmission())
	if err != nil {
		t.Fatalf("NewHasher(weak) error = %v", err)
	}
	const password = "rehash me please 123"
	encoded, err := weak.Hash(context.Background(), password)
	if err != nil {
		t.Fatalf("Hash error = %v", err)
	}

	current, err := NewHasher(tinyPolicy(64), &seqReader{}, NewAdmission())
	if err != nil {
		t.Fatalf("NewHasher(current) error = %v", err)
	}
	res, err := current.Verify(context.Background(), encoded, password)
	if err != nil {
		t.Fatalf("Verify error = %v", err)
	}
	if !res.Match {
		t.Error("Verify Match = false, want true")
	}
	if !res.NeedsRehash {
		t.Error("Verify NeedsRehash = false, want true for a weaker-parameter hash")
	}
}

func TestVerifyDummy(t *testing.T) {
	t.Parallel()
	hasher, err := NewHasher(tinyPolicy(64), &seqReader{}, NewAdmission())
	if err != nil {
		t.Fatalf("NewHasher error = %v", err)
	}
	if err := hasher.VerifyDummy(context.Background(), "some password value"); err != nil {
		t.Errorf("VerifyDummy error = %v, want nil", err)
	}
}

func TestHashEntropyFailureReleasesAdmission(t *testing.T) {
	t.Parallel()
	hasher, err := NewHasher(tinyPolicy(64), &limitedFailReader{n: 16, err: errors.New("entropy exhausted")}, NewAdmission())
	if err != nil {
		t.Fatalf("NewHasher error = %v", err)
	}
	if _, err := hasher.Hash(context.Background(), "correct horse battery staple"); err == nil {
		t.Fatal("Hash error = nil, want entropy failure")
	}
	if got := len(hasher.admission.slots); got != admissionRunning {
		t.Errorf("free slots = %d, want %d after a Hash failure", got, admissionRunning)
	}
}

func TestHashAdmissionFailure(t *testing.T) {
	admission := NewAdmission()
	hasher, err := NewHasher(tinyPolicy(64), &seqReader{}, admission)
	if err != nil {
		t.Fatalf("NewHasher error = %v", err)
	}
	ctx := context.Background()
	// Two running plus sixteen waiting: Hash becomes the seventeenth waiter.
	results := fillAdmission(ctx, t, admission)
	if _, err := hasher.Hash(ctx, "correct horse battery staple"); !errors.Is(err, ErrHashAdmission) {
		t.Errorf("Hash error = %v, want ErrHashAdmission", err)
	}
	admission.Release()
	admission.Release()
	for range admissionWaiting {
		<-results
	}
}

func TestPHCFormatStringExact(t *testing.T) {
	t.Parallel()
	// The exact canonical form of the production encoding prefix and length.
	policy := DefaultHashPolicy()
	salt := bytes.Repeat([]byte{0x00}, 16)
	key := bytes.Repeat([]byte{0x00}, 32)
	encoded := encodePHC(policy, salt, key)
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Errorf("encoded prefix = %q, want $argon2id$v=19$m=65536,t=3,p=1$", encoded)
	}
	if len(encoded) != 97 {
		t.Errorf("encoded length = %d, want 97", len(encoded))
	}
}

// BenchmarkHashDefaultPolicy measures the production 64 MiB / 3-iteration
// Argon2id cost. Run it alone (it is not part of the normal test run) with
// -benchtime=1x and record wall time and peak RSS.
func BenchmarkHashDefaultPolicy(b *testing.B) {
	hasher, err := NewHasher(DefaultHashPolicy(), &seqReader{}, NewAdmission())
	if err != nil {
		b.Fatalf("NewHasher: %v", err)
	}
	const password = "correct horse battery staple"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := hasher.Hash(context.Background(), password); err != nil {
			b.Fatalf("Hash: %v", err)
		}
	}
}
