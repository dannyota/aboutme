package projectapi

import (
	"context"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (b boundApply) change(ctx context.Context, q *store.Queries) (project.Change, error) {
	return b.service.projects.ChangeTx(ctx, q, b.actor.owner, b.id, b.revision, b.input.slug(), b.input.Document)
}

func (b boundApply) authorizeReplace(ctx context.Context, q *store.Queries) error {
	if err := b.actor.authorize(ctx, q); err != nil {
		return err
	}
	current, err := b.service.match(ctx, q, b.actor.owner, b.id, b.revision)
	if err != nil {
		return err
	}
	if current.Slug != b.input.slug() {
		return b.requireRecent(ctx, q)
	}
	return nil
}

func (b boundApply) replace(ctx context.Context, q *store.Queries, changed []resumeapi.ChangedResume) (resume.StoredResponse, error) {
	now := b.service.now()
	admit := project.SlugAdmission(func() bool { return b.service.allowSlug(b.actor.owner, now) })
	p, err := b.service.projects.ReplaceTx(ctx, q, b.actor.owner, b.id, b.revision, b.input.slug(), b.input.Document, now, admit)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	links, err := b.service.links.LinkedResumeIDs(ctx, q, b.actor.owner)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	return stored(200, replaceResult{Project: resourceOf(p, links), ChangedResumes: changedResumes(changed)}, map[string]string{"ETag": revisionTag(p.Revision)})
}
