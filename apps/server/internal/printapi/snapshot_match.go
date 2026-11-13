package printapi

import (
	"bytes"
	"encoding/json"
	"strconv"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/printsnapshot"
	"danny.vn/aboutme/apps/server/internal/renderjob"
)

func snapshotMatches(snapshot renderjob.Snapshot, requestedResumeID uuid.UUID) bool {
	if snapshot.ResumeID == uuid.Nil || snapshot.ResumeID != requestedResumeID || snapshot.Revision < 1 ||
		snapshot.SchemaVersion != int(schema.CurrentVersion) || snapshot.PublicGeneration < 0 ||
		(snapshot.PublicGeneration != 0 && snapshot.PublicGeneration != snapshot.Revision) ||
		len(snapshot.Payload) == 0 || len(snapshot.Payload) > printsnapshot.MaxEnvelopeBytes || !utf8.Valid(snapshot.Payload) ||
		validateUniqueJSON(snapshot.Payload) != nil {
		return false
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(snapshot.Payload, &envelope); err != nil {
		return false
	}
	if _, card := envelope["kind"]; card {
		return cardSnapshotMatches(snapshot)
	}
	var version int
	if json.Unmarshal(envelope["version"], &version) != nil || (version != 1 && version != 2) {
		return false
	}
	keys := []string{"version", "resumeId", "revision", "publicGeneration", "lng", "document"}
	if version == 2 {
		keys = append(keys, "projectLinks")
	}
	if len(envelope) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, present := envelope[key]; !present {
			return false
		}
	}
	var resumeID, revision, lng string
	if json.Unmarshal(envelope["resumeId"], &resumeID) != nil || resumeID != snapshot.ResumeID.String() ||
		json.Unmarshal(envelope["revision"], &revision) != nil || revision != strconv.FormatInt(snapshot.Revision, 10) ||
		json.Unmarshal(envelope["lng"], &lng) != nil || !canonicalLanguage(lng) {
		return false
	}

	wantGeneration := "null"
	if snapshot.PublicGeneration != 0 {
		encoded, err := json.Marshal(strconv.FormatInt(snapshot.PublicGeneration, 10))
		if err != nil {
			return false
		}
		wantGeneration = string(encoded)
	}
	if string(envelope["publicGeneration"]) != wantGeneration {
		return false
	}

	var document map[string]json.RawMessage
	var documentVersion int
	if json.Unmarshal(envelope["document"], &document) != nil || document == nil ||
		json.Unmarshal(document["schemaVersion"], &documentVersion) != nil || documentVersion != snapshot.SchemaVersion {
		return false
	}
	return version == 1 || printsnapshot.ProjectLinksMatch(envelope["document"], envelope["projectLinks"])
}

func cardSnapshotMatches(snapshot renderjob.Snapshot) bool {
	if snapshot.PublicGeneration != snapshot.Revision {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	var envelope printsnapshot.CardEnvelope
	if err := decoder.Decode(&envelope); err != nil || envelope.ResumeID != snapshot.ResumeID.String() {
		return false
	}
	canonical, err := printsnapshot.MarshalCard(envelope)
	return err == nil && bytes.Equal(canonical, snapshot.Payload)
}

func canonicalLanguage(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > printsnapshot.MaxLanguageCharacters {
		return false
	}
	tag, err := language.Parse(value)
	return err == nil && tag.String() == value
}
