package resumeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/oauthsrv"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

const linkedSectionKey = "projects"

func projectLinkAgentScopes() []oauthsrv.Scope {
	return []oauthsrv.Scope{oauthsrv.ScopeResumesWrite, oauthsrv.ScopeProjectsRead}
}

func projectLinkRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPost, Pattern: apiResumePath + "/{id}/project-links", Operation: "linkResumeProject", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleLinkResumeProject},
		{Method: http.MethodPost, Pattern: apiResumePath + "/{id}/entries/{entryId}/project-refresh", Operation: "refreshResumeProjectEntry", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleRefreshResumeProjectEntry},
	}
}

type projectLinkPrepared struct {
	ResumeID  uuid.UUID
	ProjectID uuid.UUID
	EntryID   string
}

func (p projectLinkPrepared) refresh() bool { return p.ProjectID == uuid.Nil }

func decodeProjectLink(r *http.Request) (boundedInput, error) {
	resumeID, pathErr := parseResumePathID(r)
	if pathErr != nil {
		return boundedInput{}, pathErr
	}
	var body struct {
		ProjectID *string `json:"projectId"`
		EntryID   *string `json:"entryId"`
	}
	shape := api.JSONShapeOf(reflect.TypeOf(&body))
	shape.Fields["entryId"] = &api.JSONShape{Kind: api.JSONString}
	input, err := decodeJSONBodyAs(r, &body, shape)
	if err != nil {
		return boundedInput{}, err
	}
	if body.ProjectID == nil {
		return boundedInput{}, requestInvalid("projectId is required")
	}
	projectID, err := uuid.Parse(*body.ProjectID)
	if err != nil || projectID == uuid.Nil || projectID.String() != *body.ProjectID {
		return boundedInput{}, requestInvalid("projectId must be one canonical UUID")
	}
	prepared := projectLinkPrepared{ResumeID: resumeID, ProjectID: projectID}
	if body.EntryID != nil {
		entryID, entryErr := uuid.Parse(*body.EntryID)
		if entryErr != nil || entryID.String() != *body.EntryID {
			return boundedInput{}, requestInvalid("entryId must be one canonical UUID")
		}
		prepared.EntryID = entryID.String()
	}
	input.Value = prepared
	return input, nil
}

func decodeProjectRefresh(r *http.Request) (boundedInput, error) {
	resumeID, pathErr := parseResumePathID(r)
	if pathErr != nil {
		return boundedInput{}, pathErr
	}
	entryID, err := uuid.Parse(r.PathValue("entryId"))
	if err != nil || entryID.String() != r.PathValue("entryId") {
		return boundedInput{}, requestInvalid("entry id must be one canonical UUID")
	}
	input, err := decodeDeleteBody(r)
	if err != nil {
		return boundedInput{}, requestInvalid("project refresh takes no body")
	}
	input.Value = projectLinkPrepared{ResumeID: resumeID, EntryID: entryID.String()}
	return input, nil
}

func projectLinkTargets(input boundedInput) ([]string, error) {
	prepared, ok := input.Value.(projectLinkPrepared)
	if !ok {
		return nil, errors.New("project link input has the wrong type")
	}
	if prepared.refresh() {
		return []string{"resume_id", prepared.ResumeID.String(), "entry_id", prepared.EntryID}, nil
	}
	return []string{"resume_id", prepared.ResumeID.String(), "project_id", prepared.ProjectID.String()}, nil
}

type projectLinkOperation struct{ service *Service }

func (op projectLinkOperation) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext,
	prepared preparedInput,
) (mutationRunResult, error) {
	input, ok := prepared.Value.(projectLinkPrepared)
	if !ok || mutation.ExpectedRevision == nil {
		return mutationRunResult{}, fmt.Errorf("resumeapi: project link operation received the wrong prepared input")
	}
	current, err := op.service.currentMutationResume(ctx, qtx, mutation, input.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	raw, err := linkedDocument(ctx, qtx, current, input)
	if err != nil {
		return mutationRunResult{}, err
	}
	doc, err := strictDecodeCurrentDocument(raw)
	if err != nil {
		return mutationRunResult{}, err
	}
	if doc, err = op.service.prepareDocumentForPersistence(doc); err != nil {
		return mutationRunResult{}, err
	}
	if err = requireLinkAuthority(mutation, &current.Doc, doc); err != nil {
		return mutationRunResult{}, err
	}
	updated, err := op.service.resumes.SaveDocumentTx(ctx, qtx, mutation.UserID, input.ResumeID, doc, *mutation.ExpectedRevision)
	if err != nil {
		return mutationRunResult{}, err
	}
	if err = requireNextRevision(updated, *mutation.ExpectedRevision); err != nil {
		return mutationRunResult{}, err
	}
	response, err := op.service.makeResumeResponse(updated, updated.Doc, mutation.WireVersion, http.StatusOK, false)
	return mutationRunResult{Response: response}, err
}

func linkedDocument(ctx context.Context, qtx *store.Queries, current resume.Resume, input projectLinkPrepared) (json.RawMessage, error) {
	raw, err := resume.AssembleCanonical(current.Doc)
	if err != nil {
		return nil, err
	}
	if !input.refresh() && input.EntryID != "" {
		return linkExistingEntry(raw, input.EntryID, input.ProjectID)
	}
	projectID := input.ProjectID
	var entry schema.ProjectEntry
	var sectionKey string
	if input.refresh() {
		var found bool
		sectionKey, entry, found = findProjectEntry(current.Doc, input.EntryID)
		if !found {
			return nil, resume.ErrNotFound
		}
		if entry.ProjectID == nil {
			return nil, resume.ErrProjectNotFound
		}
		if projectID, err = uuid.Parse(*entry.ProjectID); err != nil {
			return nil, resume.ErrProjectNotFound
		}
	} else {
		entryID, idErr := uuid.NewRandom()
		if idErr != nil {
			return nil, fmt.Errorf("resumeapi: new project entry id: %w", idErr)
		}
		link := projectID.String()
		entry = schema.ProjectEntry{ID: entryID.String(), ProjectID: &link}
		sectionKey = firstProjectSection(current.Doc)
		if sectionKey == "" {
			sectionKey = linkedSectionKey
			raw, err = applyStructureCommands(raw, []structureCommand{{
				Op: "createSection", Key: linkedSectionKey, SectionType: string(schema.Project), Column: "main",
				Index: len(current.Doc.Customization.Layout.Sections.Main), HasIndex: true,
			}})
			if err != nil {
				return nil, err
			}
		}
	}
	library, err := project.NewStore().GetTx(ctx, qtx, current.UserID, projectID)
	if errors.Is(err, project.ErrNotFound) {
		return nil, resume.ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	prefillProjectEntry(&entry, library, current.Lng)
	encoded, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("resumeapi: encode project entry: %w", err)
	}
	return applyEntryUpsert(raw, sectionKey, encoded)
}

func firstProjectSection(doc schema.Resume) string {
	layout := doc.Customization.Layout.Sections
	for _, key := range append(append([]string(nil), layout.Main...), layout.Sidebar...) {
		if doc.Content[key].SectionType == schema.Project {
			return key
		}
	}
	return ""
}

func findProjectEntry(doc schema.Resume, entryID string) (string, schema.ProjectEntry, bool) {
	for key, section := range doc.Content {
		for _, entry := range section.ProjectEntries {
			if entry.ID == entryID {
				return key, entry, true
			}
		}
	}
	return "", schema.ProjectEntry{}, false
}

func (s *Service) prepareProjectLink(_ context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
	prepared, ok := input.Value.(projectLinkPrepared)
	if !ok {
		return preparedInput{}, errors.New("project link input has the wrong type")
	}
	return preparedInput{Input: input, Value: prepared}, nil
}

func (s *Service) handleLinkResumeProject(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, mutationSpec{
		RegisteredOperation: "linkResumeProject",
		RequireMatch:        true,
		Decode:              decodeProjectLink,
		CanonicalTargets:    projectLinkTargets,
		Prepare:             s.prepareProjectLink,
		Run:                 projectLinkOperation{service: s},
		Transition:          s.nonDrainingTransition,
		AgentRequired:       projectLinkAgentScopes(),
	})
}

func (s *Service) handleRefreshResumeProjectEntry(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, mutationSpec{
		RegisteredOperation: "refreshResumeProjectEntry",
		RequireMatch:        true,
		Decode:              decodeProjectRefresh,
		CanonicalTargets:    projectLinkTargets,
		Prepare:             s.prepareProjectLink,
		Run:                 projectLinkOperation{service: s},
		Transition:          s.nonDrainingTransition,
		AgentRequired:       projectLinkAgentScopes(),
	})
}
