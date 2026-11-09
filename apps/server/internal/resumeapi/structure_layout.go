package resumeapi

// Resume structure layout: the raw document's layout columns, the structure
// commands applied to them in order, and the string slice helpers they use.

import (
	"encoding/json"
	"fmt"
)

// rawLayout is a resume document decoded down to its layout columns, with
// every other value kept as raw JSON.
type rawLayout struct {
	// Document is the decoded document and its content sections.
	Document rawDocumentParts
	// Customization is the document's customization object.
	Customization map[string]json.RawMessage
	// Layout is the customization layout object.
	Layout map[string]json.RawMessage
	// Sections is the layout's sections object that holds both columns.
	Sections map[string]json.RawMessage
	// Main is the main column's section keys in display order.
	Main []string
	// Sidebar is the sidebar column's section keys in display order.
	Sidebar []string
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
