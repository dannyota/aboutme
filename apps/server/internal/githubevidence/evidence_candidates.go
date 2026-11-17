package githubevidence

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Service) Candidates(ctx context.Context, sess store.Session, projectID uuid.UUID, query project.EvidenceQuery) (project.EvidenceCandidates, error) {
	if !s.enabled {
		return project.EvidenceCandidates{}, project.ErrEvidenceUnavailable
	}
	if !validPage(query.Page) || !project.ValidEvidenceKind(query.Kind) || (query.Kind == kindRelease) != (query.Repository > 0) {
		return project.EvidenceCandidates{}, project.ErrEvidenceRequest
	}
	stored, err := s.storedEvidence(ctx, sess, projectID)
	if err != nil {
		return project.EvidenceCandidates{}, err
	}
	opened, err := s.openReader(ctx, sess)
	if err != nil {
		return project.EvidenceCandidates{}, err
	}
	memory := opened.reader.session.memory
	var page candidatePage
	switch query.Kind {
	case kindRepository:
		page, err = opened.reader.listRepositories(ctx, query.Page)
	case kindPullRequest:
		page, err = opened.reader.listPullRequests(ctx, opened.login, query.Page)
	default:
		repo, listed := memory.repository(query.Repository)
		if !listed {
			return project.EvidenceCandidates{}, project.ErrEvidenceSessionRequired
		}
		page, err = opened.reader.listReleases(ctx, repo, opened.githubUserID, query.Page)
	}
	if errors.Is(err, errGone) {
		return project.EvidenceCandidates{}, project.ErrEvidenceUpstream
	}
	if err != nil {
		return project.EvidenceCandidates{}, readError(err)
	}
	ids, err := s.candidateIDs(len(page.items))
	if err != nil {
		return project.EvidenceCandidates{}, err
	}
	memory.remember(query.Kind, query.Repository, query.Page, page.items, ids)
	out := project.EvidenceCandidates{Items: make([]project.EvidenceCandidate, len(page.items)), Incomplete: page.incomplete}
	if page.nextPage != 0 {
		next := page.nextPage
		out.NextPage = &next
	}
	for i, item := range page.items {
		out.Items[i] = candidateOut(ids[i], item, attached(stored, item))
	}
	return out, nil
}

func (s *Service) storedEvidence(ctx context.Context, sess store.Session, projectID uuid.UUID) ([]store.ListProjectEvidenceRow, error) {
	owned, err := s.q.ProjectBelongsToUser(ctx, store.ProjectBelongsToUserParams{ProjectID: projectID, UserID: sess.UserID})
	if err != nil {
		return nil, fmt.Errorf("githubevidence: read project: %w", err)
	}
	if !owned {
		return nil, project.ErrNotFound
	}
	rows, err := s.q.ListProjectEvidence(ctx, store.ListProjectEvidenceParams{UserID: sess.UserID, ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("githubevidence: read project evidence: %w", err)
	}
	return rows, nil
}

func candidateOut(id string, item candidate, attached bool) project.EvidenceCandidate {
	repo := project.CandidateRepository{FullName: item.repository.fullName}
	if item.repository.id > 0 {
		repositoryID := item.repository.id
		repo.ID = &repositoryID
	}
	return project.EvidenceCandidate{CandidateID: id, Kind: item.kind, Title: item.title, Repository: repo, Date: item.date.UTC(), Attached: attached}
}

func attached(stored []store.ListProjectEvidenceRow, item candidate) bool {
	for _, row := range stored {
		switch {
		case row.Kind != item.kind:
		case item.kind == kindPullRequest:
			if row.PullNumber != nil && *row.PullNumber == item.number && strings.EqualFold(row.RepositoryName, item.repository.fullName) {
				return true
			}
		case row.ItemID == item.itemID:
			return true
		}
	}
	return false
}
