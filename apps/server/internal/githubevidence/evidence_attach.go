package githubevidence

import (
	"context"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/store"
)

type attachTarget struct {
	kind      string
	owner     string
	name      string
	number    int64
	releaseID int64
	tag       string
}

func (s *Service) Attach(ctx context.Context, sess store.Session, request project.EvidenceRequest) (project.EvidenceCheck, error) {
	if !s.enabled {
		return project.EvidenceCheck{}, project.ErrEvidenceUnavailable
	}
	var target attachTarget
	if request.CandidateID == "" {
		link, err := parseEvidenceLink(request.Kind, request.Link)
		if err != nil {
			return project.EvidenceCheck{}, project.ErrEvidenceUnsupportedLink
		}
		target = attachTarget{kind: link.kind, owner: link.owner, name: link.name, number: link.number, tag: link.tag}
	}
	opened, err := s.openReader(ctx, sess)
	if err != nil {
		return project.EvidenceCheck{}, err
	}
	if request.CandidateID != "" {
		if target, err = candidateTarget(opened, request); err != nil {
			return project.EvidenceCheck{}, err
		}
	}
	fields, err := fetchForAttach(ctx, opened, target)
	if err != nil {
		return project.EvidenceCheck{}, err
	}
	fields.RuleMet = true
	return project.EvidenceCheck{Kind: target.kind, Fields: fields, Confirm: s.confirm(opened)}, nil
}

func candidateTarget(opened evidenceSession, request project.EvidenceRequest) (attachTarget, error) {
	item, found := opened.reader.session.memory.candidate(request.CandidateID)
	if !found {
		return attachTarget{}, project.ErrEvidenceSessionRequired
	}
	if item.kind != request.Kind {
		return attachTarget{}, project.ErrEvidenceRequest
	}
	owner, name, valid := splitFullName(item.repository.fullName)
	if !valid {
		return attachTarget{}, project.ErrEvidenceUpstream
	}
	target := attachTarget{kind: item.kind, owner: owner, name: name, number: item.number}
	if item.kind == kindRelease {
		target.releaseID = item.itemID
	}
	return target, nil
}

func fetchForAttach(ctx context.Context, opened evidenceSession, target attachTarget) (project.EvidenceFields, error) {
	rd, account := opened.reader, opened.githubUserID
	repo, err := rd.repositoryByName(ctx, target.owner, target.name, 0)
	if err != nil {
		return project.EvidenceFields{}, readError(err)
	}
	owner, name, valid := splitFullName(repo.fullName)
	if !valid {
		return project.EvidenceFields{}, project.ErrEvidenceUpstream
	}
	switch target.kind {
	case kindRepository:
		if !repositoryRuleMet(repo, account) {
			return project.EvidenceFields{}, project.ErrEvidenceNotConfirmed
		}
		return repositoryFields(repo), nil
	case kindPullRequest:
		pull, pullErr := rd.pullRequest(ctx, owner, name, target.number)
		if pullErr != nil {
			return project.EvidenceFields{}, readError(pullErr)
		}
		if !pullRequestRuleMet(repo, pull, account) {
			return project.EvidenceFields{}, project.ErrEvidenceNotConfirmed
		}
		return pullRequestFields(repo, pull), nil
	default:
		release, releaseErr := attachRelease(ctx, rd, owner, name, target)
		if releaseErr != nil {
			return project.EvidenceFields{}, readError(releaseErr)
		}
		if !releaseRuleMet(repo, release, account) {
			return project.EvidenceFields{}, project.ErrEvidenceNotConfirmed
		}
		fields, fits := releaseFields(repo, release)
		if !fits {
			return project.EvidenceFields{}, project.ErrEvidenceUnsupportedLink
		}
		return fields, nil
	}
}

func attachRelease(ctx context.Context, rd *githubReader, owner, name string, target attachTarget) (githubRelease, error) {
	if target.releaseID != 0 {
		return rd.releaseByID(ctx, owner, name, target.releaseID)
	}
	return rd.releaseByTag(ctx, owner, name, target.tag)
}
