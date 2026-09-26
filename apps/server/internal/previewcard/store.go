package previewcard

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

var (
	// ErrNotLive reports a resume that is not live, so no card may be
	// stored or served for it.
	ErrNotLive = errors.New("previewcard: resume is not live")
	// ErrStale reports a card whose version is no longer the resume's
	// current one.
	ErrStale = errors.New("previewcard: card version is not current")
)

// Database is the pool boundary the store needs: plain queries and a
// transaction.
type Database interface {
	store.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

// StoredCard is one stored card row.
type StoredCard struct {
	Version string
	PNG     []byte
}

// LiveCard is one live resume and its stored card version, "" when none.
type LiveCard struct {
	ResumeID      uuid.UUID
	StoredVersion string
}

// Store reads and writes stored cards. It performs no public admission: a
// public route passes the live-state gate before it calls Stored.
type Store struct {
	db     Database
	reader *publicresume.Reader
	now    func() time.Time
}

// NewStore creates a card store. reader projects rows so a store can
// recompute the card version from the row it holds locked.
func NewStore(db Database, reader *publicresume.Reader, now func() time.Time) (*Store, error) {
	if db == nil || reader == nil {
		return nil, errors.New("previewcard: invalid store dependencies")
	}
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, reader: reader, now: now}, nil
}

// Stored returns the stored card of resumeID, and false when there is none.
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

// Live returns the public snapshot of resumeID while it is live, and false
// when it is not.
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

// LiveCards lists every live resume with the version of its stored card.
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

// Save stores png as the card of resumeID in one transaction that locks the
// resume row for share, recomputes the card version from the committed row,
// and writes only while the resume is live and version is still current.
// Unpublish, rename, and delete lock the same row for update and remove the
// card in their own transaction, so a store never outlives one of them.
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
