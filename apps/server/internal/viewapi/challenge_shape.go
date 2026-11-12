package viewapi

import (
	"reflect"

	"danny.vn/aboutme/apps/server/internal/api"
)

// collectShape is the closed collect body: the derived shape of
// collectRequest with the challenge replaced by its explicit wire shape.
func collectShape() *api.JSONShape {
	shape := api.JSONShapeOf(reflect.TypeFor[collectRequest]())
	shape.Fields["challenge"] = challengeShape()
	return shape
}

// challengeShape is the complete ALTCHA v2 challenge wire shape of the pinned
// altcha-lib-go v2.4.1: Challenge (altcha.go:38-41) and ChallengeParameters
// (altcha.go:44-55). ChallengeParameters.UnmarshalJSON (canonical_json.go:
// 268-294) decodes through encoding/json, which folds name case, keeps the
// last of repeated keys, and ignores unknown keys, and the signature covers
// the values it decoded. A shape derived from the Go type would treat that
// decoder as opaque, so the names, uniqueness, and types are stated here and
// checked before the library sees the body. The library keeps a null
// memoryCost, parallelism, or expiresAt (altcha.go:62-68,
// canonical_json.go:287-293) and a null data object or data value
// (canonical_json.go:281-286); every other member is non-null. Update this
// shape with the library version.
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
