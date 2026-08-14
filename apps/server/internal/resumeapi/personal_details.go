package resumeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func personalDetailsRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/personal-details", Operation: "updateResumePersonalDetails", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumePersonalDetails},
	}
}

type personalDetailsInput struct {
	ResumeID    uuid.UUID
	Replacement map[string]json.RawMessage
}

type personalDetailsPrepared struct {
	ResumeID    uuid.UUID
	Replacement map[string]json.RawMessage
	Response    mutationResponseBuilder
}

type personalDetailsMutation struct{ service *Service }

// Run implements mutationOperation for whole personal-details replacement.
func (op personalDetailsMutation) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext,
	prepared preparedInput,
) (mutationRunResult, error) {
	input, ok := prepared.Value.(personalDetailsPrepared)
	if !ok || input.Response == nil || mutation.ExpectedRevision == nil {
		return mutationRunResult{}, fmt.Errorf("resumeapi: personal-details mutation received the wrong prepared input")
	}
	current, err := op.service.currentMutationResume(ctx, qtx, mutation, input.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	doc, err := op.service.applyAtWireVersion(current.Doc, mutation.WireVersion,
		func(wire json.RawMessage) (json.RawMessage, error) {
			return replaceWirePersonalDetails(wire, input.Replacement)
		}, false)
	if err != nil {
		return mutationRunResult{}, err
	}
	if _, saveErr := op.service.resumes.SaveDocumentTx(
		ctx, qtx, mutation.UserID, input.ResumeID, doc, *mutation.ExpectedRevision,
	); saveErr != nil {
		return mutationRunResult{}, saveErr
	}
	updated, err := op.service.resumes.GetTx(ctx, qtx, mutation.UserID, input.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	response, err := input.Response(updated, updated.Doc, mutation.WireVersion)
	return mutationRunResult{Response: response}, err
}

func (s *Service) handleUpdateResumePersonalDetails(w http.ResponseWriter, r *http.Request) {
	spec := mutationSpec{
		RegisteredOperation: "updateResumePersonalDetails", RequireMatch: true,
		Decode: func(r *http.Request) (boundedInput, error) {
			id, err := parseResumePathID(r)
			if err != nil {
				return boundedInput{}, err
			}
			var replacement map[string]json.RawMessage
			decoded, decodeErr := decodeJSONBody(r, &replacement)
			if decodeErr != nil {
				return boundedInput{}, decodeErr
			}
			decoded.Value = personalDetailsInput{ResumeID: id, Replacement: replacement}
			return decoded, nil
		},
		CanonicalTargets: func(input boundedInput) ([]string, error) {
			decoded, ok := input.Value.(personalDetailsInput)
			if !ok {
				return nil, internalClientError()
			}
			return []string{"resume_id", decoded.ResumeID.String()}, nil
		},
		Prepare: func(_ context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
			decoded, ok := input.Value.(personalDetailsInput)
			if !ok {
				return preparedInput{}, internalClientError()
			}
			if decoded.Replacement == nil {
				return preparedInput{}, documentInvalid("personalDetails", "personal-details must be an object")
			}
			for name := range decoded.Replacement {
				switch name {
				case "fullName", "headline", "details":
				case "photo":
					return preparedInput{}, documentInvalid("personalDetails.photo", "photo is server-owned")
				default:
					return preparedInput{}, &clientError{
						Status: http.StatusBadRequest, Code: "request_invalid",
						Message: "personal-details contains an unknown field",
					}
				}
			}
			return preparedInput{Input: input, Value: personalDetailsPrepared{
				ResumeID: decoded.ResumeID, Replacement: cloneRawObject(decoded.Replacement),
				Response: s.resumeResponseBuilder(http.StatusOK, false),
			}}, nil
		},
		Run:        personalDetailsMutation{service: s},
		Transition: s.nonDrainingTransition,
	}
	s.executeMutation(w, r, spec)
}

func replaceWirePersonalDetails(document json.RawMessage,
	replacement map[string]json.RawMessage,
) (json.RawMessage, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(document, &root); err != nil {
		return nil, fmt.Errorf("resumeapi: decode emitted document for personal-details: %w", err)
	}
	var current map[string]json.RawMessage
	if err := json.Unmarshal(root["personalDetails"], &current); err != nil {
		return nil, fmt.Errorf("resumeapi: decode emitted personal-details: %w", err)
	}
	next := cloneRawObject(replacement)
	if photo, present := current["photo"]; present {
		next["photo"] = append(json.RawMessage(nil), photo...)
	}
	rawPersonalDetails, err := json.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("resumeapi: encode replacement personal-details: %w", err)
	}
	root["personalDetails"] = rawPersonalDetails
	changed, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("resumeapi: encode document after personal-details replacement: %w", err)
	}
	return changed, nil
}

func cloneRawObject(value map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(value))
	for name, raw := range value {
		cloned[name] = append(json.RawMessage(nil), raw...)
	}
	return cloned
}
