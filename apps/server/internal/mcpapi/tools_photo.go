package mcpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
)

const maxDecodedPhotoBytes = 2_097_152

type photoOutput struct {
	ContentType string `json:"content_type"`
	DataBase64  string `json:"data_base64"`
}

type uploadPhotoInput struct {
	ResumeID   string `json:"resume_id" jsonschema:"resume UUID"`
	Revision   string `json:"revision" jsonschema:"current decimal revision"`
	DataBase64 string `json:"data_base64" jsonschema:"base64-encoded JPEG, PNG, or WebP image"`
}

type updatePhotoCropInput struct {
	ResumeID string `json:"resume_id" jsonschema:"resume UUID"`
	Revision string `json:"revision" jsonschema:"current decimal revision"`
	Crop     any    `json:"crop" jsonschema:"crop rectangle or null"`
}

func registerPhotoTools(server *mcp.Server, runtime *toolRuntime) {
	mcp.AddTool(server, &mcp.Tool{Name: "get_photo", Description: "Read the private resume photo as base64 with its normalized content type."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input resumeIDInput) (*mcp.CallToolResult, photoOutput, error) {
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesRead, resumeapi.AgentCall{
				Operation: resumeapi.AgentGetPhoto, ResumeID: input.ResumeID,
			})
			if err != nil {
				return nil, photoOutput{}, err
			}
			contentType := response.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, "image/") {
				return nil, photoOutput{}, closedToolError("agent_access_unavailable")
			}
			return nil, photoOutput{ContentType: contentType, DataBase64: base64.StdEncoding.EncodeToString(response.Body)}, nil
		})

	mcp.AddTool(server, &mcp.Tool{Name: "upload_photo", Description: "Normalize and store a base64-encoded JPEG, PNG, or WebP photo."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input uploadPhotoInput) (*mcp.CallToolResult, mutationOutput, error) {
			if _, err := runtime.require(ctx, oauthsrv.ScopeResumesWrite); err != nil {
				return nil, mutationOutput{}, err
			}
			if base64.StdEncoding.DecodedLen(len(input.DataBase64)) > maxDecodedPhotoBytes {
				return nil, mutationOutput{}, closedToolError("payload_too_large")
			}
			file, err := base64.StdEncoding.DecodeString(input.DataBase64)
			if err != nil {
				return nil, mutationOutput{}, closedToolError("validation_failed")
			}
			if len(file) > maxDecodedPhotoBytes {
				return nil, mutationOutput{}, closedToolError("payload_too_large")
			}
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentUploadPhoto, ResumeID: input.ResumeID,
				Revision: input.Revision, File: file,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "update_photo_crop", Description: "Set or clear the normalized photo crop using revision compare-and-swap."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input updatePhotoCropInput) (*mcp.CallToolResult, mutationOutput, error) {
			payload, err := json.Marshal(map[string]any{"crop": input.Crop})
			if err != nil {
				return nil, mutationOutput{}, closedToolError("validation_failed")
			}
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentUpdatePhotoCrop, ResumeID: input.ResumeID,
				Revision: input.Revision, Payload: payload,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "delete_photo", Description: "Delete the private resume photo using revision compare-and-swap."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input resumeMutationInput) (*mcp.CallToolResult, mutationOutput, error) {
			_, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentDeletePhoto, ResumeID: input.ResumeID, Revision: input.Revision,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := runtime.refreshAfterMutation(ctx, input.ResumeID)
			return nil, output, err
		})
}
