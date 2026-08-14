package publicresume

import (
	"encoding/json"
	"testing"
)

func TestPublicDetailsPresence(t *testing.T) {
	entry := PublicPersonalDetail{ID: "d1", Type: "email", Value: "ada@example.test"}
	for _, test := range []struct {
		name    string
		details PublicDetails
		want    string
	}{
		{"absent", AbsentPublicDetails(), `{"fullName":""}`},
		{"present empty", PresentPublicDetails([]PublicPersonalDetail{}), `{"details":[],"fullName":""}`},
		{"present value", PresentPublicDetails([]PublicPersonalDetail{entry}), `{"details":[{"id":"d1","type":"email","value":"ada@example.test"}],"fullName":""}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := json.Marshal(PublicPersonalDetails{Details: test.details})
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("MarshalJSON() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestPublicContentSortsKeys(t *testing.T) {
	encoded, err := json.Marshal(PublicContent{
		"z": {SectionType: "profile", ProfileEntries: []PublicProfileEntry{}},
		"a": {SectionType: "profile", ProfileEntries: []PublicProfileEntry{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"a":{"sectionType":"profile","entries":[]},"z":{"sectionType":"profile","entries":[]}}`; got != want {
		t.Fatalf("content = %s, want %s", got, want)
	}
}
