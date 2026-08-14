package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

func entryRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/entries/{sectionKey}", Operation: "upsertResumeEntry", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpsertResumeEntry},
		{Method: http.MethodDelete, Pattern: apiResumePath + "/{id}/entries/{sectionKey}/{entryId}", Operation: "deleteResumeEntry", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleDeleteResumeEntry},
	}
}

type entryMutationInput struct {
	ResumeID uuid.UUID
	Section  string
	EntryID  string
	Entry    json.RawMessage
	IsDelete bool
}

func decodeEntryUpsert(r *http.Request) (boundedInput, error) {
	resumeID, section, err := decodeEntryPath(r, false)
	if err != nil {
		return boundedInput{}, err
	}
	var body struct {
		Entry json.RawMessage `json:"entry"`
	}
	input, err := decodeJSONBody(r, &body)
	if err != nil {
		return boundedInput{}, err
	}
	if len(body.Entry) == 0 || bytes.Equal(bytes.TrimSpace(body.Entry), []byte("null")) {
		return boundedInput{}, requestInvalid("entry is required")
	}
	entryID, err := rawEntryID(body.Entry)
	if err != nil {
		return boundedInput{}, err
	}
	input.Value = entryMutationInput{ResumeID: resumeID, Section: section, EntryID: entryID, Entry: body.Entry}
	return input, nil
}

func decodeEntryDelete(r *http.Request) (boundedInput, error) {
	resumeID, section, err := decodeEntryPath(r, true)
	if err != nil {
		return boundedInput{}, err
	}
	entryID := r.PathValue("entryId")
	parsedEntryID, err := uuid.Parse(entryID)
	if err != nil {
		return boundedInput{}, requestInvalid("entry id is not a UUID")
	}
	input, err := decodeDeleteBody(r)
	if err != nil {
		return boundedInput{}, err
	}
	input.Value = entryMutationInput{ResumeID: resumeID, Section: section, EntryID: parsedEntryID.String(), IsDelete: true}
	return input, nil
}

func decodeEntryPath(r *http.Request, _ bool) (uuid.UUID, string, error) {
	resumeID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, "", requestInvalid("resume id is not a UUID")
	}
	section := r.PathValue("sectionKey")
	if !validRouteSectionKey(section) {
		return uuid.Nil, "", requestInvalid("section key is invalid")
	}
	return resumeID, section, nil
}

func validRouteSectionKey(section string) bool {
	if section == "" || len(section) > 36 {
		return false
	}
	builtin := true
	for _, value := range []byte(section) {
		if value < 'a' || value > 'z' {
			builtin = false
			break
		}
	}
	if builtin {
		return true
	}
	if len(section) != 36 {
		return false
	}
	for _, value := range []byte(section) {
		if (value < '0' || value > '9') && (value < 'a' || value > 'f') && value != '-' {
			return false
		}
	}
	return true
}

func requestInvalid(message string) error {
	return &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: message}
}

func entryCanonicalTargets(input boundedInput) ([]string, error) {
	entry, ok := input.Value.(entryMutationInput)
	if !ok {
		return nil, errors.New("entry mutation input has the wrong type")
	}
	targets := []string{"resume_id", entry.ResumeID.String(), "section_key", entry.Section}
	targets = append(targets, "entry_id", entry.EntryID)
	return targets, nil
}

func (s *Service) prepareEntryMutation(_ context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
	entry, ok := input.Value.(entryMutationInput)
	if !ok {
		return preparedInput{}, errors.New("entry mutation input has the wrong type")
	}
	apply := func(document json.RawMessage) (json.RawMessage, error) {
		if entry.IsDelete {
			return applyEntryDelete(document, entry.Section, entry.EntryID)
		}
		return applyEntryUpsert(document, entry.Section, entry.Entry)
	}
	response := s.resumeResponseBuilder(http.StatusOK, false)
	if entry.IsDelete {
		response = deletedChildResponse
	}
	return preparedInput{Input: input, Value: aggregatePreparedInput{
		ResumeID: entry.ResumeID,
		Apply:    apply,
		Response: response,
	}}, nil
}

func deletedChildResponse(row resume.Resume, _ schema.Resume, wireVersion int32) (resume.StoredResponse, error) {
	return resume.StoredResponse{
		Status: http.StatusNoContent,
		Headers: map[string]string{
			"ETag":            fmt.Sprintf(`"r%d"`, row.Revision),
			wireVersionHeader: wireVersionString(wireVersion),
		},
	}, nil
}

type rawDocumentParts struct {
	Document map[string]json.RawMessage
	Content  map[string]json.RawMessage
}

type rawSectionParts struct {
	Section     map[string]json.RawMessage
	SectionType string
	Entries     []json.RawMessage
}

func decodeRawDocument(raw json.RawMessage) (rawDocumentParts, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return rawDocumentParts{}, fmt.Errorf("decode resume document: %w", err)
	}
	var content map[string]json.RawMessage
	if err := json.Unmarshal(document["content"], &content); err != nil || content == nil {
		return rawDocumentParts{}, fmt.Errorf("decode resume content: %w", err)
	}
	return rawDocumentParts{Document: document, Content: content}, nil
}

func (parts rawDocumentParts) marshal() (json.RawMessage, error) {
	content, err := json.Marshal(parts.Content)
	if err != nil {
		return nil, fmt.Errorf("encode resume content: %w", err)
	}
	parts.Document["content"] = content
	changed, err := json.Marshal(parts.Document)
	if err != nil {
		return nil, fmt.Errorf("encode resume document: %w", err)
	}
	return changed, nil
}

func decodeRawSection(raw json.RawMessage) (rawSectionParts, error) {
	var section map[string]json.RawMessage
	if err := json.Unmarshal(raw, &section); err != nil {
		return rawSectionParts{}, fmt.Errorf("decode section: %w", err)
	}
	var sectionType string
	if err := json.Unmarshal(section["sectionType"], &sectionType); err != nil {
		return rawSectionParts{}, fmt.Errorf("decode section type: %w", err)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(section["entries"], &entries); err != nil || entries == nil {
		return rawSectionParts{}, fmt.Errorf("decode section entries: %w", err)
	}
	return rawSectionParts{Section: section, SectionType: sectionType, Entries: entries}, nil
}

func (parts rawSectionParts) marshal() (json.RawMessage, error) {
	entries, err := json.Marshal(parts.Entries)
	if err != nil {
		return nil, fmt.Errorf("encode section entries: %w", err)
	}
	parts.Section["entries"] = entries
	changed, err := json.Marshal(parts.Section)
	if err != nil {
		return nil, fmt.Errorf("encode section: %w", err)
	}
	return changed, nil
}

func validationIssueAt(path, message string) error {
	return &resume.ValidationError{
		Issues:     []string{message},
		Structured: []resume.ValidationIssue{{Path: path, Code: "invalid", Message: message}},
	}
}

func rawEntryID(raw json.RawMessage) (string, error) {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entry); err != nil || entry == nil {
		return "", validationIssueAt("entry", "entry must be a JSON object")
	}
	var id string
	if err := json.Unmarshal(entry["id"], &id); err != nil {
		return "", validationIssueAt("entry.id", "entry.id must be a UUID")
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return "", validationIssueAt("entry.id", "entry.id must be a UUID")
	}
	return parsed.String(), nil
}

func validateEntryShape(sectionType string, entry json.RawMessage) error {
	section, err := json.Marshal(map[string]any{
		"sectionType": sectionType,
		"entries":     []json.RawMessage{entry},
	})
	if err != nil {
		return fmt.Errorf("encode entry validation section: %w", err)
	}
	var typed schema.Section
	if err := json.Unmarshal(section, &typed); err != nil {
		return validationIssueAt("entry", fmt.Sprintf("entry does not match the %s entry shape", sectionType))
	}
	return nil
}

func applyEntryUpsert(raw json.RawMessage, sectionKey string, entry json.RawMessage) (json.RawMessage, error) {
	parts, err := decodeRawDocument(raw)
	if err != nil {
		return nil, err
	}
	sectionRaw, ok := parts.Content[sectionKey]
	if !ok {
		return nil, resume.ErrNotFound
	}
	section, err := decodeRawSection(sectionRaw)
	if err != nil {
		return nil, err
	}
	id, err := rawEntryID(entry)
	if err != nil {
		return nil, err
	}
	if validationErr := validateEntryShape(section.SectionType, entry); validationErr != nil {
		return nil, validationErr
	}

	replaceAt := -1
	for key, candidateRaw := range parts.Content {
		candidate, decodeErr := decodeRawSection(candidateRaw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		for index, existing := range candidate.Entries {
			existingID, idErr := rawEntryID(existing)
			if idErr != nil {
				return nil, idErr
			}
			if existingID != id {
				continue
			}
			if key != sectionKey {
				return nil, validationIssueAt("entry.id", fmt.Sprintf("entry id %q is not unique across the whole resume", id))
			}
			replaceAt = index
		}
	}
	if replaceAt >= 0 {
		section.Entries[replaceAt] = append(json.RawMessage(nil), entry...)
	} else {
		if len(section.Entries) >= 64 {
			return nil, validationIssueAt("content."+sectionKey+".entries", "a section cannot contain more than 64 entries")
		}
		section.Entries = append(section.Entries, append(json.RawMessage(nil), entry...))
	}
	parts.Content[sectionKey], err = section.marshal()
	if err != nil {
		return nil, err
	}
	return parts.marshal()
}

func applyEntryDelete(raw json.RawMessage, sectionKey, entryID string) (json.RawMessage, error) {
	if _, err := uuid.Parse(entryID); err != nil {
		return nil, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "entry id is not a UUID"}
	}
	parts, err := decodeRawDocument(raw)
	if err != nil {
		return nil, err
	}
	sectionRaw, ok := parts.Content[sectionKey]
	if !ok {
		return nil, resume.ErrNotFound
	}
	section, err := decodeRawSection(sectionRaw)
	if err != nil {
		return nil, err
	}
	removeAt := -1
	for index, entry := range section.Entries {
		id, idErr := rawEntryID(entry)
		if idErr != nil {
			return nil, idErr
		}
		if id == entryID {
			removeAt = index
			break
		}
	}
	if removeAt < 0 {
		return nil, resume.ErrNotFound
	}
	section.Entries = append(section.Entries[:removeAt], section.Entries[removeAt+1:]...)
	if section.Entries == nil {
		section.Entries = []json.RawMessage{}
	}
	parts.Content[sectionKey], err = section.marshal()
	if err != nil {
		return nil, err
	}
	return parts.marshal()
}

func (s *Service) handleUpsertResumeEntry(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, mutationSpec{
		RegisteredOperation: "upsertResumeEntry",
		RequireMatch:        true,
		Decode:              decodeEntryUpsert,
		CanonicalTargets:    entryCanonicalTargets,
		Prepare:             s.prepareEntryMutation,
		Run:                 aggregateOperation{service: s},
		Transition:          s.nonDrainingTransition,
	})
}
func (s *Service) handleDeleteResumeEntry(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, mutationSpec{
		RegisteredOperation: "deleteResumeEntry",
		RequireMatch:        true,
		Decode:              decodeEntryDelete,
		CanonicalTargets:    entryCanonicalTargets,
		Prepare:             s.prepareEntryMutation,
		Run:                 aggregateOperation{service: s},
		Transition:          s.nonDrainingTransition,
	})
}
