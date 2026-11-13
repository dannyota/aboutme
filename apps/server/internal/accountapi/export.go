package accountapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	exportRequestsPerMinute = 5
	exportCacheControl      = "no-store, no-transform"
	exportAttachment        = `attachment; filename="aboutme-export.json"`
	exportSchemaHeader      = "X-Resume-Schema-Version"
	exportMaxBytes          = 16 * 1024 * 1024
	exportTimeout           = 20 * time.Second
	exportPhotoTimeout      = 5 * time.Second
	exportJPEGMaxEdge       = 2048
	exportPNGMaxEdge        = 1024
)

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
	ExportVersion int               `json:"exportVersion"`
	ExportedAt    time.Time         `json:"exportedAt"`
	Account       exportProfile     `json:"account"`
	Resumes       []exportResume    `json:"resumes"`
	Projects      []project.Project `json:"projects"`
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
	PublicTitle     *string         `json:"publicTitle"`
	FaviconEmoji    *string         `json:"faviconEmoji"`
	SignInToView    bool            `json:"signInToView"`
	Showcase        *exportShowcase `json:"showcase"`
	Revision        string          `json:"revision"`
	SchemaVersion   int32           `json:"schemaVersion"`
	Lng             string          `json:"lng"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	Document        json.RawMessage `json:"document"`
	Photo           *exportPhoto    `json:"photo"`
}

type exportShowcase struct {
	State       string    `json:"state"`
	Role        *string   `json:"role"`
	RequestedAt time.Time `json:"requestedAt"`
}

func exportShowcaseOf(row store.ListAccountExportResumesRow) *exportShowcase {
	if row.ShowcaseRequestedAt == nil {
		return nil
	}
	return &exportShowcase{
		State:       resume.ShowcaseListed,
		Role:        row.ShowcaseRole,
		RequestedAt: *row.ShowcaseRequestedAt,
	}
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
			PublicTitle:     row.PublicTitle,
			FaviconEmoji:    row.FaviconEmoji,
			SignInToView:    row.SignInToView,
			Showcase:        exportShowcaseOf(row),
			Revision:        strconv.FormatInt(row.Revision, 10),
			SchemaVersion:   s.projector.CurrentVersion(),
			Lng:             projectExportLanguage(row.Lng),
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
			Document:        document,
			Photo:           photo,
		}
	}
	projects, projectErr := s.exportProjects(ctx, q, userID)
	if projectErr != nil {
		return nil, projectErr
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit snapshot: %w", err)
	}

	return marshalExportPayload(exportData{
		ExportVersion: 2,
		ExportedAt:    s.now().UTC(),
		Account: exportProfile{
			ID: profile.ID, Email: profile.Email, Name: profile.Name,
			CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt,
			LinkedProviders: providers,
		},
		Resumes:  resumes,
		Projects: projects,
	})
}

func validateExportResumeCount(rows []store.ListAccountExportResumesRow) error {
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
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(exportEnvelope{Data: data}); err != nil {
		return nil, fmt.Errorf("marshal export: %w", err)
	}
	payload := bytes.TrimSuffix(out.Bytes(), []byte("\n"))
	if len(payload) > exportMaxBytes {
		return nil, errors.New("complete export exceeds size limit")
	}
	return payload, nil
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
