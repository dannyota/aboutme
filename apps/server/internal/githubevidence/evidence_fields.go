package githubevidence

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	maxTextCharacters = 200
	maxURLCharacters  = 300
)

func unsafeText(r rune) bool {
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}

func limitText(text string) string {
	text = strings.Map(func(r rune) rune {
		if unsafeText(r) {
			return ' '
		}
		return r
	}, text)
	if utf8.RuneCountInString(text) <= maxTextCharacters {
		return text
	}
	return string([]rune(text)[:maxTextCharacters])
}

func pullRequestTitle(fullName string, number int64, title string) string {
	if limited := limitText(title); strings.TrimSpace(limited) != "" {
		return limited
	}
	return limitText(fullName + "#" + strconv.FormatInt(number, 10))
}

func releaseTitle(name, tag string) string {
	if limited := limitText(name); strings.TrimSpace(limited) != "" {
		return limited
	}
	return limitText(tag)
}

func repositoryFields(repo githubRepository) project.EvidenceFields {
	return project.EvidenceFields{
		RepositoryID: repo.id, ItemID: repo.id, URL: githubWebPrefix + repo.fullName,
		RepositoryName: limitText(repo.fullName), Title: limitText(repo.fullName), ItemDate: repo.itemDate(),
	}
}

func pullRequestFields(repo githubRepository, pull githubPullRequest) project.EvidenceFields {
	number := pull.number
	return project.EvidenceFields{
		RepositoryID: repo.id, ItemID: pull.id, PullNumber: &number,
		URL:            githubWebPrefix + repo.fullName + "/pull/" + strconv.FormatInt(number, 10),
		RepositoryName: limitText(repo.fullName), Title: pullRequestTitle(repo.fullName, number, pull.title), ItemDate: pull.itemDate(),
	}
}

func releaseFields(repo githubRepository, release githubRelease) (project.EvidenceFields, bool) {
	tag := release.tag
	link := githubWebPrefix + repo.fullName + "/releases/tag/" + url.PathEscape(tag)
	if !validTag(tag) || len(link) > maxURLCharacters {
		return project.EvidenceFields{}, false
	}
	return project.EvidenceFields{
		RepositoryID: repo.id, ItemID: release.id, ReleaseTag: &tag, URL: link,
		RepositoryName: limitText(repo.fullName), Title: releaseTitle(release.name, tag), ItemDate: release.itemDate(),
	}, true
}

func judged(fields project.EvidenceFields, met bool, stored store.ProjectEvidence) project.EvidenceFields {
	if fields.ItemDate.IsZero() {
		fields.ItemDate = stored.ItemDate
	}
	fields.RuleMet = met
	if !met {
		reason := project.FlagRuleFailed
		fields.FlagReason = &reason
	}
	return fields
}

func goneFields(stored store.ProjectEvidence) project.EvidenceFields {
	reason := project.FlagGone
	return project.EvidenceFields{
		RepositoryID: stored.RepositoryID, ItemID: stored.ItemID, PullNumber: stored.PullNumber, ReleaseTag: stored.ReleaseTag,
		URL: stored.Url, RepositoryName: stored.RepositoryName, Title: stored.Title, ItemDate: stored.ItemDate, FlagReason: &reason,
	}
}
