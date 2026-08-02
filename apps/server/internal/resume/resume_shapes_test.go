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
	_ store.Resume            = store.Resume{}
	_ store.IdempotencyRecord = store.IdempotencyRecord{}

	_ uuid.UUID       = store.Resume{}.ID
	_ uuid.UUID       = store.Resume{}.UserID
	_ string          = store.Resume{}.Title
	_ *string         = store.Resume{}.Slug
	_ bool            = store.Resume{}.Live
	_ bool            = store.Resume{}.DownloadEnabled
	_ bool            = store.Resume{}.SEOGeoEnabled
	_ int32           = store.Resume{}.SchemaVersion
	_ int64           = store.Resume{}.Revision
	_ *string         = store.Resume{}.Lng
	_ json.RawMessage = store.Resume{}.PersonalDetails
	_ json.RawMessage = store.Resume{}.Content
	_ json.RawMessage = store.Resume{}.Customization
	_ time.Time       = store.Resume{}.CreatedAt
	_ time.Time       = store.Resume{}.UpdatedAt

	_ uuid.UUID       = store.IdempotencyRecord{}.ID
	_ uuid.UUID       = store.IdempotencyRecord{}.UserID
	_ string          = store.IdempotencyRecord{}.Route
	_ uuid.UUID       = store.IdempotencyRecord{}.IdempotencyKey
	_ []byte          = store.IdempotencyRecord{}.RequestHash
	_ int32           = store.IdempotencyRecord{}.ResponseStatus
	_ json.RawMessage = store.IdempotencyRecord{}.ResponseBody
	_ time.Time       = store.IdempotencyRecord{}.CreatedAt
	_ time.Time       = store.IdempotencyRecord{}.ExpiresAt
)
