package githubevidence

import (
	"context"
	"time"
)

type candidateRepository struct {
	id       int64
	fullName string
}

type candidate struct {
	kind       string
	repository candidateRepository
	itemID     int64
	number     int64
	tag        string
	title      string
	date       time.Time
}

type candidatePage struct {
	items      []candidate
	nextPage   int
	incomplete bool
}

func nextPage(page, rows int) int {
	if rows == listingPageSize && page < maxListingPages {
		return page + 1
	}
	return 0
}

func (rd *githubReader) listRepositories(ctx context.Context, page int) (candidatePage, error) {
	if !validPage(page) {
		return candidatePage{}, errPageRange
	}
	result, err := rd.fetch(ctx, "/user/repos", repositoryListingQuery(page), false)
	repos, err := decoded(result, err, parseRepositoryList)
	if err != nil {
		return candidatePage{}, err
	}
	out := candidatePage{nextPage: nextPage(page, len(repos))}
	for _, repo := range repos {
		if repo.private || repo.fork {
			continue
		}
		out.items = append(out.items, candidate{kind: kindRepository, repository: candidateRepository{id: repo.id, fullName: repo.fullName},
			itemID: repo.id, title: repo.fullName, date: repo.createdAt})
	}
	return out, nil
}

func (rd *githubReader) listPullRequests(ctx context.Context, login string, page int) (candidatePage, error) {
	if !validPage(page) {
		return candidatePage{}, errPageRange
	}
	result, err := rd.fetch(ctx, "/search/issues", searchListingQuery(login, page), false)
	search, err := decoded(result, err, parseSearch)
	if err != nil {
		return candidatePage{}, err
	}
	out := candidatePage{nextPage: nextPage(page, len(search.items)), incomplete: search.incomplete}
	for _, item := range search.items {
		out.items = append(out.items, candidate{kind: kindPullRequest, repository: candidateRepository{fullName: item.owner + "/" + item.name},
			number: item.number, title: item.title, date: item.closedAt})
	}
	return out, nil
}

func (rd *githubReader) listReleases(ctx context.Context, repo candidateRepository, account int64, page int) (candidatePage, error) {
	if !validPage(page) {
		return candidatePage{}, errPageRange
	}
	owner, name, valid := splitFullName(repo.fullName)
	if !valid || repo.id <= 0 {
		return candidatePage{}, errDestinationRefused
	}
	result, err := rd.fetch(ctx, repositoryPath(owner, name)+"/releases", releaseListingQuery(page), false)
	releases, err := decoded(result, err, parseReleaseList)
	if err != nil {
		return candidatePage{}, err
	}
	out := candidatePage{nextPage: nextPage(page, len(releases))}
	for _, release := range releases {
		if !releasedBy(release, account) {
			continue
		}
		title := release.name
		if title == "" {
			title = release.tag
		}
		out.items = append(out.items, candidate{kind: kindRelease, repository: repo, itemID: release.id, tag: release.tag,
			title: title, date: release.itemDate()})
	}
	return out, nil
}
