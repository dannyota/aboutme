package resumeapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	remainingExistingEntryID = "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"
	remainingNewEntryID      = "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"
)

type remainingOpenAPIMutation struct {
	operationID string
	method      string
	path        string
	route       routeSpec
}

type remainingFixture struct {
	resume   resume.Resume
	photoKey string
}

type remainingMutationRequest struct {
	operation   remainingOpenAPIMutation
	body        []byte
	contentType string
}

type remainingExitState struct {
	resumes string
	records string
	jobs    string
	objects []string
}

type remainingIdempotencyCounts struct {
	inspect int64
	execute int64
}

type remainingCountingIdempotency struct {
	idempotencyBoundary
	inspect atomic.Int64
	execute atomic.Int64
}

func (s *remainingCountingIdempotency) Inspect(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, requestHash [32]byte,
) (resume.StoredResponse, bool, error) {
	s.inspect.Add(1)
	return s.idempotencyBoundary.Inspect(ctx, userID, operation, key, requestHash)
}

func (s *remainingCountingIdempotency) Execute(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, requestHash [32]byte,
	fn func(*store.Queries) (resume.StoredResponse, error),
) (resume.ExecuteResult, error) {
	s.execute.Add(1)
	return s.idempotencyBoundary.Execute(ctx, userID, operation, key, requestHash, fn)
}

func (s *remainingCountingIdempotency) counts() remainingIdempotencyCounts {
	return remainingIdempotencyCounts{inspect: s.inspect.Load(), execute: s.execute.Load()}
}

func TestIfMatch_ExistingWritesAndCreate(t *testing.T) {
	h := newResumeAPITestHarness(t)
	fixture := newRemainingFixture(t, h)
	mutations := remainingOpenAPIMutations(t, fixture.resume.ID)

	var create remainingOpenAPIMutation
	var existing []remainingOpenAPIMutation
	for _, mutation := range mutations {
		if mutation.operationID == "createResume" {
			create = mutation
			continue
		}
		existing = append(existing, mutation)
	}
	if create.operationID == "" || len(existing) != 11 {
		t.Fatalf("OpenAPI mutation split = create %q and %d existing writes, want createResume and 11",
			create.operationID, len(existing))
	}

	acceptedCreate := remainingPerformMutation(t, h,
		remainingRequestForOperation(t, create, fixture), uuid.NewString(), nil, "2")
	if acceptedCreate.status != http.StatusCreated {
		t.Fatalf("create without If-Match = %d %s, want 201", acceptedCreate.status, acceptedCreate.body)
	}

	malformed := []string{
		"*",
		"r1",
		`W/"r1"`,
		`"1"`,
		`"r"`,
		`"r-1"`,
		`"r 1"`,
		`"r999999999999999999999999999999999999999999999999"`,
		`"rnot-a-number"`,
	}
	createSupplied := append([]string{""}, malformed...)
	for index, match := range createSupplied {
		t.Run(fmt.Sprintf("create/supplied-%02d", index), func(t *testing.T) {
			before := snapshotRemainingExitState(t, h, uuid.Nil)
			response := remainingPerformMutation(t, h,
				remainingRequestForOperation(t, create, fixture), uuid.NewString(), []string{match}, "2")
			assertRouteError(t, response, http.StatusBadRequest, "precondition_not_supported")
			assertRemainingExitState(t, h, uuid.Nil, before)
		})
	}

	for _, mutation := range existing {
		mutation := mutation
		request := remainingRequestForOperation(t, mutation, fixture)
		t.Run(mutation.operationID+"/missing", func(t *testing.T) {
			before := snapshotRemainingExitState(t, h, fixture.resume.ID)
			response := remainingPerformMutation(t, h, request, uuid.NewString(), nil, "2")
			assertRouteError(t, response, http.StatusPreconditionRequired, "precondition_required")
			assertRemainingExitState(t, h, fixture.resume.ID, before)
		})
		for index, match := range malformed {
			t.Run(fmt.Sprintf("%s/malformed-%02d", mutation.operationID, index), func(t *testing.T) {
				before := snapshotRemainingExitState(t, h, fixture.resume.ID)
				response := remainingPerformMutation(t, h, request, uuid.NewString(), []string{match}, "2")
				assertRouteError(t, response, http.StatusBadRequest, "precondition_malformed")
				assertRemainingExitState(t, h, fixture.resume.ID, before)
			})
		}
		t.Run(mutation.operationID+"/stale", func(t *testing.T) {
			before := snapshotRemainingExitState(t, h, fixture.resume.ID)
			response := remainingPerformMutation(t, h, request, uuid.NewString(), []string{`"r999999"`}, "2")
			assertRouteError(t, response, http.StatusPreconditionFailed, "revision_mismatch")
			assertRemainingExitState(t, h, fixture.resume.ID, before)
		})
	}
}

func TestIdempotency_ReplayIsByteIdentical(t *testing.T) {
	representatives := map[operationKind]string{
		operationCreate:         "createResume",
		operationMetadata:       "updateResumeMetadata",
		operationDelete:         "deleteResume",
		operationAggregate:      "upsertResumeEntry",
		operationPhotoCandidate: "uploadResumePhoto",
	}

	for kind, operationID := range representatives {
		kind, operationID := kind, operationID
		t.Run(operationID, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			fixture := newRemainingFixture(t, h)
			mutation := remainingMutationByOperation(t, remainingOpenAPIMutations(t, fixture.resume.ID), operationID)
			if mutation.route.OperationKind != kind {
				t.Fatalf("%s operation kind = %d, want %d", operationID, mutation.route.OperationKind, kind)
			}
			request := remainingRequestForOperation(t, mutation, fixture)
			counter := &remainingCountingIdempotency{idempotencyBoundary: h.service.idempotency}
			h.service.idempotency = counter
			key := uuid.NewString()
			match := []string(nil)
			if operationID != "createResume" {
				match = []string{fmt.Sprintf(`"r%d"`, fixture.resume.Revision)}
			}

			first := remainingPerformMutation(t, h, request, key, match, "2")
			remainingAssertSuccessStatus(t, operationID, first)
			stateAfterFirst := snapshotRemainingExitState(t, h, fixture.resume.ID)
			countsAfterFirst := counter.counts()
			if countsAfterFirst.execute != 1 {
				t.Fatalf("%s first execute calls = %d, want 1", operationID, countsAfterFirst.execute)
			}

			replay := remainingPerformMutation(t, h, request, key, match, "2")
			if replay.status != first.status || !bytes.Equal(replay.body, first.body) {
				t.Fatalf("%s replay differs: first=%d %q replay=%d %q",
					operationID, first.status, first.body, replay.status, replay.body)
			}
			if got, want := remainingApprovedHeaders(replay.header), remainingApprovedHeaders(first.header); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s stable replay headers = %#v, want %#v", operationID, got, want)
			}
			remainingAssertRequestScopedHeaders(t, operationID, first.header, replay.header)
			if got := counter.counts(); got.execute != countsAfterFirst.execute || got.inspect != countsAfterFirst.inspect+1 {
				t.Fatalf("%s replay idempotency calls = %+v after first %+v, want one inspect and no execute",
					operationID, got, countsAfterFirst)
			}
			assertRemainingExitState(t, h, fixture.resume.ID, stateAfterFirst)
		})
	}

	observed := make(map[operationKind]bool)
	h := newResumeAPITestHarness(t)
	fixture := newRemainingFixture(t, h)
	for _, mutation := range remainingOpenAPIMutations(t, fixture.resume.ID) {
		observed[mutation.route.OperationKind] = true
	}
	for kind := range representatives {
		if !observed[kind] {
			t.Errorf("OpenAPI mutation inventory has no operation kind %d", kind)
		}
		delete(observed, kind)
	}
	if len(observed) != 0 {
		t.Fatalf("OpenAPI mutation inventory has untested operation kinds: %#v", observed)
	}
}

func TestHeadersAndDeleteBodiesFailClosed(t *testing.T) {
	deleteIDs := []string{"deleteResume", "deleteResumeEntry", "deleteResumePhoto"}

	for _, operationID := range deleteIDs {
		operationID := operationID
		t.Run(operationID+"/immediate-eof", func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			fixture := newRemainingFixture(t, h)
			mutation := remainingMutationByOperation(t, remainingOpenAPIMutations(t, fixture.resume.ID), operationID)
			response := remainingPerformMutation(t, h, remainingRequestForOperation(t, mutation, fixture),
				uuid.NewString(), []string{fmt.Sprintf(`"r%d"`, fixture.resume.Revision)}, "2")
			if response.status != http.StatusNoContent || len(response.body) != 0 || response.header.Get("Content-Type") != "" {
				t.Fatalf("%s EOF response = %d headers=%v body=%q, want bodyless 204",
					operationID, response.status, response.header, response.body)
			}
		})

		bodyCases := []struct {
			name          string
			body          func() io.ReadCloser
			contentLength int64
			chunked       bool
			mustNotRead   bool
		}{
			{name: "positive-length", body: func() io.ReadCloser { return &remainingObservedBody{} }, contentLength: 1, mustNotRead: true},
			{name: "whitespace", body: func() io.ReadCloser { return io.NopCloser(strings.NewReader(" ")) }, contentLength: 1},
			{name: "object", body: func() io.ReadCloser { return io.NopCloser(strings.NewReader(`{}`)) }, contentLength: 2},
			{name: "chunked", body: func() io.ReadCloser { return io.NopCloser(strings.NewReader("x")) }, contentLength: -1, chunked: true},
			{name: "trailing", body: func() io.ReadCloser { return io.NopCloser(strings.NewReader("{}\n{}")) }, contentLength: -1, chunked: true},
		}
		for _, bodyCase := range bodyCases {
			bodyCase := bodyCase
			t.Run(operationID+"/"+bodyCase.name, func(t *testing.T) {
				h := newResumeAPITestHarness(t)
				fixture := newRemainingFixture(t, h)
				mutation := remainingMutationByOperation(t, remainingOpenAPIMutations(t, fixture.resume.ID), operationID)
				request := remainingBaseRequest(t, h, remainingRequestForOperation(t, mutation, fixture), bodyCase.body())
				remainingSetValidMutationHeaders(request, fixture.resume.Revision, true)
				request.ContentLength = bodyCase.contentLength
				if bodyCase.chunked {
					request.TransferEncoding = []string{"chunked"}
				}
				counter := &remainingCountingIdempotency{idempotencyBoundary: h.service.idempotency}
				h.service.idempotency = counter
				before := snapshotRemainingExitState(t, h, fixture.resume.ID)
				var response testHTTPResponse
				if bodyCase.mustNotRead {
					response = remainingServeRouteDirect(t, h, request)
				} else {
					response = remainingServeDirect(t, h, request)
				}
				assertRouteError(t, response, http.StatusBadRequest, "request_invalid")
				if counter.counts() != (remainingIdempotencyCounts{}) {
					t.Fatalf("%s %s reached idempotency inspection: %+v", operationID, bodyCase.name, counter.counts())
				}
				observedBody, observed := request.Body.(*remainingObservedBody)
				if bodyCase.mustNotRead && !observed {
					t.Fatalf("%s body type = %T, want *remainingObservedBody", operationID, request.Body)
				}
				if bodyCase.mustNotRead && observedBody.read.Load() {
					t.Fatalf("%s positive Content-Length body was read", operationID)
				}
				assertRemainingExitState(t, h, fixture.resume.ID, before)
			})
		}

		headerCases := remainingSingletonHeaderCases()
		for _, headerCase := range headerCases {
			headerCase := headerCase
			t.Run(operationID+"/"+headerCase.name, func(t *testing.T) {
				h := newResumeAPITestHarness(t)
				fixture := newRemainingFixture(t, h)
				mutation := remainingMutationByOperation(t, remainingOpenAPIMutations(t, fixture.resume.ID), operationID)
				request := remainingBaseRequest(t, h, remainingRequestForOperation(t, mutation, fixture), nil)
				remainingSetValidMutationHeaders(request, fixture.resume.Revision, true)
				request.Header.Set("Content-Type", "application/json")
				headerCase.change(request)
				counter := &remainingCountingIdempotency{idempotencyBoundary: h.service.idempotency}
				h.service.idempotency = counter
				before := snapshotRemainingExitState(t, h, fixture.resume.ID)
				response := remainingServeDirect(t, h, request)
				assertRouteError(t, response, headerCase.status, headerCase.code)
				if counter.counts() != (remainingIdempotencyCounts{}) {
					t.Fatalf("%s %s reached idempotency inspection: %+v", operationID, headerCase.name, counter.counts())
				}
				assertRemainingExitState(t, h, fixture.resume.ID, before)
			})
		}
	}
}

func TestWireVersion_FailsClosed(t *testing.T) {
	h := newResumeAPITestHarness(t)
	fixture := newRemainingFixture(t, h)
	mutations := remainingOpenAPIMutations(t, fixture.resume.ID)
	counter := &remainingCountingIdempotency{idempotencyBoundary: h.service.idempotency}
	h.service.idempotency = counter

	versions := []struct {
		name  string
		value string
	}{
		{name: "undeclared", value: "3"},
		{name: "non-numeric", value: "future"},
		{name: "negative", value: "-1"},
		{name: "zero", value: "0"},
		{name: "far-future", value: "2147483647"},
	}
	for _, mutation := range mutations {
		mutation := mutation
		request := remainingRequestForOperation(t, mutation, fixture)
		for _, version := range versions {
			version := version
			t.Run(mutation.operationID+"/"+version.name, func(t *testing.T) {
				before := snapshotRemainingExitState(t, h, fixture.resume.ID)
				beforeCalls := counter.counts()
				match := []string(nil)
				if mutation.operationID != "createResume" {
					match = []string{fmt.Sprintf(`"r%d"`, fixture.resume.Revision)}
				}
				response := remainingPerformMutation(t, h, request, uuid.NewString(), match, version.value)
				assertRouteError(t, response, http.StatusBadRequest, "unsupported_schema_version")
				if got := counter.counts(); got != beforeCalls {
					t.Fatalf("%s %s reached idempotency: before=%+v after=%+v",
						mutation.operationID, version.name, beforeCalls, got)
				}
				assertRemainingExitState(t, h, fixture.resume.ID, before)
			})
		}
	}
}

func TestStrictDecode(t *testing.T) {
	h := newResumeAPITestHarness(t)
	fixture := newRemainingFixture(t, h)
	var jsonMutations []remainingOpenAPIMutation
	for _, mutation := range remainingOpenAPIMutations(t, fixture.resume.ID) {
		if mutation.method != http.MethodDelete && !mutation.route.Upload {
			jsonMutations = append(jsonMutations, mutation)
		}
	}
	if len(jsonMutations) != 8 {
		t.Fatalf("OpenAPI JSON mutation count = %d, want 8", len(jsonMutations))
	}

	deep := strings.Repeat("[", maxJSONDepth+2) + "0" + strings.Repeat("]", maxJSONDepth+2)
	transportCases := []struct {
		name string
		body string
	}{
		{name: "malformed", body: "{"},
		{name: "duplicate", body: `{"unknown":1,"unknown":2}`},
		{name: "unknown", body: `{"unknown":true}`},
		{name: "trailing", body: `{} {}`},
		{name: "wrong-top-level", body: `[]`},
		{name: "depth", body: deep},
	}
	for _, mutation := range jsonMutations {
		mutation := mutation
		base := remainingRequestForOperation(t, mutation, fixture)
		for _, transport := range transportCases {
			transport := transport
			t.Run(mutation.operationID+"/"+transport.name, func(t *testing.T) {
				request := base
				request.body = []byte(transport.body)
				before := snapshotRemainingExitState(t, h, fixture.resume.ID)
				match := []string(nil)
				if mutation.operationID != "createResume" {
					match = []string{fmt.Sprintf(`"r%d"`, fixture.resume.Revision)}
				}
				response := remainingPerformMutation(t, h, request, uuid.NewString(), match, "2")
				assertRouteError(t, response, http.StatusBadRequest, "request_invalid")
				assertRemainingExitState(t, h, fixture.resume.ID, before)
			})
		}
	}

	schemaCases := []struct {
		operationID string
		body        string
	}{
		{operationID: "createResume", body: `{"title":null}`},
		{operationID: "updateResumeMetadata", body: `{"title":null}`},
		{operationID: "updateResumeSection", body: `{"displayName":42}`},
		{operationID: "updateResumeStructure", body: `{"commands":[{"op":"createSection","key":"skill","sectionType":"skill","displayName":null,"column":"sidebar","index":0}]}`},
		{operationID: "updateResumePersonalDetails", body: `{"fullName":null}`},
		{operationID: "updateResumePhotoCrop", body: `{"crop":{"x":null,"y":0,"width":1,"height":1}}`},
	}
	for _, schemaCase := range schemaCases {
		schemaCase := schemaCase
		t.Run(schemaCase.operationID+"/parsed-schema-null-or-type", func(t *testing.T) {
			mutation := remainingMutationByOperation(t, jsonMutations, schemaCase.operationID)
			request := remainingRequestForOperation(t, mutation, fixture)
			request.body = []byte(schemaCase.body)
			before := snapshotRemainingExitState(t, h, fixture.resume.ID)
			match := []string(nil)
			if schemaCase.operationID != "createResume" {
				match = []string{fmt.Sprintf(`"r%d"`, fixture.resume.Revision)}
			}
			response := remainingPerformMutation(t, h, request, uuid.NewString(), match, "2")
			assertRouteError(t, response, http.StatusUnprocessableEntity, "document_invalid")
			assertRemainingExitState(t, h, fixture.resume.ID, before)
		})
	}
}

type remainingObservedBody struct {
	read atomic.Bool
}

func (b *remainingObservedBody) Read([]byte) (int, error) {
	b.read.Store(true)
	return 0, io.EOF
}

func (*remainingObservedBody) Close() error { return nil }

type remainingHeaderCase struct {
	name   string
	status int
	code   string
	change func(*http.Request)
}

func remainingSingletonHeaderCases() []remainingHeaderCase {
	return []remainingHeaderCase{
		{name: "idempotency-repeat", status: http.StatusBadRequest, code: "idempotency_key_invalid", change: func(r *http.Request) {
			r.Header.Add("Idempotency-Key", uuid.NewString())
		}},
		{name: "idempotency-fold", status: http.StatusBadRequest, code: "idempotency_key_invalid", change: func(r *http.Request) {
			r.Header.Set("Idempotency-Key", r.Header.Get("Idempotency-Key")+", "+uuid.NewString())
		}},
		{name: "if-match-repeat", status: http.StatusBadRequest, code: "precondition_malformed", change: func(r *http.Request) {
			r.Header.Add("If-Match", r.Header.Get("If-Match"))
		}},
		{name: "if-match-fold", status: http.StatusBadRequest, code: "precondition_malformed", change: func(r *http.Request) {
			r.Header.Set("If-Match", r.Header.Get("If-Match")+", "+r.Header.Get("If-Match"))
		}},
		{name: "wire-version-repeat", status: http.StatusBadRequest, code: "unsupported_schema_version", change: func(r *http.Request) {
			r.Header.Add(wireVersionHeader, "2")
		}},
		{name: "wire-version-fold", status: http.StatusBadRequest, code: "unsupported_schema_version", change: func(r *http.Request) {
			r.Header.Set(wireVersionHeader, "2, 2")
		}},
		{name: "content-type-repeat", status: http.StatusBadRequest, code: "request_invalid", change: func(r *http.Request) {
			r.Header.Add("Content-Type", "application/json")
		}},
		{name: "content-type-fold", status: http.StatusBadRequest, code: "request_invalid", change: func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json, application/json")
		}},
		{name: "csrf-repeat", status: http.StatusForbidden, code: "csrf_rejected", change: func(r *http.Request) {
			r.Header.Add(auth.CSRFHeaderName, r.Header.Get(auth.CSRFHeaderName))
		}},
		{name: "csrf-fold", status: http.StatusForbidden, code: "csrf_rejected", change: func(r *http.Request) {
			r.Header.Set(auth.CSRFHeaderName, r.Header.Get(auth.CSRFHeaderName)+", "+r.Header.Get(auth.CSRFHeaderName))
		}},
		{name: "origin-repeat", status: http.StatusForbidden, code: "csrf_rejected", change: func(r *http.Request) {
			r.Header.Add("Origin", resumeAPITestOrigin)
		}},
		{name: "origin-fold", status: http.StatusForbidden, code: "csrf_rejected", change: func(r *http.Request) {
			r.Header.Set("Origin", resumeAPITestOrigin+", "+resumeAPITestOrigin)
		}},
	}
}

func newRemainingFixture(t *testing.T, h *resumeAPITestHarness) remainingFixture {
	t.Helper()
	document := loadMinimalDocument(t)
	document.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: remainingExistingEntryID}}),
	}
	document.Customization.Layout.Sections.Main = []string{"work"}
	document.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "Adversarial exit", document)
	if err != nil {
		t.Fatalf("create adversarial resume: %v", err)
	}
	photoKey := fmt.Sprintf("resumes/%s/photo-0123456789abcdef0123456789abcdef.jpg", created.ID)
	payload := []byte("private-photo-object")
	outcome, err := h.service.blobs.Put(h.ctx, photoKey, "image/jpeg", bytes.NewReader(payload), int64(len(payload)))
	if err != nil || outcome != media.PutCreated {
		t.Fatalf("seed adversarial photo object = (%v, %v), want PutCreated", outcome, err)
	}
	document.PersonalDetails.Photo = &schema.Photo{Key: photoKey}
	if _, saveErr := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, document, created.Revision); saveErr != nil {
		t.Fatalf("attach adversarial photo: %v", saveErr)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload adversarial resume: %v", err)
	}
	return remainingFixture{resume: stored, photoKey: photoKey}
}

func remainingOpenAPIMutations(t *testing.T, resumeID uuid.UUID) []remainingOpenAPIMutation {
	t.Helper()
	document := loadResumeContractDocument(t)
	routes := registeredRoutes()
	var mutations []remainingOpenAPIMutation
	for path, item := range document.Paths {
		if !strings.HasPrefix(path, "/resumes") {
			continue
		}
		operations := []struct {
			method    string
			operation *resumeContractOperation
		}{
			{method: http.MethodPost, operation: item.Post},
			{method: http.MethodPatch, operation: item.Patch},
			{method: http.MethodDelete, operation: item.Delete},
		}
		for _, candidate := range operations {
			if candidate.operation == nil {
				continue
			}
			// This Task02 catalog intentionally covers the twelve mutations that
			// existed before Task07 added publish. Publish has its own transition
			// policy and tests.
			if candidate.operation.OperationID == "publishResume" {
				continue
			}
			var registered *routeSpec
			for index := range routes {
				if routes[index].Operation == candidate.operation.OperationID {
					registered = &routes[index]
					break
				}
			}
			if registered == nil || !registered.Mutation || registered.Method != candidate.method {
				t.Fatalf("OpenAPI mutation %s %s (%s) has no matching registered mutation",
					candidate.method, path, candidate.operation.OperationID)
			}
			concrete := strings.NewReplacer(
				"{id}", resumeID.String(),
				"{sectionKey}", "work",
				"{entryId}", remainingExistingEntryID,
			).Replace("/api/v1" + path)
			mutations = append(mutations, remainingOpenAPIMutation{
				operationID: candidate.operation.OperationID,
				method:      candidate.method,
				path:        concrete,
				route:       *registered,
			})
		}
	}
	sort.Slice(mutations, func(i, j int) bool { return mutations[i].operationID < mutations[j].operationID })
	return mutations
}

func remainingMutationByOperation(t *testing.T, mutations []remainingOpenAPIMutation,
	operationID string,
) remainingOpenAPIMutation {
	t.Helper()
	for _, mutation := range mutations {
		if mutation.operationID == operationID {
			return mutation
		}
	}
	t.Fatalf("OpenAPI mutation %s is absent", operationID)
	return remainingOpenAPIMutation{}
}

func remainingRequestForOperation(t *testing.T, mutation remainingOpenAPIMutation,
	fixture remainingFixture,
) remainingMutationRequest {
	t.Helper()
	request := remainingMutationRequest{operation: mutation}
	switch mutation.operationID {
	case "createResume":
		request.body = []byte(`{"title":"Replay create","lng":"en-US"}`)
	case "updateResumeMetadata":
		request.body = []byte(`{"title":"Changed title"}`)
	case "deleteResume", "deleteResumeEntry", "deleteResumePhoto":
	case "upsertResumeEntry":
		request.body = []byte(`{"entry":{"id":"` + remainingNewEntryID + `","jobTitle":"Engineer"}}`)
	case "updateResumeSection":
		request.body = []byte(`{"displayName":"Experience","entryOrder":["` + remainingExistingEntryID + `"]}`)
	case "updateResumeStructure":
		request.body = []byte(`{"commands":[{"op":"createSection","key":"skill","sectionType":"skill","column":"sidebar","index":0}]}`)
	case "updateResumePersonalDetails":
		request.body = []byte(`{"fullName":"Ada Lovelace","details":[]}`)
	case "updateResumeCustomization":
		request.body = []byte(`{"deltas":[{"op":"set","path":"font.baseSizePx","value":18}]}`)
	case "uploadResumePhoto":
		request.body, request.contentType = photoMultipartBody(t, func(writer *multipart.Writer) {
			part, err := writer.CreateFormFile("file", "photo.png")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(makePhotoPNG(t)); err != nil {
				t.Fatal(err)
			}
		})
	case "updateResumePhotoCrop":
		request.body = []byte(`{"crop":{"x":0.1,"y":0.2,"width":0.7,"height":0.6}}`)
	default:
		t.Fatalf("no valid request fixture for %s", mutation.operationID)
	}
	if request.contentType == "" && mutation.method != http.MethodDelete {
		request.contentType = "application/json"
	}
	return request
}

func remainingBaseRequest(t *testing.T, h *resumeAPITestHarness,
	mutation remainingMutationRequest, body io.Reader,
) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(h.ctx, mutation.operation.method,
		h.server.URL+mutation.operation.path, body)
	if err != nil {
		t.Fatalf("build %s request: %v", mutation.operation.operationID, err)
	}
	if request.Body == nil {
		request.Body = http.NoBody
	}
	request.AddCookie(h.cookie)
	request.Header.Set("Origin", resumeAPITestOrigin)
	request.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	if mutation.contentType != "" {
		request.Header.Set("Content-Type", mutation.contentType)
	}
	return request
}

func remainingSetValidMutationHeaders(request *http.Request, revision int64, requireMatch bool) {
	request.Header.Set("Idempotency-Key", uuid.NewString())
	if requireMatch {
		request.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
	}
	request.Header.Set(wireVersionHeader, "2")
}

func remainingPerformMutation(t *testing.T, h *resumeAPITestHarness, mutation remainingMutationRequest,
	key string, match []string, version string,
) testHTTPResponse {
	t.Helper()
	var body io.Reader
	if mutation.body != nil {
		body = bytes.NewReader(mutation.body)
	}
	request := remainingBaseRequest(t, h, mutation, body)
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	for _, value := range match {
		request.Header.Add("If-Match", value)
	}
	if version != "" {
		request.Header.Set(wireVersionHeader, version)
	}
	response, err := h.client.Do(request)
	if err != nil {
		t.Fatalf("perform %s request: %v", mutation.operation.operationID, err)
	}
	return snapshotHTTPResponse(t, response)
}

func remainingServeDirect(t *testing.T, h *resumeAPITestHarness, request *http.Request) testHTTPResponse {
	t.Helper()
	setUniquePhotoDirectRemoteAddr(request)
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return snapshotHTTPResponse(t, recorder.Result()) //nolint:bodyclose // snapshotHTTPResponse closes the synthetic body.
}

func remainingServeRouteDirect(t *testing.T, h *resumeAPITestHarness, request *http.Request) testHTTPResponse {
	t.Helper()
	setUniquePhotoDirectRemoteAddr(request)
	mux := http.NewServeMux()
	h.service.RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return snapshotHTTPResponse(t, recorder.Result()) //nolint:bodyclose // snapshotHTTPResponse closes the synthetic body.
}

func snapshotRemainingExitState(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID) remainingExitState {
	t.Helper()
	var jobs string
	if resumeID != uuid.Nil {
		if err := h.pool.QueryRow(h.ctx, `
			SELECT coalesce(string_agg(job::text, '|' ORDER BY job::text), '')
			FROM media_deletion_jobs AS job
			WHERE resume_id = $1`, resumeID).Scan(&jobs); err != nil {
			t.Fatalf("snapshot deletion jobs: %v", err)
		}
	}
	return remainingExitState{
		resumes: h.snapshotUserTable(t, "resumes"),
		records: h.snapshotUserTable(t, "idempotency_records"),
		jobs:    jobs,
		objects: snapshotObjectKeys(t, h),
	}
}

func assertRemainingExitState(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID,
	want remainingExitState,
) {
	t.Helper()
	if got := snapshotRemainingExitState(t, h, resumeID); !reflect.DeepEqual(got, want) {
		t.Fatalf("rejected/replayed request changed state:\n got=%+v\nwant=%+v", got, want)
	}
}

func remainingAssertSuccessStatus(t *testing.T, operationID string, response testHTTPResponse) {
	t.Helper()
	want := map[string]int{
		"createResume":         http.StatusCreated,
		"updateResumeMetadata": http.StatusOK,
		"deleteResume":         http.StatusNoContent,
		"upsertResumeEntry":    http.StatusOK,
		"uploadResumePhoto":    http.StatusOK,
	}[operationID]
	if response.status != want {
		t.Fatalf("%s status = %d, want %d (body=%s)", operationID, response.status, want, response.body)
	}
	const wantCacheControl = "no-store, no-transform"
	if response.header.Get("Cache-Control") != wantCacheControl {
		t.Fatalf("%s Cache-Control = %q, want %q", operationID, response.header.Get("Cache-Control"), wantCacheControl)
	}
	if response.status == http.StatusNoContent && (len(response.body) != 0 || response.header.Get("Content-Type") != "") {
		t.Fatalf("%s 204 response has body/content type: %q / %q",
			operationID, response.body, response.header.Get("Content-Type"))
	}
}

func remainingApprovedHeaders(header http.Header) map[string][]string {
	approved := make(map[string][]string)
	for _, name := range []string{"Location", "ETag", wireVersionHeader, "Content-Type", "Cache-Control"} {
		if values := header.Values(name); len(values) > 0 {
			approved[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
		}
	}
	return approved
}

func remainingAssertRequestScopedHeaders(t *testing.T, operationID string, first, replay http.Header) {
	t.Helper()
	firstID := first.Get("X-Request-Id")
	replayID := replay.Get("X-Request-Id")
	if _, err := uuid.Parse(firstID); err != nil {
		t.Fatalf("%s first X-Request-Id %q is invalid: %v", operationID, firstID, err)
	}
	if _, err := uuid.Parse(replayID); err != nil {
		t.Fatalf("%s replay X-Request-Id %q is invalid: %v", operationID, replayID, err)
	}
	if firstID == replayID {
		t.Fatalf("%s replay reused request ID %q", operationID, firstID)
	}
	now := time.Now()
	for label, value := range map[string]string{"first": first.Get("Date"), "replay": replay.Get("Date")} {
		parsed, err := http.ParseTime(value)
		if err != nil {
			t.Fatalf("%s %s Date %q is invalid: %v", operationID, label, value, err)
		}
		if delta := now.Sub(parsed); delta < -time.Second || delta > 30*time.Second {
			t.Fatalf("%s %s Date %s is not fresh (delta %s)", operationID, label, parsed, delta)
		}
	}
}
