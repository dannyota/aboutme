package main

import (
	"context"
	"net"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type sourceHandle string
type targetHandle string

type loopbackListener interface {
	Accept() (net.Conn, error)
	Addr() net.Addr
	Close() error
}

type browserHandoff interface {
	Open(context.Context, string) error
}

type sdkToolClient interface {
	ListTools(context.Context, *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
	CallTool(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)
	Close() error
}
