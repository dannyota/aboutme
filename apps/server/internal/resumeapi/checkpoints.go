package resumeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	schema "danny.vn/aboutme/packages/schema/gen/go"
	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

const checkpointCollectionPath = apiResumePath + "/{id}/checkpoints"
const checkpointItemPath = checkpointCollectionPath + "/{checkpointId}"

func checkpointRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodGet, Pattern: checkpointCollectionPath, Operation: "listResumeCheckpoints", Handler: (*Service).handleListCheckpoints},
		{Method: http.MethodGet, Pattern: checkpointItemPath, Operation: "getResumeCheckpoint", Handler: (*Service).handleGetCheckpoint},
		{Method: http.MethodPost, Pattern: checkpointItemPath + "/restore", Operation: "restoreResumeCheckpoint", Mutation: true,
			OperationKind: operationCheckpointRestore, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleRestoreCheckpoint},
		{Method: http.MethodDelete, Pattern: checkpointItemPath, Operation: "deleteResumeCheckpoint", Mutation: true,
			OperationKind: operationCheckpointDelete, Handler: (*Service).handleDeleteCheckpoint},
		{Method: http.MethodDelete, Pattern: checkpointCollectionPath, Operation: "clearResumeCheckpoints", Mutation: true,
			OperationKind: operationCheckpointDelete, Handler: (*Service).handleClearCheckpoints},
	}
}

type checkpointReader interface {
	ListCheckpoints(context.Context, store.ListResumeCheckpointsParams) ([]resume.CheckpointSummary, error)
	GetCheckpoint(context.Context, store.GetResumeCheckpointParams) (resume.CheckpointDetail, error)
}

type checkpointTarget struct {
	ResumeID uuid.UUID
	ID       uuid.UUID
}

func readCheckpointTarget(r *http.Request, item bool) (checkpointTarget, *clientError) {
	id, err := parseResumePathID(r)
	if err != nil {
		return checkpointTarget{}, err
	}
	if !item {
		return checkpointTarget{ResumeID: id}, nil
	}
	raw := r.PathValue("checkpointId")
	checkpointID, parseErr := uuid.Parse(raw)
	if parseErr != nil || checkpointID == uuid.Nil || checkpointID.String() != raw {
		return checkpointTarget{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "checkpoint id must be one canonical UUID"}
	}
	return checkpointTarget{ResumeID: id, ID: checkpointID}, nil
}

func checkpointOwnerOnly(w http.ResponseWriter, r *http.Request) bool {
	if _, agent := agentPrincipalFromContext(r.Context()); agent {
		writeResumeError(w, mapMutationError(resume.ErrNotFound))
		return false
	}
	return true
}

func (s *Service) checkpointCutoff() time.Time {
	return s.clock().UTC().Add(-resume.CheckpointRetention)
}

func (s *Service) checkpointLookup(userID uuid.UUID, target checkpointTarget) store.GetResumeCheckpointParams {
	return store.GetResumeCheckpointParams{UserID: userID, ResumeID: target.ResumeID, CheckpointID: target.ID,
		Cutoff: s.checkpointCutoff(), AcceptedVersions: s.acceptedVersions}
}

func (s *Service) handleListCheckpoints(w http.ResponseWriter, r *http.Request) {
	s.readCheckpoint(w, r, false)
}

func (s *Service) handleGetCheckpoint(w http.ResponseWriter, r *http.Request) {
	s.readCheckpoint(w, r, true)
}

func (s *Service) readCheckpoint(w http.ResponseWriter, r *http.Request, item bool) {
	if !checkpointOwnerOnly(w, r) {
		return
	}
	userID, userErr := requestUserID(r)
	if userErr != nil {
		writeResumeError(w, userErr)
		return
	}
	target, pathErr := readCheckpointTarget(r, item)
	if pathErr != nil {
		writeResumeError(w, pathErr)
		return
	}
	reader, ok := s.resumes.(checkpointReader)
	if !ok {
		writeResumeError(w, internalClientError())
		return
	}
	var data any
	var err error
	if item {
		data, err = reader.GetCheckpoint(r.Context(), s.checkpointLookup(userID, target))
	} else {
		data, err = reader.ListCheckpoints(r.Context(), store.ListResumeCheckpointsParams{
			UserID: userID, ResumeID: target.ResumeID, Cutoff: s.checkpointCutoff(), AcceptedVersions: s.acceptedVersions,
		})
	}
	if err != nil {
		writeResumeError(w, mapMutationError(err))
		return
	}
	body, err := json.Marshal(struct {
		Data any `json:"data"`
	}{Data: data})
	if err != nil {
		writeResumeError(w, internalClientError())
		return
	}
	writeStoredResponse(w, resume.StoredResponse{Status: http.StatusOK, Body: body})
}

func (s *Service) handleRestoreCheckpoint(w http.ResponseWriter, r *http.Request) {
	s.mutateCheckpoint(w, r, "restoreResumeCheckpoint", operationCheckpointRestore, true)
}

func (s *Service) handleDeleteCheckpoint(w http.ResponseWriter, r *http.Request) {
	s.mutateCheckpoint(w, r, "deleteResumeCheckpoint", operationCheckpointDelete, true)
}

func (s *Service) handleClearCheckpoints(w http.ResponseWriter, r *http.Request) {
	s.mutateCheckpoint(w, r, "clearResumeCheckpoints", operationCheckpointDelete, false)
}

func (s *Service) mutateCheckpoint(w http.ResponseWriter, r *http.Request, name string, kind operationKind, item bool) {
	if !checkpointOwnerOnly(w, r) {
		return
	}
	spec := mutationSpec{
		RegisteredOperation: name, RequireMatch: kind == operationCheckpointRestore, Versionless: kind != operationCheckpointRestore,
		Decode: func(r *http.Request) (boundedInput, error) {
			target, err := readCheckpointTarget(r, item)
			if err != nil {
				return boundedInput{}, err
			}
			input, decodeErr := decodeEmptyBody(r)
			input.Value = target
			return input, decodeErr
		},
		CanonicalTargets: func(input boundedInput) ([]string, error) {
			target, ok := input.Value.(checkpointTarget)
			if !ok {
				return nil, internalClientError()
			}
			fields := []string{"resume_id", target.ResumeID.String()}
			if item {
				fields = append(fields, "checkpoint_id", target.ID.String())
			}
			return fields, nil
		},
		Run: kind.build(s),
	}
	if kind == operationCheckpointRestore {
		spec.Transition = s.nonDrainingTransition
	}
	s.executeMutation(w, r, spec)
}

type checkpointDeleteMutation struct{ service *Service }

func (op checkpointDeleteMutation) Run(ctx context.Context, q *store.Queries, m mutationContext, prepared preparedInput) (mutationRunResult, error) {
	target, ok := prepared.Value.(checkpointTarget)
	if !ok {
		return mutationRunResult{}, internalClientError()
	}
	if target.ID == uuid.Nil {
		if err := resume.RequireCheckpointResumeTx(ctx, q, m.UserID, target.ResumeID); err != nil {
			return mutationRunResult{}, err
		}
		if err := q.ClearResumeCheckpoints(ctx, store.ClearResumeCheckpointsParams{UserID: m.UserID, ResumeID: target.ResumeID}); err != nil {
			return mutationRunResult{}, fmt.Errorf("resumeapi: clear checkpoints: %w", err)
		}
	} else {
		n, err := q.DeleteResumeCheckpoint(ctx, store.DeleteResumeCheckpointParams(op.service.checkpointLookup(m.UserID, target)))
		if err != nil {
			return mutationRunResult{}, fmt.Errorf("resumeapi: delete checkpoint: %w", err)
		}
		if n == 0 {
			return mutationRunResult{}, resume.ErrNotFound
		}
	}
	return mutationRunResult{Response: resume.StoredResponse{Status: http.StatusNoContent}}, nil
}

type checkpointRestoreMutation struct{ service *Service }

func (op checkpointRestoreMutation) Run(ctx context.Context, q *store.Queries, m mutationContext, prepared preparedInput) (mutationRunResult, error) {
	target, ok := prepared.Value.(checkpointTarget)
	if !ok || m.ExpectedRevision == nil {
		return mutationRunResult{}, internalClientError()
	}
	s := op.service
	current, err := s.currentMutationResume(ctx, q, m, target.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	row, err := resume.ReadCheckpointTx(ctx, q, s.checkpointLookup(m.UserID, target))
	if err != nil {
		return mutationRunResult{}, err
	}
	checkpoint, err := resume.ProjectCheckpoint(s.projector, row)
	if err != nil {
		return mutationRunResult{}, documentInvalid("document", "checkpoint cannot be converted to the current schema")
	}
	doc := checkpoint.ResumeDocument()
	doc.PersonalDetails.Photo = current.Doc.PersonalDetails.Photo
	if err = unlinkMissingCheckpointProjects(ctx, q, m.UserID, &doc); err != nil {
		return mutationRunResult{}, err
	}
	if doc, err = s.prepareDocumentForPersistence(doc); err != nil {
		return mutationRunResult{}, err
	}
	if _, err = s.resumes.SaveMetadataAndDocumentTx(ctx, q, m.UserID, target.ResumeID, checkpoint.Title, checkpoint.Lng, doc,
		*m.ExpectedRevision, resume.CheckpointWrite{Cause: "restore", Force: true}); err != nil {
		return mutationRunResult{}, err
	}
	saved, err := s.resumes.GetTx(ctx, q, m.UserID, target.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	response, err := s.makeResumeResponse(saved, saved.Doc, m.WireVersion, http.StatusOK, false)
	return mutationRunResult{Response: response}, err
}

func unlinkMissingCheckpointProjects(ctx context.Context, q *store.Queries, owner uuid.UUID, doc *schema.Resume) error {
	links, err := resume.ProjectLinks(*doc)
	if err != nil {
		return documentInvalid("document.content", "invalid checkpoint project link")
	}
	if len(links) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(links))
	for _, id := range links {
		ids = append(ids, id)
	}
	owned, err := q.ListCheckpointProjectIDs(ctx, store.ListCheckpointProjectIDsParams{UserID: owner, Ids: ids})
	if err != nil {
		return fmt.Errorf("resumeapi: read restore project ownership: %w", err)
	}
	for _, id := range ids {
		if !slices.Contains(owned, id) {
			resume.UnlinkProject(doc, id)
		}
	}
	return nil
}
