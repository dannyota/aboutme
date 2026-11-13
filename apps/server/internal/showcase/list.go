package showcase

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/previewmeta"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	PageSize = 12
	MaxPage  = 100
)

type Filter struct {
	Role     string
	Lang     string
	Template string
	Page     int
}

type Item struct {
	Slug        string  `json:"slug"`
	CardVersion string  `json:"cardVersion"`
	ImageText   string  `json:"imageText"`
	Language    string  `json:"language"`
	TemplateID  *string `json:"templateId"`
	Role        *string `json:"role"`
}

type Page struct {
	Items     []Item `json:"items"`
	Page      int    `json:"page"`
	PageCount int    `json:"pageCount"`
	Total     int    `json:"total"`
}

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

func cardImageText(slug string, details schema.PersonalDetails) string {
	document := publicresume.ProjectDocument(schema.Resume{PersonalDetails: details}, "")
	public := publicresume.PublicResume{Slug: slug, Lng: "und", Document: document}
	return previewmeta.For(public, nil).ImageAlt
}
