package resumeapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	idempotencyOperationDomain = "aboutme.idempotency.operation.v1"
	idempotencyRequestDomain   = "aboutme.idempotency.request.v1"
	maxJSONBodyBytes           = 256 * 1024
	maxJSONDepth               = 100
)

type mutationHeaders struct {
	Key              uuid.UUID
	ExpectedRevision *int64
	WireVersion      int32
}

type boundedInput struct {
	Payload []byte
	Value   any
}

type idempotencyInspection struct {
	Operation   string
	RequestHash [32]byte
	Response    resume.StoredResponse
	Replayed    bool
}

type preparedInput struct {
	Input boundedInput
	Value any
}

type mutationContext struct {
	UserID           uuid.UUID
	ExpectedRevision *int64
	WireVersion      int32
	Operation        string
	RequestHash      [32]byte
}

type mutationRunResult struct {
	Response resume.StoredResponse
}

type mutationOperation interface {
	Run(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error)
}

type mutationOperationFunc func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error)

// Run adapts a function to mutationOperation.
func (f mutationOperationFunc) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext,
	prepared preparedInput,
) (mutationRunResult, error) {
	return f(ctx, qtx, mutation, prepared)
}

type mutationSpec struct {
	RegisteredOperation string
	RequireMatch        bool
	Decode              func(*http.Request) (boundedInput, error)
	CanonicalTargets    func(boundedInput) ([]string, error)
	SemanticInputs      func(boundedInput) ([]string, error)
	Prepare             func(context.Context, boundedInput, idempotencyInspection) (preparedInput, error)
	Run                 mutationOperation
	Finalize            func(context.Context, preparedInput, resume.ExecuteResult, error)
}

type boundMutation struct {
	ctx       context.Context
	operation mutationOperation
	mutation  mutationContext
	prepared  preparedInput
}

func (b boundMutation) run(qtx *store.Queries) (resume.StoredResponse, error) {
	runResult, err := b.operation.Run(b.ctx, qtx, b.mutation, b.prepared)
	return runResult.Response, err
}

// executeMutation applies the shared mutation order. Once preparation
// succeeds, Finalize runs for every Execute outcome, including errors.
func (s *Service) executeMutation(w http.ResponseWriter, r *http.Request, spec mutationSpec) {
	headers, headerErr := parseMutationHeaders(r, spec.RequireMatch, s.acceptedVersions)
	if headerErr != nil {
		writeResumeError(w, headerErr)
		return
	}
	if spec.Decode == nil || spec.CanonicalTargets == nil || spec.Run == nil {
		writeResumeError(w, &clientError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "an internal error occurred"})
		return
	}
	input, err := spec.Decode(r)
	if err != nil {
		writeResumeError(w, mapMutationError(err))
		return
	}
	targets, err := spec.CanonicalTargets(input)
	if err != nil {
		writeResumeError(w, mapMutationError(err))
		return
	}
	if len(targets)%2 != 0 {
		writeResumeError(w, internalClientError())
		return
	}
	semanticInputs := []string(nil)
	if spec.SemanticInputs != nil {
		semanticInputs, err = spec.SemanticInputs(input)
		if err != nil {
			writeResumeError(w, mapMutationError(err))
			return
		}
		if len(semanticInputs)%2 != 0 {
			writeResumeError(w, internalClientError())
			return
		}
	}
	operationDigest := operationHash(r.Method, spec.RegisteredOperation, targets)
	operation := hexDigest(operationDigest)
	precondition := "absent"
	if headers.ExpectedRevision != nil {
		precondition = strconv.FormatInt(*headers.ExpectedRevision, 10)
	}
	fingerprint := requestHash(headers.WireVersion, precondition, semanticInputs, input.Payload)
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeResumeError(w, &clientError{Status: http.StatusUnauthorized, Code: "session_required", Message: "a valid session is required"})
		return
	}
	if s.idempotency == nil {
		writeResumeError(w, &clientError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "an internal error occurred"})
		return
	}
	stored, replayed, inspectErr := s.idempotency.Inspect(r.Context(), sess.UserID, operation, headers.Key, fingerprint)
	inspection := idempotencyInspection{Operation: operation, RequestHash: fingerprint, Response: stored, Replayed: replayed}
	if inspectErr != nil {
		writeResumeError(w, mapMutationError(inspectErr))
		return
	}
	if replayed {
		s.writeMutationResponse(w, stored)
		return
	}
	prepared := preparedInput{Input: input, Value: input.Value}
	if spec.Prepare != nil {
		prepared, err = spec.Prepare(r.Context(), input, inspection)
		if err != nil {
			writeResumeError(w, mapMutationError(err))
			return
		}
	}
	mutation := mutationContext{
		UserID: sess.UserID, ExpectedRevision: headers.ExpectedRevision,
		WireVersion: headers.WireVersion, Operation: operation, RequestHash: fingerprint,
	}
	callback := boundMutation{ctx: r.Context(), operation: spec.Run, mutation: mutation, prepared: prepared}
	result, executeErr := s.idempotency.Execute(
		r.Context(), sess.UserID, operation, headers.Key, fingerprint, callback.run,
	)
	if spec.Finalize != nil {
		spec.Finalize(r.Context(), prepared, result, executeErr)
	}
	if executeErr != nil {
		writeResumeError(w, s.mapMutationErrorAtWire(executeErr, headers.WireVersion))
		return
	}
	s.writeMutationResponse(w, result.Response)
}

func (s *Service) writeMutationResponse(w http.ResponseWriter, response resume.StoredResponse) {
	if s.writeResponse == nil {
		writeStoredResponse(w, response)
		return
	}
	s.writeResponse(w, response)
}

func parseMutationHeaders(r *http.Request, requireMatch bool, accepted []int32) (mutationHeaders, *clientError) {
	keyValues := r.Header.Values("Idempotency-Key")
	if len(keyValues) == 0 {
		return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "idempotency_key_required", Message: "Idempotency-Key is required"}
	}
	if len(keyValues) != 1 || keyValues[0] == "" || strings.Contains(keyValues[0], ",") {
		return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "idempotency_key_invalid", Message: "Idempotency-Key must be one UUID"}
	}
	key, err := uuid.Parse(keyValues[0])
	if err != nil {
		return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "idempotency_key_invalid", Message: "Idempotency-Key must be one UUID"}
	}

	matchValues := r.Header.Values("If-Match")
	var revision *int64
	if !requireMatch {
		if len(matchValues) != 0 {
			return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "precondition_not_supported", Message: "If-Match is not supported when creating a resume"}
		}
	} else {
		if len(matchValues) == 0 {
			return mutationHeaders{}, &clientError{Status: http.StatusPreconditionRequired, Code: "precondition_required", Message: "If-Match is required"}
		}
		parsed, parseErr := parseIfMatch(matchValues)
		if parseErr != nil {
			return mutationHeaders{}, parseErr
		}
		revision = &parsed
	}

	version, versionErr := resolveWireVersion(r.Header, accepted)
	if versionErr != nil {
		return mutationHeaders{}, versionErr
	}
	return mutationHeaders{Key: key, ExpectedRevision: revision, WireVersion: version}, nil
}

func parseIfMatch(values []string) (int64, *clientError) {
	malformed := func() (int64, *clientError) {
		return 0, &clientError{Status: http.StatusBadRequest, Code: "precondition_malformed", Message: `If-Match must have the form "r<revision>"`}
	}
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return malformed()
	}
	value := values[0]
	if len(value) < 4 || !strings.HasPrefix(value, `"r`) || !strings.HasSuffix(value, `"`) {
		return malformed()
	}
	digits := value[2 : len(value)-1]
	if digits == "" || (len(digits) > 1 && digits[0] == '0') {
		return malformed()
	}
	revision, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || revision < 1 {
		return malformed()
	}
	return revision, nil
}

func decodeJSONBody(r *http.Request, target any) (boundedInput, error) {
	if err := requireJSONContentType(r.Header); err != nil {
		return boundedInput{}, err
	}
	raw, err := readBoundedBody(r.Body, maxJSONBodyBytes)
	if err != nil {
		return boundedInput{}, err
	}
	if err := validateJSONTokens(raw, maxJSONDepth); err != nil {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body is not valid JSON"}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body does not match the operation"}
	}
	if err := requireDecoderEOF(dec); err != nil {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body has trailing data"}
	}
	return boundedInput{Payload: raw, Value: target}, nil
}

func decodeDeleteBody(r *http.Request) (boundedInput, error) {
	if values := r.Header.Values("Content-Type"); len(values) > 0 {
		if len(values) != 1 || !isJSONContentType(values[0]) {
			return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "DELETE Content-Type must be one application/json value"}
		}
	}
	if r.ContentLength > 0 {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "DELETE requests have no body"}
	}
	var one [1]byte
	n, err := r.Body.Read(one[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "DELETE requests have no body"}
	}
	return boundedInput{Payload: []byte{}}, nil
}

func requireJSONContentType(header http.Header) error {
	values := header.Values("Content-Type")
	if len(values) != 1 || !isJSONContentType(values[0]) {
		return &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "Content-Type must be one application/json value"}
	}
	return nil
}

func isJSONContentType(value string) bool {
	if strings.Contains(value, ",") {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil || mediaType != "application/json" {
		return false
	}
	delete(params, "charset")
	return len(params) == 0
}

func readBoundedBody(body io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body could not be read"}
	}
	if int64(len(raw)) > limit {
		return nil, &clientError{Status: http.StatusRequestEntityTooLarge, Code: "body_too_large", Message: "request body exceeds the 262144 byte limit"}
	}
	return raw, nil
}

func validateJSONTokens(raw []byte, maxDepth int) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := consumeJSONValue(dec, 0, maxDepth); err != nil {
		return err
	}
	return requireDecoderEOF(dec)
}

func consumeJSONValue(dec *json.Decoder, depth, maxDepth int) error {
	if depth > maxDepth {
		return errors.New("JSON nesting is too deep")
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, keyErr := dec.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if consumeErr := consumeJSONValue(dec, depth+1, maxDepth); consumeErr != nil {
				return consumeErr
			}
		}
		_, err = dec.Token()
		return err
	case '[':
		for dec.More() {
			if consumeErr := consumeJSONValue(dec, depth+1, maxDepth); consumeErr != nil {
				return consumeErr
			}
		}
		_, err = dec.Token()
		return err
	default:
		return errors.New("unexpected closing JSON delimiter")
	}
}

func requireDecoderEOF(dec *json.Decoder) error {
	var trailing any
	err := dec.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func operationHash(method, operation string, targets []string) [32]byte {
	fields := [][]byte{[]byte("method"), []byte(strings.ToUpper(method)), []byte("operation"), []byte(operation)}
	for _, target := range targets {
		fields = append(fields, []byte(target))
	}
	return tupleHash(idempotencyOperationDomain, fields)
}

func requestHash(version int32, precondition string, semanticInputs []string, payload []byte) [32]byte {
	fields := [][]byte{
		[]byte("wire_version"), []byte(wireVersionString(version)),
		[]byte("if_match"), []byte(precondition),
	}
	for _, input := range semanticInputs {
		fields = append(fields, []byte(input))
	}
	fields = append(fields, []byte("payload"), payload)
	return tupleHash(idempotencyRequestDomain, fields)
}

func tupleHash(domain string, fields [][]byte) [32]byte {
	var encoded bytes.Buffer
	var length [4]byte
	encoded.WriteString(domain)
	encoded.WriteByte(0)
	binary.BigEndian.PutUint32(length[:], tupleLength(len(fields)))
	encoded.Write(length[:])
	for _, field := range fields {
		binary.BigEndian.PutUint32(length[:], tupleLength(len(field)))
		encoded.Write(length[:])
		encoded.Write(field)
	}
	return sha256.Sum256(encoded.Bytes())
}

func tupleLength(length int) uint32 {
	if length < 0 || uint64(length) > uint64(^uint32(0)) {
		panic("resumeapi: tuple field exceeds uint32 length encoding")
	}
	return uint32(length)
}

func hexDigest(digest [32]byte) string { return hex.EncodeToString(digest[:]) }
