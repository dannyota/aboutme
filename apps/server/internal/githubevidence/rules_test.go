package githubevidence

import (
	"testing"
	"time"
)

func TestRules_Repository(t *testing.T) {
	owned := githubRepository{id: 101, fullName: "octocat/hello", ownerID: testGitHubUserID}
	archived, err := parseRepository([]byte(repositoryJSON))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		repo githubRepository
		user int64
		want bool
	}{
		{"owned and public", owned, testGitHubUserID, true},
		{"archived counts", archived, testGitHubUserID, true},
		{"private", githubRepository{id: 101, ownerID: testGitHubUserID, private: true}, testGitHubUserID, false},
		{"a fork", githubRepository{id: 101, ownerID: testGitHubUserID, fork: true}, testGitHubUserID, false},
		{"another owner", githubRepository{id: 101, ownerID: 42}, testGitHubUserID, false},
		{"no connected account", owned, 0, false},
	} {
		if got := repositoryRuleMet(tc.repo, tc.user); got != tc.want {
			t.Errorf("%s: rule met = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRules_PullRequest(t *testing.T) {
	merged := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	public := githubRepository{id: 101, ownerID: 42}
	pull := githubPullRequest{id: 9001, number: 42, merged: true, mergedAt: &merged, userID: testGitHubUserID}
	for _, tc := range []struct {
		name string
		repo githubRepository
		pull githubPullRequest
		user int64
		want bool
	}{
		{"merged by the account in someone else's repository", public, pull, testGitHubUserID, true},
		{"private repository", githubRepository{id: 101, private: true}, pull, testGitHubUserID, false},
		{"not merged", public, githubPullRequest{id: 9001, userID: testGitHubUserID}, testGitHubUserID, false},
		{"merged flag without merged at", public, githubPullRequest{id: 9001, merged: true, userID: testGitHubUserID}, testGitHubUserID, false},
		{"another author", public, githubPullRequest{id: 9001, mergedAt: &merged, userID: 7}, testGitHubUserID, false},
		{"deleted author", public, githubPullRequest{id: 9001, mergedAt: &merged}, testGitHubUserID, false},
		{"no connected account", public, githubPullRequest{id: 9001, mergedAt: &merged}, 0, false},
	} {
		if got := pullRequestRuleMet(tc.repo, tc.pull, tc.user); got != tc.want {
			t.Errorf("%s: rule met = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRules_Release(t *testing.T) {
	published := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	public := githubRepository{id: 101, ownerID: 42}
	release := githubRelease{id: 7001, tag: "v1", publishedAt: &published, authorID: testGitHubUserID}
	with := func(change func(*githubRelease)) githubRelease {
		r := release
		change(&r)
		return r
	}
	for _, tc := range []struct {
		name    string
		repo    githubRepository
		release githubRelease
		user    int64
		want    bool
	}{
		{"published by the account", public, release, testGitHubUserID, true},
		{"prerelease counts", public, with(func(r *githubRelease) { r.prerelease = true }), testGitHubUserID, true},
		{"private repository", githubRepository{id: 101, private: true}, release, testGitHubUserID, false},
		{"draft", public, with(func(r *githubRelease) { r.draft = true }), testGitHubUserID, false},
		{"not published", public, with(func(r *githubRelease) { r.publishedAt = nil }), testGitHubUserID, false},
		{"another author", public, with(func(r *githubRelease) { r.authorID = 7 }), testGitHubUserID, false},
		{"a bot author", public, with(func(r *githubRelease) { r.authorBot = true }), testGitHubUserID, false},
		{"deleted author", public, with(func(r *githubRelease) { r.authorID = 0 }), testGitHubUserID, false},
		{"no connected account", public, with(func(r *githubRelease) { r.authorID = 0 }), 0, false},
	} {
		if got := releaseRuleMet(tc.repo, tc.release, tc.user); got != tc.want {
			t.Errorf("%s: rule met = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRules_ItemDates(t *testing.T) {
	created := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	merged := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	published := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	if got := (githubRepository{createdAt: created}).itemDate(); !got.Equal(created) {
		t.Errorf("repository date = %v, want created_at", got)
	}
	if got := (githubPullRequest{mergedAt: &merged}).itemDate(); !got.Equal(merged) {
		t.Errorf("pull request date = %v, want merged_at", got)
	}
	if got := (githubRelease{publishedAt: &published}).itemDate(); !got.Equal(published) {
		t.Errorf("release date = %v, want published_at", got)
	}
	if got := (githubRelease{}).itemDate(); !got.IsZero() {
		t.Errorf("unpublished release date = %v, want zero", got)
	}
}
