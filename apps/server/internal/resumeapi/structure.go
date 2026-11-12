package resumeapi

// Resume structure route: decoding the structure command list, its canonical
// targets, and the prepared aggregate mutation that applies the commands.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
)

func structureRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/structure", Operation: "updateResumeStructure", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumeStructure},
	}
}

// structureMutationInput is a decoded structure request.
type structureMutationInput struct {
	// ResumeID is the resume the commands change, from the request path.
	ResumeID uuid.UUID
	// Commands holds 1 to 100 decoded commands in request order.
	Commands []structureCommand
}

func decodeStructureMutation(r *http.Request) (boundedInput, error) {
	resumeID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return boundedInput{}, requestInvalid("resume id is not a UUID")
	}
	var body struct {
		// Commands holds the raw commands, decoded one by one in request
		// order.
		Commands []json.RawMessage `json:"commands"`
	}
	input, err := decodeJSONBody(r, &body)
	if err != nil {
		return boundedInput{}, err
	}
	if len(body.Commands) == 0 || len(body.Commands) > 100 {
		return boundedInput{}, documentInvalid("commands", "commands must contain between 1 and 100 items")
	}
	commands := make([]structureCommand, len(body.Commands))
	for index, raw := range body.Commands {
		command, decodeErr := decodeStructureCommand(raw, index)
		if decodeErr != nil {
			return boundedInput{}, decodeErr
		}
		commands[index] = command
	}
	input.Value = structureMutationInput{ResumeID: resumeID, Commands: commands}
	return input, nil
}

// decodeStructureCommand decodes the command at position. Its exact "op"
// member selects the strict shape; a case variant of "op" selects nothing.
func decodeStructureCommand(raw json.RawMessage, position int) (structureCommand, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil || members == nil {
		return structureCommand{}, commandInvalid(position, "", "must be an object")
	}
	rawOp, present := members["op"]
	if !present {
		return structureCommand{}, commandInvalid(position, "op", "is required")
	}
	var op string
	if err := json.Unmarshal(rawOp, &op); err != nil || op == "" {
		return structureCommand{}, commandInvalid(position, "op", "must be a non-empty string")
	}
	switch op {
	case "createSection":
		var command struct {
			// Op is createSection.
			Op string `json:"op"`
			// Key is the required key of the new section.
			Key *string `json:"key"`
			// SectionType is the required type of the new section.
			SectionType *string `json:"sectionType"`
			// DisplayName is the optional display name, kept raw so a null
			// value is rejected.
			DisplayName json.RawMessage `json:"displayName"`
			// IconKey is the optional icon key, kept raw so an omitted member
			// stays distinct from a present one.
			IconKey json.RawMessage `json:"iconKey"`
			// Column is the required target layout column.
			Column *string `json:"column"`
			// Index is the required zero-based target position, kept raw for
			// decodeStructureIndex.
			Index json.RawMessage `json:"index"`
		}
		if err := strictDecodeCommand(raw, position, &command); err != nil {
			return structureCommand{}, err
		}
		if command.Key == nil || command.SectionType == nil || command.Column == nil || len(command.Index) == 0 {
			return structureCommand{}, requestInvalid("createSection command does not match its contract")
		}
		index, outside, err := decodeStructureIndex(command.Index)
		if err != nil {
			return structureCommand{}, err
		}
		result := structureCommand{Op: op, Key: *command.Key, SectionType: *command.SectionType, Column: *command.Column, Index: index, HasIndex: true, IndexOutside: outside}
		if len(command.DisplayName) > 0 {
			if bytes.Equal(bytes.TrimSpace(command.DisplayName), []byte("null")) {
				return structureCommand{}, documentInvalid("commands.displayName", "createSection displayName must be a string")
			}
			var value string
			if err := json.Unmarshal(command.DisplayName, &value); err != nil {
				return structureCommand{}, documentInvalid("commands.displayName", "createSection displayName must be a string")
			}
			result.DisplayName = optionalString{Present: true, Value: &value}
		}
		if len(command.IconKey) > 0 {
			var value string
			if err := json.Unmarshal(command.IconKey, &value); err != nil {
				return structureCommand{}, documentInvalid("commands.iconKey", "createSection iconKey must be a string")
			}
			result.IconKey = optionalString{Present: true, Value: &value}
		}
		return result, nil
	case "deleteSection":
		var command struct {
			// Op is deleteSection.
			Op string `json:"op"`
			// Key is the required key of the section to delete.
			Key *string `json:"key"`
		}
		if err := strictDecodeCommand(raw, position, &command); err != nil {
			return structureCommand{}, err
		}
		if command.Key == nil {
			return structureCommand{}, requestInvalid("deleteSection command does not match its contract")
		}
		return structureCommand{Op: op, Key: *command.Key}, nil
	case "moveSection":
		var command struct {
			// Op is moveSection.
			Op string `json:"op"`
			// Key is the required key of the section to move.
			Key *string `json:"key"`
			// Column is the required target layout column.
			Column *string `json:"column"`
			// Index is the required zero-based target position, kept raw for
			// decodeStructureIndex.
			Index json.RawMessage `json:"index"`
		}
		if err := strictDecodeCommand(raw, position, &command); err != nil {
			return structureCommand{}, err
		}
		if command.Key == nil || command.Column == nil || len(command.Index) == 0 {
			return structureCommand{}, requestInvalid("moveSection command does not match its contract")
		}
		index, outside, err := decodeStructureIndex(command.Index)
		if err != nil {
			return structureCommand{}, err
		}
		return structureCommand{Op: op, Key: *command.Key, Column: *command.Column, Index: index, HasIndex: true, IndexOutside: outside}, nil
	case "reorderColumn":
		var command struct {
			// Op is reorderColumn.
			Op string `json:"op"`
			// Column is the required layout column to reorder.
			Column *string `json:"column"`
			// Keys is the required section order for Column.
			Keys *[]string `json:"keys"`
		}
		if err := strictDecodeCommand(raw, position, &command); err != nil {
			return structureCommand{}, err
		}
		if command.Column == nil || command.Keys == nil {
			return structureCommand{}, requestInvalid("reorderColumn command does not match its contract")
		}
		return structureCommand{Op: op, Column: *command.Column, Keys: *command.Keys}, nil
	default:
		return structureCommand{}, validationIssueAt("commands", fmt.Sprintf("unknown structure command %q", op))
	}
}

func decodeStructureIndex(raw json.RawMessage) (int, bool, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.ContainsAny(value, ".eE") {
		return 0, false, requestInvalid("structure command index must be an integer")
	}
	if _, ok := new(big.Int).SetString(value, 10); !ok {
		return 0, false, requestInvalid("structure command index must be an integer")
	}
	if parsed, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil {
		converted := int(parsed)
		if int64(converted) == parsed {
			return converted, false, nil
		}
	}
	return 0, true, nil
}

// strictDecodeCommand decodes the command at position into target after
// checking its property names exactly, answering request_invalid with the
// failing path.
func strictDecodeCommand(raw json.RawMessage, position int, target any) error {
	if err := api.CheckJSONBinding(raw, target); err != nil {
		var pathErr *api.JSONPathError
		if errors.As(err, &pathErr) {
			return commandInvalid(position, pathErr.Path, pathErr.Problem)
		}
		return commandInvalid(position, "", "is not valid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var trailing any
	if err := decoder.Decode(target); err != nil || !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return commandInvalid(position, "", "does not match its command")
	}
	return nil
}

// commandInvalid answers request_invalid naming path inside the command at
// position.
func commandInvalid(position int, path, problem string) error {
	failure := &api.JSONPathError{Path: path, Problem: problem}
	return requestInvalid(failure.Under(fmt.Sprintf("commands[%d]", position)).Error())
}

func structureCanonicalTargets(input boundedInput) ([]string, error) {
	structure, ok := input.Value.(structureMutationInput)
	if !ok {
		return nil, errors.New("structure mutation input has the wrong type")
	}
	return []string{"resume_id", structure.ResumeID.String()}, nil
}

func (s *Service) prepareStructureMutation(_ context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
	structure, ok := input.Value.(structureMutationInput)
	if !ok {
		return preparedInput{}, errors.New("structure mutation input has the wrong type")
	}
	return preparedInput{Input: input, Value: aggregatePreparedInput{
		ResumeID: structure.ResumeID,
		Apply: func(document json.RawMessage) (json.RawMessage, error) {
			return applyStructureCommands(document, structure.Commands)
		},
		Response: s.resumeResponseBuilder(http.StatusOK, false),
	}}, nil
}

// structureCommand is one decoded structure command. Fields other than Op
// are set only for the commands that take them.
type structureCommand struct {
	// Op is the command name: createSection, deleteSection, moveSection, or
	// reorderColumn.
	Op string
	// Key is the section key a create, delete, or move command names.
	Key string
	// SectionType is the type of the section createSection adds.
	SectionType string
	// DisplayName is createSection's optional display name.
	DisplayName optionalString
	// IconKey is createSection's optional icon key.
	IconKey optionalString
	// Column is the layout column name for createSection, moveSection, and
	// reorderColumn.
	Column string
	// Index is the zero-based position in Column for createSection and
	// moveSection.
	Index int
	// HasIndex reports whether the command carried an index.
	HasIndex bool
	// IndexOutside reports that the index is an integer outside the int
	// range.
	IndexOutside bool
	// Keys is reorderColumn's requested column order.
	Keys []string
}

func (s *Service) handleUpdateResumeStructure(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, mutationSpec{
		RegisteredOperation: "updateResumeStructure",
		RequireMatch:        true,
		Decode:              decodeStructureMutation,
		CanonicalTargets:    structureCanonicalTargets,
		Prepare:             s.prepareStructureMutation,
		Run:                 aggregateOperation{service: s},
		Transition:          s.nonDrainingTransition,
	})
}
