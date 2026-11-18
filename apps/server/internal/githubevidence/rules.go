package githubevidence

import "time"

func repositoryRuleMet(repo githubRepository, account int64) bool {
	return account > 0 && !repo.private && !repo.fork && repo.ownerID == account
}

func pullRequestRuleMet(repo githubRepository, pull githubPullRequest, account int64) bool {
	return account > 0 && !repo.private && pull.mergedAt != nil && pull.userID == account
}

func releaseRuleMet(repo githubRepository, release githubRelease, account int64) bool {
	return !repo.private && releasedBy(release, account)
}

func releasedBy(release githubRelease, account int64) bool {
	return account > 0 && !release.draft && release.publishedAt != nil && !release.authorBot && release.authorID == account
}

func (r githubRepository) itemDate() time.Time {
	return r.createdAt
}

func (p githubPullRequest) itemDate() time.Time {
	if p.mergedAt == nil {
		return time.Time{}
	}
	return *p.mergedAt
}

func (r githubRelease) itemDate() time.Time {
	if r.publishedAt == nil {
		return time.Time{}
	}
	return *r.publishedAt
}
