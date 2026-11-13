package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	errMutationCap    = errors.New("mutation_cap_exceeded")
	errToolOutput     = errors.New("tool_output_invalid")
	errCreateRejected = errors.New("create_rejected")
)

const (
	maxListResultBytes     = 64 << 10
	maxDocumentResultBytes = 2 << 20
	maxPhotoResultBytes    = 3 << 20
	maxToolCalls           = 96
	createSendWindow       = time.Minute
	toolCallTimeout        = 2 * time.Minute
)

var closedToolErrors = map[string]bool{
	"not_found": true, "revision_conflict": true, "validation_failed": true, "payload_too_large": true,
	"rate_limited": true, "agent_access_unavailable": true, "scope_denied": true,
}

var (
	readTools     = map[string]bool{"list_resumes": true, "get_resume": true, "get_photo": true}
	mutationTools = map[string]bool{"create_resume": true, "upload_photo": true, "update_photo_crop": true}
)

type resumeRef interface{ resumeID() string }

func (h sourceHandle) resumeID() string { return string(h) }
func (h targetHandle) resumeID() string { return string(h) }

type toolGuard struct {
	client sdkToolClient
	local  bool

	mu            sync.Mutex
	calls         int
	source        sourceHandle
	intent        *createIntent
	target        targetHandle
	freshIntent   bool
	createSends   int
	uploads       int
	crops         int
	conflictProbe bool
}

func newToolGuard(client sdkToolClient, local bool) *toolGuard {
	return &toolGuard{client: client, local: local}
}

func (g *toolGuard) setSource(source sourceHandle) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.source != "" || !validUUID(string(source)) {
		return errMutationCap
	}
	g.source = source
	return nil
}

func (g *toolGuard) bindIntent(intent createIntent, fresh bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.intent != nil || g.source == "" || intent.SourceID != g.source {
		return errMutationCap
	}
	copied := intent
	copied.Payload = append([]byte(nil), intent.Payload...)
	g.intent = &copied
	g.freshIntent = fresh
	return nil
}

func (g *toolGuard) bindTarget(id string) (targetHandle, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !validUUID(id) || id == string(g.source) || g.intent == nil || (g.target != "" && string(g.target) != id) {
		return "", errMutationCap
	}
	g.target = targetHandle(id)
	return g.target, nil
}

func (g *toolGuard) call(ctx context.Context, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	if !readTools[name] && !mutationTools[name] {
		return nil, errMutationCap
	}
	g.mu.Lock()
	if g.calls >= maxToolCalls || (mutationTools[name] && !g.mutationBound(name, arguments)) {
		g.mu.Unlock()
		return nil, errMutationCap
	}
	g.calls++
	g.mu.Unlock()
	callContext, cancel := context.WithTimeout(ctx, toolCallTimeout)
	defer cancel()
	if arguments == nil {
		arguments = map[string]any{}
	}
	return g.client.CallTool(callContext, &mcp.CallToolParams{Name: name, Arguments: arguments})
}

func (g *toolGuard) mutationBound(name string, arguments map[string]any) bool {
	if name == "create_resume" {
		key, keyOK := arguments["idempotency_key"].(string)
		document, documentOK := arguments["document"].(json.RawMessage)
		_, namesResume := arguments["resume_id"]
		return g.intent != nil && keyOK && documentOK && !namesResume && key == g.intent.IdempotencyKey &&
			bytes.Equal(document, g.intent.Payload)
	}
	resumeID, ok := arguments["resume_id"].(string)
	return ok && g.target != "" && resumeID == string(g.target) && resumeID != string(g.source)
}

func (g *toolGuard) list(ctx context.Context) ([]resumeSummary, error) {
	result, err := g.call(ctx, "list_resumes", nil)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeToolOutput[listedResumes](result, maxListResultBytes)
	if err != nil || len(decoded.Resumes) > maxListedResumes {
		return nil, errToolOutput
	}
	for _, item := range decoded.Resumes {
		if !validResumeSummary(item) {
			return nil, errToolOutput
		}
	}
	return decoded.Resumes, nil
}

func (g *toolGuard) getResume(ctx context.Context, ref resumeRef) (resumeState, error) {
	if !validUUID(ref.resumeID()) {
		return resumeState{}, errToolOutput
	}
	result, err := g.call(ctx, "get_resume", map[string]any{"resume_id": ref.resumeID()})
	if err != nil {
		return resumeState{}, err
	}
	decoded, err := decodeToolOutput[getResumeResult](result, maxDocumentResultBytes)
	if err != nil || decoded.State.ID != ref.resumeID() || !validResumeState(decoded.State) {
		return resumeState{}, errToolOutput
	}
	return decoded.State, nil
}

func (g *toolGuard) getPhoto(ctx context.Context, ref resumeRef) (photoBytes, error) {
	if !validUUID(ref.resumeID()) {
		return photoBytes{}, errPhoto
	}
	result, err := g.call(ctx, "get_photo", map[string]any{"resume_id": ref.resumeID()})
	if err != nil {
		return photoBytes{}, err
	}
	decoded, err := decodeToolOutput[getPhotoResult](result, maxPhotoResultBytes)
	if err != nil || base64.StdEncoding.DecodedLen(len(decoded.DataBase64)) > maxDecodedPhotoBytes+2 {
		return photoBytes{}, errPhoto
	}
	data, err := base64.StdEncoding.Strict().DecodeString(decoded.DataBase64)
	if err != nil {
		return photoBytes{}, errPhoto
	}
	photo := photoBytes{ContentType: decoded.ContentType, Data: data}
	if validateErr := validatePhotoBytes(photo); validateErr != nil {
		return photoBytes{}, validateErr
	}
	return photo, nil
}

type createOutcome int

const (
	createCreated createOutcome = iota + 1
	createDefinitiveFailure
	createUnknown
)

func (g *toolGuard) create(ctx context.Context, deadline time.Time) (resumeState, createOutcome, error) {
	g.mu.Lock()
	if !g.createAllowed() {
		g.mu.Unlock()
		return resumeState{}, createUnknown, errMutationCap
	}
	intent := *g.intent
	firstSend := g.createSends == 0 && g.freshIntent
	g.createSends++
	g.mu.Unlock()
	sendContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	result, err := g.call(sendContext, "create_resume", map[string]any{
		"idempotency_key": intent.IdempotencyKey, "lng": targetLanguage, "title": targetTitle,
		"document": intent.Payload,
	})
	if err != nil {
		return resumeState{}, createUnknown, err
	}
	if code, ok := toolErrorCode(result); ok {
		if firstSend && (code == "validation_failed" || code == "payload_too_large") {
			return resumeState{}, createDefinitiveFailure, errCreateRejected
		}
		return resumeState{}, createUnknown, errCreateRejected
	}
	output, err := decodeToolOutput[mutationResult](result, maxDocumentResultBytes)
	if err != nil || output.Revision != output.State.Revision || !validResumeState(output.State) {
		return resumeState{}, createUnknown, errToolOutput
	}
	return output.State, createCreated, nil
}

func (g *toolGuard) createAllowed() bool {
	if g.intent == nil {
		return false
	}
	if g.local {
		return g.createSends == 0 || (g.createSends == 1 && g.target != "")
	}
	return g.createSends == 0 && g.target == ""
}

func (g *toolGuard) uploadPhoto(ctx context.Context, target targetHandle, revision string, key string, data []byte) (mutationResult, string, error) {
	if err := g.reserveTargetWrite(target, &g.uploads); err != nil {
		return mutationResult{}, "", err
	}
	if !validDecimalRevision(revision) || !validUUID(key) || len(data) == 0 || len(data) > maxDecodedPhotoBytes {
		return mutationResult{}, "", errMutationCap
	}
	return g.targetMutation(ctx, "upload_photo", map[string]any{
		"idempotency_key": key, "resume_id": string(target), "revision": revision,
		"data_base64": base64.StdEncoding.EncodeToString(data),
	})
}

func (g *toolGuard) cropPhoto(ctx context.Context, target targetHandle, revision string, key string, crop *photoCrop) (mutationResult, string, error) {
	if err := g.reserveTargetWrite(target, &g.crops); err != nil {
		return mutationResult{}, "", err
	}
	if !validDecimalRevision(revision) || !validUUID(key) || crop == nil || !crop.valid() {
		return mutationResult{}, "", errMutationCap
	}
	return g.targetMutation(ctx, "update_photo_crop", map[string]any{
		"idempotency_key": key, "resume_id": string(target), "revision": revision, "crop": crop.arguments(),
	})
}

func (g *toolGuard) staleCropProbe(ctx context.Context, target targetHandle, staleRevision, key string, crop *photoCrop) (string, error) {
	g.mu.Lock()
	if !g.local || g.conflictProbe || g.target == "" || target != g.target || target.resumeID() == g.source.resumeID() {
		g.mu.Unlock()
		return "", errMutationCap
	}
	g.conflictProbe = true
	g.mu.Unlock()
	arguments := map[string]any{"idempotency_key": key, "resume_id": string(target), "revision": staleRevision, "crop": nil}
	if crop != nil {
		arguments["crop"] = crop.arguments()
	}
	result, err := g.call(ctx, "update_photo_crop", arguments)
	if err != nil {
		return "", err
	}
	code, closed := toolErrorCode(result)
	if !closed {
		return "", nil
	}
	return code, nil
}

func (g *toolGuard) reserveTargetWrite(target targetHandle, counter *int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.target == "" || target != g.target || target.resumeID() == g.source.resumeID() || *counter != 0 {
		return errMutationCap
	}
	*counter = 1
	return nil
}

func (g *toolGuard) targetMutation(ctx context.Context, name string, arguments map[string]any) (mutationResult, string, error) {
	result, err := g.call(ctx, name, arguments)
	if err != nil {
		return mutationResult{}, "", err
	}
	if code, ok := toolErrorCode(result); ok {
		return mutationResult{}, code, nil
	}
	output, err := decodeToolOutput[mutationResult](result, maxDocumentResultBytes)
	if err != nil || output.Revision != output.State.Revision || !validResumeState(output.State) {
		return mutationResult{}, "", errToolOutput
	}
	return output, "", nil
}

func decodeToolOutput[T any](result *mcp.CallToolResult, limit int) (T, error) {
	var decoded T
	if result == nil || result.IsError || result.StructuredContent == nil || len(result.Content) > 1 {
		return decoded, errToolOutput
	}
	if len(result.Content) == 1 {
		if _, ok := result.Content[0].(*mcp.TextContent); !ok {
			return decoded, errToolOutput
		}
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil || len(encoded) > limit {
		return decoded, errToolOutput
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decodeErr := decoder.Decode(&decoded); decodeErr != nil {
		return decoded, errToolOutput
	}
	if trailingErr := decoder.Decode(&struct{}{}); !errors.Is(trailingErr, io.EOF) {
		return decoded, errToolOutput
	}
	return decoded, nil
}

func toolErrorCode(result *mcp.CallToolResult) (string, bool) {
	if result == nil || !result.IsError || len(result.Content) != 1 {
		return "", false
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || !closedToolErrors[text.Text] {
		return "", false
	}
	return text.Text, true
}
