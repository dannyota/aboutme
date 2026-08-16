package password

import (
	"context"
	"crypto/sha256"
	"errors"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Closed policy errors (D2). Errors never contain input or dependency text.
var (
	ErrPasswordLength   = errors.New("password length invalid")
	ErrPasswordCommon   = errors.New("password is common")
	ErrPasswordBreached = errors.New("password is breached")
)

// Password input bounds (D2): at most 1024 raw UTF-8 bytes before NFC, then
// 15–128 Unicode code points after NFC. Controls, spaces, and case are all
// preserved; normalization only folds canonically-equivalent Unicode.
const (
	MaxRawBytes   = 1024
	MinCodePoints = 15
	MaxCodePoints = 128
)

// BreachChecker reports whether a normalized password is in a breach corpus.
type BreachChecker interface {
	Breached(context.Context, string) (bool, error)
}

// Policy checks candidate new passwords: blocklist and breach lookup.
type Policy struct {
	blocklist *Blocklist
	breach    BreachChecker
}

// CheckResult carries the NFC-normalized password on a successful check.
type CheckResult struct {
	Normalized string
}

// NewPolicy returns a policy with the given blocklist and breach checker.
// Either may be nil to disable that stage (used by focused tests); production
// supplies both.
func NewPolicy(blocklist *Blocklist, breach BreachChecker) *Policy {
	return &Policy{blocklist: blocklist, breach: breach}
}

// Normalize applies the shared password input bounds. Login normalizes and
// bounds through this exact function but does not call the blocklist or HIBP.
func Normalize(raw string) (string, error) {
	if len(raw) > MaxRawBytes {
		return "", ErrPasswordLength
	}
	normalized := norm.NFC.String(raw)
	if n := utf8.RuneCountInString(normalized); n < MinCodePoints || n > MaxCodePoints {
		return "", ErrPasswordLength
	}
	return normalized, nil
}

// CheckNew runs the full new-password check in order: raw byte cap, NFC and
// code-point bounds, blocklist digest, then HIBP breach lookup. It returns the
// normalized password only when every stage passes.
func (p *Policy) CheckNew(ctx context.Context, raw string) (CheckResult, error) {
	normalized, err := Normalize(raw)
	if err != nil {
		return CheckResult{}, err
	}
	if p.blocklist != nil {
		digest := sha256.Sum256([]byte(normalized))
		if p.blocklist.Contains(digest) {
			return CheckResult{}, ErrPasswordCommon
		}
	}
	if p.breach != nil {
		breached, err := p.breach.Breached(ctx, normalized)
		if err != nil {
			return CheckResult{}, err
		}
		if breached {
			return CheckResult{}, ErrPasswordBreached
		}
	}
	return CheckResult{Normalized: normalized}, nil
}
