package publicresume

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

var (
	ErrNotFound    = errors.New("public resume not found")
	ErrUnavailable = errors.New("public resume unavailable")
)

type Snapshot struct {
	ResumeID         uuid.UUID
	Revision         int64
	DiscoveryEnabled bool
	Public           PublicResume
	RevisionTime     time.Time
	PublicTitle      *string
	FaviconEmoji     *string
	SignInToView     bool
	ViewPassEpoch    int32
	ProjectLinks     []ProjectLink
	photoKey         string
}

type ReaderDependencies struct {
	Store       store.PublicReadQueries
	Projector   *docmigrate.Projector
	Coordinator *publicstate.Coordinator
	Media       media.Backend
	Origin      PublicOrigin
}

type Reader struct {
	store       store.PublicReadQueries
	projector   *docmigrate.Projector
	coordinator *publicstate.Coordinator
	media       media.Backend
	origin      PublicOrigin
}

func NewReader(dependencies ReaderDependencies) (*Reader, error) {
	if dependencies.Store == nil || dependencies.Projector == nil || dependencies.Coordinator == nil || dependencies.Origin.value == "" {
		return nil, errors.New("invalid public reader dependencies")
	}
	return &Reader{store: dependencies.Store, projector: dependencies.Projector, coordinator: dependencies.Coordinator, media: dependencies.Media, origin: dependencies.Origin}, nil
}

func (r *Reader) ReadResume(ctx context.Context, slug string, representation publicstate.Representation) (Snapshot, *publicstate.Lease, error) {
	for attempt := 0; attempt < 2; attempt++ {
		read, err := r.store.GetPublicResumeBySlug(ctx, slug)
		row := read.Resume
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, nil, ErrNotFound
		}
		if err != nil {
			return Snapshot{}, nil, ErrUnavailable
		}
		if row.Slug == nil || *row.Slug != slug || !row.Live {
			return Snapshot{}, nil, ErrNotFound
		}
		if row.Revision <= 0 {
			return Snapshot{}, nil, ErrUnavailable
		}
		lease, err := r.coordinator.AcquireResume(ctx, row.ID, row.Revision, representation)
		if err != nil {
			var mismatch *publicstate.GenerationMismatchError
			if errors.As(err, &mismatch) && attempt == 0 {
				continue
			}
			return Snapshot{}, nil, ErrUnavailable
		}
		snapshot, err := r.snapshot(row)
		if err == nil {
			snapshot.ProjectLinks, err = projectLinks(read.ProjectLinks, snapshot.Public, r.origin)
		}
		if err != nil {
			lease.Release()
			return Snapshot{}, nil, ErrUnavailable
		}
		if representation == publicstate.RepresentationPhoto && snapshot.photoKey == "" {
			lease.Release()
			return Snapshot{}, nil, ErrNotFound
		}
		if representation == publicstate.RepresentationPDF && !snapshot.Public.DownloadEnabled {
			lease.Release()
			return Snapshot{}, nil, ErrNotFound
		}
		return snapshot, lease, nil
	}
	return Snapshot{}, nil, ErrUnavailable
}

func (r *Reader) ProjectRow(row store.Resume) (Snapshot, error) {
	if row.Slug == nil || row.Revision <= 0 {
		return Snapshot{}, ErrUnavailable
	}
	return r.snapshot(row)
}

func (s Snapshot) PhotoKeyDigest() string {
	if s.photoKey == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(s.photoKey))
	return hex.EncodeToString(digest[:])
}

func (r *Reader) snapshot(row store.Resume) (Snapshot, error) {
	pd, content, customization, err := r.projector.Project(row.PersonalDetails, row.Content, row.Customization, row.SchemaVersion)
	if err != nil {
		return Snapshot{}, err
	}
	doc, err := resume.DecodeParts(pd, content, customization, docmigrate.CurrentVersion)
	if err != nil {
		return Snapshot{}, err
	}
	public, err := Project(resume.Resume{ID: row.ID, Slug: row.Slug, Live: row.Live, DownloadEnabled: row.DownloadEnabled, Revision: row.Revision, Lng: row.Lng, Doc: doc}, r.origin)
	if err != nil {
		return Snapshot{}, err
	}
	photoKey := ""
	if doc.PersonalDetails.Photo != nil {
		photoKey = doc.PersonalDetails.Photo.Key
	}
	return Snapshot{
		ResumeID: row.ID, Revision: row.Revision, DiscoveryEnabled: row.SEOGeoEnabled && !row.SignInToView, Public: public, RevisionTime: row.UpdatedAt,
		PublicTitle: row.PublicTitle, FaviconEmoji: row.FaviconEmoji, SignInToView: row.SignInToView, ViewPassEpoch: row.ViewPassEpoch, photoKey: photoKey,
	}, nil
}

func (r *Reader) ReadPhoto(ctx context.Context, snapshot Snapshot) ([]byte, string, error) {
	if r.media == nil || snapshot.photoKey == "" {
		return nil, "", ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	body, contentType, err := r.media.Get(ctx, snapshot.photoKey)
	if err != nil {
		if body != nil {
			_ = body.Close() //nolint:errcheck // The storage read already failed; cleanup cannot change its result.
		}
		return nil, "", ErrUnavailable
	}
	if body == nil {
		return nil, "", ErrUnavailable
	}
	var closeOnce sync.Once
	var closeErr error
	closeBody := func() { closeOnce.Do(func() { closeErr = body.Close() }) }
	stopClose := make(chan struct{})
	closeJoined := make(chan struct{})
	go func() {
		defer close(closeJoined)
		select {
		case <-ctx.Done():
			closeBody()
		case <-stopClose:
		}
	}()
	defer func() {
		close(stopClose)
		<-closeJoined
		closeBody()
	}()
	bytes, err := io.ReadAll(io.LimitReader(body, media.MaxObjectBytes+1))
	closeBody()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, "", ctxErr
	}
	if err != nil || closeErr != nil || int64(len(bytes)) > media.MaxObjectBytes || (contentType != "image/jpeg" && contentType != "image/png") {
		return nil, "", ErrUnavailable
	}
	return bytes, contentType, nil
}
