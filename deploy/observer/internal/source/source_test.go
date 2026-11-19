package source

import (
	"regexp"
	"slices"
	"testing"
)

func TestOwners(t *testing.T) {
	if RepositoryID != "1318874703" {
		t.Errorf("RepositoryID = %q", RepositoryID)
	}
	want := map[string]string{"dannyota": "244348267", "dntmakers": "342323513"}
	if got := Owners(); !slices.Equal(got, []string{"dannyota", "dntmakers"}) {
		t.Errorf("Owners() = %v", got)
	}
	for owner, id := range want {
		if !Valid(owner) || OwnerID(owner) != id {
			t.Errorf("%s: valid %v, id %q, want %q", owner, Valid(owner), OwnerID(owner), id)
		}
		if got := Repository(owner); got != "https://github.com/"+owner+"/aboutme" {
			t.Errorf("Repository(%s) = %q", owner, got)
		}
	}
	re := regexp.MustCompile(`^(` + OwnerPattern + `)$`)
	for owner := range want {
		if !re.MatchString(owner) {
			t.Errorf("OwnerPattern refuses %s", owner)
		}
	}
}

func TestOwnersRefuseOthers(t *testing.T) {
	re := regexp.MustCompile(`^(` + OwnerPattern + `)$`)
	for _, owner := range []string{"", "someone", "Dannyota", "DNTMAKERS", "dannyota ", "dannyota/aboutme", "dntmakers2"} {
		if Valid(owner) || OwnerID(owner) != "" || Repository(owner) != "" || re.MatchString(owner) {
			t.Errorf("accepted owner %q", owner)
		}
	}
	owners := Owners()
	owners[0] = "someone"
	if Owners()[0] != "dannyota" {
		t.Error("Owners shares its slice")
	}
}
