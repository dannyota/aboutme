package printapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/printsnapshot"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/renderjob"
)

const (
	linkedEntryID   = "3abcdef0-abcd-4def-8abc-abcdefabcdef"
	unlinkedEntryID = "3abcdef0-abcd-4def-8abc-abcdefabcdee"
	outsideEntryID  = "3abcdef0-abcd-4def-8abc-abcdefabcded"
	linkedPage      = "https://aboutme.example/ada-lovelace/projects/atlas"
)

func linkedSnapshot(t *testing.T, publicGeneration int64) renderjob.Snapshot {
	t.Helper()
	title := "Atlas"
	document := publicresume.PublicResumeDocument{
		SchemaVersion:   schema.CurrentVersion,
		PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Ada Lovelace"},
		Content: publicresume.PublicContent{
			"projects": {SectionType: string(schema.Project), ProjectEntries: []publicresume.PublicProjectEntry{{ID: linkedEntryID, Title: &title}, {ID: unlinkedEntryID}}},
			"outside":  {SectionType: string(schema.Project), ProjectEntries: []publicresume.PublicProjectEntry{{ID: outsideEntryID, Title: &title}}},
		},
	}
	document.Customization.Layout.Sections.Main = []string{"projects"}
	envelope := printsnapshot.Envelope{Version: 1, ResumeID: testResumeID, Revision: "7", Lng: "en", Document: document}
	if publicGeneration != 0 {
		generation := "7"
		envelope.PublicGeneration = &generation
	}
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	linked, err := printsnapshot.WithProjectLinks(envelope, origin, "ada-lovelace", []publicresume.ProjectLink{{EntryID: linkedEntryID, ProjectSlug: "atlas", URL: linkedPage}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := printsnapshot.Marshal(linked)
	if err != nil {
		t.Fatal(err)
	}
	return renderjob.Snapshot{ResumeID: uuid.MustParse(testResumeID), Revision: 7, SchemaVersion: schema.CurrentVersion, PublicGeneration: publicGeneration, Payload: payload}
}

func redeemSnapshot(t *testing.T, snapshot renderjob.Snapshot) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	mustHandler(t, &recordingRedeemer{snapshot: snapshot}).ServeHTTP(response, validRequest(t))
	return response
}

func TestRedemptionAcceptsALinkedEnvelope(t *testing.T) {
	for name, generation := range map[string]int64{"public PDF": 7, "owner export": 0} {
		t.Run(name, func(t *testing.T) {
			snapshot := linkedSnapshot(t, generation)
			if !bytes.Contains(snapshot.Payload, []byte(`"version":2`)) || !bytes.Contains(snapshot.Payload, []byte(`"projectLinks":{"`+linkedEntryID+`":"`+linkedPage+`"}`)) {
				t.Fatalf("fixture is not a linked envelope: %s", snapshot.Payload)
			}
			response := redeemSnapshot(t, snapshot)
			if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), snapshot.Payload) {
				t.Fatalf("linked envelope = %d %q, want the exact payload", response.Code, response.Body.String())
			}
		})
	}
}

func TestRedemptionRefusesAMalformedLinkedEnvelope(t *testing.T) {
	base := linkedSnapshot(t, 7)
	links := []byte(`,"projectLinks":{"` + linkedEntryID + `":"` + linkedPage + `"}`)
	replace := func(from, to []byte) func([]byte) []byte {
		return func(payload []byte) []byte {
			if bytes.Count(payload, from) != 1 {
				t.Fatalf("fixture text %s appears %d times", from, bytes.Count(payload, from))
			}
			return bytes.Replace(payload, from, to, 1)
		}
	}
	withLinks := func(value string) func([]byte) []byte {
		return replace(links, []byte(`,"projectLinks":`+value))
	}
	for name, mutate := range map[string]func([]byte) []byte{
		"version 2 without links":  replace(links, nil),
		"version 1 with links":     replace([]byte(`"version":2`), []byte(`"version":1`)),
		"version 3":                replace([]byte(`"version":2`), []byte(`"version":3`)),
		"empty map":                withLinks(`{}`),
		"null map":                 withLinks(`null`),
		"array":                    withLinks(`["` + linkedPage + `"]`),
		"unknown entry":            withLinks(`{"3abcdef0-abcd-4def-8abc-abcdefabcdea":"` + linkedPage + `"}`),
		"entry outside the layout": withLinks(`{"` + outsideEntryID + `":"` + linkedPage + `"}`),
		"value not a page":         withLinks(`{"` + linkedEntryID + `":"https://aboutme.example/ada-lovelace"}`),
		"value with a query":       withLinks(`{"` + linkedEntryID + `":"` + linkedPage + `?x=1"}`),
		"value of another scheme":  withLinks(`{"` + linkedEntryID + `":"javascript:alert(1)"}`),
		"value not a string":       withLinks(`{"` + linkedEntryID + `":1}`),
		"duplicate link key":       withLinks(`{"` + linkedEntryID + `":"` + linkedPage + `","` + linkedEntryID + `":"` + linkedPage + `"}`),
		"eighth envelope key":      replace(links, append(append([]byte{}, links...), []byte(`,"extra":true`)...)),
		"resume mismatch":          replace([]byte(`"resumeId":"`+testResumeID+`"`), []byte(`"resumeId":"`+testOtherID+`"`)),
		"revision mismatch":        replace([]byte(`"revision":"7"`), []byte(`"revision":"8"`)),
		"schema mismatch":          replace([]byte(`"schemaVersion":6`), []byte(`"schemaVersion":7`)),
		"generation mismatch":      replace([]byte(`"publicGeneration":"7"`), []byte(`"publicGeneration":null`)),
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := base
			snapshot.Payload = mutate(append([]byte{}, base.Payload...))
			assertFailure(t, redeemSnapshot(t, snapshot), http.StatusNotFound, false)
		})
	}
}
