package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

var (
	errRecovery   = errors.New("recovery_required")
	errRevocation = errors.New("revocation_unconfirmed")
)

const (
	journalVersion   = 1
	workflowLabel    = "owner-vi-copy"
	tokenHintRefresh = "refresh_token"
	statusPending    = "pending"
	// accessRevocationCutoff and refreshRevocationCutoff are the design's
	// conservative bounds after the token response's server Date.
	accessRevocationCutoff  = 55 * time.Minute
	refreshRevocationCutoff = 29 * 24 * time.Hour
	// maxRevocationRounds allows the first proof plus one after a rotation.
	maxRevocationRounds = 2
)

// completionSentinel is the durable one-shot control. It holds no owner or
// grant identifier.
type completionSentinel struct {
	Version           int       `json:"version"`
	Origin            string    `json:"origin"`
	Workflow          string    `json:"workflow"`
	TargetConfirmed   bool      `json:"target_confirmed"`
	TargetConfirmedAt time.Time `json:"target_confirmed_at"`
}

// revocationJournal is the secret-bearing revocation authority. It is
// written before the sentinel and removed only after a timely proof.
type revocationJournal struct {
	Version           int       `json:"version"`
	Origin            string    `json:"origin"`
	Workflow          string    `json:"workflow"`
	TargetConfirmed   bool      `json:"target_confirmed"`
	TargetConfirmedAt time.Time `json:"target_confirmed_at"`
	RevokeEndpoint    string    `json:"revoke_endpoint"`
	AccessToken       string    `json:"access_token"`
	RefreshToken      string    `json:"refresh_token"`
	TokenTypeHint     string    `json:"token_type_hint"`
	AccessUntil       time.Time `json:"access_until"`
	RefreshUntil      time.Time `json:"refresh_until"`
	Status            string    `json:"status"`
}

func (j revocationJournal) sentinel() completionSentinel {
	return completionSentinel{Version: j.Version, Origin: j.Origin, Workflow: j.Workflow, TargetConfirmed: j.TargetConfirmed, TargetConfirmedAt: j.TargetConfirmedAt}
}

func validSentinel(value completionSentinel, origin string) bool {
	return value.Version == journalVersion && value.Origin == origin && value.Workflow == workflowLabel &&
		value.TargetConfirmed && !value.TargetConfirmedAt.IsZero()
}

func validJournal(journal revocationJournal, origin string) bool {
	return validSentinel(journal.sentinel(), origin) && journal.RevokeEndpoint == origin+"/oauth/revoke" &&
		validBearerValue(journal.AccessToken) && validBearerValue(journal.RefreshToken) && journal.AccessToken != journal.RefreshToken &&
		journal.TokenTypeHint == tokenHintRefresh && journal.Status == statusPending &&
		!journal.AccessUntil.IsZero() && !journal.RefreshUntil.IsZero() && !journal.AccessUntil.After(journal.RefreshUntil)
}

// validBearerValue accepts only visible ASCII so a token can never inject a
// header or form field.
func validBearerValue(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, r := range value {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}

func loadSentinel(control privateArtifacts, origin string) (completionSentinel, bool, error) {
	present, err := control.exists(completionName)
	if err != nil {
		return completionSentinel{}, false, errRecovery
	}
	if !present {
		return completionSentinel{}, false, nil
	}
	data, err := control.read(completionName)
	var sentinel completionSentinel
	if err != nil || !strictJSON(data, &sentinel) || !validSentinel(sentinel, origin) {
		return completionSentinel{}, true, errRecovery
	}
	return sentinel, true, nil
}

func loadJournal(control privateArtifacts, origin string) (revocationJournal, bool, error) {
	present, err := control.exists(revocationName)
	if err != nil {
		return revocationJournal{}, false, errRecovery
	}
	if !present {
		return revocationJournal{}, false, nil
	}
	data, err := control.read(revocationName)
	var journal revocationJournal
	if err != nil || !strictJSON(data, &journal) || !validJournal(journal, origin) {
		return revocationJournal{}, true, errRecovery
	}
	return journal, true, nil
}

// persistJournal atomically writes and syncs the journal. replace is false for
// the first journal, so an unresolved journal is never overwritten by a new
// grant.
func persistJournal(control privateArtifacts, journal revocationJournal, origin string, replace bool) error {
	if !validJournal(journal, origin) {
		return errRecovery
	}
	if present, existsErr := control.exists(revocationName); existsErr != nil || present != replace {
		return errRecovery
	}
	data, err := json.Marshal(journal) //nolint:gosec // the journal is the design's private mode-0600 revocation authority, never output.
	if err != nil {
		return errRecovery
	}
	if replace {
		return control.write(revocationName, data)
	}
	return control.createExclusive(revocationName, data)
}

// ensureSentinel makes the completion sentinel durable from the journal's
// attested fields. An existing sentinel must match exactly.
func ensureSentinel(control privateArtifacts, journal revocationJournal, origin string) error {
	expected := journal.sentinel()
	if !validSentinel(expected, origin) {
		return errRecovery
	}
	existing, present, err := loadSentinel(control, origin)
	if err != nil {
		return errRecovery
	}
	if present {
		if existing.Version != expected.Version || existing.Origin != expected.Origin || existing.Workflow != expected.Workflow ||
			existing.TargetConfirmed != expected.TargetConfirmed || !existing.TargetConfirmedAt.Equal(expected.TargetConfirmedAt) {
			return errRecovery
		}
		return nil
	}
	data, err := json.Marshal(expected)
	if err != nil {
		return errRecovery
	}
	if createErr := control.createExclusive(completionName, data); createErr != nil {
		return errRecovery
	}
	return nil
}

// revocationTransport is the narrow RFC 7009, probe, and refresh surface.
type revocationTransport interface {
	Revoke(ctx context.Context, refreshToken string) error
	Probe(ctx context.Context, accessToken string) (status int, serverDate time.Time, err error)
	Refresh(ctx context.Context, refreshToken string) (refreshOutcome, error)
}

type refreshOutcome struct {
	Dead         bool
	AccessToken  string
	RefreshToken string
	ServerDate   time.Time
}

// settleRevocation revokes and proves the grant dead. A timely 401 or a timely
// invalid_grant proves revocation. A rotation proves the grant is live: the
// successor is journaled before it is revoked. Anything else keeps the journal.
func settleRevocation(ctx context.Context, transport revocationTransport, control privateArtifacts, origin string, journal revocationJournal) error {
	for round := 0; round < maxRevocationRounds; round++ {
		// The revoke result is not a proof; a lost response is settled below.
		revokeErr := transport.Revoke(ctx, journal.RefreshToken)
		status, probeDate, probeErr := transport.Probe(ctx, journal.AccessToken)
		switch {
		case probeErr != nil || probeDate.IsZero():
			return errors.Join(errRevocation, revokeErr)
		case probeDate.Before(journal.AccessUntil) && status == http.StatusUnauthorized:
			return nil
		case probeDate.Before(journal.AccessUntil) || !probeDate.Before(journal.RefreshUntil):
			return errors.Join(errRevocation, revokeErr)
		}
		outcome, err := transport.Refresh(ctx, journal.RefreshToken)
		if err != nil || outcome.ServerDate.IsZero() || !outcome.ServerDate.Before(journal.RefreshUntil) {
			return errRevocation
		}
		if outcome.Dead {
			return nil
		}
		successor := journal
		successor.AccessToken, successor.RefreshToken = outcome.AccessToken, outcome.RefreshToken
		successor.AccessUntil = outcome.ServerDate.Add(accessRevocationCutoff)
		if successor.AccessUntil.After(successor.RefreshUntil) {
			successor.AccessUntil = successor.RefreshUntil
		}
		if persistErr := persistJournal(control, successor, origin, true); persistErr != nil {
			return errRevocation
		}
		journal = successor
	}
	return errRevocation
}

// finishRevocation deletes the journal only after proof and a durable
// sentinel, then drops the inert create intent. Local synthetic runs also
// remove their sentinel so the separate local root starts clean.
func finishRevocation(control privateArtifacts, journal revocationJournal, origin string, local bool) error {
	if err := ensureSentinel(control, journal, origin); err != nil {
		return err
	}
	if err := control.remove(revocationName); err != nil {
		return errRecovery
	}
	if err := control.remove(createIntentName); err != nil {
		return errRecovery
	}
	if local {
		return control.remove(completionName)
	}
	return nil
}

// recoverRevocationOnly is the only path when a journal exists. It never
// initializes SDK OAuth, opens a browser, or reads resumes.
func recoverRevocationOnly(ctx context.Context, transport revocationTransport, control privateArtifacts, origin string, local bool) error {
	journal, present, err := loadJournal(control, origin)
	if err != nil || !present {
		return errRecovery
	}
	if sentinelErr := ensureSentinel(control, journal, origin); sentinelErr != nil {
		return errRecovery
	}
	if removeErr := control.remove(createIntentName); removeErr != nil {
		return errRecovery
	}
	if settleErr := settleRevocation(ctx, transport, control, origin, journal); settleErr != nil {
		return settleErr
	}
	current, present, err := loadJournal(control, origin)
	if err != nil || !present {
		return errRecovery
	}
	return finishRevocation(control, current, origin, local)
}
