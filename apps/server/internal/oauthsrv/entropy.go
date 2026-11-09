package oauthsrv

import (
	"io"
	"sync"
)

// lockedEntropy is a Service's entropy reader and the lock that serializes
// its reads. io.Reader promises no concurrent safety, while consent approvals
// and token exchanges draw secrets together.
type lockedEntropy struct {
	mu     sync.Mutex
	reader io.Reader
}

// tokenPair is one freshly drawn access token and refresh token with the
// SHA-256 digests that are their only stored forms.
type tokenPair struct {
	access        string
	accessDigest  [32]byte
	refresh       string
	refreshDigest [32]byte
}

// newCode draws one authorization code while holding the entropy lock.
func (s *Service) newCode() (string, [32]byte, error) {
	s.entropy.mu.Lock()
	defer s.entropy.mu.Unlock()
	return NewCode(s.entropy.reader)
}

// newTokenPair draws an access token and then a refresh token under one hold
// of the entropy lock, so the pair reads adjacent bytes from the reader. It
// returns no pair when either draw fails.
func (s *Service) newTokenPair() (tokenPair, error) {
	s.entropy.mu.Lock()
	defer s.entropy.mu.Unlock()
	access, accessDigest, err := NewToken(TokenKindAccess, s.entropy.reader)
	if err != nil {
		return tokenPair{}, err
	}
	refresh, refreshDigest, err := NewToken(TokenKindRefresh, s.entropy.reader)
	if err != nil {
		return tokenPair{}, err
	}
	return tokenPair{access: access, accessDigest: accessDigest, refresh: refresh, refreshDigest: refreshDigest}, nil
}
