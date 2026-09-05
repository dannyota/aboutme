package resumeapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/printsnapshot"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

const (
	ownerPDFRequestsPerMinute = 10
	maxOwnerPDFBytes          = 16_777_216
)

var errOwnerPDFPreparation = errors.New("resumeapi: owner PDF preparation failed")

type ownerResumeReader interface {
	Get(context.Context, uuid.UUID, uuid.UUID) (resume.Resume, error)
}

type ownerPDFPreparationFailure uint32

const (
	ownerPDFPreparationOK ownerPDFPreparationFailure = iota
	ownerPDFPreparationNotFound
	ownerPDFPreparationUnavailable
)

func pdfRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodGet, Pattern: apiResumePath + "/{id}/pdf",
			Operation: "downloadResumePDF", Render: true, Handler: (*Service).handleDownloadResumePDF},
		{Method: http.MethodHead, Pattern: apiResumePath + "/{id}/pdf",
			Operation: "headResumePDF", Render: true, Handler: (*Service).handleDownloadResumePDF},
	}
}

func newOwnerPDFAdmission(opts Options) api.Middleware {
	config := api.RateLimiterConfig{
		Requests: ownerPDFRequestsPerMinute, Window: time.Minute,
		TrustedProxies: opts.TrustedProxies, Clock: opts.Clock, Logger: opts.Logger,
	}
	accountConfig := config
	accountConfig.Key = resumeAccountKey
	ipConfig := config
	ipConfig.Key = api.IPKeyFunc
	account := api.RateLimit(accountConfig)
	ip := api.RateLimit(ipConfig)
	return func(next http.Handler) http.Handler {
		return account(ip(next))
	}
}

func (s *Service) wrapPDFAdmission(next http.Handler) http.Handler {
	if s.pdfAdmission == nil {
		return next
	}
	return s.pdfAdmission(next)
}

func (s *Service) handleDownloadResumePDF(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", api.CacheControlNoStore)
	if err := validateOwnerPDFRequest(r); err != nil {
		writeResumeError(w, mapMutationError(err))
		return
	}
	resumeID, pathErr := parseResumePathID(r)
	if pathErr != nil {
		writeResumeError(w, pathErr)
		return
	}
	userID, userErr := requestUserID(r)
	if userErr != nil {
		writeResumeError(w, userErr)
		return
	}
	reader, ok := s.resumes.(ownerResumeReader)
	if !ok || s.printQueue == nil {
		writeResumeError(w, ownerPDFUnavailableError())
		return
	}

	var preparation atomic.Uint32
	var preparedRevision atomic.Int64
	result, renderErr := s.printQueue.Render(r.Context(), renderjob.Request{
		Format: renderjob.PDF,
		Prepare: func(ctx context.Context) (renderjob.Snapshot, error) {
			row, err := reader.Get(ctx, userID, resumeID)
			if err != nil {
				if errors.Is(err, resume.ErrNotFound) {
					preparation.Store(uint32(ownerPDFPreparationNotFound))
				} else {
					preparation.Store(uint32(ownerPDFPreparationUnavailable))
				}
				return renderjob.Snapshot{}, errOwnerPDFPreparation
			}
			photo, contentType, err := s.readOwnerPDFPhoto(ctx, row)
			if err != nil {
				preparation.Store(uint32(ownerPDFPreparationUnavailable))
				return renderjob.Snapshot{}, errOwnerPDFPreparation
			}
			envelope, err := printsnapshot.FromOwner(row, photo, contentType)
			if err != nil {
				preparation.Store(uint32(ownerPDFPreparationUnavailable))
				return renderjob.Snapshot{}, errOwnerPDFPreparation
			}
			payload, err := printsnapshot.Marshal(envelope)
			if err != nil {
				preparation.Store(uint32(ownerPDFPreparationUnavailable))
				return renderjob.Snapshot{}, errOwnerPDFPreparation
			}
			preparedRevision.Store(row.Revision)
			preparation.Store(uint32(ownerPDFPreparationOK))
			return renderjob.Snapshot{
				ResumeID: row.ID, Revision: row.Revision, SchemaVersion: int(row.Doc.SchemaVersion),
				Payload: payload,
			}, nil
		},
	})
	if renderErr != nil {
		if ownerPDFPreparationFailure(preparation.Load()) == ownerPDFPreparationNotFound {
			writeResumeError(w, mapMutationError(resume.ErrNotFound))
			return
		}
		writeResumeError(w, ownerPDFUnavailableError())
		return
	}
	if len(result.Bytes) == 0 || len(result.Bytes) > maxOwnerPDFBytes || result.Revision <= 0 || result.Revision != preparedRevision.Load() {
		writeResumeError(w, ownerPDFUnavailableError())
		return
	}

	w.Header().Set("Cache-Control", api.CacheControlNoStore)
	w.Header().Set("Content-Disposition", `attachment; filename="resume.pdf"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(result.Bytes)))
	w.Header().Set("Content-Type", "application/pdf")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(result.Bytes) //nolint:errcheck // The response is committed; retrying can duplicate bytes, and this owner boundary must not log details.
}

func validateOwnerPDFRequest(r *http.Request) error {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return requestInvalid("PDF export does not accept query options")
	}
	if len(r.Header.Values(wireVersionHeader)) != 0 {
		return requestInvalid("PDF export does not accept schema-version negotiation")
	}
	if len(r.Header.Values("If-None-Match")) != 0 {
		return requestInvalid("PDF export does not accept conditional requests")
	}
	if len(r.Header.Values("Content-Encoding")) != 0 {
		return requestInvalid("PDF export does not accept encoded request bodies")
	}
	if len(r.TransferEncoding) != 0 || r.ContentLength < 0 {
		return requestInvalid("PDF export requests require empty fixed-length framing")
	}
	if r.ContentLength > 0 {
		return requestInvalid("PDF export requests have no body")
	}
	if r.Body == nil {
		return nil
	}
	var one [1]byte
	n, err := r.Body.Read(one[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		return requestInvalid("PDF export requests have no body")
	}
	return nil
}

func (s *Service) readOwnerPDFPhoto(ctx context.Context, row resume.Resume) ([]byte, string, error) {
	photo := row.Doc.PersonalDetails.Photo
	if photo == nil {
		return nil, "", nil
	}
	extension, err := media.ParsePhotoKey(row.ID, photo.Key)
	if err != nil {
		s.recordPhotoKeyInvariant(ctx)
		return nil, "", errOwnerPDFPreparation
	}
	if s.blobs == nil {
		return nil, "", errOwnerPDFPreparation
	}
	body, contentType, err := s.blobs.Get(ctx, photo.Key)
	if err != nil {
		if body != nil {
			// Both storage and close failures map to the same opaque response.
			_ = body.Close() //nolint:errcheck // The storage read already failed; cleanup cannot change its result.
		}
		return nil, "", errOwnerPDFPreparation
	}
	if body == nil {
		return nil, "", errOwnerPDFPreparation
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

	wantContentType := "image/jpeg"
	if extension == "png" {
		wantContentType = "image/png"
	}
	if contentType != wantContentType {
		return nil, "", errOwnerPDFPreparation
	}
	photoBytes, err := io.ReadAll(io.LimitReader(body, printsnapshot.MaxPhotoBytes+1))
	if err != nil || ctx.Err() != nil || len(photoBytes) == 0 || len(photoBytes) > printsnapshot.MaxPhotoBytes {
		return nil, "", errOwnerPDFPreparation
	}
	closeBody()
	if closeErr != nil {
		return nil, "", errOwnerPDFPreparation
	}
	return photoBytes, contentType, nil
}

func ownerPDFUnavailableError() *clientError {
	return &clientError{
		Status: http.StatusServiceUnavailable, Code: "internal_error",
		Message: "PDF export is temporarily unavailable", Headers: map[string]string{"Retry-After": "1"},
	}
}
