package projectapi

import (
	"context"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

// change compares the stored project with the replacement. It plans on the
// pool and rechecks in the owner-locked transaction.
func (b boundApply) change(ctx context.Context, q *store.Queries) (project.Change, error) {
	return b.service.projects.ChangeTx(ctx, q, b.sess.UserID, b.id, b.revision, b.input.slug(), b.input.Document)
}

// authorizeReplace rechecks the session, the project precondition and, for
// a slug change, recent reauthentication before any linking resume advances.
func (b boundApply) authorizeReplace(ctx context.Context, q *store.Queries) error {
	if err := b.service.reauthenticate(ctx, q, b.sess); err != nil {
		return err
	}
	current, err := b.service.match(ctx, q, b.sess.UserID, b.id, b.revision)
	if err != nil {
		return err
	}
	if current.Slug != b.input.slug() {
		return b.service.requireRecent(ctx, q, b.sess, b.service.now())
	}
	return nil
}

// replace writes the project after its linking live resumes advanced and
// stores the project with those resumes as the response.
func (b boundApply) replace(ctx context.Context, q *store.Queries, changed []resumeapi.ChangedResume) (resume.StoredResponse, error) {
	now := b.service.now()
	admit := project.SlugAdmission(func() bool { return b.service.allowSlug(b.sess.UserID, now) })
	p, err := b.service.projects.ReplaceTx(ctx, q, b.sess.UserID, b.id, b.revision, b.input.slug(), b.input.Document, now, admit)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	links, err := b.service.links.LinkedResumeIDs(ctx, q, b.sess.UserID)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	return stored(200, replaceResult{Project: resourceOf(p, links), ChangedResumes: changedResumes(changed)}, map[string]string{"ETag": revisionTag(p.Revision)})
}
