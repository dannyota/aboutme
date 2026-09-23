package main

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeToolClient struct {
	pages    map[string]*mcp.ListToolsResult
	calls    []string
	maxCalls int
}

var errFakeToolClientCallLimit = errors.New("fake tool client call limit reached")

func (c *fakeToolClient) ListTools(_ context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	callLimit := c.maxCalls
	if callLimit == 0 {
		callLimit = maxToolListPages + 1
	}
	if len(c.calls) >= callLimit {
		return nil, errFakeToolClientCallLimit
	}
	c.calls = append(c.calls, params.Cursor)
	return c.pages[params.Cursor], nil
}
func (*fakeToolClient) CallTool(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	return nil, nil
}
func (*fakeToolClient) Close() error { return nil }

func TestTools(t *testing.T) {
	tools := make([]*mcp.Tool, 0, len(requiredTools))
	for _, name := range requiredTools {
		tools = append(tools, &mcp.Tool{Name: name})
	}
	client := &fakeToolClient{pages: map[string]*mcp.ListToolsResult{
		"":     {Tools: tools[:7], NextCursor: "next"},
		"next": {Tools: tools[7:]},
	}}
	names, err := discoverTools(t.Context(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 15 || !sort.StringsAreSorted(names) {
		t.Fatalf("tools = %v", names)
	}
	if got := strings.Join(client.calls, ","); got != ",next" {
		t.Fatalf("ListTools cursors = %q", got)
	}
}

func TestToolsAcceptOneToolPerPage(t *testing.T) {
	pages := make(map[string]*mcp.ListToolsResult, len(requiredTools))
	cursor := ""
	for i, name := range requiredTools {
		nextCursor := ""
		if i < len(requiredTools)-1 {
			nextCursor = "cursor-" + strconv.Itoa(i+1)
		}
		pages[cursor] = &mcp.ListToolsResult{
			Tools:      []*mcp.Tool{{Name: name}},
			NextCursor: nextCursor,
		}
		cursor = nextCursor
	}
	client := &fakeToolClient{pages: pages}
	names, err := discoverTools(t.Context(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != len(requiredTools) || !sort.StringsAreSorted(names) {
		t.Fatalf("tools = %v", names)
	}
	if got, want := len(client.calls), len(requiredTools); got != want {
		t.Fatalf("ListTools calls = %d, want %d", got, want)
	}
}

func TestToolsAcceptEmptyContinuedPage(t *testing.T) {
	pages := make(map[string]*mcp.ListToolsResult, len(requiredTools)+1)
	pages[""] = &mcp.ListToolsResult{NextCursor: "tools"}
	cursor := "tools"
	for i, name := range requiredTools {
		nextCursor := ""
		if i < len(requiredTools)-1 {
			nextCursor = "cursor-" + strconv.Itoa(i+1)
		}
		pages[cursor] = &mcp.ListToolsResult{
			Tools:      []*mcp.Tool{{Name: name}},
			NextCursor: nextCursor,
		}
		cursor = nextCursor
	}
	client := &fakeToolClient{pages: pages}
	if _, err := discoverTools(t.Context(), client); err != nil {
		t.Fatal(err)
	}
	if got, want := len(client.calls), len(requiredTools)+1; got != want {
		t.Fatalf("ListTools calls = %d, want %d", got, want)
	}
}

func TestToolsRejectUnexpectedRegistry(t *testing.T) {
	client := &fakeToolClient{pages: map[string]*mcp.ListToolsResult{"": {Tools: []*mcp.Tool{{Name: "evil"}}}}}
	if _, err := discoverTools(t.Context(), client); !errors.Is(err, errToolRegistry) {
		t.Fatalf("discoverTools error = %v, want %v", err, errToolRegistry)
	}
}

func TestToolsRejectCursorCycle(t *testing.T) {
	client := &fakeToolClient{pages: map[string]*mcp.ListToolsResult{
		"":  {NextCursor: "a"},
		"a": {NextCursor: "b"},
		"b": {NextCursor: "a"},
	}, maxCalls: 3}
	if _, err := discoverTools(t.Context(), client); !errors.Is(err, errToolRegistry) {
		t.Fatalf("discoverTools error = %v, want %v", err, errToolRegistry)
	}
	if got := strings.Join(client.calls, ","); got != ",a,b" {
		t.Fatalf("ListTools cursors = %q", got)
	}
}

func TestToolsRejectDistinctCursorStreamAtPageLimit(t *testing.T) {
	pages := make(map[string]*mcp.ListToolsResult, maxToolListPages)
	cursor := ""
	for i := 0; i < maxToolListPages; i++ {
		nextCursor := "cursor-" + strconv.Itoa(i)
		pages[cursor] = &mcp.ListToolsResult{NextCursor: nextCursor}
		cursor = nextCursor
	}
	client := &fakeToolClient{pages: pages, maxCalls: maxToolListPages}
	if _, err := discoverTools(t.Context(), client); !errors.Is(err, errToolRegistry) {
		t.Fatalf("discoverTools error = %v, want %v", err, errToolRegistry)
	}
	if got, want := len(client.calls), maxToolListPages; got != want {
		t.Fatalf("ListTools calls = %d, want %d", got, want)
	}
}

func TestToolsRejectMoreThanRequired(t *testing.T) {
	tools := make([]*mcp.Tool, 0, len(requiredTools)+1)
	for _, name := range requiredTools {
		tools = append(tools, &mcp.Tool{Name: name})
	}
	tools = append(tools, &mcp.Tool{Name: "extra"})
	client := &fakeToolClient{pages: map[string]*mcp.ListToolsResult{"": {Tools: tools}}}
	if _, err := discoverTools(t.Context(), client); !errors.Is(err, errToolRegistry) {
		t.Fatalf("discoverTools error = %v, want %v", err, errToolRegistry)
	}
}
