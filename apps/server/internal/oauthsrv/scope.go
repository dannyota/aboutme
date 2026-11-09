package oauthsrv

import (
	"errors"
	"strings"
)

// ErrScopeInvalid is the closed error for every scope parameter outside the
// set or the delimiter rules. It never carries the presented value.
var ErrScopeInvalid = errors.New("oauth scope invalid")

// Scope is one member of the closed scope set.
type Scope string

// The closed scope set. Read tools require ScopeResumesRead; every mutating
// tool requires ScopeResumesWrite.
const (
	// ScopeResumesRead grants the read tools over every resume of the account.
	ScopeResumesRead Scope = "resumes:read"
	// ScopeResumesWrite grants the mutating tools over every resume of the
	// account.
	ScopeResumesWrite Scope = "resumes:write"
)

// canonicalScopes is the closed set in canonical order. Every parsed result is
// a subsequence of it, so one granted set has exactly one stored spelling.
func canonicalScopes() [2]Scope { return [2]Scope{ScopeResumesRead, ScopeResumesWrite} }

// scopesMaxBytes is the length of the longest valid scope parameter — the
// whole closed set with one space between members — so a longer input is
// rejected before it is split.
const scopesMaxBytes = len(ScopeResumesRead) + 1 + len(ScopeResumesWrite)

// Scopes is a set of granted scopes held in canonical order.
type Scopes []Scope

// ParseScopes parses an OAuth scope parameter into the closed scope set. Members
// are separated by exactly one space, must all be inside the set, and may not
// repeat; the result is in canonical order whatever order the request used. An
// empty parameter is invalid: a caller that treats an absent scope as a
// default decides that for itself.
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

// String renders the scopes as the canonical space-delimited OAuth scope
// parameter. The zero value renders as the empty string.
func (s Scopes) String() string {
	parts := make([]string, 0, len(s))
	for _, scope := range s {
		parts = append(parts, string(scope))
	}
	return strings.Join(parts, " ")
}

// Has reports whether the set contains scope.
func (s Scopes) Has(scope Scope) bool {
	for _, granted := range s {
		if granted == scope {
			return true
		}
	}
	return false
}

// canonicalScopeIndex returns the closed-set index of field.
func canonicalScopeIndex(field string) (int, bool) {
	for i, scope := range canonicalScopes() {
		if field == string(scope) {
			return i, true
		}
	}
	return 0, false
}
