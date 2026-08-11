package resume

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// BackfillResult is BackfillOne's outcome. It is meaningful only when the
// accompanying error is nil.
type BackfillResult int

const (
	// BackfillUnknown is the zero value and accompanies every non-nil error.
	// It occupies 0 deliberately: this is a system job that rewrites personal
	// data, so a caller that ignores the error and branches on the result
	// must not read "applied" and mark a failed row done, never to retry it.
	BackfillUnknown BackfillResult = iota

	// BackfillApplied means the row was rewritten at the current version.
	BackfillApplied

	// BackfillSkippedCurrent means the row was already at the current
	// version, so nothing was written at all.
	BackfillSkippedCurrent

	// BackfillLostRace means the observation the CAS was built from went
	// stale between the read and the write, so zero rows were updated and
	// nothing was written. It is RETRYABLE and never terminal: the row may
	// still be behind. A title-only write bumps revision without touching
	// schema_version, so this outcome does not imply the row became current
	// (docs/plans/phase-2a/task-08-doc-shape-migration.md, B6).
	BackfillLostRace
)

func (r BackfillResult) String() string {
	switch r {
	case BackfillUnknown:
		return "BackfillUnknown"
	case BackfillApplied:
		return "BackfillApplied"
	case BackfillSkippedCurrent:
		return "BackfillSkippedCurrent"
	case BackfillLostRace:
		return "BackfillLostRace"
	default:
		return fmt.Sprintf("BackfillResult(%d)", int(r))
	}
}

// BackfillOne migrates one stored row to the current document version: read,
// project, decode, validate, then a CAS keyed on the observed schema_version
// AND revision. It is a system job, not user-scoped, and it leaves revision
// and updated_at alone (D12) so an editor holding a pre-backfill revision is
// not forced into a conflict by a migration it cannot see.
//
// It returns ErrNotFound for an unknown id, and an error -- with no write --
// when the stored document cannot be projected, decoded, or validated.
//
// The rationale for every part of that contract, including why a lost race is
// a retry signal, lives in
// docs/plans/phase-2a/task-08-doc-shape-migration.md.
func (s *Store) BackfillOne(ctx context.Context, id uuid.UUID) (BackfillResult, error) {
	return s.backfillOne(ctx, id, nil)
}

// backfillOne is BackfillOne's core with a test-only pause seam between the
// read and the CAS (export_test.go). There is deliberately no transaction
// around the two statements: the CAS predicate IS the atomicity, so holding a
// transaction open across the projection would only make a system job block
// user writes.
func (s *Store) backfillOne(ctx context.Context, id uuid.UUID, pause func()) (BackfillResult, error) {
	row, err := s.q.GetResumeByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackfillUnknown, ErrNotFound
		}
		return BackfillUnknown, fmt.Errorf("resume: backfill: read row: %w", err)
	}

	// The target is the version the parts are actually projected TO, exactly
	// as projectRow decodes at that version rather than at the package
	// constant. In production the two are the same value by construction.
	target := s.proj.CurrentVersion()
	if row.SchemaVersion == target {
		return BackfillSkippedCurrent, nil
	}

	pd, content, customization, err := s.proj.Project(row.PersonalDetails, row.Content, row.Customization, row.SchemaVersion)
	if err != nil {
		return BackfillUnknown, fmt.Errorf("resume: backfill: project document: %w", err)
	}
	doc, err := DecodeParts(pd, content, customization, target)
	if err != nil {
		return BackfillUnknown, fmt.Errorf("resume: backfill: decode projected document: %w", err)
	}

	// encodeParts drops schemaVersion from all three jsonb parts (D4), so
	// this field decides only which schema ValidateForStore checks against:
	// the compiled-in current one, as on every other write path (D19).
	doc.SchemaVersion = int64(docmigrate.CurrentVersion)
	if err = ValidateForStore(doc); err != nil {
		return BackfillUnknown, fmt.Errorf("resume: backfill: %w", err)
	}

	nextPD, nextContent, nextCustomization, err := encodeParts(doc)
	if err != nil {
		return BackfillUnknown, fmt.Errorf("resume: backfill: encode document: %w", err)
	}

	if pause != nil {
		pause()
	}

	// Both legs of the predicate come from the observation above: a
	// concurrent document write moves schema_version or the parts, and a
	// title-only write moves revision alone. Either one must lose this CAS.
	affected, err := s.q.BackfillResumeDocumentCAS(ctx, store.BackfillResumeDocumentCASParams{
		PersonalDetails:   nextPD,
		Content:           nextContent,
		Customization:     nextCustomization,
		ToSchemaVersion:   target,
		ID:                id,
		FromSchemaVersion: row.SchemaVersion,
		Revision:          row.Revision,
	})
	if err != nil {
		return BackfillUnknown, fmt.Errorf("resume: backfill: %w", err)
	}
	if affected == 0 {
		return BackfillLostRace, nil
	}
	return BackfillApplied, nil
}
