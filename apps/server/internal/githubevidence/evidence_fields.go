package githubevidence

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	maxTextCharacters = 200
	maxTagCharacters  = 255
	maxURLCharacters  = 300
)

func limitText(text string) string {
	text = strings.ReplaceAll(text, "\x00", "")
	if utf8.RuneCountInString(text) <= maxTextCharacters {
		return text
	}
	return string([]rune(text)[:maxTextCharacters])
}

func repositoryFields(repo githubRepository) project.EvidenceFields {
	return project.EvidenceFields{
		RepositoryID: repo.id, ItemID: repo.id, URL: githubWebPrefix + repo.fullName,
		RepositoryName: limitText(repo.fullName), Title: limitText(repo.fullName), ItemDate: repo.itemDate(),
	}
}

func pullRequestFields(repo githubRepository, pull githubPullRequest) project.EvidenceFields {
	number := pull.number
	title := limitText(pull.title)
	if strings.TrimSpace(title) == "" {
		title = repo.fullName + "#" + strconv.FormatInt(number, 10)
	}
	return project.EvidenceFields{
		RepositoryID: repo.id, ItemID: pull.id, PullNumber: &number,
		URL:            githubWebPrefix + repo.fullName + "/pull/" + strconv.FormatInt(number, 10),
		RepositoryName: limitText(repo.fullName), Title: title, ItemDate: pull.itemDate(),
	}
}

func releaseFields(repo githubRepository, release githubRelease) (project.EvidenceFields, bool) {
	tag := release.tag
	link := githubWebPrefix + repo.fullName + "/releases/tag/" + url.PathEscape(tag)
	if strings.ContainsRune(tag, 0) || utf8.RuneCountInString(tag) > maxTagCharacters || len(link) > maxURLCharacters {
		return project.EvidenceFields{}, false
	}
	title := limitText(release.name)
	if strings.TrimSpace(title) == "" {
		title = limitText(tag)
	}
	return project.EvidenceFields{
		RepositoryID: repo.id, ItemID: release.id, ReleaseTag: &tag, URL: link,
		RepositoryName: limitText(repo.fullName), Title: title, ItemDate: release.itemDate(),
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
