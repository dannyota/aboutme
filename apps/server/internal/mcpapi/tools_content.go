package mcpapi

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
)

type entryInput struct {
	IdempotencyKey string         `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	ResumeID       string         `json:"resume_id" jsonschema:"resume UUID"`
	Revision       string         `json:"revision" jsonschema:"current decimal revision"`
	SectionKey     string         `json:"section_key" jsonschema:"section key"`
	Entry          map[string]any `json:"entry" jsonschema:"complete entry object"`
}

type deleteEntryInput struct {
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	ResumeID       string `json:"resume_id" jsonschema:"resume UUID"`
	Revision       string `json:"revision" jsonschema:"current decimal revision"`
	SectionKey     string `json:"section_key" jsonschema:"section key"`
	EntryID        string `json:"entry_id" jsonschema:"entry UUID"`
}

type updateSectionInput struct {
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	ResumeID       string `json:"resume_id" jsonschema:"resume UUID"`
	Revision       string `json:"revision" jsonschema:"current decimal revision"`
	SectionKey     string `json:"section_key" jsonschema:"section key"`
	DisplayName    any    `json:"display_name,omitempty" jsonschema:"section display name"`
	IconKey        any    `json:"icon_key,omitempty" jsonschema:"section icon key or null"`
	EntryOrder     any    `json:"entry_order,omitempty" jsonschema:"complete entry UUID permutation"`
}

type updateStructureInput struct {
	IdempotencyKey string           `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	ResumeID       string           `json:"resume_id" jsonschema:"resume UUID"`
	Revision       string           `json:"revision" jsonschema:"current decimal revision"`
	Commands       []map[string]any `json:"commands" jsonschema:"ordered structure commands"`
}

type updatePersonalDetailsInput struct {
	IdempotencyKey  string         `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	ResumeID        string         `json:"resume_id" jsonschema:"resume UUID"`
	Revision        string         `json:"revision" jsonschema:"current decimal revision"`
	PersonalDetails map[string]any `json:"personal_details" jsonschema:"complete personal details replacement"`
}

type updateCustomizationInput struct {
	IdempotencyKey string           `json:"idempotency_key" jsonschema:"caller-generated UUID reused only for an exact retry"`
	ResumeID       string           `json:"resume_id" jsonschema:"resume UUID"`
	Revision       string           `json:"revision" jsonschema:"current decimal revision"`
	Deltas         []map[string]any `json:"deltas" jsonschema:"ordered customization deltas"`
}

func marshalPayload(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, closedToolError("validation_failed")
	}
	return payload, nil
}

func registerContentTools(server *mcp.Server, runtime *toolRuntime) {
	mcp.AddTool(server, &mcp.Tool{Name: "upsert_entry", Description: "Insert or replace one entry using revision compare-and-swap."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input entryInput) (*mcp.CallToolResult, mutationOutput, error) {
			payload, err := marshalPayload(map[string]any{"entry": input.Entry})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentUpsertEntry, IdempotencyKey: input.IdempotencyKey,
				ResumeID: input.ResumeID, Revision: input.Revision,
				SectionKey: input.SectionKey, Payload: payload,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "delete_entry", Description: "Delete one entry using revision compare-and-swap."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input deleteEntryInput) (*mcp.CallToolResult, mutationOutput, error) {
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentDeleteEntry, IdempotencyKey: input.IdempotencyKey,
				ResumeID: input.ResumeID, Revision: input.Revision,
				SectionKey: input.SectionKey, EntryID: input.EntryID,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "update_section", Description: "Update section metadata or entry order using revision compare-and-swap."},
		func(ctx context.Context, request *mcp.CallToolRequest, input updateSectionInput) (*mcp.CallToolResult, mutationOutput, error) {
			var arguments map[string]json.RawMessage
			if err := json.Unmarshal(request.Params.Arguments, &arguments); err != nil {
				return nil, mutationOutput{}, closedToolError("validation_failed")
			}
			fields := make(map[string]json.RawMessage, 3)
			if raw, ok := arguments["display_name"]; ok {
				fields["displayName"] = raw
			}
			if raw, ok := arguments["icon_key"]; ok {
				fields["iconKey"] = raw
			}
			if raw, ok := arguments["entry_order"]; ok {
				fields["entryOrder"] = raw
			}
			payload, err := marshalPayload(fields)
			if err != nil {
				return nil, mutationOutput{}, err
			}
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentUpdateSection, IdempotencyKey: input.IdempotencyKey,
				ResumeID: input.ResumeID, Revision: input.Revision,
				SectionKey: input.SectionKey, Payload: payload,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "update_structure", Description: "Apply ordered section structure commands using revision compare-and-swap."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input updateStructureInput) (*mcp.CallToolResult, mutationOutput, error) {
			payload, err := marshalPayload(map[string]any{"commands": input.Commands})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentUpdateStructure, IdempotencyKey: input.IdempotencyKey, ResumeID: input.ResumeID,
				Revision: input.Revision, Payload: payload,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "update_personal_details", Description: "Replace personal details while preserving server-owned photo data."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input updatePersonalDetailsInput) (*mcp.CallToolResult, mutationOutput, error) {
			payload, err := marshalPayload(input.PersonalDetails)
			if err != nil {
				return nil, mutationOutput{}, err
			}
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentUpdatePersonalDetails, IdempotencyKey: input.IdempotencyKey, ResumeID: input.ResumeID,
				Revision: input.Revision, Payload: payload,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})

	mcp.AddTool(server, &mcp.Tool{Name: "update_customization", Description: "Apply bounded customization deltas using revision compare-and-swap."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input updateCustomizationInput) (*mcp.CallToolResult, mutationOutput, error) {
			payload, err := marshalPayload(map[string]any{"deltas": input.Deltas})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			response, err := runtime.execute(ctx, oauthsrv.ScopeResumesWrite, resumeapi.AgentCall{
				Operation: resumeapi.AgentUpdateCustomization, IdempotencyKey: input.IdempotencyKey, ResumeID: input.ResumeID,
				Revision: input.Revision, Payload: payload,
			})
			if err != nil {
				return nil, mutationOutput{}, err
			}
			output, err := responseMutation(response.Body)
			return nil, output, err
		})
}
