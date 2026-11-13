package resume

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

const MaxTitleCharacters = 160

type Resume struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	Title           string
	Slug            *string
	Live            bool
	DownloadEnabled bool
	SEOGeoEnabled   bool

	PublicTitle  *string
	FaviconEmoji *string

	SignInToView  bool
	ViewPassEpoch int32

	StoredSchemaVersion int32

	Revision int64

	Lng *string

	Doc schema.Resume

	Showcase *Showcase

	CreatedAt, UpdatedAt time.Time
}

var (
	ErrNotFound = errors.New("resume: not found")

	ErrCapExceeded = errors.New("resume: user resume cap exceeded")

	ErrTitleTooLong = errors.New("resume: title exceeds 160 characters")
)

type RevisionMismatchError struct {
	CurrentRevision int64
	Current         Resume
}

func (e *RevisionMismatchError) Error() string {
	return fmt.Sprintf("resume: revision mismatch: current revision is %d", e.CurrentRevision)
}
