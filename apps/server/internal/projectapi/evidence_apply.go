package projectapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

type evidenceResult struct {
	Evidence        []project.Evidence `json:"evidence"`
	ProjectRevision int64              `json:"projectRevision,string"`
	ResumeRevisions []changedResume    `json:"resumeRevisions"`
}

func strictBody(body []byte, shape *api.JSONShape, target any) error {
	if err := api.CheckJSONShape(body, shape); err != nil {
		return project.ErrEvidenceRequest
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return project.ErrEvidenceRequest
	}
	return nil
}

func decodeAttach(body []byte) (project.EvidenceRequest, error) {
	text := &api.JSONShape{Kind: api.JSONString}
	shape := &api.JSONShape{Fields: map[string]*api.JSONShape{"kind": text, "link": text, "candidateId": text}, Required: []string{"kind"}}
	var input struct {
		Kind        string  `json:"kind"`
		Link        *string `json:"link"`
		CandidateID *string `json:"candidateId"`
	}
	if err := strictBody(body, shape, &input); err != nil {
		return project.EvidenceRequest{}, err
	}
	if !project.ValidEvidenceKind(input.Kind) || (input.Link == nil) == (input.CandidateID == nil) {
		return project.EvidenceRequest{}, project.ErrEvidenceRequest
	}
	request := project.EvidenceRequest{Kind: input.Kind}
	if input.Link != nil {
		request.Link = *input.Link
		return request, nil
	}
	if *input.CandidateID == "" {
		return project.EvidenceRequest{}, project.ErrEvidenceRequest
	}
	request.CandidateID = *input.CandidateID
	return request, nil
}

func decodeOrder(body []byte) ([]uuid.UUID, error) {
	shape := &api.JSONShape{Fields: map[string]*api.JSONShape{"order": {Items: &api.JSONShape{Kind: api.JSONString}}}, Required: []string{"order"}}
	var input struct {
		Order []uuid.UUID `json:"order"`
	}
	if err := strictBody(body, shape, &input); err != nil || input.Order == nil || len(input.Order) > project.MaxEvidence {
		return nil, project.ErrEvidenceRequest
	}
	seen := make(map[uuid.UUID]bool, len(input.Order))
	for _, id := range input.Order {
		if seen[id] {
			return nil, project.ErrEvidenceRequest
		}
		seen[id] = true
	}
	return input.Order, nil
}

func (s *Service) applyEvidence(ctx context.Context, q *store.Queries, call *evidenceCall, changed []resumeapi.ChangedResume) (resume.StoredResponse, error) {
	var err error
	switch call.route {
	case evidenceAttach:
		err = s.attachTx(ctx, q, call)
	case evidenceRecheck:
		err = s.recheckTx(ctx, q, call)
	case evidenceOrder:
		err = reorderTx(ctx, q, call)
	default:
		err = removeTx(ctx, q, call)
	}
	if err != nil {
		return resume.StoredResponse{}, err
	}
	advanced, err := q.AdvanceProjectRevision(ctx, store.AdvanceProjectRevisionParams{
		ID: call.project, UserID: call.owner, ExpectedRevision: call.revision, UpdatedAt: s.now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return resume.StoredResponse{}, project.ErrNotFound
	}
	if err != nil {
		return resume.StoredResponse{}, fmt.Errorf("projectapi: advance project revision: %w", err)
	}
	items, err := s.projects.EvidenceTx(ctx, q, call.owner, call.project)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	result := evidenceResult{Evidence: items, ProjectRevision: advanced.Revision, ResumeRevisions: changedResumes(changed)}
	return stored(200, result, map[string]string{"ETag": revisionTag(advanced.Revision)})
}

func (s *Service) attachTx(ctx context.Context, q *store.Queries, call *evidenceCall) error {
	fields := call.check.Fields
	_, err := q.FindProjectEvidenceItem(ctx, store.FindProjectEvidenceItemParams{ProjectID: call.project, Kind: call.check.Kind, ItemID: fields.ItemID})
	if err == nil {
		return project.ErrEvidenceDuplicate
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("projectapi: find evidence: %w", err)
	}
	count, err := q.CountProjectEvidence(ctx, call.project)
	if err != nil {
		return fmt.Errorf("projectapi: count evidence: %w", err)
	}
	if count >= project.MaxEvidence {
		return project.ErrEvidenceLimit
	}
	if err = s.projects.EvidenceRoomTx(ctx, q, call.owner, call.project, int(count)+1); err != nil {
		return err
	}
	_, err = q.InsertProjectEvidence(ctx, store.InsertProjectEvidenceParams{
		ProjectID: call.project, Kind: call.check.Kind, RepositoryID: fields.RepositoryID, ItemID: fields.ItemID,
		PullNumber: fields.PullNumber, ReleaseTag: fields.ReleaseTag, Url: fields.URL, RepositoryName: fields.RepositoryName,
		Title: fields.Title, ItemDate: fields.ItemDate, CheckedAt: s.now(), CheckedBinding: call.binding, RuleMet: true,
	})
	if err != nil {
		return fmt.Errorf("projectapi: insert evidence: %w", err)
	}
	return nil
}

func (s *Service) recheckTx(ctx context.Context, q *store.Queries, call *evidenceCall) error {
	fields := call.check.Fields
	_, err := q.RewriteProjectEvidence(ctx, store.RewriteProjectEvidenceParams{
		PullNumber: fields.PullNumber, ReleaseTag: fields.ReleaseTag, Url: fields.URL, RepositoryName: fields.RepositoryName,
		Title: fields.Title, ItemDate: fields.ItemDate, CheckedAt: s.now(), CheckedBinding: call.binding, RuleMet: fields.RuleMet,
		FlagReason: fields.FlagReason, ID: call.item, ProjectID: call.project,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return project.ErrEvidenceNotFound
	}
	if err != nil {
		return fmt.Errorf("projectapi: rewrite evidence: %w", err)
	}
	return nil
}

func reorderTx(ctx context.Context, q *store.Queries, call *evidenceCall) error {
	rows, err := q.ListProjectEvidence(ctx, store.ListProjectEvidenceParams{UserID: call.owner, ProjectID: call.project})
	if err != nil {
		return fmt.Errorf("projectapi: list evidence: %w", err)
	}
	named := make(map[uuid.UUID]bool, len(call.order))
	for _, id := range call.order {
		named[id] = true
	}
	if len(rows) != len(call.order) {
		return project.ErrEvidenceRequest
	}
	for _, row := range rows {
		if !named[row.ID] {
			return project.ErrEvidenceRequest
		}
	}
	n, err := q.ReorderProjectEvidence(ctx, store.ReorderProjectEvidenceParams{Ids: call.order, ProjectID: call.project})
	if err != nil {
		return fmt.Errorf("projectapi: reorder evidence: %w", err)
	}
	if n != int64(len(rows)) {
		return project.ErrEvidenceRequest
	}
	return nil
}

func removeTx(ctx context.Context, q *store.Queries, call *evidenceCall) error {
	n, err := q.DeleteProjectEvidence(ctx, store.DeleteProjectEvidenceParams{ID: call.item, ProjectID: call.project})
	if err != nil {
		return fmt.Errorf("projectapi: delete evidence: %w", err)
	}
	if n != 1 {
		return project.ErrEvidenceNotFound
	}
	return nil
}
