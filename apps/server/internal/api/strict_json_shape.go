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
// accepts a string. []byte and json.Number keep every form encoding/json
// accepts for them.
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
	switch {
	case decodesItself(t, reflect.TypeFor[json.Unmarshaler]()):
	case decodesItself(t, reflect.TypeFor[encoding.TextUnmarshaler]()):
		shape.Kind = JSONString
	case t == reflect.TypeFor[json.Number]():
		shape.Kind = JSONNumeral
	case t.Kind() == reflect.Struct:
		shape.Fields = structShapes(t, built)
	case t.Kind() == reflect.Map:
		shape.Values = shapeOf(t.Elem(), built)
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8:
		// encoding/json decodes a string as base64 and an array element by
		// element, through the element's own decoder when it has one.
		if !decodesItself(t.Elem(), reflect.TypeFor[json.Unmarshaler]()) &&
			!decodesItself(t.Elem(), reflect.TypeFor[encoding.TextUnmarshaler]()) {
			shape.Kind = JSONBytes
		}
	case t.Kind() == reflect.Slice || t.Kind() == reflect.Array:
		shape.Items = shapeOf(t.Elem(), built)
	default:
		shape.Kind = scalarKind(t.Kind())
	}
	return shape
}

// decodesItself reports whether t or *t implements the decoder interface.
func decodesItself(t, face reflect.Type) bool {
	return t.Implements(face) || reflect.PointerTo(t).Implements(face)
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
	// quoted reports a ",string" option encoding/json honors, which wraps a
	// string, number, or boolean value in a JSON string.
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
			fields[name] = &JSONShape{Kind: JSONString, Nullable: true}
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
			quoted = quoted || (option == "string" && quotable(field.Type))
		}
		*out = append(*out, jsonField{name: name, tagged: tagged, depth: depth, typ: field.Type, quoted: quoted})
	}
}

// quotable reports whether encoding/json honors ",string" on a field of type
// t: a string, number, or boolean, through one unnamed pointer.
func quotable(t reflect.Type) bool {
	if t.Name() == "" && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
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
