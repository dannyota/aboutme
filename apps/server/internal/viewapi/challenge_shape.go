package viewapi

import (
	"reflect"

	"danny.vn/aboutme/apps/server/internal/api"
)

func collectShape() *api.JSONShape {
	shape := api.JSONShapeOf(reflect.TypeFor[collectRequest]())
	shape.Fields["challenge"] = challengeShape()
	return shape
}

// altcha-lib-go v2.4.1 decodes the parameters with encoding/json, which folds name case, keeps the last repeated key, and ignores unknown keys.
func challengeShape() *api.JSONShape {
	text := &api.JSONShape{Kind: api.JSONString}
	integer := &api.JSONShape{Kind: api.JSONInteger}
	optionalInteger := &api.JSONShape{Kind: api.JSONInteger, Nullable: true}
	return &api.JSONShape{Nullable: true, Fields: map[string]*api.JSONShape{
		"parameters": {Fields: map[string]*api.JSONShape{
			"algorithm": text, "nonce": text, "salt": text, "cost": integer,
			"keyLength": integer, "keyPrefix": text, "keySignature": text,
			"memoryCost": optionalInteger, "parallelism": optionalInteger, "expiresAt": optionalInteger,
			"data": {Values: &api.JSONShape{Nullable: true}, Nullable: true},
		}},
		"signature": text,
	}}
}
