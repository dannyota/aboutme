package project

import (
	"bytes"
	"encoding/json"
	"fmt"

	"danny.vn/aboutme/apps/server/internal/api"
)

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
