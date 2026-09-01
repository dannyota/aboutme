package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
)

// AgentExecutor is the closed bridge to the resume validation and mutation
// kernel.
type AgentExecutor interface {
	ExecuteAgent(context.Context, resumeapi.AgentPrincipal, resumeapi.AgentCall) resumeapi.AgentResponse
}

// ServerDependencies are the authenticated resource server dependencies.
type ServerDependencies struct {
	Bearer              *Bearer
	Resumes             AgentExecutor
	Rates               *RatePolicies
	MaxRequestBodyBytes int64
}

type requestAuthority struct {
	principal Principal
	agent     resumeapi.AgentPrincipal
}

type requestAuthorityKey struct{}

// NewServer returns the bearer-only, stateless Streamable HTTP MCP handler.
func NewServer(dependencies ServerDependencies) (http.Handler, error) {
	if dependencies.Bearer == nil || isNil(dependencies.Resumes) || dependencies.Rates == nil || dependencies.MaxRequestBodyBytes <= 0 {
		return nil, errors.New("mcp server: invalid dependencies")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "aboutme", Version: "1"}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})
	runtime := &toolRuntime{resumes: dependencies.Resumes}
	registerReadTools(server, runtime)
	registerLifecycleTools(server, runtime)
	registerContentTools(server, runtime)
	registerPhotoTools(server, runtime)
	server.AddReceivingMiddleware(closeToolErrors)

	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: dependencies.MaxRequestBodyBytes,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := dependencies.Bearer.Authenticate(r)
		if err != nil {
			writeMCPError(w, err)
			return
		}
		_, digest, err := oauthsrv.ParseToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			writeMCPError(w, dependencies.Bearer.unauthorized())
			return
		}
		agent, err := resumeapi.NewAgentPrincipal(principal.UserID, principal.GrantID, principal.TokenID, digest)
		if err != nil {
			writeMCPError(w, errInternal)
			return
		}
		dependencies.Rates.ServeRequest(principal, w, r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				body, readErr := io.ReadAll(io.LimitReader(r.Body, dependencies.MaxRequestBodyBytes+1))
				if readErr != nil {
					writeMCPError(w, errInternal)
					return
				}
				if int64(len(body)) > dependencies.MaxRequestBodyBytes {
					writeMCPError(w, errPayloadTooLarge)
					return
				}
				if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
					writeMCPError(w, errInvalidRequest)
					return
				}
				if jsonRPCMethod(body) == "tools/call" {
					if allowed, retry := dependencies.Rates.AdmitTool(principal); !allowed {
						dependencies.Rates.writeToolRateError(w, retry)
						return
					}
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				r.ContentLength = int64(len(body))
			}
			ctx := context.WithValue(r.Context(), requestAuthorityKey{}, requestAuthority{principal: principal, agent: agent})
			streamable.ServeHTTP(w, r.WithContext(ctx))
		}))
	}), nil
}

func jsonRPCMethod(body []byte) string {
	var envelope struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	return envelope.Method
}

func closeToolErrors(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, request)
		if err != nil || method != "tools/call" {
			return result, err
		}
		toolResult, ok := result.(*mcp.CallToolResult)
		if !ok || !toolResult.IsError {
			return result, nil
		}
		if len(toolResult.Content) == 1 {
			if content, ok := toolResult.Content[0].(*mcp.TextContent); ok && closedToolCode(content.Text) {
				return result, nil
			}
		}
		code := "agent_access_unavailable"
		if cause := toolResult.GetError(); cause != nil {
			message := cause.Error()
			if strings.Contains(message, "validating \"arguments\"") || strings.Contains(message, "unmarshal") {
				code = "validation_failed"
			}
		}
		toolResult.Content = []mcp.Content{&mcp.TextContent{Text: code}}
		toolResult.StructuredContent = nil
		return toolResult, nil
	}
}

func closedToolCode(code string) bool {
	switch code {
	case "validation_failed", "revision_conflict", "not_found", "payload_too_large", "scope_denied",
		"rate_limited", "agent_access_unavailable":
		return true
	default:
		return false
	}
}

func authorityFromContext(ctx context.Context) (requestAuthority, error) {
	authority, ok := ctx.Value(requestAuthorityKey{}).(requestAuthority)
	if !ok {
		return requestAuthority{}, errAgentAccessUnavailable
	}
	return authority, nil
}
