package resume_test

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Compile-time assertions that the generated resume and idempotency-record
// shapes match what Tasks 6-8 build on. A failure here means schema.sql,
// queries.sql, or the sqlc.yaml overrides drifted from what later tasks
// expect. The field-level lines pin the nullable-column contract (native
// pointers, per the P1 Task 1 pattern in internal/user/user_test.go) and the
// sqlc.yaml `rename` entry that keeps seo_geo_enabled from surfacing as
// SeoGeoEnabled -- which breaks the Go initialism rule and would disagree,
// by casing alone, with Task 6's domain field SEOGeoEnabled.
var (
	_ uuid.UUID = store.Resume{}.ID
	_ uuid.UUID = store.Resume{}.UserID
	_ string    = store.Resume{}.Title
	_ *string   = store.Resume{}.Slug
	_ bool      = store.Resume{}.Live
	_ bool      = store.Resume{}.DownloadEnabled
	_ bool      = store.Resume{}.SEOGeoEnabled
	_ int32     = store.Resume{}.SchemaVersion
	_ int64     = store.Resume{}.Revision
	_ *string   = store.Resume{}.Lng
	_ time.Time = store.Resume{}.CreatedAt
	_ time.Time = store.Resume{}.UpdatedAt

	_ uuid.UUID = store.IdempotencyRecord{}.ID
	_ uuid.UUID = store.IdempotencyRecord{}.UserID
	_ string    = store.IdempotencyRecord{}.Route
	_ uuid.UUID = store.IdempotencyRecord{}.IdempotencyKey
	_ int32     = store.IdempotencyRecord{}.ResponseStatus
	_ time.Time = store.IdempotencyRecord{}.CreatedAt
	_ time.Time = store.IdempotencyRecord{}.ExpiresAt
)

// resumeShape and idempShape back the jsonb/bytea pins below: taking their
// field addresses requires addressable values, which a bare
// store.Resume{}.Field composite-literal selector is not.
var (
	resumeShape store.Resume
	idempShape  store.IdempotencyRecord
)

// The jsonb and bytea columns are pinned through pointer identity rather than
// `var _ T = value` assignability, because json.RawMessage is a named type
// whose underlying type is []byte: Go's assignability rule admits a plain
// []byte into a json.RawMessage-typed variable (and vice versa) even though
// the two are different types. That means `var _ json.RawMessage = x` still
// compiles when x is []byte, and `var _ []byte = y` still compiles when y is
// json.RawMessage -- silently passing through exactly the regression this
// test exists to catch: a dropped or altered jsonb/bytea override in
// sqlc.yaml that changes the generated field's type. A pointer closes the
// hole: *[]byte is not assignable to *json.RawMessage or vice versa, so only
// the exact declared type compiles.
var (
	_ *json.RawMessage = &resumeShape.PersonalDetails
	_ *json.RawMessage = &resumeShape.Content
	_ *json.RawMessage = &resumeShape.Customization
	_ *json.RawMessage = &idempShape.ResponseBody

	_ *[]byte = &idempShape.RequestHash
)

// BackfillResumeDocumentCASParams pins the one query in this file where a
// caller could swap two same-typed, same-shaped params and silently corrupt
// data: FromSchemaVersion (the WHERE guard -- migrating FROM this version)
// and ToSchemaVersion (the SET value -- migrating TO this version). Both are
// int32; naming them by direction at the sqlc.arg level (queries.sql) is
// what makes a swap a compile-time argument-name mismatch instead of a
// silent positional swap. Pinning the full composite literal here, not just
// each field's type, means a future regeneration that renames these back to
// positional SchemaVersion/SchemaVersion_2 -- or drops either -- breaks here
// instead of inside Task 8.
var _ = store.BackfillResumeDocumentCASParams{
	ID:                uuid.UUID{},
	FromSchemaVersion: int32(0),
	Revision:          int64(0),
	PersonalDetails:   json.RawMessage(nil),
	Content:           json.RawMessage(nil),
	Customization:     json.RawMessage(nil),
	ToSchemaVersion:   int32(0),
}
