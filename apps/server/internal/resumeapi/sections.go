package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

func sectionRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/sections/{sectionKey}", Operation: "updateResumeSection", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumeSection},
	}
}

type sectionMutationInput struct {
	ResumeID uuid.UUID
	Section  string
	Patch    sectionPatch
}

func decodeSectionMutation(r *http.Request) (boundedInput, error) {
	resumeID, section, err := decodeEntryPath(r, false)
	if err != nil {
		return boundedInput{}, err
	}
	var body struct {
		DisplayName json.RawMessage `json:"displayName"`
		IconKey     json.RawMessage `json:"iconKey"`
		EntryOrder  json.RawMessage `json:"entryOrder"`
	}
	input, err := decodeJSONBody(r, &body)
	if err != nil {
		return boundedInput{}, err
	}
	var patch sectionPatch
	if len(body.DisplayName) > 0 {
		if bytes.Equal(bytes.TrimSpace(body.DisplayName), []byte("null")) {
			return boundedInput{}, documentInvalid("displayName", "displayName must be a string")
		}
		var value string
		if err := json.Unmarshal(body.DisplayName, &value); err != nil {
			return boundedInput{}, documentInvalid("displayName", "displayName must be a string")
		}
		patch.DisplayName = optionalString{Present: true, Value: &value}
	}
	if len(body.IconKey) > 0 {
		patch.IconKey.Present = true
		if !bytes.Equal(bytes.TrimSpace(body.IconKey), []byte("null")) {
			var value string
			if err := json.Unmarshal(body.IconKey, &value); err != nil {
				return boundedInput{}, documentInvalid("iconKey", "iconKey must be a string or null")
			}
			patch.IconKey.Value = &value
		}
	}
	if len(body.EntryOrder) > 0 {
		if err := json.Unmarshal(body.EntryOrder, &patch.EntryOrder); err != nil || patch.EntryOrder == nil {
			return boundedInput{}, requestInvalid("entryOrder must be an array")
		}
		patch.HasOrder = true
	}
	if !patch.DisplayName.Present && !patch.IconKey.Present && !patch.HasOrder {
		return boundedInput{}, requestInvalid("at least one section field is required")
	}
	input.Value = sectionMutationInput{ResumeID: resumeID, Section: section, Patch: patch}
	return input, nil
}

func sectionCanonicalTargets(input boundedInput) ([]string, error) {
	section, ok := input.Value.(sectionMutationInput)
	if !ok {
		return nil, errors.New("section mutation input has the wrong type")
	}
	return []string{"resume_id", section.ResumeID.String(), "section_key", section.Section}, nil
}

func (s *Service) prepareSectionMutation(_ context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
	section, ok := input.Value.(sectionMutationInput)
	if !ok {
		return preparedInput{}, errors.New("section mutation input has the wrong type")
	}
	return preparedInput{Input: input, Value: aggregatePreparedInput{
		ResumeID: section.ResumeID,
		Apply: func(document json.RawMessage) (json.RawMessage, error) {
			return applySectionPatch(document, section.Section, section.Patch)
		},
		Response: s.resumeResponseBuilder(http.StatusOK, false),
	}}, nil
}

type optionalString struct {
	Present bool
	Value   *string
}

type sectionPatch struct {
	DisplayName optionalString
	IconKey     optionalString
	EntryOrder  []string
	HasOrder    bool
}

func applySectionPatch(raw json.RawMessage, sectionKey string, patch sectionPatch) (json.RawMessage, error) {
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
	if patch.DisplayName.Present {
		if patch.DisplayName.Value == nil {
			return nil, validationIssueAt("content."+sectionKey+".displayName", "displayName must be a string")
		}
		section.Section["displayName"], err = json.Marshal(*patch.DisplayName.Value)
		if err != nil {
			return nil, fmt.Errorf("encode displayName: %w", err)
		}
	}
	if patch.IconKey.Present {
		if patch.IconKey.Value == nil {
			delete(section.Section, "iconKey")
		} else {
			section.Section["iconKey"], err = json.Marshal(*patch.IconKey.Value)
			if err != nil {
				return nil, fmt.Errorf("encode iconKey: %w", err)
			}
		}
	}
	if patch.HasOrder {
		if permutationErr := validatePermutation(section.Entries, patch.EntryOrder); permutationErr != nil {
			return nil, permutationErr
		}
		byID := make(map[string]json.RawMessage, len(section.Entries))
		for _, entry := range section.Entries {
			id, idErr := rawEntryID(entry)
			if idErr != nil {
				return nil, idErr
			}
			byID[id] = entry
		}
		reordered := make([]json.RawMessage, len(patch.EntryOrder))
		for index, id := range patch.EntryOrder {
			reordered[index] = byID[id]
		}
		section.Entries = reordered
	}
	parts.Content[sectionKey], err = section.marshal()
	if err != nil {
		return nil, err
	}
	return parts.marshal()
}

func validatePermutation(entries []json.RawMessage, order []string) error {
	if len(entries) != len(order) {
		return validationIssueAt("entryOrder", "entryOrder must be a permutation of the existing entry ids")
	}
	existing := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		id, err := rawEntryID(entry)
		if err != nil {
			return err
		}
		existing[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(order))
	for _, id := range order {
		if _, duplicate := seen[id]; duplicate {
			return validationIssueAt("entryOrder", "entryOrder must not contain duplicate ids")
		}
		seen[id] = struct{}{}
		if _, ok := existing[id]; !ok {
			return validationIssueAt("entryOrder", "entryOrder must be a permutation of the existing entry ids")
		}
	}
	return nil
}

func (s *Service) handleUpdateResumeSection(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, mutationSpec{
		RegisteredOperation: "updateResumeSection",
		RequireMatch:        true,
		Decode:              decodeSectionMutation,
		CanonicalTargets:    sectionCanonicalTargets,
		Prepare:             s.prepareSectionMutation,
		Run:                 aggregateOperation{service: s},
	})
}
