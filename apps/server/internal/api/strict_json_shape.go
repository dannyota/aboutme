package api

// Deriving a JSONShape from the Go type encoding/json decodes into.

import (
	"encoding"
	"encoding/json"
	"reflect"
	"strings"
	"unicode"
)

// JSONShapeOf derives the shape encoding/json binds into t: exact JSON names
// for every struct level, free keys for maps, elements for slices and arrays,
// and scalar kinds. Null is admitted everywhere, as encoding/json admits it.
// A type with its own UnmarshalJSON accepts any value; a text unmarshaler
// accepts a string.
func JSONShapeOf(t reflect.Type) *JSONShape {
	return shapeOf(t, make(map[reflect.Type]*JSONShape))
}

func shapeOf(t reflect.Type, built map[reflect.Type]*JSONShape) *JSONShape {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if shape, ok := built[t]; ok {
		return shape
	}
	shape := &JSONShape{Nullable: true}
	built[t] = shape
	pointer := reflect.PointerTo(t)
	jsonUnmarshaler := reflect.TypeFor[json.Unmarshaler]()
	textUnmarshaler := reflect.TypeFor[encoding.TextUnmarshaler]()
	switch {
	case t.Implements(jsonUnmarshaler) || pointer.Implements(jsonUnmarshaler):
	case t.Implements(textUnmarshaler) || pointer.Implements(textUnmarshaler):
		shape.Kind = JSONString
	case t.Kind() == reflect.Struct:
		shape.Fields = structShapes(t, built)
	case t.Kind() == reflect.Map:
		shape.Values = shapeOf(t.Elem(), built)
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8:
		shape.Kind = JSONString
	case t.Kind() == reflect.Slice || t.Kind() == reflect.Array:
		shape.Items = shapeOf(t.Elem(), built)
	default:
		shape.Kind = scalarKind(t.Kind())
	}
	return shape
}

func scalarKind(kind reflect.Kind) JSONKind {
	switch kind {
	case reflect.String:
		return JSONString
	case reflect.Bool:
		return JSONBoolean
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return JSONInteger
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return JSONNumber
	default:
		return JSONAny
	}
}

// jsonField is one struct field encoding/json could bind a property to.
type jsonField struct {
	// name is the property name the field binds.
	name string
	// tagged reports that the name came from a json tag.
	tagged bool
	// depth is the embedding depth, 0 for the struct's own fields.
	depth int
	// typ is the field's Go type.
	typ reflect.Type
	// quoted reports the ",string" option, which wraps the value in a string.
	quoted bool
}

// structShapes applies encoding/json's field rules: exported fields named by
// their tag or Go name, "-" skipped, untagged embedded structs promoted, and
// the shallowest (then the only tagged) field winning a name.
func structShapes(t reflect.Type, built map[reflect.Type]*JSONShape) map[string]*JSONShape {
	var found []jsonField
	collectFields(t, 0, map[reflect.Type]bool{}, &found)
	byName := make(map[string][]jsonField)
	for _, field := range found {
		byName[field.name] = append(byName[field.name], field)
	}
	fields := make(map[string]*JSONShape, len(byName))
	for name, candidates := range byName {
		field, ok := dominantField(candidates)
		switch {
		case !ok:
		case field.quoted:
			fields[name] = &JSONShape{Nullable: true}
		default:
			fields[name] = shapeOf(field.typ, built)
		}
	}
	return fields
}

func collectFields(t reflect.Type, depth int, visiting map[reflect.Type]bool, out *[]jsonField) {
	if visiting[t] {
		return
	}
	visiting[t] = true
	defer delete(visiting, t)
	for index := range t.NumField() {
		field := t.Field(index)
		fieldType := field.Type
		if field.Anonymous {
			if fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			if !field.IsExported() && fieldType.Kind() != reflect.Struct {
				continue
			}
		} else if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if !validTagName(name) {
			name = ""
		}
		if field.Anonymous && name == "" && fieldType.Kind() == reflect.Struct {
			collectFields(fieldType, depth+1, visiting, out)
			continue
		}
		tagged := name != ""
		if !tagged {
			name = field.Name
		}
		quoted := false
		for option := range strings.SplitSeq(options, ",") {
			quoted = quoted || option == "string"
		}
		*out = append(*out, jsonField{name: name, tagged: tagged, depth: depth, typ: field.Type, quoted: quoted})
	}
}

func dominantField(candidates []jsonField) (jsonField, bool) {
	shallowest := candidates[0].depth
	for _, field := range candidates {
		shallowest = min(shallowest, field.depth)
	}
	var top, tagged []jsonField
	for _, field := range candidates {
		if field.depth == shallowest {
			top = append(top, field)
			if field.tagged {
				tagged = append(tagged, field)
			}
		}
	}
	switch {
	case len(tagged) == 1:
		return tagged[0], true
	case len(tagged) == 0 && len(top) == 1:
		return top[0], true
	default:
		return jsonField{}, false
	}
}

func validTagName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", r) && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
