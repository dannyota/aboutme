package secondfactor

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // ADR 0017 approves HMAC-SHA-1 only for the RFC 6238 interoperability profile
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	totpSecretBytes   = 20
	totpDigits        = 6
	totpPeriodSeconds = 30
	totpGroupSize     = 4
)

var ErrTOTPMalformedCode = errors.New("secondfactor: totp code is not six ascii digits")

var ErrTOTPNegativeTime = errors.New("secondfactor: totp time is before the unix epoch")

var totpBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

type TOTPSecret [totpSecretBytes]byte

func GenerateTOTPSecret(entropy io.Reader) (TOTPSecret, error) {
	var secret TOTPSecret
	if _, err := io.ReadFull(entropy, secret[:]); err != nil {
		return TOTPSecret{}, fmt.Errorf("secondfactor: totp secret entropy: %w", err)
	}
	return secret, nil
}

func (s TOTPSecret) Base32() string {
	return totpBase32.EncodeToString(s[:])
}

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

func totpCode(secret TOTPSecret, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, secret[:]) //nolint:gosec // ADR 0017 approves HMAC-SHA-1 only for the RFC 6238 interoperability profile
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	code %= 1000000
	return fmt.Sprintf("%0*d", totpDigits, code)
}
