package oauthsrv

import (
	"io"
	"sync"
)

type lockedEntropy struct {
	mu     sync.Mutex
	reader io.Reader
}

type tokenPair struct {
	access        string
	accessDigest  [32]byte
	refresh       string
	refreshDigest [32]byte
}

func (s *Service) newCode() (string, [32]byte, error) {
	s.entropy.mu.Lock()
	defer s.entropy.mu.Unlock()
	return NewCode(s.entropy.reader)
}

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
