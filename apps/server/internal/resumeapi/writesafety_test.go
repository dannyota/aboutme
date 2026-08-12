package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestMutationHeaders_PrecedenceAndContract(t *testing.T) {
	t.Parallel()

	validKey := "00000000-0000-0000-0000-000000000001"
	for _, tc := range []struct {
		name         string
		requireMatch bool
		headers      http.Header
		wantCode     string
		wantStatus   int
	}{
		{"missing key first", true, http.Header{}, "idempotency_key_required", http.StatusBadRequest},
		{"invalid key", true, http.Header{"Idempotency-Key": {"not-a-uuid"}}, "idempotency_key_invalid", http.StatusBadRequest},
		{"missing match", true, http.Header{"Idempotency-Key": {validKey}}, "precondition_required", http.StatusPreconditionRequired},
		{"malformed match", true, http.Header{"Idempotency-Key": {validKey}, "If-Match": {`W/"r42"`}}, "precondition_malformed", http.StatusBadRequest},
		{"match unsupported on create", false, http.Header{"Idempotency-Key": {validKey}, "If-Match": {`"r42"`}}, "precondition_not_supported", http.StatusBadRequest},
		{"unsupported wire version after match", true, http.Header{"Idempotency-Key": {validKey}, "If-Match": {`"r42"`}, "X-Resume-Schema-Version": {"999999"}}, "unsupported_schema_version", http.StatusBadRequest},
		{"duplicate key", true, http.Header{"Idempotency-Key": {validKey, validKey}, "If-Match": {`"r42"`}}, "idempotency_key_invalid", http.StatusBadRequest},
		{"folded match", true, http.Header{"Idempotency-Key": {validKey}, "If-Match": {`"r42", "r42"`}}, "precondition_malformed", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/", nil)
			req.Header = tc.headers.Clone()
			_, err := parseMutationHeaders(req, tc.requireMatch, nil)
			if err == nil {
				t.Fatalf("parseMutationHeaders returned nil, want %s", tc.wantCode)
			}
			if err.Code != tc.wantCode || err.Status != tc.wantStatus {
				t.Fatalf("error = (%d, %q), want (%d, %q)", err.Status, err.Code, tc.wantStatus, tc.wantCode)
			}
		})
	}
}

func TestExecuteMutation_HeaderFailuresPrecedeDecodeAndIdempotency(t *testing.T) {
	t.Parallel()

	validKey := uuid.NewString()
	for _, test := range []struct {
		name    string
		headers http.Header
		code    string
		status  int
	}{
		{
			name: "key before precondition and wire",
			headers: http.Header{
				"Idempotency-Key": {"invalid"},
				wireVersionHeader: {"999"},
			},
			code: "idempotency_key_invalid", status: http.StatusBadRequest,
		},
		{
			name: "precondition before wire",
			headers: http.Header{
				"Idempotency-Key": {validKey},
				wireVersionHeader: {"999"},
			},
			code: "precondition_required", status: http.StatusPreconditionRequired,
		},
		{
			name: "wire before decode",
			headers: http.Header{
				"Idempotency-Key": {validKey},
				"If-Match":        {`"r1"`},
				wireVersionHeader: {"999"},
			},
			code: "unsupported_schema_version", status: http.StatusBadRequest,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decoded := false
			service := &Service{acceptedVersions: []int32{2}}
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/", strings.NewReader("not JSON"))
			req.Header = test.headers.Clone()
			recorder := httptest.NewRecorder()
			service.executeMutation(recorder, req, mutationSpec{
				RegisteredOperation: "precedenceTest",
				RequireMatch:        true,
				Decode: func(*http.Request) (boundedInput, error) {
					decoded = true
					return boundedInput{}, errors.New("decode ran")
				},
				CanonicalTargets: func(boundedInput) ([]string, error) {
					return nil, nil
				},
				Run: mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
					t.Fatal("operation ran")
					return mutationRunResult{}, nil
				}),
			})
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, test.status, recorder.Body.String())
			}
			var envelope errorEnvelope
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if envelope.Error.Code != test.code {
				t.Fatalf("code = %q, want %q", envelope.Error.Code, test.code)
			}
			if decoded {
				t.Fatal("header rejection reached decode")
			}
		})
	}
}

func TestIdempotencyCanonicalVectors(t *testing.T) {
	t.Parallel()

	entryPayload := []byte(`{"entry":{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60","title":"Engineer"}}`)
	for _, tc := range []struct {
		name string
		got  [32]byte
		want string
	}{
		{"entry operation", operationHash(http.MethodPatch, "upsertResumeEntry", []string{"resume_id", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f", "section_key", "work", "entry_id", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"}), "cb58dbc9b27ce8bcb0b3944e1728b5bd64f7a73dfc02a7a509ea6dbd9a74a4f6"},
		{"entry request", requestHash(2, "42", nil, entryPayload), "a2a62948b5a388770183b9432076c79e9c78f0bfbd1d4c29359d3b882949bf5d"},
		{"create operation", operationHash(http.MethodPost, "createResume", nil), "6c2be02042f6fffe1a4cd202618012e1c3007fa64dc9a86f8630f084560e341f"},
		{"create request", requestHash(2, "absent", nil, []byte(`{}`)), "5f271718e815f9a55c7f1a4dae30f9ebdc196ff0cce7a7b156febc260d9de745"},
		{"photo operation", operationHash(http.MethodPost, "uploadResumePhoto", []string{"resume_id", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f"}), "96a20fbd9cb1b9e2b61e27629c8672d2ccb5a96c7f44d9ae6db741c5fd8094ac"},
		{"photo request", requestHash(2, "42", nil, []byte{0x00, 0xff, 0x10, 0x0a}), "126d8283725a991ab0814e10874b9c2a52f287ffc71418708364848108fcbb63"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hexDigest(tc.got); got != tc.want {
				t.Fatalf("digest = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDecodeDeleteBody_ImmediateEOFOnly(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		body    []byte
		wantErr bool
	}{
		{"absent", nil, false},
		{"object", []byte(`{}`), true},
		{"whitespace", []byte(" "), true},
		{"one byte", []byte("x"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/", bytes.NewReader(tc.body))
			_, err := decodeDeleteBody(req)
			if (err != nil) != tc.wantErr {
				t.Fatalf("decodeDeleteBody error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestDecodeJSONBody_StrictBoundsDepthDuplicatesTrailingUnknownAndContentType(t *testing.T) {
	t.Parallel()

	type knownRequest struct {
		Value string `json:"value"`
	}
	deep := strings.Repeat("[", maxJSONDepth+2) + "0" + strings.Repeat("]", maxJSONDepth+2)
	cases := []struct {
		name        string
		body        string
		contentType []string
		target      any
		wantCode    string
	}{
		{"valid", `{"value":"ok"}`, []string{"application/json"}, &knownRequest{}, ""},
		{"oversize", strings.Repeat(" ", maxJSONBodyBytes+1), []string{"application/json"}, &knownRequest{}, "body_too_large"},
		{"too deep", deep, []string{"application/json"}, new(any), "request_invalid"},
		{"duplicate key", `{"value":"a","value":"b"}`, []string{"application/json"}, &knownRequest{}, "request_invalid"},
		{"trailing", `{"value":"a"}{}`, []string{"application/json"}, &knownRequest{}, "request_invalid"},
		{"unknown", `{"unknown":true}`, []string{"application/json"}, &knownRequest{}, "request_invalid"},
		{"duplicate content type", `{"value":"a"}`, []string{"application/json", "application/json"}, &knownRequest{}, "request_invalid"},
		{"folded content type", `{"value":"a"}`, []string{"application/json, application/json"}, &knownRequest{}, "request_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/", strings.NewReader(tc.body))
			for _, value := range tc.contentType {
				req.Header.Add("Content-Type", value)
			}
			_, err := decodeJSONBody(req, tc.target)
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("decodeJSONBody: %v", err)
				}
				return
			}
			mapped := mapMutationError(err)
			if mapped.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q (err=%v)", mapped.Code, tc.wantCode, err)
			}
		})
	}
}

func TestDeleteBodyMatrixAndZeroPayloadFingerprint(t *testing.T) {
	t.Parallel()

	valid := func(contentTypes []string) boundedInput {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/", nil)
		for _, value := range contentTypes {
			req.Header.Add("Content-Type", value)
		}
		input, err := decodeDeleteBody(req)
		if err != nil {
			t.Fatalf("valid DELETE %v: %v", contentTypes, err)
		}
		return input
	}
	absent := valid(nil)
	present := valid([]string{"application/json"})
	if len(absent.Payload) != 0 || len(present.Payload) != 0 ||
		requestHash(2, "1", nil, absent.Payload) != requestHash(2, "1", nil, present.Payload) {
		t.Fatal("optional DELETE Content-Type changed the zero-payload fingerprint")
	}

	for _, tc := range []struct {
		name          string
		body          string
		contentLength int64
		chunked       bool
		contentTypes  []string
	}{
		{"object", `{}`, 2, false, nil},
		{"whitespace", ` `, 1, false, nil},
		{"chunked", `x`, -1, true, nil},
		{"duplicate content type", ``, 0, false, []string{"application/json", "application/json"}},
		{"folded content type", ``, 0, false, []string{"application/json, application/json"}},
		{"wrong content type", ``, 0, false, []string{"text/plain"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/", strings.NewReader(tc.body))
			req.ContentLength = tc.contentLength
			if tc.chunked {
				req.TransferEncoding = []string{"chunked"}
			}
			for _, value := range tc.contentTypes {
				req.Header.Add("Content-Type", value)
			}
			if _, err := decodeDeleteBody(req); err == nil {
				t.Fatal("decodeDeleteBody error = nil")
			}
		})
	}
}

func TestExecuteMutation_OddCanonicalTupleFailsClosed(t *testing.T) {
	t.Parallel()

	service := &Service{acceptedVersions: []int32{1}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	req.Header.Set("Idempotency-Key", "00000000-0000-0000-0000-000000000001")
	rec := httptest.NewRecorder()
	service.executeMutation(rec, req, mutationSpec{
		RegisteredOperation: "testOperation",
		Decode: func(*http.Request) (boundedInput, error) {
			return boundedInput{}, nil
		},
		CanonicalTargets: func(boundedInput) ([]string, error) {
			return []string{"unpaired"}, nil
		},
		Run: mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
			t.Fatal("operation ran with an invalid canonical tuple")
			return mutationRunResult{}, nil
		}),
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestExecuteMutation_ReplayIsByteIdenticalAndDoesNotRerun(t *testing.T) {
	h := newResumeAPITestHarness(t)
	var calls atomic.Int32
	spec := mutationSpec{
		RegisteredOperation: "kernelReplayTest",
		Decode: func(r *http.Request) (boundedInput, error) {
			raw, err := io.ReadAll(r.Body)
			return boundedInput{Payload: raw, Value: raw}, err
		},
		CanonicalTargets: func(boundedInput) ([]string, error) {
			return []string{"resume_id", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f"}, nil
		},
		Run: mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
			calls.Add(1)
			return mutationRunResult{Response: resume.StoredResponse{
				Status: http.StatusNoContent,
				Headers: map[string]string{
					"ETag": `"r2"`, wireVersionHeader: wireVersionString(docmigrate.CurrentVersion),
				},
			}}, nil
		}),
	}
	key := uuid.NewString()
	serve := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(auth.ContextWithSession(context.Background(), h.session), http.MethodPost, "/", bytes.NewBufferString(body))
		req.Header.Set("Idempotency-Key", key)
		rec := httptest.NewRecorder()
		h.service.executeMutation(rec, req, spec)
		return rec
	}

	first := serve(`{"value":1}`)
	replay := serve(`{"value":1}`)
	if first.Code != http.StatusNoContent || replay.Code != first.Code {
		t.Fatalf("statuses = (%d, %d), want (204, 204); bodies = (%s, %s)", first.Code, replay.Code, first.Body.String(), replay.Body.String())
	}
	if !bytes.Equal(first.Body.Bytes(), replay.Body.Bytes()) || !reflect.DeepEqual(first.Header(), replay.Header()) {
		t.Fatalf("replay differs: first=(%s, %v) replay=(%s, %v)", first.Body.Bytes(), first.Header(), replay.Body.Bytes(), replay.Header())
	}
	if first.Body.Len() != 0 || replay.Body.Len() != 0 {
		t.Fatalf("204 bodies = (%q, %q), want zero bytes", first.Body.String(), replay.Body.String())
	}
	if first.Header().Get("Content-Type") != "" || replay.Header().Get("Content-Type") != "" {
		t.Fatalf("204 Content-Type = (%q, %q), want absent", first.Header().Get("Content-Type"), replay.Header().Get("Content-Type"))
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("operation calls = %d, want 1", got)
	}

	reused := serve(`{"value":2}`)
	if reused.Code != http.StatusConflict {
		t.Fatalf("changed-body status = %d, want 409 (body=%s)", reused.Code, reused.Body.String())
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("operation calls after key reuse = %d, want 1", got)
	}
}
