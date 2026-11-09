package main

import (
	"context"
	"errors"
	"time"
)

// verifyFinal proves one new private Vietnamese target, the unchanged
// English source and photo, the unchanged baseline, and count delta one. It
// returns the server Date of these reads.
func (r *ownerRun) verifyFinal(ctx context.Context) (time.Time, error) {
	mark := r.deps.Observations.mark()
	listing, err := r.guard.list(ctx)
	if err != nil || len(listing) != len(r.baseline)+1 || !distinctIDs(listing) {
		return time.Time{}, errWorkflowContract
	}
	var others []resumeSummary
	for _, item := range listing {
		if item.ID == string(r.target) {
			if item.Lng != targetLanguage || item.Live || item.Revision != r.revision {
				return time.Time{}, errWorkflowContract
			}
			continue
		}
		others = append(others, item)
	}
	if !sameListing(r.baseline, others) {
		return time.Time{}, errSourceChanged
	}
	source, err := readSource(ctx, r.guard, r.source.Handle)
	if err != nil || !sameSnapshot(r.source, source) {
		return time.Time{}, errSourceChanged
	}
	state, err := r.guard.getResume(ctx, r.target)
	if err != nil || state.Revision != r.revision {
		return time.Time{}, errWorkflowContract
	}
	photo, exact := validTarget(state, r.intent.Payload)
	if !exact {
		return time.Time{}, errWorkflowContract
	}
	if r.source.Photo == nil {
		if photo != nil {
			return time.Time{}, errPhoto
		}
	} else {
		if photo == nil {
			return time.Time{}, errPhoto
		}
		targetPhoto, photoErr := r.guard.getPhoto(ctx, r.target)
		if photoErr != nil {
			return time.Time{}, photoErr
		}
		if parityErr := validatePhotoParity(*r.source.Photo, r.source.Crop, targetPhoto, cropFromSchema(photo.Crop)); parityErr != nil {
			return time.Time{}, parityErr
		}
	}
	confirmedAt, dated := r.deps.Observations.authenticatedDateSince(mark)
	if !dated {
		return time.Time{}, errWorkflowBlocked
	}
	return confirmedAt, nil
}

// complete journals revocation authority, makes the sentinel durable, drops
// the resolved intent, inspects the target without media, revokes, and
// requires the next SDK request to receive 401 and a refused reauthorization.
func (r *ownerRun) complete(ctx context.Context, confirmedAt time.Time) error {
	journal, err := r.prepareJournal(ctx, confirmedAt)
	if err != nil {
		return err
	}
	if persistErr := persistJournal(r.config.Control, journal, r.config.Origin, false); persistErr != nil {
		return persistErr
	}
	if sentinelErr := ensureSentinel(r.config.Control, journal, r.config.Origin); sentinelErr != nil {
		return sentinelErr
	}
	if removeErr := r.config.Control.remove(createIntentName); removeErr != nil {
		return removeErr
	}
	state, err := r.guard.getResume(ctx, r.target)
	if err != nil || state.Revision != r.revision {
		return errWorkflowContract
	}
	if _, exact := validTarget(state, r.intent.Payload); !exact {
		return errWorkflowContract
	}
	mark := r.deps.Observations.mark()
	revokeErr := r.deps.Revocation.Revoke(ctx, journal.RefreshToken)
	_, listErr := r.guard.list(ctx)
	deniedAt, denied := r.deps.Observations.deniedSince(mark, digest([]byte(journal.AccessToken)))
	refused := r.deps.Observations.reauthorizationRefusedSince(mark)
	sdkProof := listErr != nil && denied && deniedAt.Before(journal.AccessUntil) && refused
	if !sdkProof {
		if settleErr := settleRevocation(ctx, r.deps.Revocation, r.config.Control, r.config.Origin, journal); settleErr != nil {
			return errors.Join(settleErr, revokeErr, writeEvidence(r.config.Run, workflowEvidence(r.config.Mode, r.outcome, revocationUnconfirmed, false)))
		}
		current, present, loadErr := loadJournal(r.config.Control, r.config.Origin)
		if loadErr != nil || !present {
			return errRecovery
		}
		journal = current
	}
	if finishErr := finishRevocation(r.config.Control, journal, r.config.Origin, r.config.local()); finishErr != nil {
		return finishErr
	}
	if evidenceErr := writeEvidence(r.config.Run, workflowEvidence(r.config.Mode, r.outcome, revocationRevoked, sdkProof)); evidenceErr != nil {
		return evidenceErr
	}
	if !sdkProof {
		return errReauthorizationUnproved
	}
	return nil
}

var errReauthorizationUnproved = errors.New("post_revocation_proof_incomplete")

// prepareJournal binds the current grant to server-bound cutoffs: access 55
// minutes after the latest token response Date and refresh 29 days after the
// first. When the current grant cannot be read, it journals the grant captured
// before create, which is always a valid revocation authority.
func (r *ownerRun) prepareJournal(ctx context.Context, confirmedAt time.Time) (revocationJournal, error) {
	grant := r.grant
	accessToken, refreshToken, err := r.deps.Tokens(ctx)
	first, latest, dated := r.deps.Observations.tokenDates()
	if err == nil && dated && validBearerValue(accessToken) && validBearerValue(refreshToken) && accessToken != refreshToken {
		grant = grantSnapshot{accessToken: accessToken, refreshToken: refreshToken, firstDate: first, latestDate: latest}
	}
	if journal, ok := r.journalFor(grant, confirmedAt); ok {
		return journal, nil
	}
	if journal, ok := r.journalFor(r.grant, confirmedAt); ok {
		return journal, nil
	}
	return revocationJournal{}, errRecovery
}

func (r *ownerRun) journalFor(grant grantSnapshot, confirmedAt time.Time) (revocationJournal, bool) {
	journal := revocationJournal{
		Version: journalVersion, Origin: r.config.Origin, Workflow: workflowLabel, TargetConfirmed: true,
		TargetConfirmedAt: confirmedAt, RevokeEndpoint: r.config.Origin + "/oauth/revoke", AccessToken: grant.accessToken,
		RefreshToken: grant.refreshToken, TokenTypeHint: tokenHintRefresh, AccessUntil: grant.latestDate.Add(accessRevocationCutoff),
		RefreshUntil: grant.firstDate.Add(refreshRevocationCutoff), Status: statusPending,
	}
	if journal.AccessUntil.After(journal.RefreshUntil) {
		journal.AccessUntil = journal.RefreshUntil
	}
	return journal, !grant.firstDate.IsZero() && validJournal(journal, r.config.Origin)
}
