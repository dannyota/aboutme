package showcase

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/previewmeta"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	// PageSize is the number of listings on one page.
	PageSize = 12
	// MaxPage is the highest page number a request may ask for.
	MaxPage = 100
)

// Filter selects a listing page. An empty field applies no filter. The caller
// has already validated every value (see the Valid functions); Page is 1 to
// MaxPage.
type Filter struct {
	Role     string
	Lang     string
	Template string
	Page     int
}

// Item is one listing. Its fields are closed: it carries no account
// identifier, contact detail, date, or count (docs/design/showcase.md "What a
// listing shows").
type Item struct {
	Slug        string  `json:"slug"`
	CardVersion string  `json:"cardVersion"`
	ImageText   string  `json:"imageText"`
	Language    string  `json:"language"`
	TemplateID  *string `json:"templateId"`
	Role        *string `json:"role"`
}

// Page is one page of the listing.
type Page struct {
	Items     []Item `json:"items"`
	Page      int    `json:"page"`
	PageCount int    `json:"pageCount"`
	Total     int    `json:"total"`
}

// List reads one page of the listing from committed state. One statement
// checks all five listing conditions (the opt-in row, an approved result, an
// approved key equal to the current key, a live resume, sign in to view off),
// and nothing caches it, so a request admitted after an opt-out, unpublish,
// sign in to view, decline, rename, or delete commits cannot list the resume.
func (s *Service) List(ctx context.Context, filter Filter) (Page, error) {
	queries := store.New(s.db)
	total, err := queries.CountShowcase(ctx, store.CountShowcaseParams{
		Role: optional(filter.Role), Lang: optional(filter.Lang), Template: optional(filter.Template),
	})
	if err != nil {
		return Page{}, fmt.Errorf("showcase: count listing: %w", err)
	}
	rows, err := queries.ListShowcase(ctx, store.ListShowcaseParams{
		Role: optional(filter.Role), Lang: optional(filter.Lang), Template: optional(filter.Template),
		PageSize: PageSize, PageOffset: int32((filter.Page - 1) * PageSize), //nolint:gosec // Page is bounded to 1..MaxPage by the caller.
	})
	if err != nil {
		return Page{}, fmt.Errorf("showcase: read listing: %w", err)
	}
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		if row.Slug == nil {
			s.logSkipped(ctx, row.ID)
			continue
		}
		text, textErr := s.imageText(ctx, queries, row)
		if textErr != nil {
			// A resume whose details cannot be read is not shown; it is not
			// served publicly either. It still counts in Total.
			s.logSkipped(ctx, row.ID)
			continue
		}
		items = append(items, Item{
			Slug: *row.Slug, CardVersion: row.CardVersion, ImageText: text,
			Language: row.Language, TemplateID: row.TemplateID, Role: row.Role,
		})
	}
	return Page{Items: items, Page: filter.Page, PageCount: pageCount(total), Total: int(total)}, nil
}

// logSkipped records a listed resume whose row could not be read. It names the
// resume ID only, never a name or headline.
func (s *Service) logSkipped(ctx context.Context, id uuid.UUID) {
	if s.logger != nil {
		s.logger.ErrorContext(ctx, "showcase listing skipped a row", "resume_id", id.String())
	}
}

func pageCount(total int64) int {
	return int((total + PageSize - 1) / PageSize)
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// imageText is the card's image text of one listed resume. It reads only the
// personal details when they are at the current document version, which is
// every row once the backfill has run, and the whole document otherwise.
func (s *Service) imageText(ctx context.Context, queries *store.Queries, row store.ListShowcaseRow) (string, error) {
	details := row.PersonalDetails
	version := row.SchemaVersion
	content, customization := json.RawMessage("{}"), json.RawMessage("{}")
	if version != s.projector.CurrentVersion() {
		full, err := queries.GetResumeByID(ctx, row.ID)
		if err != nil {
			return "", fmt.Errorf("showcase: read resume: %w", err)
		}
		details, content, customization = full.PersonalDetails, full.Content, full.Customization
		version = full.SchemaVersion
	}
	personalDetails, content, customization, err := s.projector.Project(details, content, customization, version)
	if err != nil {
		return "", fmt.Errorf("showcase: project resume: %w", err)
	}
	doc, err := resume.DecodeParts(personalDetails, content, customization, s.projector.CurrentVersion())
	if err != nil {
		return "", fmt.Errorf("showcase: decode resume: %w", err)
	}
	return cardImageText(*row.Slug, doc.PersonalDetails), nil
}

// cardImageText applies the preview card's image text rule to personal
// details, so it holds no contact detail.
func cardImageText(slug string, details schema.PersonalDetails) string {
	document := publicresume.ProjectDocument(schema.Resume{PersonalDetails: details}, "")
	public := publicresume.PublicResume{Slug: slug, Lng: "und", Document: document}
	return previewmeta.For(public, nil).ImageAlt
}
