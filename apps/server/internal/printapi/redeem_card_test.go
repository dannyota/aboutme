package printapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/printsnapshot"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

func validCardSnapshot(t *testing.T) renderjob.Snapshot {
	t.Helper()
	envelope, err := printsnapshot.NewCardEnvelope(uuid.MustParse(testResumeID), printsnapshot.CardInput{
		LayoutVersion: 1, Lng: "vi", Slug: "ada-lovelace", Name: "Ada Lovelace", Headline: "Kỹ sư", Accent: "#1d4ed8",
	}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := printsnapshot.MarshalCard(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return renderjob.Snapshot{
		ResumeID: uuid.MustParse(testResumeID), Revision: 7, SchemaVersion: schema.CurrentVersion,
		PublicGeneration: 7, Payload: payload,
	}
}

func TestCardRedemptionReturnsTheExactCardEnvelope(t *testing.T) {
	snapshot := validCardSnapshot(t)
	response := httptest.NewRecorder()
	mustHandler(t, &recordingRedeemer{snapshot: snapshot}).ServeHTTP(response, validRequest(t))
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), snapshot.Payload) {
		t.Fatalf("card response = %d %q", response.Code, response.Body.String())
	}
}

func TestCardRedemptionRejectsAnyOtherShape(t *testing.T) {
	base := validCardSnapshot(t)
	for _, test := range []struct {
		name   string
		mutate func(*renderjob.Snapshot)
	}{
		{"owner generation", func(s *renderjob.Snapshot) { s.PublicGeneration = 0 }},
		{"wrong kind", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`"kind":"card"`), []byte(`"kind":"resume"`), 1)
		}},
		{"extra envelope key", func(s *renderjob.Snapshot) {
			s.Payload = append(append([]byte{}, s.Payload[:len(s.Payload)-1]...), []byte(`,"document":{}}`)...)
		}},
		{"extra card key", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`"slug":`), []byte(`"email":"ada@example.com","slug":`), 1)
		}},
		{"missing card key", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`"headline":"Kỹ sư",`), nil, 1)
		}},
		{"other resume", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(testResumeID), []byte(testOtherID), 1)
		}},
		{"non-canonical spacing", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`"kind":"card"`), []byte(`"kind": "card"`), 1)
		}},
		{"trailing value", func(s *renderjob.Snapshot) { s.Payload = append(append([]byte{}, s.Payload...), []byte(` {}`)...) }},
		{"uppercase accent", func(s *renderjob.Snapshot) {
			s.Payload = bytes.Replace(s.Payload, []byte(`#1d4ed8`), []byte(`#1D4ED8`), 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := base
			snapshot.Payload = append([]byte{}, base.Payload...)
			test.mutate(&snapshot)
			response := httptest.NewRecorder()
			mustHandler(t, &recordingRedeemer{snapshot: snapshot}).ServeHTTP(response, validRequest(t))
			assertFailure(t, response, http.StatusNotFound, false)
		})
	}
}
