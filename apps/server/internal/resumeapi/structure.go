package resumeapi

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
)

func structureRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/structure", Operation: "updateResumeStructure", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumeStructure},
	}
}

type structureMutationInput struct {
	ResumeID uuid.UUID
	Commands []structureCommand
}

func decodeStructureMutation(r *http.Request) (boundedInput, error) {
	resumeID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return boundedInput{}, requestInvalid("resume id is not a UUID")
	}
	var body struct {
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
		command, decodeErr := decodeStructureCommand(raw)
		if decodeErr != nil {
			return boundedInput{}, decodeErr
		}
		commands[index] = command
	}
	input.Value = structureMutationInput{ResumeID: resumeID, Commands: commands}
	return input, nil
}

func decodeStructureCommand(raw json.RawMessage) (structureCommand, error) {
	var discriminator struct {
		Op string `json:"op"`
	}
	if err := json.Unmarshal(raw, &discriminator); err != nil || discriminator.Op == "" {
		return structureCommand{}, requestInvalid("structure command op is required")
	}
	switch discriminator.Op {
	case "createSection":
		var command struct {
			Op          string          `json:"op"`
			Key         *string         `json:"key"`
			SectionType *string         `json:"sectionType"`
			DisplayName json.RawMessage `json:"displayName"`
			IconKey     json.RawMessage `json:"iconKey"`
			Column      *string         `json:"column"`
			Index       json.RawMessage `json:"index"`
		}
		if err := strictDecodeRaw(raw, &command); err != nil || command.Key == nil || command.SectionType == nil || command.Column == nil || len(command.Index) == 0 {
			return structureCommand{}, requestInvalid("createSection command does not match its contract")
		}
		index, outside, err := decodeStructureIndex(command.Index)
		if err != nil {
			return structureCommand{}, err
		}
		result := structureCommand{Op: discriminator.Op, Key: *command.Key, SectionType: *command.SectionType, Column: *command.Column, Index: index, HasIndex: true, IndexOutside: outside}
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
			Op  string  `json:"op"`
			Key *string `json:"key"`
		}
		if err := strictDecodeRaw(raw, &command); err != nil || command.Key == nil {
			return structureCommand{}, requestInvalid("deleteSection command does not match its contract")
		}
		return structureCommand{Op: discriminator.Op, Key: *command.Key}, nil
	case "moveSection":
		var command struct {
			Op     string          `json:"op"`
			Key    *string         `json:"key"`
			Column *string         `json:"column"`
			Index  json.RawMessage `json:"index"`
		}
		if err := strictDecodeRaw(raw, &command); err != nil || command.Key == nil || command.Column == nil || len(command.Index) == 0 {
			return structureCommand{}, requestInvalid("moveSection command does not match its contract")
		}
		index, outside, err := decodeStructureIndex(command.Index)
		if err != nil {
			return structureCommand{}, err
		}
		return structureCommand{Op: discriminator.Op, Key: *command.Key, Column: *command.Column, Index: index, HasIndex: true, IndexOutside: outside}, nil
	case "reorderColumn":
		var command struct {
			Op     string    `json:"op"`
			Column *string   `json:"column"`
			Keys   *[]string `json:"keys"`
		}
		if err := strictDecodeRaw(raw, &command); err != nil || command.Column == nil || command.Keys == nil {
			return structureCommand{}, requestInvalid("reorderColumn command does not match its contract")
		}
		return structureCommand{Op: discriminator.Op, Column: *command.Column, Keys: *command.Keys}, nil
	default:
		return structureCommand{}, validationIssueAt("commands", fmt.Sprintf("unknown structure command %q", discriminator.Op))
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

func strictDecodeRaw(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("structure command has trailing data")
	}
	return nil
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

type structureCommand struct {
	Op           string
	Key          string
	SectionType  string
	DisplayName  optionalString
	IconKey      optionalString
	Column       string
	Index        int
	HasIndex     bool
	IndexOutside bool
	Keys         []string
}

type rawLayout struct {
	Document      rawDocumentParts
	Customization map[string]json.RawMessage
	Layout        map[string]json.RawMessage
	Sections      map[string]json.RawMessage
	Main          []string
	Sidebar       []string
}

func decodeRawLayout(raw json.RawMessage) (rawLayout, error) {
	document, err := decodeRawDocument(raw)
	if err != nil {
		return rawLayout{}, err
	}
	var customization map[string]json.RawMessage
	if err := json.Unmarshal(document.Document["customization"], &customization); err != nil {
		return rawLayout{}, fmt.Errorf("decode customization: %w", err)
	}
	var layout map[string]json.RawMessage
	if err := json.Unmarshal(customization["layout"], &layout); err != nil {
		return rawLayout{}, fmt.Errorf("decode layout: %w", err)
	}
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(layout["sections"], &sections); err != nil {
		return rawLayout{}, fmt.Errorf("decode layout sections: %w", err)
	}
	var main, sidebar []string
	if err := json.Unmarshal(sections["main"], &main); err != nil {
		return rawLayout{}, fmt.Errorf("decode main layout: %w", err)
	}
	if err := json.Unmarshal(sections["sidebar"], &sidebar); err != nil {
		return rawLayout{}, fmt.Errorf("decode sidebar layout: %w", err)
	}
	return rawLayout{Document: document, Customization: customization, Layout: layout, Sections: sections, Main: main, Sidebar: sidebar}, nil
}

func (layout rawLayout) marshal() (json.RawMessage, error) {
	main, err := json.Marshal(layout.Main)
	if err != nil {
		return nil, fmt.Errorf("encode main layout: %w", err)
	}
	sidebar, err := json.Marshal(layout.Sidebar)
	if err != nil {
		return nil, fmt.Errorf("encode sidebar layout: %w", err)
	}
	layout.Sections["main"] = main
	layout.Sections["sidebar"] = sidebar
	sections, err := json.Marshal(layout.Sections)
	if err != nil {
		return nil, fmt.Errorf("encode layout sections: %w", err)
	}
	layout.Layout["sections"] = sections
	layoutRaw, err := json.Marshal(layout.Layout)
	if err != nil {
		return nil, fmt.Errorf("encode layout: %w", err)
	}
	layout.Customization["layout"] = layoutRaw
	customization, err := json.Marshal(layout.Customization)
	if err != nil {
		return nil, fmt.Errorf("encode customization: %w", err)
	}
	layout.Document.Document["customization"] = customization
	return layout.Document.marshal()
}

func applyStructureCommands(raw json.RawMessage, commands []structureCommand) (json.RawMessage, error) {
	layout, err := decodeRawLayout(raw)
	if err != nil {
		return nil, err
	}
	for _, command := range commands {
		switch command.Op {
		case "createSection":
			err = layout.createSection(command)
		case "deleteSection":
			err = layout.deleteSection(command.Key)
		case "moveSection":
			err = layout.moveSection(command)
		case "reorderColumn":
			err = layout.reorderColumn(command.Column, command.Keys)
		default:
			err = validationIssueAt("commands", fmt.Sprintf("unknown structure command %q", command.Op))
		}
		if err != nil {
			return nil, err
		}
	}
	return layout.marshal()
}

func (layout *rawLayout) createSection(command structureCommand) error {
	if _, exists := layout.Document.Content[command.Key]; exists {
		return validationIssueAt("commands.key", fmt.Sprintf("section key %q already exists", command.Key))
	}
	if len(layout.Document.Content) >= 24 {
		return validationIssueAt("content", "a resume cannot contain more than 24 sections")
	}
	if !validSectionType(command.SectionType) {
		return validationIssueAt("commands.sectionType", fmt.Sprintf("unknown sectionType %q", command.SectionType))
	}
	column, err := layout.column(command.Column)
	if err != nil {
		return err
	}
	if !command.HasIndex || command.IndexOutside || command.Index < 0 || command.Index > len(*column) {
		return validationIssueAt("commands.index", "createSection index is outside the target column")
	}
	section := map[string]any{"sectionType": command.SectionType, "entries": []any{}}
	if command.DisplayName.Present {
		if command.DisplayName.Value == nil {
			return validationIssueAt("commands.displayName", "createSection displayName must be a string")
		}
		section["displayName"] = *command.DisplayName.Value
	}
	if command.IconKey.Present {
		if command.IconKey.Value == nil {
			return validationIssueAt("commands.iconKey", "createSection iconKey must be a string")
		}
		section["iconKey"] = *command.IconKey.Value
	}
	sectionRaw, marshalErr := json.Marshal(section)
	if marshalErr != nil {
		return fmt.Errorf("encode created section: %w", marshalErr)
	}
	layout.Document.Content[command.Key] = sectionRaw
	*column = insertString(*column, command.Index, command.Key)
	return nil
}

func (layout *rawLayout) deleteSection(key string) error {
	if _, exists := layout.Document.Content[key]; !exists {
		return validationIssueAt("commands.key", fmt.Sprintf("section key %q does not exist", key))
	}
	var found bool
	layout.Main, found = removeString(layout.Main, key)
	var sidebarFound bool
	layout.Sidebar, sidebarFound = removeString(layout.Sidebar, key)
	if found == sidebarFound {
		return validationIssueAt("customization.layout.sections", fmt.Sprintf("section key %q is not placed exactly once", key))
	}
	delete(layout.Document.Content, key)
	return nil
}

func (layout *rawLayout) moveSection(command structureCommand) error {
	if _, exists := layout.Document.Content[command.Key]; !exists {
		return validationIssueAt("commands.key", fmt.Sprintf("section key %q does not exist", command.Key))
	}
	var mainFound, sidebarFound bool
	layout.Main, mainFound = removeString(layout.Main, command.Key)
	layout.Sidebar, sidebarFound = removeString(layout.Sidebar, command.Key)
	if mainFound == sidebarFound {
		return validationIssueAt("customization.layout.sections", fmt.Sprintf("section key %q is not placed exactly once", command.Key))
	}
	column, err := layout.column(command.Column)
	if err != nil {
		return err
	}
	if !command.HasIndex || command.IndexOutside || command.Index < 0 || command.Index > len(*column) {
		return validationIssueAt("commands.index", "moveSection index is outside the target column after removal")
	}
	*column = insertString(*column, command.Index, command.Key)
	return nil
}

func (layout *rawLayout) reorderColumn(columnName string, keys []string) error {
	column, err := layout.column(columnName)
	if err != nil {
		return err
	}
	if len(*column) != len(keys) {
		return validationIssueAt("commands.keys", "reorderColumn keys must be a permutation of the current column")
	}
	want := make(map[string]struct{}, len(*column))
	for _, key := range *column {
		want[key] = struct{}{}
	}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, duplicate := seen[key]; duplicate {
			return validationIssueAt("commands.keys", "reorderColumn keys must not contain duplicates")
		}
		seen[key] = struct{}{}
		if _, exists := want[key]; !exists {
			return validationIssueAt("commands.keys", "reorderColumn keys must be a permutation of the current column")
		}
	}
	*column = append([]string(nil), keys...)
	return nil
}

func (layout *rawLayout) column(name string) (*[]string, error) {
	switch name {
	case "main":
		return &layout.Main, nil
	case "sidebar":
		return &layout.Sidebar, nil
	default:
		return nil, validationIssueAt("commands.column", fmt.Sprintf("unknown layout column %q", name))
	}
}

func validSectionType(value string) bool {
	switch value {
	case "profile", "work", "education", "skill", "language", "certificate", "project", "custom":
		return true
	default:
		return false
	}
}

func insertString(values []string, index int, value string) []string {
	values = append(values, "")
	copy(values[index+1:], values[index:])
	values[index] = value
	return values
}

func removeString(values []string, target string) ([]string, bool) {
	for index, value := range values {
		if value == target {
			return append(values[:index], values[index+1:]...), true
		}
	}
	return values, false
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
