package resumeapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

const maxCustomizationPathBytes = 256

type customizationPathSet map[string]struct{}

type customizationAllowlist struct {
	Set   customizationPathSet
	Unset customizationPathSet
}

type customizationValueKind uint8

const (
	customizationString customizationValueKind = iota + 1
	customizationInteger
	customizationNumber
	customizationBoolean
)

var fixedCustomizationAllowlist = customizationAllowlist{
	Set: customizationPathSet{
		"font.family": {}, "font.baseSizePx": {},
		"colors.primary": {}, "colors.text": {}, "colors.background": {},
		"colors.accent": {}, "colors.surface": {},
		"spacing.sectionGap": {}, "spacing.entryGap": {}, "spacing.lineHeight": {},
		"spacing.pageMargin.x": {}, "spacing.pageMargin.y": {},
		"heading.style": {}, "heading.showRule": {},
		"header.align": {}, "header.detailsLayout": {}, "header.iconStyle": {},
		"layout.columns": {}, "layout.surfaceTarget": {},
		"sectionDisplay.skill.style": {}, "sectionDisplay.language.style": {},
		"pageFormat": {}, "dateFormat": {},
	},
	Unset: customizationPathSet{
		"colors.accent": {}, "colors.surface": {},
		"spacing.pageMargin": {}, "header": {}, "layout.surfaceTarget": {},
	},
}

var customizationSetValueKinds = map[string]customizationValueKind{
	"font.family": customizationString, "font.baseSizePx": customizationInteger,
	"colors.primary": customizationString, "colors.text": customizationString,
	"colors.background": customizationString, "colors.accent": customizationString,
	"colors.surface":     customizationString,
	"spacing.sectionGap": customizationNumber, "spacing.entryGap": customizationNumber,
	"spacing.lineHeight": customizationNumber, "spacing.pageMargin.x": customizationNumber,
	"spacing.pageMargin.y": customizationNumber,
	"heading.style":        customizationString, "heading.showRule": customizationBoolean,
	"header.align": customizationString, "header.detailsLayout": customizationString,
	"header.iconStyle": customizationString,
	"layout.columns":   customizationInteger, "layout.surfaceTarget": customizationString,
	"sectionDisplay.skill.style":    customizationString,
	"sectionDisplay.language.style": customizationString,
	"pageFormat":                    customizationString, "dateFormat": customizationString,
}

func init() {
	if err := validateCustomizationAllowlist(schema.RawSchema, fixedCustomizationAllowlist); err != nil {
		panic("resumeapi: customization allowlist does not match embedded schema: " + err.Error())
	}
	if err := validateCustomizationValueKinds(schema.RawSchema, customizationSetValueKinds); err != nil {
		panic("resumeapi: customization value-kind table does not match embedded schema: " + err.Error())
	}
}

func validateCustomizationValueKinds(rawSchema []byte, actual map[string]customizationValueKind) error {
	expected, err := deriveCustomizationValueKinds(rawSchema)
	if err != nil {
		return err
	}
	var differences []string
	for path, want := range expected {
		if got := actual[path]; got != want {
			differences = append(differences, fmt.Sprintf("%s kind=%d, want %d", path, got, want))
		}
	}
	for path := range actual {
		if _, ok := expected[path]; !ok {
			differences = append(differences, "undeclared kind for "+path)
		}
	}
	if len(differences) > 0 {
		sort.Strings(differences)
		return fmt.Errorf("%s", strings.Join(differences, "; "))
	}
	return nil
}

func deriveCustomizationValueKinds(rawSchema []byte) (map[string]customizationValueKind, error) {
	var root map[string]any
	if err := json.Unmarshal(rawSchema, &root); err != nil {
		return nil, fmt.Errorf("decode schema: %w", err)
	}
	definitions, ok := root["$defs"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema has no $defs object")
	}
	customization, ok := definitions["customization"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema has no customization definition")
	}
	paths, err := deriveCustomizationAllowlist(rawSchema)
	if err != nil {
		return nil, err
	}
	out := make(map[string]customizationValueKind, len(paths.Set))
	for path := range paths.Set {
		node := customization
		for _, segment := range strings.Split(path, ".") {
			resolved, resolveErr := resolveCustomizationSchemaNode(node, definitions)
			if resolveErr != nil {
				return nil, fmt.Errorf("resolve %s: %w", path, resolveErr)
			}
			properties, ok := resolved["properties"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s has no property %s", path, segment)
			}
			child, ok := properties[segment].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s property %s is not a schema", path, segment)
			}
			node = child
		}
		resolved, resolveErr := resolveCustomizationSchemaNode(node, definitions)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve %s leaf: %w", path, resolveErr)
		}
		typeName, ok := resolved["type"].(string)
		if !ok {
			return nil, fmt.Errorf("%s leaf has no scalar type", path)
		}
		switch typeName {
		case "string":
			out[path] = customizationString
		case "integer":
			out[path] = customizationInteger
		case "number":
			out[path] = customizationNumber
		case "boolean":
			out[path] = customizationBoolean
		default:
			return nil, fmt.Errorf("%s leaf has unsupported type %q", path, typeName)
		}
	}
	return out, nil
}

func resolveCustomizationSchemaNode(node, definitions map[string]any) (map[string]any, error) {
	ref, ok := node["$ref"].(string)
	if !ok {
		return node, nil
	}
	const prefix = "#/$defs/"
	if !strings.HasPrefix(ref, prefix) || strings.Contains(strings.TrimPrefix(ref, prefix), "/") {
		return nil, fmt.Errorf("unsupported reference %q", ref)
	}
	resolved, ok := definitions[strings.TrimPrefix(ref, prefix)].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unknown reference %q", ref)
	}
	return resolved, nil
}

func validateCustomizationAllowlist(rawSchema []byte, actual customizationAllowlist) error {
	expected, err := deriveCustomizationAllowlist(rawSchema)
	if err != nil {
		return err
	}
	var differences []string
	differences = append(differences, compareCustomizationPathSets("set", expected.Set, actual.Set)...)
	differences = append(differences, compareCustomizationPathSets("unset", expected.Unset, actual.Unset)...)
	if len(differences) > 0 {
		sort.Strings(differences)
		return fmt.Errorf("%s", strings.Join(differences, "; "))
	}
	return nil
}

func deriveCustomizationAllowlist(rawSchema []byte) (customizationAllowlist, error) {
	var root map[string]any
	if err := json.Unmarshal(rawSchema, &root); err != nil {
		return customizationAllowlist{}, fmt.Errorf("decode schema: %w", err)
	}
	definitions, ok := root["$defs"].(map[string]any)
	if !ok {
		return customizationAllowlist{}, fmt.Errorf("schema has no $defs object")
	}
	customization, ok := definitions["customization"].(map[string]any)
	if !ok {
		return customizationAllowlist{}, fmt.Errorf("schema has no customization definition")
	}
	derived := customizationAllowlist{Set: customizationPathSet{}, Unset: customizationPathSet{}}
	if err := walkCustomizationSchema(customization, "", true, &derived); err != nil {
		return customizationAllowlist{}, err
	}
	for path := range derived.Set {
		if deniedCustomizationPlacementPath(path) {
			delete(derived.Set, path)
		}
	}
	for path := range derived.Unset {
		if deniedCustomizationPlacementPath(path) {
			delete(derived.Unset, path)
		}
	}
	return derived, nil
}

func walkCustomizationSchema(node map[string]any, prefix string, requiredByParent bool, out *customizationAllowlist) error {
	properties, object := node["properties"].(map[string]any)
	if !object {
		if prefix == "" {
			return fmt.Errorf("customization definition is not an object")
		}
		out.Set[prefix] = struct{}{}
		if !requiredByParent {
			out.Unset[prefix] = struct{}{}
		}
		return nil
	}
	required := make(map[string]struct{})
	if values, ok := node["required"].([]any); ok {
		for _, value := range values {
			name, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s required member is not a string", prefix)
			}
			required[name] = struct{}{}
		}
	}
	if prefix != "" && !requiredByParent {
		out.Unset[prefix] = struct{}{}
	}
	for name, rawChild := range properties {
		child, ok := rawChild.(map[string]any)
		if !ok {
			return fmt.Errorf("schema node %s.%s is not an object", prefix, name)
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		_, childRequired := required[name]
		if err := walkCustomizationSchema(child, path, childRequired, out); err != nil {
			return err
		}
	}
	return nil
}

func compareCustomizationPathSets(operation string, expected, actual customizationPathSet) []string {
	var differences []string
	for path := range expected {
		if _, ok := actual[path]; !ok {
			differences = append(differences, fmt.Sprintf("missing (%s, %s)", operation, path))
		}
	}
	for path := range actual {
		if _, ok := expected[path]; !ok {
			differences = append(differences, fmt.Sprintf("undeclared (%s, %s)", operation, path))
		}
	}
	return differences
}

func deniedCustomizationPlacementPath(path string) bool {
	return path == "layout.sections" || strings.HasPrefix(path, "layout.sections.")
}

func customizationPathAllowed(operation, path string) bool {
	if len(path) > maxCustomizationPathBytes {
		return false
	}
	var allowed customizationPathSet
	switch operation {
	case customizationSet:
		allowed = fixedCustomizationAllowlist.Set
	case customizationUnset:
		allowed = fixedCustomizationAllowlist.Unset
	default:
		return false
	}
	_, ok := allowed[path]
	return ok
}
