package project

import (
	"bytes"
	"encoding/json"
	"fmt"

	"danny.vn/aboutme/apps/server/internal/api"
)

// Decode parses a closed owner request: a project document plus an optional
// slug. encoding/json matches field names case-insensitively, so the body is
// first checked against the exact request shape: no duplicate or undeclared
// property, no trailing JSON, scalars of the declared type, and null only for
// optional date ranges and date-range ends. Every failure wraps ErrInvalid;
// one that a single value causes also wraps the *api.JSONPathError naming it.
func Decode(raw []byte, target any) error {
	if err := api.CheckJSONShape(raw, requestShape()); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return ErrInvalid
	}
	return nil
}

func requestShape() *api.JSONShape {
	text := &api.JSONShape{Kind: api.JSONString}
	integer := &api.JSONShape{Kind: api.JSONInteger}
	yearMonth := func(nullable bool) *api.JSONShape {
		return &api.JSONShape{Fields: map[string]*api.JSONShape{"y": integer, "m": integer}, Nullable: nullable}
	}
	translation := &api.JSONShape{Fields: map[string]*api.JSONShape{
		"name": text, "role": text, "summary": text, "outcome": text, "description": text,
	}}
	return &api.JSONShape{Fields: map[string]*api.JSONShape{
		"slug": text, "repositoryUrl": text, "demoUrl": text, "defaultLanguage": text,
		"dates": {
			Fields: map[string]*api.JSONShape{
				"start": yearMonth(false), "end": yearMonth(true), "present": {Kind: api.JSONBoolean},
			},
			Required: []string{"start", "end", "present"}, Nullable: true,
		},
		"technologies": {Items: text},
		"text":         {Values: translation},
	}}
}
