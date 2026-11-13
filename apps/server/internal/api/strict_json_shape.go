package api

import (
	"encoding"
	"encoding/json"
	"reflect"
	"strings"
	"unicode"
)

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

type jsonField struct {
	name   string
	tagged bool
	depth  int
	typ    reflect.Type
	quoted bool
}

// Field selection follows encoding/json: tag or Go name, "-" skipped, untagged embedded structs promoted, shallowest then tagged field wins.
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

// encoding/json honors ",string" only on a string, number, or boolean field, through one unnamed pointer.
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
