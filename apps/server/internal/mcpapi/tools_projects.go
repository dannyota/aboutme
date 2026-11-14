package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"danny.vn/aboutme/apps/server/internal/oauthsrv"
	"danny.vn/aboutme/apps/server/internal/projectapi"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
)

type ProjectExecutor interface {
	ExecuteAgent(context.Context, resumeapi.AgentPrincipal, projectapi.AgentCall) resumeapi.AgentResponse
}

type projectIDInput struct {
	ProjectID string `json:"project_id" jsonschema:"project UUID"`
}

type createProjectInput struct {
	IdempotencyKey string         `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	Document       map[string]any `json:"document" jsonschema:"complete project document with the REST field names: dates, repositoryUrl, demoUrl, technologies, defaultLanguage and text"`
	Slug           *string        `json:"slug,omitempty" jsonschema:"project slug; proposed from the name when omitted"`
}

type updateProjectInput struct {
	IdempotencyKey string         `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	ProjectID      string         `json:"project_id" jsonschema:"project UUID"`
	Revision       string         `json:"revision" jsonschema:"current decimal project revision"`
	Slug           string         `json:"slug" jsonschema:"project slug; a new value renames the project"`
	Document       map[string]any `json:"document" jsonschema:"complete replacement project document; an omitted field is cleared"`
}

type deleteProjectInput struct {
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	ProjectID      string `json:"project_id" jsonschema:"project UUID"`
	Revision       string `json:"revision" jsonschema:"current decimal project revision"`
}

type linkProjectInput struct {
	IdempotencyKey string  `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	ResumeID       string  `json:"resume_id" jsonschema:"resume UUID"`
	Revision       string  `json:"revision" jsonschema:"current decimal resume revision"`
	ProjectID      string  `json:"project_id" jsonschema:"library project UUID"`
	EntryID        *string `json:"entry_id,omitempty" jsonschema:"existing unlinked project entry UUID to link instead of inserting a new entry"`
}

type refreshProjectEntryInput struct {
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	ResumeID       string `json:"resume_id" jsonschema:"resume UUID"`
	Revision       string `json:"revision" jsonschema:"current decimal resume revision"`
	EntryID        string `json:"entry_id" jsonschema:"linked project entry UUID"`
}

type listProjectsOutput struct {
	Projects []map[string]any `json:"projects"`
}

type projectOutput struct {
	Project map[string]any `json:"project"`
}

type projectMutationOutput struct {
	Revision string         `json:"revision"`
	Project  map[string]any `json:"project"`
}

type changedResumeOutput struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

type updateProjectOutput struct {
	Revision       string                `json:"revision"`
	Project        map[string]any        `json:"project"`
	ChangedResumes []changedResumeOutput `json:"changed_resumes"`
}

type deleteProjectOutput struct {
	ID             string                `json:"id"`
	Deleted        bool                  `json:"deleted"`
	ChangedResumes []changedResumeOutput `json:"changed_resumes"`
}

var (
	projectsReadScopes   = []oauthsrv.Scope{oauthsrv.ScopeProjectsRead}
	projectsCreateScopes = []oauthsrv.Scope{oauthsrv.ScopeProjectsWrite}
	projectsChangeScopes = []oauthsrv.Scope{oauthsrv.ScopeProjectsWrite, oauthsrv.ScopeResumesWrite}
	projectLinkScopes    = []oauthsrv.Scope{oauthsrv.ScopeResumesWrite, oauthsrv.ScopeProjectsRead}
)

func (r *toolRuntime) executeProject(ctx context.Context, scopes []oauthsrv.Scope,
	call projectapi.AgentCall,
) (resumeapi.AgentResponse, error) {
	authority, err := r.require(ctx, scopes...)
	if err != nil {
		return resumeapi.AgentResponse{}, err
	}
	response := r.projects.ExecuteAgent(ctx, authority.agent, call)
	if response.Status < http.StatusOK || response.Status >= http.StatusMultipleChoices {
		return resumeapi.AgentResponse{}, mapAgentResponseError(response)
	}
	return response, nil
}

func projectPayload(document map[string]any, slug *string) ([]byte, error) {
	if document == nil {
		return nil, closedToolError("validation_failed")
	}
	if _, present := document["slug"]; present {
		return nil, closedToolError("validation_failed")
	}
	body := make(map[string]any, len(document)+1)
	for key, value := range document {
		body[key] = value
	}
	if slug != nil {
		body["slug"] = *slug
	}
	return marshalPayload(body)
}

func projectData(body []byte) (map[string]any, error) {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data == nil {
		return nil, closedToolError("agent_access_unavailable")
	}
	return envelope.Data, nil
}

func projectRevision(project map[string]any) (string, error) {
	revision, ok := project["revision"].(string)
	if !ok || revision == "" {
		return "", closedToolError("agent_access_unavailable")
	}
	return revision, nil
}

func changedResumesOf(data map[string]any) ([]changedResumeOutput, error) {
	raw, ok := data["changedResumes"].([]any)
	if !ok {
		return nil, closedToolError("agent_access_unavailable")
	}
	out := make([]changedResumeOutput, 0, len(raw))
	for _, item := range raw {
		changed, isObject := item.(map[string]any)
		id, idOK := changed["id"].(string)
		revision, revisionOK := changed["revision"].(string)
		if !isObject || !idOK || !revisionOK {
			return nil, closedToolError("agent_access_unavailable")
		}
		out = append(out, changedResumeOutput{ID: id, Revision: revision})
	}
	return out, nil
}

func registerProjectTools(server *mcp.Server, runtime *toolRuntime) {
	mcp.AddTool(server, &mcp.Tool{Name: "list_projects", Description: "List the projects in the authorized user's library."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, listProjectsOutput, error) {
			response, err := runtime.executeProject(ctx, projectsReadScopes, projectapi.AgentCall{Operation: projectapi.AgentListProjects})
			if err != nil {
				return nil, listProjectsOutput{}, err
			}
			var envelope struct {
				Data struct {
					Projects []map[string]any `json:"projects"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body, &envelope); err != nil || envelope.Data.Projects == nil {
				return nil, listProjectsOutput{}, closedToolError("agent_access_unavailable")
			}
			return nil, listProjectsOutput{Projects: envelope.Data.Projects}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "get_project", Description: "Read one library project."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input projectIDInput) (*mcp.CallToolResult, projectOutput, error) {
			response, err := runtime.executeProject(ctx, projectsReadScopes, projectapi.AgentCall{
				Operation: projectapi.AgentGetProject, ProjectID: input.ProjectID,
			})
			if err != nil {
				return nil, projectOutput{}, err
			}
			project, err := projectData(response.Body)
			if err != nil {
				return nil, projectOutput{}, err
			}
			return nil, projectOutput{Project: project}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "create_project", Description: "Create a library project."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input createProjectInput) (*mcp.CallToolResult, projectMutationOutput, error) {
			payload, err := projectPayload(input.Document, input.Slug)
			if err != nil {
				return nil, projectMutationOutput{}, err
			}
			response, err := runtime.executeProject(ctx, projectsCreateScopes, projectapi.AgentCall{
				Operation: projectapi.AgentCreateProject, IdempotencyKey: input.IdempotencyKey, Payload: payload,
			})
			if err != nil {
				return nil, projectMutationOutput{}, err
			}
			project, err := projectData(response.Body)
			if err != nil {
				return nil, projectMutationOutput{}, err
			}
			revision, err := projectRevision(project)
			return nil, projectMutationOutput{Revision: revision, Project: project}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "update_project", Description: "Replace a library project's slug and complete document using revision compare-and-swap."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input updateProjectInput) (*mcp.CallToolResult, updateProjectOutput, error) {
			payload, err := projectPayload(input.Document, &input.Slug)
			if err != nil {
				return nil, updateProjectOutput{}, err
			}
			response, err := runtime.executeProject(ctx, projectsChangeScopes, projectapi.AgentCall{
				Operation: projectapi.AgentUpdateProject, IdempotencyKey: input.IdempotencyKey,
				ProjectID: input.ProjectID, Revision: input.Revision, Payload: payload,
			})
			if err != nil {
				return nil, updateProjectOutput{}, err
			}
			data, err := projectData(response.Body)
			if err != nil {
				return nil, updateProjectOutput{}, err
			}
			project, ok := data["project"].(map[string]any)
			if !ok {
				return nil, updateProjectOutput{}, closedToolError("agent_access_unavailable")
			}
			revision, err := projectRevision(project)
			if err != nil {
				return nil, updateProjectOutput{}, err
			}
			changed, err := changedResumesOf(data)
			return nil, updateProjectOutput{Revision: revision, Project: project, ChangedResumes: changed}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "delete_project", Description: "Delete a library project and unlink it from every resume using its current revision."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input deleteProjectInput) (*mcp.CallToolResult, deleteProjectOutput, error) {
			response, err := runtime.executeProject(ctx, projectsChangeScopes, projectapi.AgentCall{
				Operation: projectapi.AgentDeleteProject, IdempotencyKey: input.IdempotencyKey,
				ProjectID: input.ProjectID, Revision: input.Revision,
			})
			if err != nil {
				return nil, deleteProjectOutput{}, err
			}
			data, err := projectData(response.Body)
			if err != nil {
				return nil, deleteProjectOutput{}, err
			}
			changed, err := changedResumesOf(data)
			return nil, deleteProjectOutput{ID: input.ProjectID, Deleted: true, ChangedResumes: changed}, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "link_project", Description: "Link a library project to a resume, inserting a prefilled project entry or linking an existing unlinked entry."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input linkProjectInput) (*mcp.CallToolResult, mutationOutput, error) {
			payload, err := marshalPayload(struct {
				ProjectID string  `json:"projectId"`
				EntryID   *string `json:"entryId,omitempty"`
			}{ProjectID: input.ProjectID, EntryID: input.EntryID})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			response, err := runtime.executeWith(ctx, projectLinkScopes, resumeapi.AgentCall{
				Operation: resumeapi.AgentLinkProject, IdempotencyKey: input.IdempotencyKey,
				ResumeID: input.ResumeID, Revision: input.Revision, Payload: payload,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "refresh_project_entry", Description: "Repeat the library prefill of one linked project entry using revision compare-and-swap."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input refreshProjectEntryInput) (*mcp.CallToolResult, mutationOutput, error) {
			response, err := runtime.executeWith(ctx, projectLinkScopes, resumeapi.AgentCall{
				Operation: resumeapi.AgentRefreshProjectEntry, IdempotencyKey: input.IdempotencyKey,
				ResumeID: input.ResumeID, Revision: input.Revision, EntryID: input.EntryID,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})
}
