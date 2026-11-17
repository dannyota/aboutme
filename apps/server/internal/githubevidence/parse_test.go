package githubevidence

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParse_Repository(t *testing.T) {
	repo, err := parseRepository([]byte(repositoryJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := githubRepository{id: 101, fullName: "octocat/hello", ownerID: 583231, createdAt: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)}
	if repo != want {
		t.Fatalf("repository = %+v, want %+v", repo, want)
	}
	for _, tc := range []struct{ name, body string }{
		{"id as a string", strings.Replace(repositoryJSON, `"id":101`, `"id":"101"`, 1)},
		{"id as a float", strings.Replace(repositoryJSON, `"id":101`, `"id":1.01e2`, 1)},
		{"zero id", strings.Replace(repositoryJSON, `"id":101`, `"id":0`, 1)},
		{"missing id", strings.Replace(repositoryJSON, `"id":101,`, ``, 1)},
		{"missing full name", strings.Replace(repositoryJSON, `"full_name":"octocat/hello",`, ``, 1)},
		{"full name without an owner", strings.Replace(repositoryJSON, `"octocat/hello"`, `"hello"`, 1)},
		{"full name with extra segments", strings.Replace(repositoryJSON, `"octocat/hello"`, `"octocat/hello/x"`, 1)},
		{"full name as a number", strings.Replace(repositoryJSON, `"octocat/hello"`, `5`, 1)},
		{"null owner", strings.Replace(repositoryJSON, `{"id":583231,"login":"octocat"}`, `null`, 1)},
		{"owner without an id", strings.Replace(repositoryJSON, `{"id":583231,"login":"octocat"}`, `{"login":"octocat"}`, 1)},
		{"private as a string", strings.Replace(repositoryJSON, `"private":false`, `"private":"false"`, 1)},
		{"missing private", strings.Replace(repositoryJSON, `"private":false,`, ``, 1)},
		{"null fork", strings.Replace(repositoryJSON, `"fork":false`, `"fork":null`, 1)},
		{"created at not a time", strings.Replace(repositoryJSON, `"2020-01-02T03:04:05Z"`, `"yesterday"`, 1)},
		{"missing created at", strings.Replace(repositoryJSON, `,"created_at":"2020-01-02T03:04:05Z"`, ``, 1)},
		{"repeated key", strings.Replace(repositoryJSON, `"fork":false`, `"fork":false,"fork":true`, 1)},
		{"trailing data", repositoryJSON + `{}`},
		{"an array", `[` + repositoryJSON + `]`},
		{"truncated", repositoryJSON[:40]},
	} {
		if _, err := parseRepository([]byte(tc.body)); !errors.Is(err, errMalformed) {
			t.Errorf("%s: error = %v, want malformed", tc.name, err)
		}
	}
}

func TestParse_PullRequest(t *testing.T) {
	pull, err := parsePullRequest([]byte(pullRequestJSON))
	if err != nil {
		t.Fatal(err)
	}
	merged := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if pull.id != 9001 || pull.number != 42 || pull.title != "Fix the thing" || !pull.merged || pull.mergedAt == nil || !pull.mergedAt.Equal(merged) ||
		pull.userID != 583231 || pull.htmlURL != "https://github.com/octocat/hello/pull/42" {
		t.Fatalf("pull request = %+v", pull)
	}
	open := strings.NewReplacer(`"merged":true`, `"merged":false`, `"merged_at":"2026-01-02T03:04:05Z"`, `"merged_at":null`).Replace(pullRequestJSON)
	if pull, err = parsePullRequest([]byte(open)); err != nil || pull.mergedAt != nil || pull.merged {
		t.Fatalf("open pull request = %+v, %v", pull, err)
	}
	ghost := strings.Replace(pullRequestJSON, `{"id":583231,"login":"octocat"}`, `null`, 1)
	if pull, err = parsePullRequest([]byte(ghost)); err != nil || pull.userID != 0 {
		t.Fatalf("pull request without a user = %+v, %v; want user id 0", pull, err)
	}
	for _, tc := range []struct{ name, body string }{
		{"missing number", strings.Replace(pullRequestJSON, `"number":42,`, ``, 1)},
		{"title as a number", strings.Replace(pullRequestJSON, `"Fix the thing"`, `7`, 1)},
		{"missing merged", strings.Replace(pullRequestJSON, `"merged":true,`, ``, 1)},
		{"missing merged at", strings.Replace(pullRequestJSON, `"merged_at":"2026-01-02T03:04:05Z",`, ``, 1)},
		{"merged at not a time", strings.Replace(pullRequestJSON, `"2026-01-02T03:04:05Z"`, `"soon"`, 1)},
		{"user id as a string", strings.Replace(pullRequestJSON, `"user":{"id":583231`, `"user":{"id":"583231"`, 1)},
		{"user as a string", strings.Replace(pullRequestJSON, `{"id":583231,"login":"octocat"}`, `"octocat"`, 1)},
		{"missing html url", strings.Replace(pullRequestJSON, `"html_url":"https://github.com/octocat/hello/pull/42",`, ``, 1)},
		{"missing id", strings.Replace(pullRequestJSON, `"id":9001,`, ``, 1)},
	} {
		if _, err := parsePullRequest([]byte(tc.body)); !errors.Is(err, errMalformed) {
			t.Errorf("%s: error = %v, want malformed", tc.name, err)
		}
	}
}

func TestParse_Release(t *testing.T) {
	release, err := parseRelease([]byte(releaseJSON))
	if err != nil {
		t.Fatal(err)
	}
	if release.id != 7001 || release.tag != "v1.0.0" || release.name != "First" || release.draft || !release.prerelease ||
		release.publishedAt == nil || release.authorID != 583231 || release.authorBot {
		t.Fatalf("release = %+v", release)
	}
	draft := strings.NewReplacer(`"name":"First"`, `"name":null`, `"draft":false`, `"draft":true`, `"published_at":"2026-02-03T04:05:06Z"`, `"published_at":null`).Replace(releaseJSON)
	if release, err = parseRelease([]byte(draft)); err != nil || release.name != "" || !release.draft || release.publishedAt != nil {
		t.Fatalf("draft release = %+v, %v", release, err)
	}
	bot := strings.Replace(releaseJSON, `"type":"User"`, `"type":"Bot"`, 1)
	if release, err = parseRelease([]byte(bot)); err != nil || !release.authorBot {
		t.Fatalf("bot release = %+v, %v", release, err)
	}
	ghost := strings.Replace(releaseJSON, `{"id":583231,"login":"octocat","type":"User"}`, `null`, 1)
	if release, err = parseRelease([]byte(ghost)); err != nil || release.authorID != 0 {
		t.Fatalf("release without an author = %+v, %v", release, err)
	}
	for _, tc := range []struct{ name, body string }{
		{"missing tag", strings.Replace(releaseJSON, `"tag_name":"v1.0.0",`, ``, 1)},
		{"empty tag", strings.Replace(releaseJSON, `"tag_name":"v1.0.0"`, `"tag_name":""`, 1)},
		{"name as a number", strings.Replace(releaseJSON, `"name":"First"`, `"name":1`, 1)},
		{"missing draft", strings.Replace(releaseJSON, `"draft":false,`, ``, 1)},
		{"prerelease as a string", strings.Replace(releaseJSON, `"prerelease":true`, `"prerelease":"true"`, 1)},
		{"published at not a time", strings.Replace(releaseJSON, `"2026-02-03T04:05:06Z"`, `"2026-02-03"`, 1)},
		{"author without an id", strings.Replace(releaseJSON, `"author":{"id":583231,`, `"author":{`, 1)},
		{"author without a type", strings.Replace(releaseJSON, `,"type":"User"`, ``, 1)},
		{"missing author", strings.Replace(releaseJSON, `"author":{"id":583231,"login":"octocat","type":"User"},`, ``, 1)},
	} {
		if _, err := parseRelease([]byte(tc.body)); !errors.Is(err, errMalformed) {
			t.Errorf("%s: error = %v, want malformed", tc.name, err)
		}
	}
}

const searchItemJSON = `{"repository_url":"https://api.github.com/repos/octocat/hello","number":42,"title":"Fix the thing","closed_at":"2026-01-02T03:04:05Z","state":"closed"}`

func TestParse_Search(t *testing.T) {
	result, err := parseSearch([]byte(`{"total_count":1,"incomplete_results":true,"items":[` + searchItemJSON + `]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := searchItem{owner: "octocat", name: "hello", number: 42, title: "Fix the thing", closedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	if !result.incomplete || len(result.items) != 1 || result.items[0] != want {
		t.Fatalf("search = %+v, want one item %+v and incomplete", result, want)
	}
	for _, tc := range []struct{ name, body string }{
		{"missing incomplete", `{"items":[]}`},
		{"incomplete as a string", `{"incomplete_results":"false","items":[]}`},
		{"missing items", `{"incomplete_results":false}`},
		{"items as an object", `{"incomplete_results":false,"items":{}}`},
		{"an array", `[]`},
		{"repository on another host", `{"incomplete_results":false,"items":[` + strings.Replace(searchItemJSON, "api.github.com", "api.github.com.evil.example", 1) + `]}`},
		{"repository URL without a name", `{"incomplete_results":false,"items":[` + strings.Replace(searchItemJSON, "repos/octocat/hello", "repos/octocat", 1) + `]}`},
		{"repository URL with a path after it", `{"incomplete_results":false,"items":[` + strings.Replace(searchItemJSON, "repos/octocat/hello", "repos/octocat/hello/x", 1) + `]}`},
		{"missing number", `{"incomplete_results":false,"items":[` + strings.Replace(searchItemJSON, `"number":42,`, ``, 1) + `]}`},
		{"null closed at", `{"incomplete_results":false,"items":[` + strings.Replace(searchItemJSON, `"2026-01-02T03:04:05Z"`, `null`, 1) + `]}`},
		{"missing title", `{"incomplete_results":false,"items":[` + strings.Replace(searchItemJSON, `"title":"Fix the thing",`, ``, 1) + `]}`},
		{"an item that is not an object", `{"incomplete_results":false,"items":[1]}`},
	} {
		if _, err := parseSearch([]byte(tc.body)); !errors.Is(err, errMalformed) {
			t.Errorf("%s: error = %v, want malformed", tc.name, err)
		}
	}
}

func TestParse_Lists(t *testing.T) {
	repos, err := parseRepositoryList([]byte(`[` + repositoryJSON + `,` + repositoryJSON + `]`))
	if err != nil || len(repos) != 2 {
		t.Fatalf("repositories = %d, %v", len(repos), err)
	}
	releases, err := parseReleaseList([]byte(` [ ` + releaseJSON + ` ] `))
	if err != nil || len(releases) != 1 {
		t.Fatalf("releases = %d, %v", len(releases), err)
	}
	if empty, err := parseRepositoryList([]byte(`[]`)); err != nil || len(empty) != 0 {
		t.Fatalf("empty list = %v, %v", empty, err)
	}
	for _, body := range []string{`{}`, `null`, `[` + repositoryJSON + `,{"id":1}]`, `[` + repositoryJSON + `] []`, `[` + repositoryJSON, `[1]`} {
		if _, err := parseRepositoryList([]byte(body)); !errors.Is(err, errMalformed) {
			t.Errorf("repository list %.40q error = %v, want malformed", body, err)
		}
	}
	if _, err := parseReleaseList([]byte(`[` + strings.Replace(releaseJSON, `"draft":false`, `"draft":0`, 1) + `]`)); !errors.Is(err, errMalformed) {
		t.Errorf("release list with a bad draft error = %v, want malformed", err)
	}
}
