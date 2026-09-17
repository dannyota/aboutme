package resumeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func resumeRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodGet, Pattern: apiResumePath, Operation: "listResumes", AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleListResumes},
		{Method: http.MethodPost, Pattern: apiResumePath, Operation: "createResume", Mutation: true, OperationKind: operationCreate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleCreateResume},
		{Method: http.MethodGet, Pattern: apiResumePath + "/{id}", Operation: "getResume", AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleGetResume},
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}", Operation: "updateResumeMetadata", Mutation: true, OperationKind: operationMetadata, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumeMetadata},
		{Method: http.MethodDelete, Pattern: apiResumePath + "/{id}", Operation: "deleteResume", Mutation: true, OperationKind: operationDelete, AcceptsWireVersion: true, Handler: (*Service).handleDeleteResume},
		{Method: http.MethodPost, Pattern: apiResumePath + "/{id}/publish", Operation: "publishResume", Mutation: true, OperationKind: operationPublish, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handlePublishResume},
	}
}

type resumePoolReader interface {
	Get(context.Context, uuid.UUID, uuid.UUID) (resume.Resume, error)
	List(context.Context, uuid.UUID) ([]resume.Resume, error)
}

type resumeMediaDeletionQueue interface {
	EnqueueMediaDeletionTx(context.Context, *store.Queries, uuid.UUID, string) error
}

type resumeSummaryJSON struct {
	ID              uuid.UUID `json:"id"`
	Title           string    `json:"title"`
	Lng             string    `json:"lng"`
	Revision        string    `json:"revision"`
	Live            bool      `json:"live"`
	Slug            *string   `json:"slug"`
	DownloadEnabled bool      `json:"downloadEnabled"`
	SEOGeoEnabled   bool      `json:"seoGeoEnabled"`
	SchemaVersion   int32     `json:"schemaVersion"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type resumeJSON struct {
	resumeSummaryJSON
	Document json.RawMessage `json:"document"`
}

func (s *Service) handleListResumes(w http.ResponseWriter, r *http.Request) {
	version, versionErr := resolveWireVersion(r.Header, s.acceptedVersions)
	if versionErr != nil {
		writeResumeError(w, versionErr)
		return
	}
	userID, userErr := requestUserID(r)
	if userErr != nil {
		writeResumeError(w, userErr)
		return
	}
	reader, ok := s.resumes.(resumePoolReader)
	if !ok {
		writeResumeError(w, internalClientError())
		return
	}
	rows, err := reader.List(r.Context(), userID)
	if err != nil {
		writeResumeError(w, mapMutationError(err))
		return
	}
	data := make([]resumeSummaryJSON, len(rows))
	for i := range rows {
		data[i] = makeResumeSummary(rows[i], version)
	}
	body, err := json.Marshal(struct {
		Data []resumeSummaryJSON `json:"data"`
	}{Data: data})
	if err != nil {
		writeResumeError(w, internalClientError())
		return
	}
	writeStoredResponse(w, resume.StoredResponse{
		Status: http.StatusOK, Body: body,
		Headers: map[string]string{wireVersionHeader: wireVersionString(version)},
	})
}
func (s *Service) handleGetResume(w http.ResponseWriter, r *http.Request) {
	version, versionErr := resolveWireVersion(r.Header, s.acceptedVersions)
	if versionErr != nil {
		writeResumeError(w, versionErr)
		return
	}
	id, err := parseResumePathID(r)
	if err != nil {
		writeResumeError(w, err)
		return
	}
	userID, userErr := requestUserID(r)
	if userErr != nil {
		writeResumeError(w, userErr)
		return
	}
	reader, ok := s.resumes.(resumePoolReader)
	if !ok {
		writeResumeError(w, internalClientError())
		return
	}
	row, getErr := reader.Get(r.Context(), userID, id)
	if getErr != nil {
		writeResumeError(w, mapMutationError(getErr))
		return
	}
	response, responseErr := s.makeResumeResponse(row, row.Doc, version, http.StatusOK, false)
	if responseErr != nil {
		writeResumeError(w, internalClientError())
		return
	}
	writeStoredResponse(w, response)
}
func parseResumePathID(r *http.Request) (uuid.UUID, *clientError) {
	raw := r.PathValue("id")
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.String() != raw {
		return uuid.Nil, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "resume id must be one canonical UUID"}
	}
	return id, nil
}
func makeResumeSummary(row resume.Resume, version int32) resumeSummaryJSON {
	return resumeSummaryJSON{
		ID: row.ID, Title: row.Title, Lng: projectResumeLanguage(row.Lng),
		Revision: strconv.FormatInt(row.Revision, 10), Live: row.Live, Slug: row.Slug,
		DownloadEnabled: row.DownloadEnabled, SEOGeoEnabled: row.SEOGeoEnabled,
		SchemaVersion: version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func (s *Service) resumeResponseBuilder(status int, location bool) mutationResponseBuilder {
	return func(row resume.Resume, doc schema.Resume, version int32) (resume.StoredResponse, error) {
		return s.makeResumeResponse(row, doc, version, status, location)
	}
}

func (s *Service) makeResumeResponse(row resume.Resume, doc schema.Resume, version int32, status int,
	location bool,
) (resume.StoredResponse, error) {
	if s.projector == nil {
		return resume.StoredResponse{}, fmt.Errorf("resumeapi: no document projector")
	}
	canonical, err := resume.AssembleCanonical(doc)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	wire, err := s.projector.EmitWire(canonical, version)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	body, err := json.Marshal(struct {
		Data resumeJSON `json:"data"`
	}{Data: resumeJSON{resumeSummaryJSON: makeResumeSummary(row, version), Document: wire}})
	if err != nil {
		return resume.StoredResponse{}, err
	}
	headers := map[string]string{
		"ETag": fmt.Sprintf(`"r%d"`, row.Revision), wireVersionHeader: wireVersionString(version),
	}
	if location {
		headers["Location"] = apiResumePath + "/" + row.ID.String()
	}
	return resume.StoredResponse{Status: status, Body: body, Headers: headers}, nil
}
