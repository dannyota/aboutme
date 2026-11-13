package resume

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

type BackfillResult int

const (
	BackfillUnknown BackfillResult = iota

	BackfillApplied

	BackfillSkippedCurrent

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

func (s *Store) BackfillOne(ctx context.Context, id uuid.UUID) (BackfillResult, error) {
	return s.backfillOne(ctx, id, nil)
}

func (s *Store) backfillOne(ctx context.Context, id uuid.UUID, pause func()) (BackfillResult, error) {
	row, err := s.q.GetResumeByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BackfillUnknown, ErrNotFound
		}
		return BackfillUnknown, fmt.Errorf("resume: backfill: read row: %w", err)
	}

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
