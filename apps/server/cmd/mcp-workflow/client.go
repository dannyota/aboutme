package main

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connectSDKClient(ctx context.Context, origin string, client *http.Client, oauth auth.OAuthHandler) (sdkToolClient, error) {
	transport := &mcp.StreamableClientTransport{
		Endpoint:             origin + "/mcp",
		HTTPClient:           client,
		OAuthHandler:         oauth,
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	clientSDK := mcp.NewClient(&mcp.Implementation{Name: "aboutme-mcp-workflow", Version: "v0.4.4"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}})
	return clientSDK.Connect(ctx, transport, nil)
}
