package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
)

type toolRuntime struct {
	resumes AgentExecutor
}

type emptyInput struct{}

type resumeIDInput struct {
	ResumeID string `json:"resume_id" jsonschema:"resume UUID"`
}

type listResumesOutput struct {
	Resumes []map[string]any `json:"resumes"`
}

type resumeStateOutput struct {
	State map[string]any `json:"state"`
}

type mutationOutput struct {
	Revision string         `json:"revision"`
	State    map[string]any `json:"state"`
}

type deleteResumeOutput struct {
	Revision string `json:"revision"`
	ID       string `json:"id"`
	Deleted  bool   `json:"deleted"`
}

type closedToolError string

func (e closedToolError) Error() string { return string(e) }

func (r *toolRuntime) execute(ctx context.Context, scope oauthsrv.Scope,
	call resumeapi.AgentCall,
) (resumeapi.AgentResponse, error) {
	authority, err := r.require(ctx, scope)
	if err != nil {
		return resumeapi.AgentResponse{}, err
	}
	response := r.resumes.ExecuteAgent(ctx, authority.agent, call)
	if response.Status < http.StatusOK || response.Status >= http.StatusMultipleChoices {
		return resumeapi.AgentResponse{}, mapAgentResponseError(response)
	}
	return response, nil
}

func (r *toolRuntime) require(ctx context.Context, scope oauthsrv.Scope) (requestAuthority, error) {
	authority, err := authorityFromContext(ctx)
	if err != nil {
		return requestAuthority{}, closedToolError("agent_access_unavailable")
	}
	if err := RequireScope(authority.principal, scope); err != nil {
		return requestAuthority{}, closedToolError("scope_denied")
	}
	return authority, nil
}

func mapAgentResponseError(response resumeapi.AgentResponse) error {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	unmarshalErr := json.Unmarshal(response.Body, &envelope)
	switch {
	case response.Status == http.StatusNotFound:
		return closedToolError("not_found")
	case response.Status == http.StatusPreconditionFailed || (unmarshalErr == nil && envelope.Error.Code == "revision_mismatch"):
		return closedToolError("revision_conflict")
	case unmarshalErr == nil && envelope.Error.Code == "idempotency_key_reuse":
		return closedToolError("validation_failed")
	case response.Status == http.StatusRequestEntityTooLarge:
		return closedToolError("payload_too_large")
	case response.Status == http.StatusTooManyRequests || (unmarshalErr == nil && envelope.Error.Code == "media_busy") ||
		(unmarshalErr == nil && envelope.Error.Code == "public_state_busy"):
		return closedToolError("rate_limited")
	case unmarshalErr == nil && envelope.Error.Code == "agent_access_unavailable":
		return closedToolError("agent_access_unavailable")
	case response.Status == http.StatusBadRequest || response.Status == http.StatusUnsupportedMediaType ||
		response.Status == http.StatusUnprocessableEntity:
		return closedToolError("validation_failed")
	default:
		return closedToolError("agent_access_unavailable")
	}
}

func responseData(body []byte) (map[string]any, error) {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil || envelope.Data == nil {
		return nil, errors.New("mcp tools: invalid resume response")
	}
	return envelope.Data, nil
}

func responseMutation(body []byte) (mutationOutput, error) {
	state, err := responseData(body)
	if err != nil {
		return mutationOutput{}, closedToolError("agent_access_unavailable")
	}
	revision, ok := state["revision"].(string)
	if !ok || revision == "" {
		return mutationOutput{}, closedToolError("agent_access_unavailable")
	}
	return mutationOutput{Revision: revision, State: state}, nil
}

func registerReadTools(server *mcp.Server, runtime *toolRuntime) {
	mcp.AddTool(server, &mcp.Tool{Name: "list_resumes", Description: "List resumes owned by the authorized user."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, listResumesOutput, error) {
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesRead, resumeapi.AgentCall{Operation: resumeapi.AgentListResumes})
			if err != nil {
				return nil, listResumesOutput{}, err
			}
			var envelope struct {
				Data []map[string]any `json:"data"`
			}
			if err := json.Unmarshal(response.Body, &envelope); err != nil || envelope.Data == nil {
				return nil, listResumesOutput{}, closedToolError("agent_access_unavailable")
			}
			return nil, listResumesOutput{Resumes: envelope.Data}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "get_resume", Description: "Read one complete canonical resume."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input resumeIDInput) (*mcp.CallToolResult, resumeStateOutput, error) {
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesRead, resumeapi.AgentCall{
				Operation: resumeapi.AgentGetResume, ResumeID: input.ResumeID,
			})
			if err != nil {
				return nil, resumeStateOutput{}, err
			}
			state, err := responseData(response.Body)
			if err != nil {
				return nil, resumeStateOutput{}, closedToolError("agent_access_unavailable")
			}
			return nil, resumeStateOutput{State: state}, nil
		})
}
