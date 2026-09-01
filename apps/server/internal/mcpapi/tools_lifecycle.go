package mcpapi

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
)

type createResumeInput struct {
	Title    *string        `json:"title,omitempty" jsonschema:"resume title"`
	Lng      *string        `json:"lng,omitempty" jsonschema:"BCP 47 language tag"`
	Document map[string]any `json:"document,omitempty" jsonschema:"optional complete resume document"`
}

type resumeMutationInput struct {
	ResumeID string `json:"resume_id" jsonschema:"resume UUID"`
	Revision string `json:"revision" jsonschema:"current decimal revision"`
}

type updateResumeMetadataInput struct {
	ResumeID string  `json:"resume_id" jsonschema:"resume UUID"`
	Revision string  `json:"revision" jsonschema:"current decimal revision"`
	Title    *string `json:"title,omitempty" jsonschema:"replacement title"`
	Lng      *string `json:"lng,omitempty" jsonschema:"replacement BCP 47 language tag"`
}

func registerLifecycleTools(server *mcp.Server, runtime *toolRuntime) {
	mcp.AddTool(server, &mcp.Tool{Name: "create_resume", Description: "Create a private resume draft."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input createResumeInput) (*mcp.CallToolResult, mutationOutput, error) {
			payload, err := json.Marshal(input)
			if err != nil {
				return nil, mutationOutput{}, closedToolError("validation_failed")
			}
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentCreateResume, Payload: payload,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "delete_resume", Description: "Delete a private or published resume using its current revision."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input resumeMutationInput) (*mcp.CallToolResult, deleteResumeOutput, error) {
			_, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentDeleteResume, ResumeID: input.ResumeID, Revision: input.Revision,
			})
			if err != nil {
				return nil, deleteResumeOutput{}, err
			}
			return nil, deleteResumeOutput{Revision: input.Revision, ID: input.ResumeID, Deleted: true}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "update_resume_metadata", Description: "Update resume title or language using revision compare-and-swap."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input updateResumeMetadataInput) (*mcp.CallToolResult, mutationOutput, error) {
			payload, err := json.Marshal(struct {
				Title *string `json:"title,omitempty"`
				Lng   *string `json:"lng,omitempty"`
			}{Title: input.Title, Lng: input.Lng})
			if err != nil {
				return nil, mutationOutput{}, closedToolError("validation_failed")
			}
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentUpdateResumeMetadata, ResumeID: input.ResumeID,
				Revision: input.Revision, Payload: payload,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})
}
