package githubevidence

import (
	"context"
	"errors"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Service) Recheck(ctx context.Context, sess store.Session, stored store.ProjectEvidence) (project.EvidenceCheck, error) {
	if !s.enabled {
		return project.EvidenceCheck{}, project.ErrEvidenceUnavailable
	}
	opened, err := s.openReader(ctx, sess)
	if err != nil {
		return project.EvidenceCheck{}, err
	}
	fields, err := fetchForRecheck(ctx, opened, stored)
	if err != nil {
		return project.EvidenceCheck{}, err
	}
	return project.EvidenceCheck{Kind: stored.Kind, Fields: fields, Confirm: s.confirm(opened)}, nil
}

func fetchForRecheck(ctx context.Context, opened evidenceSession, stored store.ProjectEvidence) (project.EvidenceFields, error) {
	rd, account := opened.reader, opened.githubUserID
	owner, name, valid := splitFullName(stored.RepositoryName)
	if !valid {
		return project.EvidenceFields{}, project.ErrEvidenceUpstream
	}
	repo, err := rd.repositoryByName(ctx, owner, name, stored.RepositoryID)
	if err != nil || repo.id != stored.RepositoryID {
		return goneOr(stored, err)
	}
	if owner, name, valid = splitFullName(repo.fullName); !valid {
		return project.EvidenceFields{}, project.ErrEvidenceUpstream
	}
	switch stored.Kind {
	case kindRepository:
		return judged(repositoryFields(repo), repositoryRuleMet(repo, account), stored), nil
	case kindPullRequest:
		if stored.PullNumber == nil {
			return project.EvidenceFields{}, project.ErrEvidenceUpstream
		}
		pull, pullErr := rd.pullRequest(ctx, owner, name, *stored.PullNumber)
		if pullErr != nil || pull.id != stored.ItemID {
			return goneOr(stored, pullErr)
		}
		return judged(pullRequestFields(repo, pull), pullRequestRuleMet(repo, pull, account), stored), nil
	default:
		release, releaseErr := rd.releaseByID(ctx, owner, name, stored.ItemID)
		if releaseErr != nil || release.id != stored.ItemID {
			return goneOr(stored, releaseErr)
		}
		fields, fits := releaseFields(repo, release)
		if !fits {
			return project.EvidenceFields{}, project.ErrEvidenceUpstream
		}
		return judged(fields, releaseRuleMet(repo, release, account), stored), nil
	}
}

func goneOr(stored store.ProjectEvidence, err error) (project.EvidenceFields, error) {
	if err == nil || errors.Is(err, errGone) {
		return goneFields(stored), nil
	}
	return project.EvidenceFields{}, readError(err)
}
