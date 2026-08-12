package resume

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// MaxTitleCharacters is the budgets.md resume-title limit. It is measured in
// Unicode code points, matching PostgreSQL char_length(text), not UTF-8 bytes.
const MaxTitleCharacters = 160

// Resume is the store's domain shape for one resumes row: the row's own
// scalar columns, plus Doc -- the three jsonb parts assembled and
// projected to docmigrate.CurrentVersion -- rather than the four
// separate columns a caller would otherwise have to reassemble by hand.
type Resume struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	Title           string
	Slug            *string
	Live            bool
	DownloadEnabled bool
	SEOGeoEnabled   bool

	// StoredSchemaVersion is the row's own schema_version column, BEFORE
	// projection: observable so a caller can tell backfill progress
	// apart from Doc.SchemaVersion, which is always CurrentVersion.
	StoredSchemaVersion int32

	// Revision is the row's optimistic-concurrency counter. The API
	// serializes it as a string to preserve JavaScript and Dart precision;
	// this package always deals in the native int64.
	Revision int64

	Lng *string

	// Doc is always projected to docmigrate.CurrentVersion, never
	// the row's own possibly-stale StoredSchemaVersion.
	Doc schema.Resume

	CreatedAt, UpdatedAt time.Time
}

var (
	// ErrNotFound is returned by Get, Delete, SaveDocument, and SaveTitle
	// when no row matches both id AND the caller's own userID: a
	// resume that exists but belongs to a different user is reported
	// identically to one that does not exist at all -- there is no
	// existence oracle at this layer.
	ErrNotFound = errors.New("resume: not found")

	// ErrCapExceeded is returned by Create when userID already owns 3
	// resumes -- the store's own LockUserForResumeWrite + CountResumesForUser
	// check, backstopped by the database trigger (mapped from the exact
	// SQLSTATE 23514 / "resumes_user_cap_exceeded" pair a cap violation
	// raises).
	ErrCapExceeded = errors.New("resume: user resume cap exceeded")

	// ErrTitleTooLong is returned before a transaction or write when a title
	// exceeds MaxTitleCharacters. Empty titles are valid for draft editing.
	ErrTitleTooLong = errors.New("resume: title exceeds 160 characters")
)

// RevisionMismatchError is SaveDocument's and SaveTitle's failure mode
// when expectedRevision no longer matches the row's actual revision: a
// concurrent write already moved it. Current carries the row exactly as
// it stands after that concurrent write -- the WINNING content and
// revision, read inside the same transaction as the failed CAS attempt --
// so an API caller can return the current state for rebasing.
type RevisionMismatchError struct {
	CurrentRevision int64
	Current         Resume
}

func (e *RevisionMismatchError) Error() string {
	return fmt.Sprintf("resume: revision mismatch: current revision is %d", e.CurrentRevision)
}
