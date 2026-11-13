package previewcard

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/store"
)

var (
	ErrNotLive = errors.New("previewcard: resume is not live")
	ErrStale   = errors.New("previewcard: card version is not current")
)

type Database interface {
	store.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type StoredCard struct {
	Version string
	PNG     []byte
}

type LiveCard struct {
	ResumeID      uuid.UUID
	StoredVersion string
}

type Store struct {
	db     Database
	reader *publicresume.Reader
	now    func() time.Time
}

func NewStore(db Database, reader *publicresume.Reader, now func() time.Time) (*Store, error) {
	if db == nil || reader == nil {
		return nil, errors.New("previewcard: invalid store dependencies")
	}
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, reader: reader, now: now}, nil
}

func (s *Store) Stored(ctx context.Context, resumeID uuid.UUID) (StoredCard, bool, error) {
	row, err := store.New(s.db).GetResumePreviewCard(ctx, resumeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredCard{}, false, nil
	}
	if err != nil {
		return StoredCard{}, false, err
	}
	return StoredCard{Version: row.Version, PNG: row.PNG}, true, nil
}

func (s *Store) Live(ctx context.Context, resumeID uuid.UUID) (publicresume.Snapshot, bool, error) {
	row, err := store.New(s.db).GetLiveResumeByID(ctx, resumeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return publicresume.Snapshot{}, false, nil
	}
	if err != nil {
		return publicresume.Snapshot{}, false, err
	}
	snapshot, err := s.reader.ProjectRow(row)
	if err != nil {
		return publicresume.Snapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s *Store) LiveCards(ctx context.Context) ([]LiveCard, error) {
	rows, err := store.New(s.db).ListLiveResumePreviewCardVersions(ctx)
	if err != nil {
		return nil, err
	}
	cards := make([]LiveCard, 0, len(rows))
	for _, row := range rows {
		card := LiveCard{ResumeID: row.ID}
		if row.CardVersion != nil {
			card.StoredVersion = *row.CardVersion
		}
		cards = append(cards, card)
	}
	return cards, nil
}

func (s *Store) Save(ctx context.Context, resumeID uuid.UUID, version string, png []byte) (resultErr error) {
	if resumeID == uuid.Nil || !ValidVersion(version) || len(png) == 0 || len(png) > MaxPNGBytes {
		return ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); resultErr == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			resultErr = rollbackErr
		}
	}()
	queries := store.New(tx)
	row, err := queries.LockResumeForPreviewCard(ctx, resumeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotLive
	}
	if err != nil {
		return err
	}
	if !row.Live {
		return ErrNotLive
	}
	snapshot, err := s.reader.ProjectRow(row)
	if err != nil {
		return err
	}
	current, err := VersionOf(snapshot)
	if err != nil {
		return err
	}
	if current != version {
		return ErrStale
	}
	if err := queries.UpsertResumePreviewCard(ctx, store.UpsertResumePreviewCardParams{
		ResumeID: resumeID, Version: version, PNG: png, RenderedAt: s.now(),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
