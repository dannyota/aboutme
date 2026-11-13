package resume_test

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/store"
)

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

var (
	resumeShape store.Resume
	idempShape  store.IdempotencyRecord
)

// Go assignability admits []byte for json.RawMessage and back, so these columns are pinned through pointer types.
var (
	_ *json.RawMessage = &resumeShape.PersonalDetails
	_ *json.RawMessage = &resumeShape.Content
	_ *json.RawMessage = &resumeShape.Customization
	_ *json.RawMessage = &idempShape.ResponseBody

	_ *[]byte = &idempShape.RequestHash
)

var _ = store.BackfillResumeDocumentCASParams{
	ID:                uuid.UUID{},
	FromSchemaVersion: int32(0),
	Revision:          int64(0),
	PersonalDetails:   json.RawMessage(nil),
	Content:           json.RawMessage(nil),
	Customization:     json.RawMessage(nil),
	ToSchemaVersion:   int32(0),
}
