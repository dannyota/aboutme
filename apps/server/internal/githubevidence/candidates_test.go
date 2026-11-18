package githubevidence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func repositoryRow(id int64, private, fork bool) string {
	return fmt.Sprintf(`{"id":%d,"full_name":"octocat/repo-%d","owner":{"id":583231},"private":%v,"fork":%v,"created_at":"2020-01-%02dT00:00:00Z"}`,
		id, id, private, fork, id%28+1)
}

func rows(n int, row func(i int) string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = row(i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestCandidates_RepositoriesDropForksAndPrivateOnes(t *testing.T) {
	api := newFakeAPI(t)
	api.json("/user/repos", http.StatusOK, rows(30, func(i int) string {
		return repositoryRow(int64(i+1), i%3 == 1, i%3 == 2)
	}))
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	page, err := fx.reader.listRepositories(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.items) != 10 || page.nextPage != 2 || page.incomplete {
		t.Fatalf("page = %d items, next %d, incomplete %v; want 10, 2, false", len(page.items), page.nextPage, page.incomplete)
	}
	first := page.items[0]
	want := candidate{kind: kindRepository, repository: candidateRepository{id: 1, fullName: "octocat/repo-1"}, itemID: 1,
		title: "octocat/repo-1", date: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)}
	if first != want {
		t.Fatalf("first candidate = %+v, want %+v", first, want)
	}
	for _, c := range page.items {
		if c.itemID%3 != 1 {
			t.Fatalf("candidate %d is a fork or private", c.itemID)
		}
	}
}

func TestCandidates_NextPage(t *testing.T) {
	for _, tc := range []struct {
		page, rows, next int
	}{
		{1, 30, 2}, {9, 30, 10}, {10, 30, 0}, {1, 29, 0}, {3, 0, 0},
	} {
		api := newFakeAPI(t)
		api.json("/user/repos", http.StatusOK, rows(tc.rows, func(i int) string { return repositoryRow(int64(i+1), true, false) }))
		fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
		page, err := fx.reader.listRepositories(context.Background(), tc.page)
		if err != nil || page.nextPage != tc.next || len(page.items) != 0 {
			t.Errorf("page %d with %d rows = next %d, %d items, %v; want next %d", tc.page, tc.rows, page.nextPage, len(page.items), err, tc.next)
		}
	}
}

func TestCandidates_PageOutOfRangeSendsNothing(t *testing.T) {
	api := newFakeAPI(t)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	repo := candidateRepository{id: 101, fullName: "octocat/hello"}
	for _, page := range []int{0, 11, -1} {
		if _, err := fx.reader.listRepositories(context.Background(), page); !errors.Is(err, errPageRange) {
			t.Errorf("repositories page %d error = %v, want out of range", page, err)
		}
		if _, err := fx.reader.listPullRequests(context.Background(), "octocat", page); !errors.Is(err, errPageRange) {
			t.Errorf("pull requests page %d error = %v, want out of range", page, err)
		}
		if _, err := fx.reader.listReleases(context.Background(), repo, testGitHubUserID, page); !errors.Is(err, errPageRange) {
			t.Errorf("releases page %d error = %v, want out of range", page, err)
		}
	}
	if len(api.seen()) != 0 {
		t.Fatalf("out-of-range pages sent %d requests", len(api.seen()))
	}
}

func TestCandidates_PullRequestsCarryNoRepositoryIDAndTheClosedDate(t *testing.T) {
	api := newFakeAPI(t)
	api.json("/search/issues", http.StatusOK, `{"total_count":31,"incomplete_results":true,"items":`+rows(30, func(i int) string {
		return fmt.Sprintf(`{"repository_url":"https://api.github.com/repos/someone/lib-%d","number":%d,"title":"Change %d","closed_at":"2026-03-04T05:06:07Z"}`, i, i+1, i)
	})+`}`)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	page, err := fx.reader.listPullRequests(context.Background(), "octocat", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.items) != 30 || page.nextPage != 2 || !page.incomplete {
		t.Fatalf("page = %d items, next %d, incomplete %v; want 30, 2, true", len(page.items), page.nextPage, page.incomplete)
	}
	want := candidate{kind: kindPullRequest, repository: candidateRepository{fullName: "someone/lib-0"}, number: 1, title: "Change 0",
		date: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	if page.items[0] != want {
		t.Fatalf("first candidate = %+v, want %+v", page.items[0], want)
	}
	if !strings.Contains(api.seen()[0].uri, "q=is%3Apr+is%3Amerged+is%3Apublic+author%3Aoctocat") {
		t.Fatalf("search query = %s", api.seen()[0].uri)
	}
}

func TestCandidates_PullRequestsRefuseAnInvalidLogin(t *testing.T) {
	api := newFakeAPI(t)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	for _, login := range []string{"", "octo cat", "octocat repo:x/y", "-octocat", strings.Repeat("a", 40)} {
		if _, err := fx.reader.listPullRequests(context.Background(), login, 1); !errors.Is(err, errDestinationRefused) {
			t.Errorf("login %q error = %v, want refused", login, err)
		}
	}
	if len(api.seen()) != 0 {
		t.Fatalf("invalid logins sent %d requests", len(api.seen()))
	}
}

func TestCandidates_ReleasesDropDraftsAndOtherAuthors(t *testing.T) {
	release := func(id int, draft bool, author int64, kind string, published string) string {
		return fmt.Sprintf(`{"id":%d,"tag_name":"v%d","name":%s,"draft":%v,"prerelease":%v,"published_at":%s,"author":{"id":%d,"type":%q}}`,
			id, id, map[bool]string{true: `null`, false: fmt.Sprintf(`"Release %d"`, id)}[id%2 == 0], draft, id%5 == 0, published, author, kind)
	}
	const published = `"2026-04-05T06:07:08Z"`
	api := newFakeAPI(t)
	api.json("/repos/octocat/hello/releases", http.StatusOK, rows(6, func(i int) string {
		switch i {
		case 0:
			return release(1, false, testGitHubUserID, "User", published)
		case 1:
			return release(2, true, testGitHubUserID, "User", `null`)
		case 2:
			return release(3, false, 7, "User", published)
		case 3:
			return release(4, false, testGitHubUserID, "User", published)
		case 4:
			return release(5, false, testGitHubUserID, "Bot", published)
		default:
			return strings.Replace(release(6, false, testGitHubUserID, "User", published), `"author":{"id":583231,"type":"User"}`, `"author":null`, 1)
		}
	}))
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	repo := candidateRepository{id: 101, fullName: "octocat/hello"}
	page, err := fx.reader.listReleases(context.Background(), repo, testGitHubUserID, 1)
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	want := []candidate{
		{kind: kindRelease, repository: repo, itemID: 1, tag: "v1", title: "Release 1", date: date},
		{kind: kindRelease, repository: repo, itemID: 4, tag: "v4", title: "v4", date: date},
	}
	if len(page.items) != len(want) || page.nextPage != 0 || page.incomplete {
		t.Fatalf("page = %+v, want %+v", page, want)
	}
	for i := range want {
		if page.items[i] != want[i] {
			t.Errorf("candidate %d = %+v, want %+v", i, page.items[i], want[i])
		}
	}
	for _, bad := range []candidateRepository{{id: 101, fullName: "octocat"}, {id: 101, fullName: "octocat/hello/x"}, {id: 0, fullName: "octocat/hello"}} {
		if _, err := fx.reader.listReleases(context.Background(), bad, testGitHubUserID, 1); !errors.Is(err, errDestinationRefused) {
			t.Errorf("releases of %+v error = %v, want refused", bad, err)
		}
	}
	if len(api.seen()) != 1 {
		t.Fatalf("requests = %d, want 1", len(api.seen()))
	}
}

func TestCandidates_ListingFailuresChangeNothing(t *testing.T) {
	api := newFakeAPI(t)
	api.json("/user/repos", http.StatusOK, `[`+repositoryRow(1, false, false)+`,{"id":2}]`)
	api.json("/search/issues", http.StatusOK, `[]`)
	api.json("/repos/octocat/hello/releases", http.StatusNotFound, `{}`)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	if _, err := fx.reader.listRepositories(context.Background(), 1); !errors.Is(err, errUpstream) {
		t.Errorf("malformed repository listing error = %v, want upstream", err)
	}
	if _, err := fx.reader.listPullRequests(context.Background(), "octocat", 1); !errors.Is(err, errUpstream) {
		t.Errorf("malformed search error = %v, want upstream", err)
	}
	if _, err := fx.reader.listReleases(context.Background(), candidateRepository{id: 101, fullName: "octocat/hello"}, testGitHubUserID, 1); !errors.Is(err, errGone) {
		t.Errorf("release listing of a missing repository error = %v, want gone", err)
	}
}

func TestCandidates_TitlesAreLimitedText(t *testing.T) {
	long := strings.Repeat("x", 250)
	api := newFakeAPI(t)
	api.json("/search/issues", http.StatusOK, `{"incomplete_results":false,"items":[`+
		`{"repository_url":"https://api.github.com/repos/octocat/hello","number":1,"title":"Fix\r\n\u202ethe\u2066thing\u200b","closed_at":"2026-03-04T05:06:07Z"},`+
		`{"repository_url":"https://api.github.com/repos/octocat/hello","number":2,"title":"`+long+`","closed_at":"2026-03-04T05:06:07Z"},`+
		`{"repository_url":"https://api.github.com/repos/octocat/hello","number":3,"title":"\u202e\u200f","closed_at":"2026-03-04T05:06:07Z"}]}`)
	api.json("/repos/octocat/hello/releases", http.StatusOK, `[`+
		`{"id":1,"tag_name":"v1","name":"First\u2028release\t","draft":false,"prerelease":false,"published_at":"2026-04-05T06:07:08Z","author":{"id":583231,"type":"User"}},`+
		`{"id":2,"tag_name":"v2","name":"`+long+`","draft":false,"prerelease":false,"published_at":"2026-04-05T06:07:08Z","author":{"id":583231,"type":"User"}},`+
		`{"id":3,"tag_name":"v3","name":"\u2066\u2069","draft":false,"prerelease":false,"published_at":"2026-04-05T06:07:08Z","author":{"id":583231,"type":"User"}}]`)
	fx := newReaderFixture(t, api.server.URL, newTransport(), fixedClock())
	pulls, err := fx.reader.listPullRequests(context.Background(), "octocat", 1)
	if err != nil {
		t.Fatal(err)
	}
	releases, err := fx.reader.listReleases(context.Background(), candidateRepository{id: 101, fullName: "octocat/hello"}, testGitHubUserID, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Fix   the thing ", long[:maxTextCharacters], "octocat/hello#3", "First release ", long[:maxTextCharacters], "v3"}
	got := []string{}
	for _, item := range append(pulls.items, releases.items...) {
		requireNoFormat(t, "candidate title", item.title)
		got = append(got, item.title)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("candidate titles = %q, want %q", got, want)
	}
}
