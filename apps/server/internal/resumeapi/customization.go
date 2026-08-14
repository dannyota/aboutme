package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const (
	customizationSet       = "set"
	customizationUnset     = "unset"
	maxCustomizationDeltas = 100
)

type customizationDelta struct {
	Op    string
	Path  string
	Value json.RawMessage
}

type customizationDeltaWire struct {
	Op    *string         `json:"op"`
	Path  *string         `json:"path"`
	Value json.RawMessage `json:"value"`
}

type customizationRequestWire struct {
	Deltas []customizationDeltaWire `json:"deltas"`
}

type customizationRequest struct {
	Deltas []customizationDelta
}

func customizationRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/customization", Operation: "updateResumeCustomization", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumeCustomization},
	}
}

func (s *Service) handleUpdateResumeCustomization(w http.ResponseWriter, r *http.Request) {
	var resumeID uuid.UUID
	s.executeMutation(w, r, mutationSpec{
		RegisteredOperation: "updateResumeCustomization",
		RequireMatch:        true,
		Decode:              decodeCustomizationRequest,
		CanonicalTargets: func(boundedInput) ([]string, error) {
			parsed, err := canonicalCustomizationResumeID(r.PathValue("id"))
			if err != nil {
				return nil, err
			}
			resumeID = parsed
			return []string{"resume_id", resumeID.String()}, nil
		},
		Prepare: func(_ context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
			request, ok := input.Value.(customizationRequest)
			if !ok {
				return preparedInput{}, fmt.Errorf("resumeapi: customization input has the wrong type")
			}
			targetsFont := false
			for _, delta := range request.Deltas {
				if delta.Path == "font.family" {
					targetsFont = true
					break
				}
			}
			return preparedInput{Input: input, Value: aggregatePreparedInput{
				ResumeID: resumeID,
				Apply: func(document json.RawMessage) (json.RawMessage, error) {
					return applyCustomizationDeltas(document, request.Deltas)
				},
				Response: s.resumeResponseBuilder(http.StatusOK, false), TargetsFont: targetsFont,
			}}, nil
		},
		Run:        aggregateOperation{service: s},
		Transition: s.nonDrainingTransition,
	})
}

func decodeCustomizationRequest(r *http.Request) (boundedInput, error) {
	var wire customizationRequestWire
	input, err := decodeJSONBody(r, &wire)
	if err != nil {
		return boundedInput{}, err
	}
	if len(wire.Deltas) == 0 {
		return boundedInput{}, customizationRequestInvalid("deltas must contain at least one change")
	}
	if len(wire.Deltas) > maxCustomizationDeltas {
		return boundedInput{}, customizationDocumentInvalid("deltas")
	}
	request := customizationRequest{Deltas: make([]customizationDelta, len(wire.Deltas))}
	for index, delta := range wire.Deltas {
		if delta.Op == nil || delta.Path == nil {
			return boundedInput{}, customizationRequestInvalid("each delta requires op and path")
		}
		converted := customizationDelta{Op: *delta.Op, Path: *delta.Path, Value: delta.Value}
		if !customizationPathAllowed(converted.Op, converted.Path) &&
			(converted.Op == customizationSet || converted.Op == customizationUnset) {
			return boundedInput{}, customizationPathDenied()
		}
		switch converted.Op {
		case customizationSet:
			if len(converted.Value) == 0 || bytes.Equal(bytes.TrimSpace(converted.Value), []byte("null")) {
				return boundedInput{}, customizationRequestInvalid("set requires a non-null value")
			}
			if err := validateCustomizationSetValue(converted.Path, converted.Value); err != nil {
				return boundedInput{}, err
			}
		case customizationUnset:
			if len(converted.Value) != 0 {
				return boundedInput{}, customizationRequestInvalid("unset forbids value")
			}
		default:
			return boundedInput{}, customizationRequestInvalid("delta op must be set or unset")
		}
		request.Deltas[index] = converted
	}
	input.Value = request
	return input, nil
}

func validateCustomizationSetValue(path string, raw json.RawMessage) error {
	value, err := decodeCustomizationValue(raw)
	if err != nil {
		return err
	}
	kind := customizationSetValueKinds[path]
	valid := false
	switch kind {
	case customizationString:
		_, valid = value.(string)
	case customizationBoolean:
		_, valid = value.(bool)
	case customizationNumber:
		_, valid = value.(json.Number)
	case customizationInteger:
		number, numberOK := value.(json.Number)
		if numberOK {
			_, parseErr := strconv.ParseInt(number.String(), 10, 64)
			valid = parseErr == nil
		}
	}
	if !valid {
		return customizationDocumentInvalid(path)
	}
	return nil
}

func applyCustomizationDeltas(document json.RawMessage, deltas []customizationDelta) (json.RawMessage, error) {
	if len(deltas) == 0 {
		return nil, customizationRequestInvalid("deltas must contain at least one change")
	}
	if len(deltas) > maxCustomizationDeltas {
		return nil, customizationDocumentInvalid("deltas")
	}
	for _, delta := range deltas {
		if !customizationPathAllowed(delta.Op, delta.Path) {
			return nil, customizationPathDenied()
		}
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(document, &fields); err != nil {
		return nil, fmt.Errorf("resumeapi: decode document for customization: %w", err)
	}
	customizationRaw, ok := fields["customization"]
	if !ok {
		return nil, fmt.Errorf("resumeapi: document has no customization subtree")
	}
	customization, err := decodeCustomizationObject(customizationRaw)
	if err != nil {
		return nil, err
	}
	for _, delta := range deltas {
		segments := strings.Split(delta.Path, ".")
		parent := customization
		for _, segment := range segments[:len(segments)-1] {
			child, exists := parent[segment]
			if !exists {
				created := make(map[string]any)
				parent[segment] = created
				parent = created
				continue
			}
			childObject, ok := child.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("resumeapi: customization parent %q is not an object", segment)
			}
			parent = childObject
		}
		leaf := segments[len(segments)-1]
		switch delta.Op {
		case customizationSet:
			value, decodeErr := decodeCustomizationValue(delta.Value)
			if decodeErr != nil {
				return nil, decodeErr
			}
			parent[leaf] = value
		case customizationUnset:
			delete(parent, leaf)
		}
	}
	changedCustomization, err := json.Marshal(customization)
	if err != nil {
		return nil, fmt.Errorf("resumeapi: encode customization subtree: %w", err)
	}
	fields["customization"] = changedCustomization
	changed, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("resumeapi: encode customized document: %w", err)
	}
	return changed, nil
}

func decodeCustomizationObject(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("resumeapi: decode customization subtree: %w", err)
	}
	if err := requireDecoderEOF(decoder); err != nil {
		return nil, fmt.Errorf("resumeapi: decode customization subtree: %w", err)
	}
	return object, nil
}

func decodeCustomizationValue(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, customizationRequestInvalid("set value is not valid JSON")
	}
	if err := requireDecoderEOF(decoder); err != nil {
		return nil, customizationRequestInvalid("set value has trailing data")
	}
	if value == nil {
		return nil, customizationRequestInvalid("set requires a non-null value")
	}
	return value, nil
}

func canonicalCustomizationResumeID(raw string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed.String() != raw {
		return uuid.Nil, customizationRequestInvalid("resume id must be one canonical UUID")
	}
	return parsed, nil
}

func customizationRequestInvalid(message string) *clientError {
	return &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: message}
}

func customizationPathDenied() *clientError {
	return &clientError{Status: http.StatusUnprocessableEntity, Code: "customization_path_denied", Message: "that customization path cannot be written"}
}

func customizationDocumentInvalid(path string) *clientError {
	return &clientError{
		Status: http.StatusUnprocessableEntity, Code: "document_invalid",
		Message: "resume document is invalid",
		Details: map[string]any{"issues": []map[string]string{{
			"path": path, "code": "invalid", "message": "the customization value is invalid",
		}}},
	}
}
