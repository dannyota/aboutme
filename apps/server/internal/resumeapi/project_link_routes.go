package resumeapi

// Project link and refresh routes: each inserts or rewrites one prefilled
// project entry through the entry-write chain (docs/design/projects.md
// "Selection").

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

// linkedSectionKey is the key of the project section a link creates when the
// resume has none.
const linkedSectionKey = "projects"

func projectLinkRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPost, Pattern: apiResumePath + "/{id}/project-links", Operation: "linkResumeProject", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleLinkResumeProject},
		{Method: http.MethodPost, Pattern: apiResumePath + "/{id}/entries/{entryId}/project-refresh", Operation: "refreshResumeProjectEntry", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleRefreshResumeProjectEntry},
	}
}

// projectLinkPrepared is a link or a refresh. A link names ProjectID and a
// refresh names EntryID; the other is zero.
type projectLinkPrepared struct {
	// ResumeID is the resume the entry belongs to.
	ResumeID uuid.UUID
	// ProjectID is the project a link adds.
	ProjectID uuid.UUID
	// EntryID is the linked entry a refresh rewrites.
	EntryID string
}

func decodeProjectLink(r *http.Request) (boundedInput, error) {
	resumeID, pathErr := parseResumePathID(r)
	if pathErr != nil {
		return boundedInput{}, pathErr
	}
	var body struct {
		// ProjectID is the canonical UUID of an owned library project.
		ProjectID *string `json:"projectId"`
	}
	input, err := decodeJSONBody(r, &body)
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
	input.Value = projectLinkPrepared{ResumeID: resumeID, ProjectID: projectID}
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
	if prepared.EntryID != "" {
		return []string{"resume_id", prepared.ResumeID.String(), "entry_id", prepared.EntryID}, nil
	}
	return []string{"resume_id", prepared.ResumeID.String(), "project_id", prepared.ProjectID.String()}, nil
}

type projectLinkOperation struct{ service *Service }

// Run reads the owned project under the owner lock, writes the prefilled
// entry, and saves the document at the next revision.
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

// linkedDocument returns the current-version document with the linked entry
// inserted or refreshed. It works on the current version whatever the wire
// version, because an older client cannot express the link it asks for.
func linkedDocument(ctx context.Context, qtx *store.Queries, current resume.Resume, input projectLinkPrepared) (json.RawMessage, error) {
	raw, err := resume.AssembleCanonical(current.Doc)
	if err != nil {
		return nil, err
	}
	projectID := input.ProjectID
	var entry schema.ProjectEntry
	var sectionKey string
	if input.EntryID != "" {
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

// firstProjectSection returns the first project section in layout order.
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
	})
}
