package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

// listedHandle reads a listed resume whose role is not yet known. It cannot
// reach a mutation; only toolGuard.bindTarget produces a targetHandle.
type listedHandle string

func (h listedHandle) resumeID() string { return string(h) }

// ownerRun carries one process's workflow state after OAuth succeeds.
type ownerRun struct {
	config   workflowConfig
	deps     workflowDeps
	guard    *toolGuard
	baseline []resumeSummary
	source   sourceSnapshot
	intent   createIntent
	target   targetHandle
	revision string
	outcome  string
	grant    grantSnapshot
}

// grantSnapshot is the revocation authority captured before any create. It
// lets completion journal a known-valid grant, so a create is never followed
// by a failure that leaves no revocation state.
type grantSnapshot struct {
	accessToken  string
	refreshToken string
	firstDate    time.Time
	latestDate   time.Time
}

// runOwnerWorkflow is the only normal path: SDK connect, exact tool
// discovery, source reads, candidate handoff, one create intent, photo copy,
// verification, durable completion, and revocation. See
// docs/design/mcp-owner-workflow.md#workflow.
func runOwnerWorkflow(ctx context.Context, config workflowConfig, deps workflowDeps, intent createIntent, intentPresent bool) (err error) {
	defer func() {
		if cleanupErr := removeOwnerContent(config.Run); cleanupErr != nil && err == nil {
			err = cleanupErr
		}
	}()
	client, err := deps.Connect(ctx)
	if err != nil {
		return err
	}
	revoked := false
	defer func() {
		// After revocation the session close is itself refused by the server,
		// so a close error matters only before the grant is revoked.
		if closeErr := client.Close(); closeErr != nil && err == nil && !revoked {
			err = errWorkflowBlocked
		}
	}()
	if _, discoverErr := discoverTools(ctx, client); discoverErr != nil {
		return discoverErr
	}
	owner := &ownerRun{config: config, deps: deps, guard: newToolGuard(client, config.local())}
	if grantErr := owner.captureGrant(ctx); grantErr != nil {
		return grantErr
	}
	// A failure between here and complete leaves no revocation journal, so
	// nothing else would ever revoke this grant; complete owns revocation
	// once the run reaches it. A live create intent means a mutation may
	// still be pending server side, and a restart needs this same grant to
	// reconcile it, so the revoke waits for that intent to clear first. The
	// call is best effort and never touches err, so the runner still reports
	// the one closed word its original failure named. See
	// docs/design/mcp-owner-workflow.md#privacy-revocation-and-evidence.
	defer func() {
		if err == nil || revoked {
			return
		}
		if present, existsErr := config.Control.exists(createIntentName); existsErr != nil || present {
			return
		}
		if revokeErr := deps.Revocation.Revoke(context.WithoutCancel(ctx), owner.grant.refreshToken); revokeErr != nil {
			// Best effort: a failed cleanup revoke still leaves err (and the
			// closed word it reports) untouched, and a later run or the
			// grant's own 30-day expiry still closes it eventually.
			return
		}
	}()
	if intentPresent {
		err = owner.reconcile(ctx, intent)
	} else {
		err = owner.createFresh(ctx)
	}
	if err != nil {
		return err
	}
	if owner.config.local() && owner.outcome == reconcileCreated {
		if proofErr := owner.proveLocalReplay(ctx); proofErr != nil {
			return proofErr
		}
	}
	if copyErr := owner.copyPhoto(ctx); copyErr != nil {
		return copyErr
	}
	if owner.config.local() {
		if proofErr := owner.proveLocalConflict(ctx); proofErr != nil {
			return proofErr
		}
	}
	confirmedAt, err := owner.verifyFinal(ctx)
	if err != nil {
		return err
	}
	revoked = true
	return owner.complete(ctx, confirmedAt)
}

// createFresh enforces the account shape, reads the source and photo, hands
// the source to the manager, validates the reviewed candidate, re-reads the
// source, persists the intent, and sends it once.
func (r *ownerRun) createFresh(ctx context.Context) error {
	listing, err := r.guard.list(ctx)
	if err != nil {
		return err
	}
	sourceID, err := selectEnglishSource(listing)
	if err != nil {
		return err
	}
	if r.config.Mode == modeProduction && existingTarget(listing, sourceID) {
		return errSourceSelection
	}
	if sourceErr := r.guard.setSource(sourceID); sourceErr != nil {
		return sourceErr
	}
	first, err := readSource(ctx, r.guard, sourceID)
	if err != nil {
		return err
	}
	if _, writeErr := writeSourceArtifact(r.config.Run, first); writeErr != nil {
		return writeErr
	}
	if waitErr := r.deps.WaitCandidate(ctx, r.config.Run); waitErr != nil {
		return errCandidate
	}
	payload, err := loadReviewedCandidate(r.config.Run, first)
	if err != nil {
		return err
	}
	mark := r.deps.Observations.mark()
	reread, err := r.guard.list(ctx)
	if err != nil || !sameListing(listing, reread) {
		return errSourceChanged
	}
	second, err := readSource(ctx, r.guard, sourceID)
	if err != nil || !sameSnapshot(first, second) {
		return errSourceChanged
	}
	serverTime, dated := r.deps.Observations.authenticatedDateSince(mark)
	observedAt := r.deps.LocalNow()
	if !dated {
		return errWorkflowBlocked
	}
	key, err := r.deps.NewKey()
	if err != nil || !validUUID(key) {
		return errWorkflowBlocked
	}
	r.baseline, r.source = reread, second
	r.intent = newCreateIntent(r.config.Origin, reread, second, payload, key, serverTime)
	if persistErr := persistCreateIntent(r.config.Control, r.intent); persistErr != nil {
		return persistErr
	}
	if bindErr := r.guard.bindIntent(r.intent, true); bindErr != nil {
		return bindErr
	}
	if !r.deps.LocalNow().Before(observedAt.Add(createSendWindow)) {
		return errRecovery
	}
	state, outcome, err := r.guard.create(ctx, observedAt.Add(createSendWindow))
	switch outcome {
	case createCreated:
		return r.acceptCreated(state, reconcileCreated)
	case createDefinitiveFailure:
		if removeErr := r.config.Control.remove(createIntentName); removeErr != nil {
			return removeErr
		}
		return err
	default:
		// An unknown outcome is settled by reads only in this process. A later
		// run may replay the same key before the cutoff.
		return r.reconcileByReads(ctx, errors.Join(errRecovery, err))
	}
}

// reconcile resumes an unresolved intent. It binds the account and source,
// finds an exact target by reads, and otherwise replays the exact request
// only inside the 12-hour window.
func (r *ownerRun) reconcile(ctx context.Context, intent createIntent) error {
	r.intent = intent
	if sourceErr := r.guard.setSource(intent.SourceID); sourceErr != nil {
		return sourceErr
	}
	if bindErr := r.guard.bindIntent(intent, false); bindErr != nil {
		return bindErr
	}
	mark := r.deps.Observations.mark()
	listing, err := r.guard.list(ctx)
	if err != nil {
		return err
	}
	serverNow, dated := r.deps.Observations.authenticatedDateSince(mark)
	observedAt := r.deps.LocalNow()
	baseline, extras, err := splitBaseline(listing, intent)
	if err != nil {
		return err
	}
	source, err := readSource(ctx, r.guard, intent.SourceID)
	if err != nil || !sourceMatchesIntent(source, intent) {
		return errSourceChanged
	}
	r.baseline, r.source = baseline, source
	if len(extras) == 1 {
		return r.acceptListedTarget(ctx, extras[0], reconcileReconciled)
	}
	deadline, allowed := replayDeadline(intent, serverNow, observedAt)
	if !dated || !allowed {
		return errRecovery
	}
	state, outcome, err := r.guard.create(ctx, deadline)
	if outcome != createCreated {
		return errors.Join(errRecovery, err)
	}
	return r.acceptCreated(state, reconcileReplayed)
}

// reconcileByReads settles an unknown create in the same process: exactly one
// exact new target is accepted; anything else keeps the intent.
func (r *ownerRun) reconcileByReads(ctx context.Context, cause error) error {
	listing, err := r.guard.list(ctx)
	if err != nil {
		return cause
	}
	_, extras, err := splitBaseline(listing, r.intent)
	if err != nil || len(extras) != 1 {
		return cause
	}
	if acceptErr := r.acceptListedTarget(ctx, extras[0], reconcileReconciled); acceptErr != nil {
		return errors.Join(cause, acceptErr)
	}
	return nil
}

// splitBaseline requires every baseline resume and at most one new resume.
func splitBaseline(listing []resumeSummary, intent createIntent) ([]resumeSummary, []resumeSummary, error) {
	if len(listing) > maxListedResumes || !distinctIDs(listing) {
		return nil, nil, errRecovery
	}
	expected := make(map[string]bool, len(intent.BaselineIDs))
	for _, id := range intent.BaselineIDs {
		expected[id] = true
	}
	var baseline, extras []resumeSummary
	for _, item := range listing {
		if expected[item.ID] {
			baseline = append(baseline, item)
		} else {
			extras = append(extras, item)
		}
	}
	if len(baseline) != len(intent.BaselineIDs) || len(extras) > 1 ||
		accountBinding(intent.Origin, listingIDs(baseline)) != intent.AccountBinding {
		return nil, nil, errRecovery
	}
	return baseline, extras, nil
}

func sourceMatchesIntent(source sourceSnapshot, intent createIntent) bool {
	photoDigest := ""
	if source.Photo != nil {
		photoDigest = digest(source.Photo.Data)
	}
	return source.Handle == intent.SourceID && source.State.Revision == intent.SourceRevision &&
		source.State.Live == intent.SourceLive && sameOptional(source.State.Slug, intent.SourceSlug) &&
		digest(source.Canonical) == intent.SourceDigest && photoDigest == intent.PhotoDigest
}

// existingTarget reports any resume other than the source that is already
// Vietnamese or carries the fixed target title. A fresh production run then
// refuses, so a lost intent or another checkout cannot add a second target.
func existingTarget(listing []resumeSummary, source sourceHandle) bool {
	for _, item := range listing {
		if item.ID == string(source) {
			continue
		}
		language := strings.ToLower(item.Lng)
		if language == targetLanguage || strings.HasPrefix(language, targetLanguage+"-") || item.Title == targetTitle {
			return true
		}
	}
	return false
}

// captureGrant validates the revocation authority and its server-bound dates
// before any create. A grant that cannot be journaled stops the run first.
func (r *ownerRun) captureGrant(ctx context.Context) error {
	accessToken, refreshToken, err := r.deps.Tokens(ctx)
	if err != nil || !validBearerValue(accessToken) || !validBearerValue(refreshToken) || accessToken == refreshToken {
		return errGrantUnusable
	}
	first, latest, ok := r.deps.Observations.tokenDates()
	if !ok || first.IsZero() || latest.Before(first) {
		return errGrantUnusable
	}
	r.grant = grantSnapshot{accessToken: accessToken, refreshToken: refreshToken, firstDate: first, latestDate: latest}
	return nil
}

var errGrantUnusable = errors.New("grant_unusable")

func (r *ownerRun) acceptListedTarget(ctx context.Context, listed resumeSummary, outcome string) error {
	state, err := r.guard.getResume(ctx, listedHandle(listed.ID))
	if err != nil {
		return errors.Join(errRecovery, err)
	}
	if _, exact := validTarget(state, r.intent.Payload); !exact {
		return errRecovery
	}
	return r.acceptCreated(state, outcome)
}

func (r *ownerRun) acceptCreated(state resumeState, outcome string) error {
	if _, exact := validTarget(state, r.intent.Payload); !exact {
		return errRecovery
	}
	for _, item := range r.baseline {
		if item.ID == state.ID {
			return errRecovery
		}
	}
	target, err := r.guard.bindTarget(state.ID)
	if err != nil {
		return err
	}
	r.target, r.revision, r.outcome = target, state.Revision, outcome
	return nil
}
