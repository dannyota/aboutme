package oauthsrv

import (
	"errors"
	"strings"
)

var ErrScopeInvalid = errors.New("oauth scope invalid")

type Scope string

const (
	ScopeResumesRead  Scope = "resumes:read"
	ScopeResumesWrite Scope = "resumes:write"
)

func canonicalScopes() [2]Scope { return [2]Scope{ScopeResumesRead, ScopeResumesWrite} }

const scopesMaxBytes = len(ScopeResumesRead) + 1 + len(ScopeResumesWrite)

type Scopes []Scope

func ParseScopes(raw string) (Scopes, error) {
	if len(raw) == 0 || len(raw) > scopesMaxBytes {
		return nil, ErrScopeInvalid
	}
	scopes := canonicalScopes()
	var granted [len(scopes)]bool
	for _, field := range strings.Split(raw, " ") {
		index, ok := canonicalScopeIndex(field)
		if !ok || granted[index] {
			return nil, ErrScopeInvalid
		}
		granted[index] = true
	}
	out := make(Scopes, 0, len(scopes))
	for i, ok := range granted {
		if ok {
			out = append(out, scopes[i])
		}
	}
	return out, nil
}

func (s Scopes) String() string {
	parts := make([]string, 0, len(s))
	for _, scope := range s {
		parts = append(parts, string(scope))
	}
	return strings.Join(parts, " ")
}

func (s Scopes) Has(scope Scope) bool {
	for _, granted := range s {
		if granted == scope {
			return true
		}
	}
	return false
}

func canonicalScopeIndex(field string) (int, bool) {
	for i, scope := range canonicalScopes() {
		if field == string(scope) {
			return i, true
		}
	}
	return 0, false
}
