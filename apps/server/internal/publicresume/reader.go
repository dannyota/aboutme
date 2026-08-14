package publicresume

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

var ErrUnavailable = errors.New("public resume unavailable")

type Snapshot struct {
	ResumeID         uuid.UUID
	Revision         int64
	DiscoveryEnabled bool
	Public           PublicResume
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

// ReadResume reads a live row, admits its exact revision, and retries one
// changed generation before returning unavailable. No later cache or
// conditional work can be performed without the returned lease.
func (r *Reader) ReadResume(ctx context.Context, slug string, representation publicstate.Representation) (Snapshot, *publicstate.Lease, error) {
	for attempt := 0; attempt < 2; attempt++ {
		row, err := r.store.GetPublicResumeBySlug(ctx, slug)
		if err != nil || row.Slug == nil || *row.Slug != slug || !row.Live || row.Revision <= 0 {
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
		if err != nil {
			lease.Release()
			return Snapshot{}, nil, ErrUnavailable
		}
		if representation == publicstate.RepresentationPhoto && snapshot.photoKey == "" {
			lease.Release()
			return Snapshot{}, nil, ErrUnavailable
		}
		return snapshot, lease, nil
	}
	return Snapshot{}, nil, ErrUnavailable
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
	return Snapshot{ResumeID: row.ID, Revision: row.Revision, DiscoveryEnabled: row.SEOGeoEnabled, Public: public, photoKey: photoKey}, nil
}

// ReadPhoto keeps the storage key private to Reader and uses the lease-owned
// request context so a revocation can interrupt a blocked object read.
func (r *Reader) ReadPhoto(ctx context.Context, snapshot Snapshot) ([]byte, string, error) {
	if r.media == nil || snapshot.photoKey == "" {
		return nil, "", ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	body, contentType, err := r.media.Get(ctx, snapshot.photoKey)
	if err != nil {
		return nil, "", ErrUnavailable
	}
	bytes, err := io.ReadAll(io.LimitReader(body, media.MaxObjectBytes+1))
	closeErr := body.Close()
	if err != nil || closeErr != nil || int64(len(bytes)) > media.MaxObjectBytes || (contentType != "image/jpeg" && contentType != "image/png") {
		return nil, "", ErrUnavailable
	}
	return bytes, contentType, nil
}
