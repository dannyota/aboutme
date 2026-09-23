package main

import (
	"bytes"
	"context"
	"errors"
	"strconv"
)

var errLocalProof = errors.New("local_proof_failed")

// proveLocalReplay runs only in local synthetic mode. It treats the create
// response as lost, reloads the durable intent, reconciles by reads, and then
// sends the exact same-key request, which must return the same target
// without a second resume. See docs/design/mcp-owner-workflow.md#workflow.
func (r *ownerRun) proveLocalReplay(ctx context.Context) error {
	if !r.config.local() {
		return errMutationCap
	}
	loaded, present, err := loadCreateIntent(r.config.Control, r.config.Origin)
	if err != nil || !present || loaded.IdempotencyKey != r.intent.IdempotencyKey || !bytes.Equal(loaded.Payload, r.intent.Payload) {
		return errLocalProof
	}
	mark := r.deps.Observations.mark()
	listing, err := r.guard.list(ctx)
	if err != nil {
		return errLocalProof
	}
	serverNow, dated := r.deps.Observations.authenticatedDateSince(mark)
	observedAt := r.deps.LocalNow()
	_, extras, err := splitBaseline(listing, loaded)
	if err != nil || len(extras) != 1 || extras[0].ID != string(r.target) {
		return errLocalProof
	}
	deadline, allowed := replayDeadline(loaded, serverNow, observedAt)
	if !dated || !allowed {
		return errLocalProof
	}
	state, outcome, err := r.guard.create(ctx, deadline)
	if err != nil || outcome != createCreated || state.ID != string(r.target) || state.Revision != r.revision {
		return errLocalProof
	}
	after, err := r.guard.list(ctx)
	if err != nil || len(after) != len(listing) {
		return errLocalProof
	}
	r.outcome = reconcileReplayed
	return nil
}

// proveLocalConflict runs only in local synthetic mode. One crop with a
// revision that is not current must return revision_conflict and leave the
// target unchanged.
func (r *ownerRun) proveLocalConflict(ctx context.Context) error {
	if !r.config.local() {
		return errMutationCap
	}
	current, err := strconv.ParseUint(r.revision, 10, 64)
	if err != nil || current == ^uint64(0) {
		return errLocalProof
	}
	stale := strconv.FormatUint(current+1, 10)
	if current > 1 {
		stale = strconv.FormatUint(current-1, 10)
	}
	key, err := r.deps.NewKey()
	if err != nil {
		return errLocalProof
	}
	code, err := r.guard.staleCropProbe(ctx, r.target, stale, key, r.source.Crop)
	if err != nil || code != "revision_conflict" {
		return errLocalProof
	}
	state, err := r.guard.getResume(ctx, r.target)
	if err != nil || state.Revision != r.revision {
		return errLocalProof
	}
	return nil
}
