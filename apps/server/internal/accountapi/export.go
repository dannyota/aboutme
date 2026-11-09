package accountapi

import (
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

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
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

// exportEnvelope is the top-level object of the export attachment.
type exportEnvelope struct {
	// Data is the complete export bundle.
	Data exportData `json:"data"`
}

// exportData is one account's portable export bundle.
type exportData struct {
	// ExportVersion is the bundle format version, always 1.
	ExportVersion int `json:"exportVersion"`
	// ExportedAt is when the bundle was built, in UTC.
	ExportedAt time.Time `json:"exportedAt"`
	// Account is the account profile.
	Account exportProfile `json:"account"`
	// Resumes lists the account's resumes, at most three.
	Resumes []exportResume `json:"resumes"`
}

// exportProfile is the account profile in an export bundle.
type exportProfile struct {
	// ID is the account's user ID.
	ID uuid.UUID `json:"id"`
	// Email is the account email address.
	Email string `json:"email"`
	// Name is the account display name.
	Name string `json:"name"`
	// CreatedAt is when the account was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the account profile last changed.
	UpdatedAt time.Time `json:"updatedAt"`
	// LinkedProviders lists the distinct linked sign-in providers (google,
	// github, or linkedin), at most three; provider subjects never appear.
	LinkedProviders []string `json:"linkedProviders"`
}

// exportResume is one resume in an export bundle, with its portable document
// and photo.
type exportResume struct {
	// ID is the resume ID.
	ID uuid.UUID `json:"id"`
	// Title is the owner-visible title.
	Title string `json:"title"`
	// Slug is the public slug, null until the resume is first published.
	Slug *string `json:"slug"`
	// Live reports whether the resume is published.
	Live bool `json:"live"`
	// DownloadEnabled reports whether public download representations are
	// enabled.
	DownloadEnabled bool `json:"downloadEnabled"`
	// SEOGeoEnabled reports whether the live resume participates in
	// discovery.
	SEOGeoEnabled bool `json:"seoGeoEnabled"`
	// PublicTitle is the public page's browser-tab title, null for the
	// default.
	PublicTitle *string `json:"publicTitle"`
	// FaviconEmoji is the public page's icon emoji, null for none.
	FaviconEmoji *string `json:"faviconEmoji"`
	// SignInToView is the publish switch, carried with the other publish
	// settings; the pass epoch never leaves the server
	// (docs/design/viewer-analytics/sign-in-to-view.md "Setting").
	SignInToView bool `json:"signInToView"`
	// Showcase is the community-showcase state, null without an opt-in
	// (docs/design/showcase.md "Privacy and abuse"): the state, the role, and
	// the request time.
	Showcase *exportShowcase `json:"showcase"`
	// Revision is the stored revision as a decimal string.
	Revision string `json:"revision"`
	// SchemaVersion is the current document version the document is
	// projected to.
	SchemaVersion int32 `json:"schemaVersion"`
	// Lng is the canonical BCP 47 language tag, "und" when the stored value
	// is absent, empty, invalid, or overlong.
	Lng string `json:"lng"`
	// CreatedAt is when the resume was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the resume last changed.
	UpdatedAt time.Time `json:"updatedAt"`
	// Document is the projected owner document without
	// personalDetails.photo.
	Document json.RawMessage `json:"document"`
	// Photo is the portable photo, null when the document has none.
	Photo *exportPhoto `json:"photo"`
}

// exportShowcase is a resume's showcase opt-in in an export bundle.
type exportShowcase struct {
	// State is the showcase state, always "listed".
	State string `json:"state"`
	// Role is the role picked for the listing, or null.
	Role *string `json:"role"`
	// RequestedAt is when the opt-in was requested.
	RequestedAt time.Time `json:"requestedAt"`
}

// exportShowcaseOf reads the opt-in off an export row, nil without one.
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

// exportPhoto is a resume's portable photo in an export bundle, with no
// object key.
type exportPhoto struct {
	// MediaType is image/jpeg or image/png, matching the stored key.
	MediaType string `json:"mediaType"`
	// Data is the stored photo bytes in standard base64.
	Data string `json:"data"`
	// Crop is the stored crop JSON, or JSON null without one.
	Crop json.RawMessage `json:"crop"`
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
	payload, err := json.Marshal(exportEnvelope{Data: data})
	if err != nil {
		return nil, fmt.Errorf("marshal export: %w", err)
	}
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
