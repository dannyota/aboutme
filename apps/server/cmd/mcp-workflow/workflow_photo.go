package main

import (
	"context"
	"errors"
)

// copyPhoto copies the source photo and crop to the target, chaining the
// latest decimal revision. A conflict or unknown result is accepted only when
// a read proves the target already exact; nothing is rebased or deleted.
func (r *ownerRun) copyPhoto(ctx context.Context) error {
	state, err := r.guard.getResume(ctx, r.target)
	if err != nil || state.Revision != r.revision {
		return errRecovery
	}
	photo, exact := validTarget(state, r.intent.Payload)
	if !exact {
		return errRecovery
	}
	if r.source.Photo == nil {
		if photo != nil {
			return errPhoto
		}
		return nil
	}
	if photo == nil {
		if uploadErr := r.uploadPhoto(ctx); uploadErr != nil {
			return uploadErr
		}
		state, err = r.guard.getResume(ctx, r.target)
		if err != nil || state.Revision != r.revision {
			return errRecovery
		}
		if photo, exact = validTarget(state, r.intent.Payload); !exact || photo == nil {
			return errPhoto
		}
	}
	targetPhoto, err := r.guard.getPhoto(ctx, r.target)
	if err != nil {
		return err
	}
	if parityErr := validatePhotoParity(*r.source.Photo, nil, targetPhoto, nil); parityErr != nil {
		return parityErr
	}
	targetCrop := cropFromSchema(photo.Crop)
	switch {
	case r.source.Crop.equal(targetCrop):
		return nil
	case targetCrop != nil:
		return errPhoto
	default:
		return r.cropPhoto(ctx)
	}
}

func (r *ownerRun) uploadPhoto(ctx context.Context) error {
	key, err := r.deps.NewKey()
	if err != nil {
		return errWorkflowBlocked
	}
	output, code, err := r.guard.uploadPhoto(ctx, r.target, r.revision, key, r.source.Photo.Data)
	if err == nil && code == "" && output.State.ID == string(r.target) && nextDecimalRevision(r.revision, output.Revision) {
		r.revision = output.Revision
		return nil
	}
	return r.reconcileTargetWrite(ctx, code, err, func(_ resumeState, photo *photoMeta) bool { return photo != nil })
}

func (r *ownerRun) cropPhoto(ctx context.Context) error {
	key, err := r.deps.NewKey()
	if err != nil {
		return errWorkflowBlocked
	}
	output, code, err := r.guard.cropPhoto(ctx, r.target, r.revision, key, r.source.Crop)
	if err == nil && code == "" && output.State.ID == string(r.target) && nextDecimalRevision(r.revision, output.Revision) {
		r.revision = output.Revision
		if _, exact := validTarget(output.State, r.intent.Payload); !exact {
			return errRecovery
		}
		return nil
	}
	return r.reconcileTargetWrite(ctx, code, err, func(_ resumeState, photo *photoMeta) bool {
		return photo != nil && r.source.Crop.equal(cropFromSchema(photo.Crop))
	})
}

// reconcileTargetWrite reads the target after a conflict or unknown write. It
// accepts only the next revision with the exact intended result.
func (r *ownerRun) reconcileTargetWrite(ctx context.Context, code string, cause error, applied func(resumeState, *photoMeta) bool) error {
	if cause == nil && code != "revision_conflict" && code != "" {
		return errWorkflowContract
	}
	state, err := r.guard.getResume(ctx, r.target)
	if err != nil {
		return errors.Join(errRecovery, cause)
	}
	photo, exact := validTarget(state, r.intent.Payload)
	if !exact || !nextDecimalRevision(r.revision, state.Revision) || !applied(state, photo) {
		return errors.Join(errRecovery, cause)
	}
	r.revision = state.Revision
	return nil
}
