package githubevidence

import (
	"strconv"
	"testing"
)

func TestCandidateMemory_KeepsAtMostTenPagesPerKind(t *testing.T) {
	m := newCandidateMemory()
	repo := func(page int) candidate {
		id := int64(1000 + page)
		return candidate{kind: kindRepository, repository: candidateRepository{id: id, fullName: "octocat/r" + strconv.Itoa(page)}, itemID: id}
	}
	for page := 1; page <= maxListingPages; page++ {
		m.remember(kindRepository, 0, page, []candidate{repo(page)}, []string{"r" + strconv.Itoa(page)})
	}
	m.remember(kindPullRequest, 0, 1, []candidate{{kind: kindPullRequest, number: 1}}, []string{"p1"})
	if _, ok := m.candidate("r1"); !ok {
		t.Fatal("a page of another kind evicted a repository page")
	}
	m.remember(kindRepository, 0, 3, []candidate{repo(3)}, []string{"r3-again"})
	if _, ok := m.candidate("r3"); ok {
		t.Fatal("listing a page again kept its old candidate ids")
	}
	if _, ok := m.candidate("r1"); !ok {
		t.Fatal("listing a remembered page again evicted another page")
	}
	m.remember(kindRepository, 0, 11, []candidate{repo(11)}, []string{"r11"})
	if _, ok := m.candidate("r1"); ok {
		t.Fatal("an eleventh repository page kept the oldest page")
	}
	if _, ok := m.repository(1001); ok {
		t.Fatal("an evicted page still admits its repository for release listings")
	}
	for _, id := range []string{"r2", "r3-again", "r11", "p1"} {
		if _, ok := m.candidate(id); !ok {
			t.Fatalf("candidate %s was dropped", id)
		}
	}
	if got, ok := m.repository(1011); !ok || got.fullName != "octocat/r11" {
		t.Fatalf("repository 1011 = %+v, %v", got, ok)
	}
}
