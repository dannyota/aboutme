package viewapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/viewcount"
)

const (
	dayLayout   = "2006-01-02"
	monthLayout = "2006-01"
	detailDays  = 90
	detailMonth = 12
)

// OwnerQueries is the generated query subset the owner routes read.
type OwnerQueries interface {
	ListViewSummaries(ctx context.Context, arg store.ListViewSummariesParams) ([]store.ListViewSummariesRow, error)
	GetOwnedViewResume(ctx context.Context, arg store.GetOwnedViewResumeParams) (store.GetOwnedViewResumeRow, error)
	ListResumeViewDays(ctx context.Context, arg store.ListResumeViewDaysParams) ([]store.ListResumeViewDaysRow, error)
	ListResumeShareSignals(ctx context.Context, arg store.ListResumeShareSignalsParams) ([]store.ListResumeShareSignalsRow, error)
}

var _ OwnerQueries = (*store.Queries)(nil)

type viewTotals struct {
	Real     int64 `json:"real"`
	Filtered int64 `json:"filtered"`
}

type viewSummary struct {
	ID     uuid.UUID  `json:"id"`
	Title  string     `json:"title"`
	Slug   *string    `json:"slug"`
	Live   bool       `json:"live"`
	Last7  viewTotals `json:"last7"`
	Last30 viewTotals `json:"last30"`
	Last90 viewTotals `json:"last90"`
}

type summaryResponse struct {
	Today   string        `json:"today"`
	Resumes []viewSummary `json:"resumes"`
}

func (s *Service) handleSummary(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		s.writeInternal(w, r)
		return
	}
	today := viewcount.Day(s.now())
	rows, err := s.queries.ListViewSummaries(r.Context(), store.ListViewSummariesParams{
		Since7:  viewcount.Date(today.AddDate(0, 0, -6)),
		Since30: viewcount.Date(today.AddDate(0, 0, -29)),
		Since90: viewcount.Date(today.AddDate(0, 0, -(detailDays - 1))),
		UserID:  sess.UserID,
	})
	if err != nil {
		s.writeInternal(w, r)
		return
	}
	response := summaryResponse{Today: today.Format(dayLayout), Resumes: make([]viewSummary, 0, len(rows))}
	for _, row := range rows {
		response.Resumes = append(response.Resumes, viewSummary{
			ID: row.ID, Title: row.Title, Slug: row.Slug, Live: row.Live,
			Last7:  viewTotals{Real: row.Real7, Filtered: row.Filtered7},
			Last30: viewTotals{Real: row.Real30, Filtered: row.Filtered30},
			Last90: viewTotals{Real: row.Real90, Filtered: row.Filtered90},
		})
	}
	api.WriteData(w, http.StatusOK, response)
}

type viewDay struct {
	Date       string `json:"date"`
	Real       int32  `json:"real"`
	Bot        int32  `json:"bot"`
	Datacenter int32  `json:"datacenter"`
	Anomaly    int32  `json:"anomaly"`
	Invalid    int32  `json:"invalid"`
	Crawler    int32  `json:"crawler"`
}

type viewMonth struct {
	Month    string `json:"month"`
	Real     int64  `json:"real"`
	Filtered int64  `json:"filtered"`
}

type viewPreviews struct {
	Platform string `json:"platform"`
	Fetches  int64  `json:"fetches"`
}

type detailResponse struct {
	ID       uuid.UUID      `json:"id"`
	Title    string         `json:"title"`
	Slug     *string        `json:"slug"`
	Live     bool           `json:"live"`
	Today    string         `json:"today"`
	Days     []viewDay      `json:"days"`
	Months   []viewMonth    `json:"months"`
	Previews []viewPreviews `json:"previews"`
}

func (s *Service) handleDetail(w http.ResponseWriter, r *http.Request) {
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		s.writeInternal(w, r)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || id.String() != r.PathValue("id") {
		writeResumeNotFound(w)
		return
	}
	resume, err := s.queries.GetOwnedViewResume(r.Context(), store.GetOwnedViewResumeParams{ID: id, UserID: sess.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeResumeNotFound(w)
		return
	}
	if err != nil {
		s.writeInternal(w, r)
		return
	}
	today := viewcount.Day(s.now())
	firstDay := today.AddDate(0, 0, -(detailDays - 1))
	thisMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	firstMonth := thisMonth.AddDate(0, -(detailMonth - 1), 0)
	since := firstMonth
	if firstDay.Before(since) {
		since = firstDay
	}
	rows, err := s.queries.ListResumeViewDays(r.Context(), store.ListResumeViewDaysParams{ResumeID: id, Since: viewcount.Date(since)})
	if err != nil {
		s.writeInternal(w, r)
		return
	}
	signals, err := s.queries.ListResumeShareSignals(r.Context(), store.ListResumeShareSignalsParams{ResumeID: id, Since: viewcount.Date(firstDay)})
	if err != nil {
		s.writeInternal(w, r)
		return
	}
	response := detailResponse{
		ID: resume.ID, Title: resume.Title, Slug: resume.Slug, Live: resume.Live,
		Today:    today.Format(dayLayout),
		Days:     denseDays(rows, firstDay),
		Months:   months(rows, firstMonth),
		Previews: make([]viewPreviews, 0, len(signals)),
	}
	for _, signal := range signals {
		response.Previews = append(response.Previews, viewPreviews{Platform: signal.Platform, Fetches: signal.Fetches})
	}
	api.WriteData(w, http.StatusOK, response)
}

// denseDays returns one entry per day from first for 90 days, zero where no
// row exists.
func denseDays(rows []store.ListResumeViewDaysRow, first time.Time) []viewDay {
	byDay := make(map[string]store.ListResumeViewDaysRow, len(rows))
	for _, row := range rows {
		byDay[row.Day.Time.Format(dayLayout)] = row
	}
	days := make([]viewDay, 0, detailDays)
	for i := range detailDays {
		date := first.AddDate(0, 0, i).Format(dayLayout)
		row := byDay[date]
		days = append(days, viewDay{
			Date: date, Real: row.Counted, Bot: row.Bot, Datacenter: row.Datacenter,
			Anomaly: row.Anomaly, Invalid: row.Invalid, Crawler: row.Crawler,
		})
	}
	return days
}

// months returns 12 monthly totals from first, oldest first.
func months(rows []store.ListResumeViewDaysRow, first time.Time) []viewMonth {
	out := make([]viewMonth, detailMonth)
	index := make(map[string]int, detailMonth)
	for i := range out {
		month := first.AddDate(0, i, 0).Format(monthLayout)
		out[i].Month = month
		index[month] = i
	}
	for _, row := range rows {
		i, ok := index[row.Day.Time.Format(monthLayout)]
		if !ok {
			continue
		}
		out[i].Real += int64(row.Counted)
		out[i].Filtered += int64(row.Bot) + int64(row.Datacenter) + int64(row.Anomaly) + int64(row.Invalid) + int64(row.Crawler)
	}
	return out
}

func writeResumeNotFound(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, "resume_not_found", "resume not found")
}

// writeInternal logs only fixed fields, never database error text.
func (s *Service) writeInternal(w http.ResponseWriter, r *http.Request) {
	if s.logger != nil {
		s.logger.ErrorContext(r.Context(), "viewapi: owner read failed", "request_id", api.RequestIDFromContext(r.Context()))
	}
	api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}
