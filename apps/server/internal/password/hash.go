package password

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ErrHashInvalid is the closed error returned for any malformed, noncanonical,
// or over-budget encoded password hash.
var ErrHashInvalid = errors.New("password hash invalid")

// Argon2id parameters (D2). These are the parse budget: any encoded hash that
// exceeds them is rejected before allocation; a hash below them verifies and
// reports NeedsRehash.
const (
	argon2Version     = 19
	argon2MemoryKiB   = 65536 // 64 MiB
	argon2Iterations  = 3
	argon2Parallelism = 1
	argon2SaltLen     = 16
	argon2KeyLen      = 32
	phcMaxLen         = 192
)

// HashPolicy is a complete Argon2id parameter set.
type HashPolicy struct {
	Version     int
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLen     int
	KeyLen      uint32
}

// DefaultHashPolicy returns the production Argon2id parameters from D2.
func DefaultHashPolicy() HashPolicy {
	return HashPolicy{
		Version:     argon2Version,
		MemoryKiB:   argon2MemoryKiB,
		Iterations:  argon2Iterations,
		Parallelism: argon2Parallelism,
		SaltLen:     argon2SaltLen,
		KeyLen:      argon2KeyLen,
	}
}

// equal reports whether two policies are identical.
func (p HashPolicy) equal(o HashPolicy) bool {
	return p.Version == o.Version &&
		p.MemoryKiB == o.MemoryKiB &&
		p.Iterations == o.Iterations &&
		p.Parallelism == o.Parallelism &&
		p.SaltLen == o.SaltLen &&
		p.KeyLen == o.KeyLen
}

// VerifyResult is the outcome of verifying a password against an encoded hash.
type VerifyResult struct {
	Match       bool
	NeedsRehash bool
}

// Hasher hashes and verifies passwords with Argon2id behind the shared
// admission controller.
type Hasher struct {
	policy    HashPolicy
	entropy   io.Reader
	admission *Admission
	dummy     string
}

// NewHasher returns a Hasher for the given policy, entropy source, and
// admission controller, and generates the startup dummy encoding.
func NewHasher(policy HashPolicy, entropy io.Reader, admission *Admission) (*Hasher, error) {
	h := &Hasher{policy: policy, entropy: entropy, admission: admission}
	dummy, err := h.deriveDummy()
	if err != nil {
		return nil, err
	}
	h.dummy = dummy
	return h, nil
}

// deriveDummy produces one valid encoding with the current policy, used by
// VerifyDummy so unknown/no-credential logins pay the same Argon2 cost.
func (h *Hasher) deriveDummy() (string, error) {
	salt := make([]byte, h.policy.SaltLen)
	if _, err := io.ReadFull(h.entropy, salt); err != nil {
		return "", fmt.Errorf("password: dummy salt entropy: %w", err)
	}
	key := argon2.IDKey(nil, salt, h.policy.Iterations, h.policy.MemoryKiB, h.policy.Parallelism, h.policy.KeyLen)
	return encodePHC(h.policy, salt, key), nil
}

// Hash derives an Argon2id hash for normalized and returns its PHC encoding.
func (h *Hasher) Hash(ctx context.Context, normalized string) (string, error) {
	if err := h.admission.Acquire(ctx); err != nil {
		return "", err
	}
	defer h.admission.Release()

	salt := make([]byte, h.policy.SaltLen)
	if _, err := io.ReadFull(h.entropy, salt); err != nil {
		return "", fmt.Errorf("password: salt entropy: %w", err)
	}
	key := argon2.IDKey([]byte(normalized), salt, h.policy.Iterations, h.policy.MemoryKiB, h.policy.Parallelism, h.policy.KeyLen)
	return encodePHC(h.policy, salt, key), nil
}

// Verify checks normalized against encoded. Parsing completes all bounds and
// canonical-syntax checks before any Argon2 allocation.
func (h *Hasher) Verify(ctx context.Context, encoded, normalized string) (VerifyResult, error) {
	parsed, salt, want, err := parsePHC(encoded)
	if err != nil {
		return VerifyResult{}, ErrHashInvalid
	}
	if err := h.admission.Acquire(ctx); err != nil {
		return VerifyResult{}, err
	}
	defer h.admission.Release()

	got := argon2.IDKey([]byte(normalized), salt, parsed.Iterations, parsed.MemoryKiB, parsed.Parallelism, parsed.KeyLen)
	match := subtle.ConstantTimeCompare(got, want) == 1
	return VerifyResult{Match: match, NeedsRehash: !parsed.equal(h.policy)}, nil
}

// VerifyDummy runs a full verification against the startup dummy encoding so an
// unknown or no-credential login is indistinguishable in cost from a real one.
func (h *Hasher) VerifyDummy(ctx context.Context, normalized string) error {
	_, err := h.Verify(ctx, h.dummy, normalized)
	return err
}

// encodePHC renders the closed PHC encoding for the given parameters.
func encodePHC(p HashPolicy, salt, key []byte) string {
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		p.Version, p.MemoryKiB, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

// parsePHC parses and validates an encoded hash without allocating any Argon2
// memory. It rejects over-length, non-ASCII, malformed, duplicate, missing,
// unknown, over-budget, and noncanonical encodings.
func parsePHC(encoded string) (HashPolicy, []byte, []byte, error) {
	var zero HashPolicy
	if len(encoded) > phcMaxLen {
		return zero, nil, nil, ErrHashInvalid
	}
	for i := 0; i < len(encoded); i++ {
		if encoded[i] < 0x21 || encoded[i] > 0x7e {
			return zero, nil, nil, ErrHashInvalid
		}
	}

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return zero, nil, nil, ErrHashInvalid
	}

	version, err := parsePHCVersion(parts[2])
	if err != nil {
		return zero, nil, nil, ErrHashInvalid
	}
	memory, iterations, parallelism, err := parsePHCParams(parts[3])
	if err != nil {
		return zero, nil, nil, ErrHashInvalid
	}

	if version != argon2Version {
		return zero, nil, nil, ErrHashInvalid
	}
	if memory < 1 || iterations < 1 || parallelism < 1 {
		return zero, nil, nil, ErrHashInvalid
	}

	salt, err := decodePHCField(parts[4])
	if err != nil || len(salt) != argon2SaltLen {
		return zero, nil, nil, ErrHashInvalid
	}
	key, err := decodePHCField(parts[5])
	if err != nil || len(key) != argon2KeyLen {
		return zero, nil, nil, ErrHashInvalid
	}

	policy := HashPolicy{
		Version:     argon2Version,
		MemoryKiB:   memory,
		Iterations:  iterations,
		Parallelism: parallelism,
		SaltLen:     len(salt),
		KeyLen:      argon2KeyLen,
	}
	return policy, salt, key, nil
}

func parsePHCVersion(s string) (uint64, error) {
	if !strings.HasPrefix(s, "v=") {
		return 0, errors.New("missing version")
	}
	return parsePHCDecimal(s[2:])
}

// parsePHCParams parses the canonical "m=<dec>,t=<dec>,p=<dec>" field and
// rejects over-budget values before any narrowing conversion.
func parsePHCParams(s string) (uint32, uint32, uint8, error) {
	fields := strings.Split(s, ",")
	if len(fields) != 3 {
		return 0, 0, 0, errors.New("param count")
	}
	m, err := parsePHCUintField(fields[0], "m")
	if err != nil {
		return 0, 0, 0, err
	}
	t, err := parsePHCUintField(fields[1], "t")
	if err != nil {
		return 0, 0, 0, err
	}
	p, err := parsePHCUintField(fields[2], "p")
	if err != nil {
		return 0, 0, 0, err
	}
	if m > argon2MemoryKiB || t > argon2Iterations || p > argon2Parallelism {
		return 0, 0, 0, errors.New("over budget")
	}
	return uint32(m), uint32(t), uint8(p), nil //nolint:gosec // each value is bounded by its Argon2 budget constant above
}

func parsePHCUintField(field, key string) (uint64, error) {
	if !strings.HasPrefix(field, key+"=") {
		return 0, errors.New("unknown field")
	}
	return parsePHCDecimal(field[len(key)+1:])
}

// parsePHCDecimal parses a canonical decimal: non-empty, ASCII digits, no
// leading zeros.
func parsePHCDecimal(s string) (uint64, error) {
	if len(s) == 0 {
		return 0, errors.New("empty decimal")
	}
	if s[0] == '0' && len(s) > 1 {
		return 0, errors.New("leading zero")
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, errors.New("non-digit")
		}
	}
	return strconv.ParseUint(s, 10, 64)
}

func decodePHCField(s string) ([]byte, error) {
	return base64.RawStdEncoding.Strict().DecodeString(s)
}
