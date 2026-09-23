package secondfactor

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // ADR 0049 approves HMAC-SHA-1 only for the RFC 6238 interoperability profile
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// TOTP profile constants from docs/design/totp-second-factor-contract.md
// "TOTP profile and code verification" (AC-AUTH-026): HMAC-SHA-1, a 20-byte
// secret, six decimal digits, a 30-second period, T0=0, and one step of clock
// skew in each direction.
const (
	totpSecretBytes   = 20
	totpDigits        = 6
	totpPeriodSeconds = 30
	totpGroupSize     = 4
)

// ErrTOTPMalformedCode is returned for input that is not exactly six ASCII
// digits, before any cryptographic work.
var ErrTOTPMalformedCode = errors.New("secondfactor: totp code is not six ascii digits")

// ErrTOTPNegativeTime is returned for a clock reading before the Unix epoch.
var ErrTOTPNegativeTime = errors.New("secondfactor: totp time is before the unix epoch")

// totpBase32 is the canonical unpadded RFC 4648 Base32 encoding used for both
// the provisioning secret and its grouped display.
var totpBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// TOTPSecret is one generated 20-byte RFC 6238 secret.
type TOTPSecret [totpSecretBytes]byte

// GenerateTOTPSecret draws a fresh 20-byte secret from entropy.
func GenerateTOTPSecret(entropy io.Reader) (TOTPSecret, error) {
	var secret TOTPSecret
	if _, err := io.ReadFull(entropy, secret[:]); err != nil {
		return TOTPSecret{}, fmt.Errorf("secondfactor: totp secret entropy: %w", err)
	}
	return secret, nil
}

// Base32 returns the canonical 32-character uppercase unpadded Base32 secret
// from "Provisioning data".
func (s TOTPSecret) Base32() string {
	return totpBase32.EncodeToString(s[:])
}

// GroupedDisplay returns the 39-character display form: the 32 Base32
// characters split into eight groups of four, separated by one ASCII space.
func (s TOTPSecret) GroupedDisplay() string {
	encoded := s.Base32()
	var b strings.Builder
	b.Grow(len(encoded) + len(encoded)/totpGroupSize)
	for i := 0; i < len(encoded); i++ {
		if i > 0 && i%totpGroupSize == 0 {
			b.WriteByte(' ')
		}
		b.WriteByte(encoded[i])
	}
	return b.String()
}

// VerifyTOTPCode checks code against the previous, current, and next
// 30-second steps at now, comparing every candidate in constant time, and
// returns the greatest matching step. It stores no state and excludes no step
// by prior use: the caller compares the returned step against any stored
// last-used step, so this function serves both pending verification, which
// requires a strictly greater step, and enrollment proof, which has none
// ("TOTP profile and code verification", "Enrollment proof has no stored
// step").
func VerifyTOTPCode(secret TOTPSecret, code string, now time.Time) (step uint64, matched bool, err error) {
	if !validTOTPCode(code) {
		return 0, false, ErrTOTPMalformedCode
	}
	candidates, err := totpSteps(now)
	if err != nil {
		return 0, false, err
	}
	for _, candidate := range candidates {
		generated := totpCode(secret, candidate)
		if subtle.ConstantTimeCompare([]byte(generated), []byte(code)) == 1 {
			step = candidate
			matched = true
		}
	}
	return step, matched, nil
}

// validTOTPCode reports whether code is exactly six ASCII decimal digits.
// Spaces, hyphens, signs, and non-ASCII digits all fail here, before any
// cryptographic work ("TOTP profile and code verification").
func validTOTPCode(code string) bool {
	if len(code) != totpDigits {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// totpSteps returns the eligible nonnegative previous, current, and next time
// steps at now, in increasing order. The previous step is omitted at the
// current step's first period, so verification never wraps a negative step
// into a huge unsigned counter.
func totpSteps(now time.Time) ([]uint64, error) {
	unix := now.Unix()
	if unix < 0 {
		return nil, ErrTOTPNegativeTime
	}
	current := uint64(unix) / totpPeriodSeconds
	steps := make([]uint64, 0, 3)
	if current > 0 {
		steps = append(steps, current-1)
	}
	steps = append(steps, current, current+1)
	return steps, nil
}

// totpCode computes the RFC 4226 HOTP value for secret and counter, truncated
// to totpDigits decimal digits with leading zeroes.
func totpCode(secret TOTPSecret, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, secret[:]) //nolint:gosec // ADR 0049 approves HMAC-SHA-1 only for the RFC 6238 interoperability profile
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	code %= 1000000
	return fmt.Sprintf("%0*d", totpDigits, code)
}
