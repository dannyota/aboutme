package oauthsrv

import (
	"errors"
	"slices"
	"strings"
)

var ErrScopeInvalid = errors.New("oauth scope invalid")

type Scope string

const (
	ScopeResumesRead   Scope = "resumes:read"
	ScopeResumesWrite  Scope = "resumes:write"
	ScopeProjectsRead  Scope = "projects:read"
	ScopeProjectsWrite Scope = "projects:write"
)

func requestableScopes() []Scope { return []Scope{ScopeResumesRead, ScopeResumesWrite} }

func storableScopes() []Scope {
	return []Scope{ScopeResumesRead, ScopeResumesWrite, ScopeProjectsRead, ScopeProjectsWrite}
}

type Scopes []Scope

func ParseScopes(raw string) (Scopes, error) {
	return parseScopeSet(raw, requestableScopes())
}

func ParseStoredScopes(raw string) (Scopes, error) {
	scopes, err := parseScopeSet(raw, storableScopes())
	if err != nil || scopes.String() != raw {
		return nil, ErrScopeInvalid
	}
	return scopes, nil
}

func parseScopeSet(raw string, known []Scope) (Scopes, error) {
	if len(raw) == 0 || len(raw) > joinedScopeBytes(known) {
		return nil, ErrScopeInvalid
	}
	granted := make([]bool, len(known))
	for _, field := range strings.Split(raw, " ") {
		index := slices.Index(known, Scope(field))
		if index < 0 || granted[index] {
			return nil, ErrScopeInvalid
		}
		granted[index] = true
	}
	out := make(Scopes, 0, len(known))
	for i, ok := range granted {
		if ok {
			out = append(out, known[i])
		}
	}
	return out, nil
}

func joinedScopeBytes(scopes []Scope) int {
	total := len(scopes) - 1
	for _, scope := range scopes {
		total += len(scope)
	}
	return total
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
