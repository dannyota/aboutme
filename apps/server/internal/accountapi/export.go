package accountapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"

	// Register the standard JPEG decoder used to validate stored portable media.
	_ "image/jpeg"
	// Register the standard PNG decoder used to validate stored portable media.
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/text/language"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	exportRequestsPerMinute = 5
	exportCacheControl      = "no-store, no-transform"
	exportAttachment        = `attachment; filename="aboutme-export.json"`
	exportSchemaHeader      = "X-Resume-Schema-Version"
	exportMaxBytes          = 12 * 1024 * 1024
	exportTimeout           = 20 * time.Second
	exportPhotoTimeout      = 5 * time.Second
	exportJPEGMaxEdge       = 2048
	exportPNGMaxEdge        = 1024
)

// RegisterRoutes publishes the standalone portable account export route.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	protected := auth.RequireSession(s.sessions)(
		api.RateLimit(api.RateLimiterConfig{
			Requests:       exportRequestsPerMinute,
			Window:         time.Minute,
			TrustedProxies: s.trustedProxies,
			Clock:          s.now,
			Key: api.CompositeKeyFunc(
				api.AccountKeyFunc,
				api.IPKeyFunc,
			),
			Logger: s.logger,
		})(http.HandlerFunc(s.handleExport)),
	)
	mux.Handle(ExportPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		protected.ServeHTTP(w, r)
	}))
}

func (s *Service) handleExport(w http.ResponseWriter, r *http.Request) {
	if exportRequestInvalid(r) {
		api.WriteError(w, http.StatusBadRequest, "request_invalid", "invalid account request")
		return
	}
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		s.writeExportUnavailable(w, r, "missing_session", nil)
		return
	}

	payload, err := s.exportAttachment(r.Context(), session.UserID)
	if err != nil {
		s.writeExportUnavailable(w, r, "build_export", err)
		return
	}
	if err := r.Context().Err(); err != nil {
		s.writeExportUnavailable(w, r, "canceled_export", err)
		return
	}

	w.Header().Set("Cache-Control", exportCacheControl)
	w.Header().Set("Content-Disposition", exportAttachment)
	w.Header().Set(exportSchemaHeader, strconv.FormatInt(int64(s.projector.CurrentVersion()), 10))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.WriteHeader(http.StatusOK)
	if _, writeErr := w.Write(payload); writeErr != nil {
		s.logExportFailure(r.Context(), "write_export", writeErr)
	}
}

func exportRequestInvalid(r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return true
	}
	if r.Body != nil && r.Body != http.NoBody {
		var probe [1]byte
		n, err := r.Body.Read(probe[:])
		if n != 0 || err != io.EOF {
			return true
		}
	}
	for _, header := range []string{
		"Authorization",
		exportSchemaHeader,
		"Idempotency-Key",
		"If-Match",
		"If-None-Match",
		"If-Modified-Since",
		"If-Unmodified-Since",
		"If-Range",
	} {
		if len(r.Header.Values(header)) != 0 {
			return true
		}
	}
	return false
}

type exportEnvelope struct {
	Data exportData `json:"data"`
}

type exportData struct {
	ExportVersion int            `json:"exportVersion"`
	ExportedAt    time.Time      `json:"exportedAt"`
	Account       exportProfile  `json:"account"`
	Resumes       []exportResume `json:"resumes"`
}

type exportProfile struct {
	ID              uuid.UUID `json:"id"`
	Email           string    `json:"email"`
	Name            string    `json:"name"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	LinkedProviders []string  `json:"linkedProviders"`
}

type exportResume struct {
	ID              uuid.UUID       `json:"id"`
	Title           string          `json:"title"`
	Slug            *string         `json:"slug"`
	Live            bool            `json:"live"`
	DownloadEnabled bool            `json:"downloadEnabled"`
	SEOGeoEnabled   bool            `json:"seoGeoEnabled"`
	Revision        string          `json:"revision"`
	SchemaVersion   int32           `json:"schemaVersion"`
	Lng             string          `json:"lng"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	Document        json.RawMessage `json:"document"`
	Photo           *exportPhoto    `json:"photo"`
}

type exportPhoto struct {
	MediaType string          `json:"mediaType"`
	Data      string          `json:"data"`
	Crop      json.RawMessage `json:"crop"`
}

func (s *Service) exportAttachment(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, exportTimeout)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("begin snapshot: %w", err)
	}
	defer s.rollbackExportSnapshot(ctx, tx)

	q := store.New(tx)
	profile, err := q.GetAccountExportProfile(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	providers, err := q.ListAccountExportProviders(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read providers: %w", err)
	}
	providers, err = portableProviders(providers)
	if err != nil {
		return nil, err
	}
	rows, err := q.ListAccountExportResumes(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read resumes: %w", err)
	}
	if err := validateExportResumeCount(rows); err != nil {
		return nil, err
	}

	resumes := make([]exportResume, len(rows))
	for i, row := range rows {
		document, photoRef, err := s.exportDocument(row)
		if err != nil {
			return nil, fmt.Errorf("project resume: %w", err)
		}
		photo, err := s.exportPhoto(ctx, row.ID, photoRef)
		if err != nil {
			return nil, fmt.Errorf("read resume media: %w", err)
		}
		resumes[i] = exportResume{
			ID:              row.ID,
			Title:           row.Title,
			Slug:            row.Slug,
			Live:            row.Live,
			DownloadEnabled: row.DownloadEnabled,
			SEOGeoEnabled:   row.SEOGeoEnabled,
			Revision:        strconv.FormatInt(row.Revision, 10),
			SchemaVersion:   s.projector.CurrentVersion(),
			Lng:             projectExportLanguage(row.Lng),
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
			Document:        document,
			Photo:           photo,
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit snapshot: %w", err)
	}

	return marshalExportPayload(exportData{
		ExportVersion: 1,
		ExportedAt:    s.now().UTC(),
		Account: exportProfile{
			ID: profile.ID, Email: profile.Email, Name: profile.Name,
			CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt,
			LinkedProviders: providers,
		},
		Resumes: resumes,
	})
}

func validateExportResumeCount(rows []store.Resume) error {
	if len(rows) > 3 {
		return errors.New("account export resume count exceeds limit")
	}
	return nil
}

func (s *Service) rollbackExportSnapshot(ctx context.Context, tx pgx.Tx) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), exportPhotoTimeout)
	defer cancel()
	if rollbackErr := tx.Rollback(cleanupCtx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
		s.logExportFailure(ctx, "rollback_export_snapshot", rollbackErr)
	}
}

func marshalExportPayload(data exportData) ([]byte, error) {
	payload, err := json.Marshal(exportEnvelope{Data: data})
	if err != nil {
		return nil, fmt.Errorf("marshal export: %w", err)
	}
	if len(payload) > exportMaxBytes {
		return nil, errors.New("complete export exceeds size limit")
	}
	return payload, nil
}

type storedPhotoReference struct {
	Key  string          `json:"key"`
	Crop json.RawMessage `json:"crop"`
}

func (s *Service) exportDocument(row store.Resume) (json.RawMessage, *storedPhotoReference, error) {
	personalDetails, content, customization, err := s.projector.Project(
		row.PersonalDetails, row.Content, row.Customization, row.SchemaVersion,
	)
	if err != nil {
		return nil, nil, err
	}
	if _, decodeErr := resume.DecodeParts(personalDetails, content, customization, s.projector.CurrentVersion()); decodeErr != nil {
		return nil, nil, fmt.Errorf("decode projected document: %w", decodeErr)
	}

	var personal map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(personalDetails, &personal); unmarshalErr != nil || personal == nil {
		return nil, nil, errors.New("invalid projected personal details")
	}
	var photo *storedPhotoReference
	if raw, ok := personal["photo"]; ok && !bytes.Equal(raw, []byte("null")) {
		var reference storedPhotoReference
		if unmarshalErr := json.Unmarshal(raw, &reference); unmarshalErr != nil || reference.Key == "" {
			return nil, nil, errors.New("invalid projected photo reference")
		}
		if len(reference.Crop) == 0 {
			reference.Crop = json.RawMessage("null")
		}
		photo = &reference
	}
	delete(personal, "photo")
	personalDetails, err = json.Marshal(personal)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal portable personal details: %w", err)
	}
	document, err := json.Marshal(struct {
		Content         json.RawMessage `json:"content"`
		Customization   json.RawMessage `json:"customization"`
		PersonalDetails json.RawMessage `json:"personalDetails"`
		SchemaVersion   int32           `json:"schemaVersion"`
	}{
		Content: content, Customization: customization, PersonalDetails: personalDetails,
		SchemaVersion: s.projector.CurrentVersion(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal portable document: %w", err)
	}
	return document, photo, nil
}

func (s *Service) exportPhoto(ctx context.Context, resumeID uuid.UUID, reference *storedPhotoReference) (*exportPhoto, error) {
	if reference == nil {
		return nil, nil
	}
	extension, err := media.ParsePhotoKey(resumeID, reference.Key)
	if err != nil {
		return nil, errors.New("invalid referenced media key")
	}
	ctx, cancel := context.WithTimeout(ctx, exportPhotoTimeout)
	defer cancel()
	reader, mediaType, err := s.media.Get(ctx, reference.Key)
	if err != nil {
		if reader != nil {
			if closeErr := reader.Close(); closeErr != nil {
				s.logExportFailure(ctx, "close_get_error_reader", closeErr)
			}
		}
		return nil, err
	}
	if reader == nil {
		return nil, errors.New("referenced media reader is missing")
	}

	var closeOnce sync.Once
	var closeErr error
	closeBody := func() { closeOnce.Do(func() { closeErr = reader.Close() }) }
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

	wantMediaType := "image/jpeg"
	if extension == "png" {
		wantMediaType = "image/png"
	}
	if mediaType != wantMediaType {
		return nil, errors.New("referenced media type does not match key")
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, media.MaxObjectBytes+1))
	if readErr != nil {
		return nil, fmt.Errorf("read photo: %w", readErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read photo: %w", err)
	}
	if len(data) == 0 || len(data) > media.MaxObjectBytes {
		return nil, errors.New("referenced media exceeds size limit")
	}
	if !validPortablePhoto(data, extension) {
		return nil, errors.New("referenced media is invalid")
	}
	closeBody()
	if closeErr != nil {
		return nil, fmt.Errorf("close photo: %w", closeErr)
	}
	return &exportPhoto{
		MediaType: mediaType,
		Data:      base64.StdEncoding.EncodeToString(data),
		Crop:      reference.Crop,
	}, nil
}

func validPortablePhoto(data []byte, extension string) bool {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (extension == "jpg" && format != "jpeg") || (extension == "png" && format != "png") {
		return false
	}
	maxEdge := exportJPEGMaxEdge
	if extension == "png" {
		maxEdge = exportPNGMaxEdge
	}
	if config.Width < 1 || config.Height < 1 || config.Width > maxEdge || config.Height > maxEdge {
		return false
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || (extension == "jpg" && format != "jpeg") || (extension == "png" && format != "png") {
		return false
	}
	bounds := decoded.Bounds()
	return bounds.Dx() == config.Width && bounds.Dy() == config.Height
}

func projectExportLanguage(value *string) string {
	if value == nil || *value == "" {
		return language.Und.String()
	}
	tag, err := language.Parse(*value)
	if err != nil {
		return language.Und.String()
	}
	canonical := tag.String()
	if utf8.RuneCountInString(canonical) > resume.MaxLngCharacters {
		return language.Und.String()
	}
	return canonical
}

func portableProviders(providers []string) ([]string, error) {
	out := make([]string, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		if provider != "google" && provider != "github" && provider != "linkedin" {
			return nil, errors.New("unknown linked provider")
		}
		if _, exists := seen[provider]; exists {
			continue
		}
		seen[provider] = struct{}{}
		out = append(out, provider)
	}
	if len(out) > 3 {
		return nil, errors.New("too many linked providers")
	}
	return out, nil
}

func (s *Service) writeExportUnavailable(w http.ResponseWriter, r *http.Request, operation string, err error) {
	s.logExportFailure(r.Context(), operation, err)
	api.WriteError(w, http.StatusServiceUnavailable, "account_unavailable", "account operation unavailable; try again")
}

func (s *Service) logExportFailure(ctx context.Context, operation string, err error) {
	if s.logger != nil {
		attrs := []any{"request_id", api.RequestIDFromContext(ctx), "op", operation}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			attrs = append(attrs, "sqlstate", pgErr.Code)
		}
		s.logger.ErrorContext(ctx, "account export unavailable", attrs...)
	}
}
