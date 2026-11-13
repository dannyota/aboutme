package password

import (
	"context"
	"crypto/sha256"
	"errors"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	ErrPasswordLength   = errors.New("password length invalid")
	ErrPasswordCommon   = errors.New("password is common")
	ErrPasswordBreached = errors.New("password is breached")
)

const (
	MaxRawBytes   = 1024
	MinCodePoints = 15
	MaxCodePoints = 128
)

type BreachChecker interface {
	Breached(context.Context, string) (bool, error)
}

type Policy struct {
	blocklist *Blocklist
	breach    BreachChecker
}

type CheckResult struct {
	Normalized string
}

func NewPolicy(blocklist *Blocklist, breach BreachChecker) *Policy {
	return &Policy{blocklist: blocklist, breach: breach}
}

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
