package main

import (
	"context"
	"errors"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var errToolRegistry = errors.New("tool_registry_invalid")

var requiredTools = [...]string{
	"create_resume", "delete_entry", "delete_photo", "delete_resume", "get_photo",
	"get_resume", "list_resumes", "update_customization", "update_personal_details",
	"update_photo_crop", "update_resume_metadata", "update_section", "update_structure",
	"upload_photo", "upsert_entry",
}

// maxToolListPages bounds untrusted tool-list pagination at 32 pages.
const maxToolListPages = 32

func discoverTools(ctx context.Context, client sdkToolClient) ([]string, error) {
	seenTools := make(map[string]struct{}, len(requiredTools))
	seenCursors := make(map[string]struct{}, maxToolListPages)
	var cursor string
	for page := 0; ; page++ {
		if page == maxToolListPages {
			return nil, errToolRegistry
		}
		if _, ok := seenCursors[cursor]; ok {
			return nil, errToolRegistry
		}
		seenCursors[cursor] = struct{}{}

		result, err := client.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, errToolRegistry
		}
		for _, tool := range result.Tools {
			if tool == nil || tool.Name == "" {
				return nil, errToolRegistry
			}
			if len(seenTools) == len(requiredTools) {
				return nil, errToolRegistry
			}
			if _, ok := seenTools[tool.Name]; ok {
				return nil, errToolRegistry
			}
			seenTools[tool.Name] = struct{}{}
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	if len(seenTools) != len(requiredTools) {
		return nil, errToolRegistry
	}
	names := make([]string, 0, len(seenTools))
	for _, expected := range requiredTools {
		if _, ok := seenTools[expected]; !ok {
			return nil, errToolRegistry
		}
		names = append(names, expected)
	}
	sort.Strings(names)
	return names, nil
}
