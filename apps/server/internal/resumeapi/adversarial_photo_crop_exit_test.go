package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestPhotoCrop_WriteSafety(t *testing.T) {
	assertPhotoCropChangedBodyReuse(t)
	assertPhotoCropNullClear(t)
}

func TestPhotoCropContractAndIsolation(t *testing.T) {
	assertPhotoCropDeclaredVersionMatrix(t)
	assertPhotoCropReplacementRace(t)
}

type photoCropValidCase struct {
	name     string
	field    string
	category string
	body     string
	want     schema.PhotoCrop
}

type photoCropRejectedCase struct {
	name         string
	field        string
	category     string
	body         string
	status       int
	code         string
	message      string
	issuePath    string
	issueMessage string
}

func assertPhotoCropDeclaredVersionMatrix(t *testing.T) {
	t.Helper()
	validCases := []photoCropValidCase{
		{name: "x exact lower", field: "x", category: "valid-lower", body: `{"crop":{"x":0,"y":0.25,"width":0.5,"height":0.5}}`, want: schema.PhotoCrop{X: 0, Y: 0.25, Width: 0.5, Height: 0.5}},
		{name: "x exact upper", field: "x", category: "valid-upper", body: `{"crop":{"x":1,"y":0.25,"width":0.5,"height":0.5}}`, want: schema.PhotoCrop{X: 1, Y: 0.25, Width: 0.5, Height: 0.5}},
		{name: "y exact lower", field: "y", category: "valid-lower", body: `{"crop":{"x":0.25,"y":0,"width":0.5,"height":0.5}}`, want: schema.PhotoCrop{X: 0.25, Y: 0, Width: 0.5, Height: 0.5}},
		{name: "y exact upper", field: "y", category: "valid-upper", body: `{"crop":{"x":0.25,"y":1,"width":0.5,"height":0.5}}`, want: schema.PhotoCrop{X: 0.25, Y: 1, Width: 0.5, Height: 0.5}},
		{name: "width positive epsilon", field: "width", category: "valid-lower", body: `{"crop":{"x":0.25,"y":0.25,"width":0.000001,"height":0.5}}`, want: schema.PhotoCrop{X: 0.25, Y: 0.25, Width: 0.000001, Height: 0.5}},
		{name: "width exact upper", field: "width", category: "valid-upper", body: `{"crop":{"x":0.25,"y":0.25,"width":1,"height":0.5}}`, want: schema.PhotoCrop{X: 0.25, Y: 0.25, Width: 1, Height: 0.5}},
		{name: "height positive epsilon", field: "height", category: "valid-lower", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":0.000001}}`, want: schema.PhotoCrop{X: 0.25, Y: 0.25, Width: 0.5, Height: 0.000001}},
		{name: "height exact upper", field: "height", category: "valid-upper", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":1}}`, want: schema.PhotoCrop{X: 0.25, Y: 0.25, Width: 0.5, Height: 1}},
	}

	const boundsMessage = "crop coordinates are outside their bounds"
	const shapeMessage = "crop must contain x, y, width, and height"
	const numbersMessage = "crop coordinates must be numbers"
	rejectedCases := []photoCropRejectedCase{
		{name: "x below lower by epsilon", field: "x", category: "below-lower", body: `{"crop":{"x":-0.000001,"y":0.25,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: boundsMessage},
		{name: "x above upper by epsilon", field: "x", category: "above-upper", body: `{"crop":{"x":1.000001,"y":0.25,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: boundsMessage},
		{name: "y below lower by epsilon", field: "y", category: "below-lower", body: `{"crop":{"x":0.25,"y":-0.000001,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: boundsMessage},
		{name: "y above upper by epsilon", field: "y", category: "above-upper", body: `{"crop":{"x":0.25,"y":1.000001,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: boundsMessage},
		{name: "width exact excluded lower", field: "width", category: "excluded-lower", body: `{"crop":{"x":0.25,"y":0.25,"width":0,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: boundsMessage},
		{name: "width below lower by epsilon", field: "width", category: "below-lower", body: `{"crop":{"x":0.25,"y":0.25,"width":-0.000001,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: boundsMessage},
		{name: "width above upper by epsilon", field: "width", category: "above-upper", body: `{"crop":{"x":0.25,"y":0.25,"width":1.000001,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: boundsMessage},
		{name: "height exact excluded lower", field: "height", category: "excluded-lower", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":0}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: boundsMessage},
		{name: "height below lower by epsilon", field: "height", category: "below-lower", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":-0.000001}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: boundsMessage},
		{name: "height above upper by epsilon", field: "height", category: "above-upper", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":1.000001}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: boundsMessage},

		{name: "x missing", field: "x", category: "missing", body: `{"crop":{"y":0.25,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: shapeMessage},
		{name: "y missing", field: "y", category: "missing", body: `{"crop":{"x":0.25,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: shapeMessage},
		{name: "width missing", field: "width", category: "missing", body: `{"crop":{"x":0.25,"y":0.25,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: shapeMessage},
		{name: "height missing", field: "height", category: "missing", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: shapeMessage},

		{name: "x non-number", field: "x", category: "non-number", body: `{"crop":{"x":"no","y":0.25,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},
		{name: "y non-number", field: "y", category: "non-number", body: `{"crop":{"x":0.25,"y":"no","width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},
		{name: "width non-number", field: "width", category: "non-number", body: `{"crop":{"x":0.25,"y":0.25,"width":"no","height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},
		{name: "height non-number", field: "height", category: "non-number", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":"no"}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},

		{name: "x null", field: "x", category: "null", body: `{"crop":{"x":null,"y":0.25,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop.x", issueMessage: numbersMessage},
		{name: "y null", field: "y", category: "null", body: `{"crop":{"x":0.25,"y":null,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop.y", issueMessage: numbersMessage},
		{name: "width null", field: "width", category: "null", body: `{"crop":{"x":0.25,"y":0.25,"width":null,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop.width", issueMessage: numbersMessage},
		{name: "height null", field: "height", category: "null", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":null}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop.height", issueMessage: numbersMessage},

		{name: "x positive non-finite equivalent", field: "x", category: "positive-overflow", body: `{"crop":{"x":1e10000,"y":0.25,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},
		{name: "x negative non-finite equivalent", field: "x", category: "negative-overflow", body: `{"crop":{"x":-1e10000,"y":0.25,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},
		{name: "y positive non-finite equivalent", field: "y", category: "positive-overflow", body: `{"crop":{"x":0.25,"y":1e10000,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},
		{name: "y negative non-finite equivalent", field: "y", category: "negative-overflow", body: `{"crop":{"x":0.25,"y":-1e10000,"width":0.5,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},
		{name: "width positive non-finite equivalent", field: "width", category: "positive-overflow", body: `{"crop":{"x":0.25,"y":0.25,"width":1e10000,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},
		{name: "width negative non-finite equivalent", field: "width", category: "negative-overflow", body: `{"crop":{"x":0.25,"y":0.25,"width":-1e10000,"height":0.5}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},
		{name: "height positive non-finite equivalent", field: "height", category: "positive-overflow", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":1e10000}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},
		{name: "height negative non-finite equivalent", field: "height", category: "negative-overflow", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":-1e10000}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: numbersMessage},

		{name: "crop extra field", category: "fragment-extra", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":0.5,"extra":true}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: shapeMessage},
		{name: "crop supplied key", category: "fragment-key", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":0.5,"key":"client-key"}}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: shapeMessage},
		{name: "crop wrong type", category: "fragment-type", body: `{"crop":"not-an-object"}`, status: http.StatusUnprocessableEntity, code: "document_invalid", message: "resume document is invalid", issuePath: "crop", issueMessage: shapeMessage},

		{name: "missing crop envelope", category: "envelope-missing", body: `{}`, status: http.StatusBadRequest, code: "request_invalid", message: "request body must contain only crop"},
		{name: "supplied key envelope", category: "envelope-key", body: `{"key":"client-key"}`, status: http.StatusBadRequest, code: "request_invalid", message: "request body must contain crop"},
		{name: "extra envelope field", category: "envelope-extra", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":0.5},"extra":true}`, status: http.StatusBadRequest, code: "request_invalid", message: "request body must contain only crop"},
		{name: "supplied top-level key", category: "envelope-crop-key", body: `{"crop":{"x":0.25,"y":0.25,"width":0.5,"height":0.5},"key":"client-key"}`, status: http.StatusBadRequest, code: "request_invalid", message: "request body must contain only crop"},
		{name: "NaN token", category: "envelope-nan", body: `{"crop":{"x":NaN,"y":0.25,"width":0.5,"height":0.5}}`, status: http.StatusBadRequest, code: "request_invalid", message: "request body is not valid JSON"},
		{name: "positive Infinity token", category: "envelope-positive-infinity", body: `{"crop":{"x":Infinity,"y":0.25,"width":0.5,"height":0.5}}`, status: http.StatusBadRequest, code: "request_invalid", message: "request body is not valid JSON"},
		{name: "negative Infinity token", category: "envelope-negative-infinity", body: `{"crop":{"x":-Infinity,"y":0.25,"width":0.5,"height":0.5}}`, status: http.StatusBadRequest, code: "request_invalid", message: "request body is not valid JSON"},
	}
	assertPhotoCropCaseCompleteness(t, validCases, rejectedCases)

	for _, version := range []int{1, 2} {
		t.Run("declared version "+strconv.Itoa(version), func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			created := h.createResume(t)
			uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
			if uploaded.status != http.StatusOK {
				t.Fatalf("seed upload = %d body=%s", uploaded.status, uploaded.body)
			}
			_, uploadedDocument := decodedWrittenDocument(t, uploaded)
			if uploadedDocument.PersonalDetails.Photo == nil {
				t.Fatal("seed upload returned no photo")
			}
			photoKey := uploadedDocument.PersonalDetails.Photo.Key
			backend := &photoCropExitBackend{delegate: h.service.blobs}
			h.service.blobs = backend
			path := fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID)
			revision := int64(2)

			for _, test := range validCases {
				t.Run(test.name, func(t *testing.T) {
					beforeCalls := backend.snapshotCalls()
					response := photoCropExitVersionedRequest(t, h, path, test.body, revision, version)
					if response.status != http.StatusOK || response.header.Get(wireVersionHeader) != strconv.Itoa(version) ||
						response.header.Get("ETag") != fmt.Sprintf(`"r%d"`, revision+1) {
						t.Fatalf("valid crop = %d headers=%v body=%s", response.status, response.header, response.body)
					}
					responseRevision, document := decodedWrittenDocument(t, response)
					if responseRevision != strconv.FormatInt(revision+1, 10) || document.SchemaVersion != int64(version) ||
						document.PersonalDetails.Photo == nil || document.PersonalDetails.Photo.Key != photoKey ||
						document.PersonalDetails.Photo.Crop == nil || *document.PersonalDetails.Photo.Crop != test.want {
						t.Fatalf("valid crop revision=%q schema=%d photo=%#v, want key %q crop %#v",
							responseRevision, document.SchemaVersion, document.PersonalDetails.Photo, photoKey, test.want)
					}
					stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
					if err != nil {
						t.Fatalf("read stored valid crop: %v", err)
					}
					if stored.StoredSchemaVersion != docmigrate.CurrentVersion ||
						stored.Doc.SchemaVersion != int64(docmigrate.CurrentVersion) || stored.Revision != revision+1 ||
						stored.Doc.PersonalDetails.Photo == nil ||
						stored.Doc.PersonalDetails.Photo.Key != photoKey || stored.Doc.PersonalDetails.Photo.Crop == nil ||
						*stored.Doc.PersonalDetails.Photo.Crop != test.want {
						t.Fatalf("stored valid crop versions=%d/%d revision=%d photo=%#v, want current v2, key %q, crop %#v",
							stored.StoredSchemaVersion, stored.Doc.SchemaVersion, stored.Revision,
							stored.Doc.PersonalDetails.Photo, photoKey, test.want)
					}
					if afterCalls := backend.snapshotCalls(); afterCalls != beforeCalls {
						t.Fatalf("valid crop made object calls: before=%+v after=%+v", beforeCalls, afterCalls)
					}
					revision++
				})
			}

			t.Run("null clear", func(t *testing.T) {
				beforeCalls := backend.snapshotCalls()
				response := photoCropExitVersionedRequest(t, h, path, `{"crop":null}`, revision, version)
				if response.status != http.StatusOK || response.header.Get(wireVersionHeader) != strconv.Itoa(version) ||
					response.header.Get("ETag") != fmt.Sprintf(`"r%d"`, revision+1) {
					t.Fatalf("clear crop = %d headers=%v body=%s", response.status, response.header, response.body)
				}
				responseRevision, document := decodedWrittenDocument(t, response)
				if responseRevision != strconv.FormatInt(revision+1, 10) || document.SchemaVersion != int64(version) ||
					document.PersonalDetails.Photo == nil || document.PersonalDetails.Photo.Key != photoKey ||
					document.PersonalDetails.Photo.Crop != nil {
					t.Fatalf("clear crop revision=%q schema=%d photo=%#v, want key %q without crop",
						responseRevision, document.SchemaVersion, document.PersonalDetails.Photo, photoKey)
				}
				assertPhotoCropResponseOmitsProperty(t, response.body)
				stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
				if err != nil {
					t.Fatalf("read stored cleared crop: %v", err)
				}
				if stored.StoredSchemaVersion != docmigrate.CurrentVersion ||
					stored.Doc.SchemaVersion != int64(docmigrate.CurrentVersion) || stored.Revision != revision+1 ||
					stored.Doc.PersonalDetails.Photo == nil || stored.Doc.PersonalDetails.Photo.Key != photoKey ||
					stored.Doc.PersonalDetails.Photo.Crop != nil {
					t.Fatalf("stored clear versions=%d/%d revision=%d photo=%#v, want current v2 key %q without crop",
						stored.StoredSchemaVersion, stored.Doc.SchemaVersion, stored.Revision,
						stored.Doc.PersonalDetails.Photo, photoKey)
				}
				assertStoredPhotoCropPropertyAbsent(t, snapshotStoredResumeRow(t, h, created.ID).PersonalDetails)
				if afterCalls := backend.snapshotCalls(); afterCalls != beforeCalls {
					t.Fatalf("clear crop made object calls: before=%+v after=%+v", beforeCalls, afterCalls)
				}
				revision++
			})

			for _, test := range rejectedCases {
				t.Run(test.name, func(t *testing.T) {
					before := snapshotPhotoCropExitState(t, h, backend.delegate, created.ID)
					beforeCalls := backend.snapshotCalls()
					response := photoCropExitVersionedRequest(t, h, path, test.body, revision, version)
					assertPhotoCropExitError(t, response, test)
					if afterCalls := backend.snapshotCalls(); afterCalls != beforeCalls {
						t.Fatalf("rejected crop made object calls: before=%+v after=%+v", beforeCalls, afterCalls)
					}
					after := snapshotPhotoCropExitState(t, h, backend.delegate, created.ID)
					if !reflect.DeepEqual(after, before) {
						t.Fatalf("rejected crop changed resume, idempotency, deletion, or object state:\n before=%+v\n after=%+v", before, after)
					}
				})
			}
		})
	}
}

func assertPhotoCropCaseCompleteness(t *testing.T, valid []photoCropValidCase,
	rejected []photoCropRejectedCase,
) {
	t.Helper()
	want := map[string]struct{}{}
	for _, field := range []string{"x", "y"} {
		for _, category := range []string{"valid-lower", "valid-upper", "below-lower", "above-upper", "missing", "non-number", "null", "positive-overflow", "negative-overflow"} {
			want[field+"/"+category] = struct{}{}
		}
	}
	for _, field := range []string{"width", "height"} {
		for _, category := range []string{"valid-lower", "valid-upper", "excluded-lower", "below-lower", "above-upper", "missing", "non-number", "null", "positive-overflow", "negative-overflow"} {
			want[field+"/"+category] = struct{}{}
		}
	}
	for _, category := range []string{"fragment-extra", "fragment-key", "fragment-type", "envelope-missing", "envelope-key", "envelope-extra", "envelope-crop-key", "envelope-nan", "envelope-positive-infinity", "envelope-negative-infinity"} {
		want["/"+category] = struct{}{}
	}

	got := make(map[string]struct{}, len(valid)+len(rejected))
	for _, test := range valid {
		key := test.field + "/" + test.category
		if _, duplicate := got[key]; duplicate {
			t.Fatalf("duplicate crop contract case %q", key)
		}
		got[key] = struct{}{}
	}
	for _, test := range rejected {
		key := test.field + "/" + test.category
		if _, duplicate := got[key]; duplicate {
			t.Fatalf("duplicate crop contract case %q", key)
		}
		got[key] = struct{}{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("crop contract inventory mismatch:\n got=%v\nwant=%v", sortedPhotoCropCaseKeys(got), sortedPhotoCropCaseKeys(want))
	}
}

func sortedPhotoCropCaseKeys(cases map[string]struct{}) []string {
	keys := make([]string, 0, len(cases))
	for key := range cases {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func photoCropExitVersionedRequest(t *testing.T, h *resumeAPITestHarness, path, body string,
	revision int64, version int,
) testHTTPResponse {
	t.Helper()
	request := newPhotoCropExitRequest(t, h, http.MethodPatch, path, bytes.NewBufferString(body),
		"application/json", revision, uuid.New())
	request.Header.Set(wireVersionHeader, strconv.Itoa(version))
	response, err := h.client.Do(request)
	if err != nil {
		t.Fatalf("perform declared-version crop request: %v", err)
	}
	return snapshotHTTPResponse(t, response)
}

func assertPhotoCropExitError(t *testing.T, response testHTTPResponse, want photoCropRejectedCase) {
	t.Helper()
	if response.status != want.status {
		t.Fatalf("crop error status = %d body=%s, want %d", response.status, response.body, want.status)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(response.body, &root); err != nil {
		t.Fatalf("decode crop error envelope: %v", err)
	}
	if len(root) != 1 || root["error"] == nil {
		t.Fatalf("crop error envelope fields = %v, want only error", root)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(root["error"], &object); err != nil {
		t.Fatalf("decode crop error object: %v", err)
	}
	var code, message string
	if err := json.Unmarshal(object["code"], &code); err != nil {
		t.Fatalf("decode crop error code: %v", err)
	}
	if err := json.Unmarshal(object["message"], &message); err != nil {
		t.Fatalf("decode crop error message: %v", err)
	}
	if code != want.code || message != want.message {
		t.Fatalf("crop error = (%q, %q), want (%q, %q)", code, message, want.code, want.message)
	}
	if want.status == http.StatusBadRequest {
		if len(object) != 2 {
			t.Fatalf("400 crop error fields = %v, want exactly code and message", object)
		}
		return
	}
	if len(object) != 3 || object["details"] == nil {
		t.Fatalf("422 crop error fields = %v, want code, message, and details", object)
	}
	var details struct {
		Issues []struct {
			Path    string `json:"path"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(object["details"], &details); err != nil {
		t.Fatalf("decode crop error details: %v", err)
	}
	if len(details.Issues) != 1 || details.Issues[0].Path != want.issuePath ||
		details.Issues[0].Code != "invalid" || details.Issues[0].Message != want.issueMessage {
		t.Fatalf("crop error issues = %#v, want path=%q code=invalid message=%q",
			details.Issues, want.issuePath, want.issueMessage)
	}
}

func assertPhotoCropChangedBodyReuse(t *testing.T) {
	t.Helper()
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
	if uploaded.status != http.StatusOK {
		t.Fatalf("seed upload = %d body=%s", uploaded.status, uploaded.body)
	}
	_, uploadedDocument := decodedWrittenDocument(t, uploaded)
	if uploadedDocument.PersonalDetails.Photo == nil {
		t.Fatal("seed upload returned no photo")
	}

	backend := &photoCropExitBackend{delegate: h.service.blobs}
	h.service.blobs = backend
	path := fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID)
	idempotencyKey := uuid.NewString()
	body := `{"crop":{"x":0.1,"y":0.2,"width":0.7,"height":0.6}}`
	set := h.mutationRequest(t, http.MethodPatch, path, bytes.NewBufferString(body), 2, idempotencyKey)
	if set.status != http.StatusOK || set.header.Get("ETag") != `"r3"` {
		t.Fatalf("set crop = %d headers=%v body=%s", set.status, set.header, set.body)
	}
	_, setDocument := decodedWrittenDocument(t, set)
	if setDocument.PersonalDetails.Photo == nil ||
		setDocument.PersonalDetails.Photo.Key != uploadedDocument.PersonalDetails.Photo.Key ||
		setDocument.PersonalDetails.Photo.Crop == nil {
		t.Fatalf("set crop changed key or omitted crop: %#v", setDocument.PersonalDetails.Photo)
	}
	if calls := backend.snapshotCalls(); calls != (photoCropExitObjectCalls{}) {
		t.Fatalf("crop set made object calls: %+v", calls)
	}

	before := snapshotPhotoCropExitState(t, h, backend.delegate, created.ID)
	beforeCalls := backend.snapshotCalls()
	// The command is semantically identical but its raw JSON bytes differ.
	reused := h.mutationRequest(t, http.MethodPatch, path,
		bytes.NewBufferString(`{"crop": {"x":0.1,"y":0.2,"width":0.7,"height":0.6}}`),
		2, idempotencyKey)
	if reused.status != http.StatusConflict ||
		!bytes.Contains(reused.body, []byte(`"code":"idempotency_key_reuse"`)) {
		t.Fatalf("changed-body reuse = %d body=%s", reused.status, reused.body)
	}
	if afterCalls := backend.snapshotCalls(); afterCalls != beforeCalls {
		t.Fatalf("changed-body reuse made object calls: before=%+v after=%+v", beforeCalls, afterCalls)
	}
	after := snapshotPhotoCropExitState(t, h, backend.delegate, created.ID)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("changed-body reuse changed state:\n before=%+v\n after=%+v", before, after)
	}
}

func assertPhotoCropNullClear(t *testing.T) {
	t.Helper()
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	uploaded := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
	if uploaded.status != http.StatusOK {
		t.Fatalf("seed upload = %d body=%s", uploaded.status, uploaded.body)
	}
	_, uploadedDocument := decodedWrittenDocument(t, uploaded)
	if uploadedDocument.PersonalDetails.Photo == nil {
		t.Fatal("seed upload returned no photo")
	}
	photoKey := uploadedDocument.PersonalDetails.Photo.Key

	backend := &photoCropExitBackend{delegate: h.service.blobs}
	h.service.blobs = backend
	path := fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID)
	set := h.mutationRequest(t, http.MethodPatch, path,
		bytes.NewBufferString(`{"crop":{"x":0.1,"y":0.2,"width":0.7,"height":0.6}}`),
		2, uuid.NewString())
	if set.status != http.StatusOK || set.header.Get("ETag") != `"r3"` {
		t.Fatalf("set crop = %d headers=%v body=%s", set.status, set.header, set.body)
	}
	_, setDocument := decodedWrittenDocument(t, set)
	if setDocument.PersonalDetails.Photo == nil ||
		setDocument.PersonalDetails.Photo.Key != photoKey ||
		setDocument.PersonalDetails.Photo.Crop == nil {
		t.Fatalf("set crop changed key or omitted crop: %#v", setDocument.PersonalDetails.Photo)
	}

	clearKey := uuid.NewString()
	clearBody := `{"crop":null}`
	cleared := h.mutationRequest(t, http.MethodPatch, path, bytes.NewBufferString(clearBody), 3, clearKey)
	if cleared.status != http.StatusOK || cleared.header.Get("ETag") != `"r4"` {
		t.Fatalf("clear crop = %d headers=%v body=%s", cleared.status, cleared.header, cleared.body)
	}
	clearRevision, clearedDocument := decodedWrittenDocument(t, cleared)
	if clearRevision != "4" || clearedDocument.PersonalDetails.Photo == nil ||
		clearedDocument.PersonalDetails.Photo.Key != photoKey ||
		clearedDocument.PersonalDetails.Photo.Crop != nil {
		t.Fatalf("clear crop revision=%q photo=%#v", clearRevision, clearedDocument.PersonalDetails.Photo)
	}
	assertPhotoCropResponseOmitsProperty(t, cleared.body)
	if calls := backend.snapshotCalls(); calls != (photoCropExitObjectCalls{}) {
		t.Fatalf("crop set or clear made object calls: %+v", calls)
	}

	beforeReplay := snapshotPhotoCropExitState(t, h, backend.delegate, created.ID)
	beforeCalls := backend.snapshotCalls()
	replayed := h.mutationRequest(t, http.MethodPatch, path, bytes.NewBufferString(clearBody), 3, clearKey)
	if replayed.status != cleared.status || !bytes.Equal(replayed.body, cleared.body) ||
		!reflect.DeepEqual(stableResumeHeaders(replayed.header), stableResumeHeaders(cleared.header)) {
		t.Fatalf("clear replay differs: first=%d headers=%v body=%s replay=%d headers=%v body=%s",
			cleared.status, cleared.header, cleared.body, replayed.status, replayed.header, replayed.body)
	}
	if replayed.header.Get("X-Request-ID") == "" || replayed.header.Get("X-Request-ID") == cleared.header.Get("X-Request-ID") {
		t.Fatalf("clear replay request id = %q, first = %q", replayed.header.Get("X-Request-ID"), cleared.header.Get("X-Request-ID"))
	}

	reused := h.mutationRequest(t, http.MethodPatch, path, bytes.NewBufferString(`{"crop": null}`), 3, clearKey)
	if reused.status != http.StatusConflict ||
		!bytes.Contains(reused.body, []byte(`"code":"idempotency_key_reuse"`)) {
		t.Fatalf("changed clear-body reuse = %d body=%s", reused.status, reused.body)
	}
	if afterCalls := backend.snapshotCalls(); afterCalls != beforeCalls {
		t.Fatalf("clear replay or reuse made object calls: before=%+v after=%+v", beforeCalls, afterCalls)
	}
	afterReplay := snapshotPhotoCropExitState(t, h, backend.delegate, created.ID)
	if !reflect.DeepEqual(afterReplay, beforeReplay) {
		t.Fatalf("clear replay or reuse changed state:\n before=%+v\n after=%+v", beforeReplay, afterReplay)
	}
	assertStoredPhotoCropPropertyAbsent(t, afterReplay.row.PersonalDetails)
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("read stored clear state: %v", err)
	}
	if stored.Revision != 4 || stored.Doc.PersonalDetails.Photo == nil ||
		stored.Doc.PersonalDetails.Photo.Key != photoKey ||
		stored.Doc.PersonalDetails.Photo.Crop != nil {
		t.Fatalf("stored clear state revision=%d photo=%#v", stored.Revision, stored.Doc.PersonalDetails.Photo)
	}
}

func assertPhotoCropResponseOmitsProperty(t *testing.T, raw []byte) {
	t.Helper()
	var envelope struct {
		Data struct {
			Document json.RawMessage `json:"document"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode cleared response envelope: %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data.Document, &document); err != nil {
		t.Fatalf("decode cleared response document: %v", err)
	}
	assertPhotoRawOmitsCrop(t, document["personalDetails"], "cleared response")
}

func assertStoredPhotoCropPropertyAbsent(t *testing.T, raw string) {
	t.Helper()
	assertPhotoRawOmitsCrop(t, json.RawMessage(raw), "stored personal_details")
}

func assertPhotoRawOmitsCrop(t *testing.T, personalDetails json.RawMessage, source string) {
	t.Helper()
	var personal map[string]json.RawMessage
	if err := json.Unmarshal(personalDetails, &personal); err != nil {
		t.Fatalf("decode %s: %v", source, err)
	}
	photoRaw, exists := personal["photo"]
	if !exists {
		t.Fatalf("%s omitted photo", source)
	}
	var photo map[string]json.RawMessage
	if err := json.Unmarshal(photoRaw, &photo); err != nil {
		t.Fatalf("decode %s photo: %v", source, err)
	}
	if _, exists := photo["crop"]; exists {
		t.Fatalf("%s retained crop property: %s", source, photoRaw)
	}
}

func assertPhotoCropReplacementRace(t *testing.T) {
	t.Helper()
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	seedUpload := h.uploadPhotoRequest(t, created.ID, created.Revision, uuid.NewString(), "old.png", makePhotoPNG(t))
	if seedUpload.status != http.StatusOK {
		t.Fatalf("seed upload = %d body=%s", seedUpload.status, seedUpload.body)
	}
	_, seedDocument := decodedWrittenDocument(t, seedUpload)
	if seedDocument.PersonalDetails.Photo == nil {
		t.Fatal("seed upload returned no photo")
	}
	oldKey := seedDocument.PersonalDetails.Photo.Key
	path := fmt.Sprintf("/api/v1/resumes/%s/photo", created.ID)
	seedCrop := h.mutationRequest(t, http.MethodPatch, path,
		bytes.NewBufferString(`{"crop":{"x":0.1,"y":0.2,"width":0.7,"height":0.6}}`),
		2, uuid.NewString())
	if seedCrop.status != http.StatusOK || seedCrop.header.Get("ETag") != `"r3"` {
		t.Fatalf("seed crop = %d headers=%v body=%s", seedCrop.status, seedCrop.header, seedCrop.body)
	}

	backend := &photoCropExitBackend{delegate: h.service.blobs}
	h.service.blobs = backend
	uploadKey := uuid.New()
	cropKey := uuid.New()
	ordered := newPhotoCropOrderedIdempotency(h.service.idempotency, uploadKey, cropKey)
	h.service.idempotency = ordered

	uploadBody, uploadContentType := photoMultipartBody(t, func(writer *multipart.Writer) {
		part, err := writer.CreateFormFile("file", "replacement.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(makePhotoPNG(t)); err != nil {
			t.Fatal(err)
		}
	})
	uploadRequest := newPhotoCropExitRequest(t, h, http.MethodPost, path,
		bytes.NewReader(uploadBody), uploadContentType, 3, uploadKey)
	cropRequest := newPhotoCropExitRequest(t, h, http.MethodPatch, path,
		bytes.NewBufferString(`{"crop":{"x":0,"y":0,"width":0.5,"height":0.5}}`),
		"application/json", 3, cropKey)

	type namedResult struct {
		name     string
		response testHTTPResponse
		err      error
	}
	results := make(chan namedResult, 2)
	start := make(chan struct{})
	for _, request := range []struct {
		name string
		req  *http.Request
	}{
		{name: "replacement", req: uploadRequest},
		{name: "crop", req: cropRequest},
	} {
		go func(name string, req *http.Request) {
			<-start
			response, err := performPhotoCropExitRequest(h.client, req)
			results <- namedResult{name: name, response: response, err: err}
		}(request.name, request.req)
	}
	close(start)

	responses := make(map[string]testHTTPResponse, 2)
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("%s request: %v", result.name, result.err)
			}
			responses[result.name] = result.response
		case <-time.After(15 * time.Second):
			t.Fatal("photo replacement/crop race did not finish")
		}
	}
	replacement := responses["replacement"]
	crop := responses["crop"]
	if replacement.status != http.StatusOK || replacement.header.Get("ETag") != `"r4"` {
		t.Fatalf("replacement winner = %d headers=%v body=%s", replacement.status, replacement.header, replacement.body)
	}
	if crop.status != http.StatusPreconditionFailed {
		t.Fatalf("stale crop = %d body=%s, want 412", crop.status, crop.body)
	}
	if !ordered.inspectedFresh(uploadKey, cropKey) {
		t.Fatal("race did not reach Execute from two fresh idempotency inspections")
	}

	winningRevision, winningDocument := decodedWrittenDocument(t, replacement)
	losingRevision, losingDocument := decodedRevisionMismatch(t, crop)
	if winningRevision != "4" || losingRevision != "4" || !reflect.DeepEqual(losingDocument, winningDocument) {
		t.Fatalf("412 did not carry winner state: winning=%q losing=%q\nwinner=%#v\nloser=%#v",
			winningRevision, losingRevision, winningDocument.PersonalDetails.Photo, losingDocument.PersonalDetails.Photo)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("read race winner: %v", err)
	}
	if stored.Revision != 4 || !reflect.DeepEqual(stored.Doc, winningDocument) {
		t.Fatalf("stored race winner revision=%d photo=%#v, response revision=%s photo=%#v",
			stored.Revision, stored.Doc.PersonalDetails.Photo, winningRevision, winningDocument.PersonalDetails.Photo)
	}
	if stored.Doc.PersonalDetails.Photo == nil ||
		stored.Doc.PersonalDetails.Photo.Key == oldKey ||
		stored.Doc.PersonalDetails.Photo.Crop != nil {
		t.Fatalf("replacement/crop race stored mixed state: old=%q stored=%#v", oldKey, stored.Doc.PersonalDetails.Photo)
	}
	newKey := stored.Doc.PersonalDetails.Photo.Key

	objects := photoCropExitObjectKeys(h.ctx, t, backend.delegate, created.ID)
	wantObjects := []string{oldKey, newKey}
	sort.Strings(wantObjects)
	if !reflect.DeepEqual(objects, wantObjects) {
		t.Fatalf("race objects = %v, want referenced winner and queued old object %v", objects, wantObjects)
	}
	queued := photoCropExitDeletionKeys(t, h, created.ID)
	if !reflect.DeepEqual(queued, []string{oldKey}) {
		t.Fatalf("race deletion jobs = %v, want old key %q", queued, oldKey)
	}
	if calls := backend.snapshotCalls(); calls != (photoCropExitObjectCalls{puts: 1}) {
		t.Fatalf("race object calls = %+v, want one replacement Put", calls)
	}
	if count := photoCropExitIdempotencyCount(t, h, uploadKey); count != 1 {
		t.Fatalf("replacement idempotency rows = %d, want 1", count)
	}
	if count := photoCropExitIdempotencyCount(t, h, cropKey); count != 0 {
		t.Fatalf("stale crop idempotency rows = %d, want 0", count)
	}
}

type photoCropExitObjectCalls struct {
	puts    int
	gets    int
	deletes int
	lists   int
}

type photoCropExitBackend struct {
	delegate media.Backend
	mu       sync.Mutex
	calls    photoCropExitObjectCalls
}

func (b *photoCropExitBackend) Put(ctx context.Context, key, contentType string, body io.Reader,
	size int64,
) (media.PutOutcome, error) {
	b.mu.Lock()
	b.calls.puts++
	b.mu.Unlock()
	return b.delegate.Put(ctx, key, contentType, body, size)
}

func (b *photoCropExitBackend) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	b.mu.Lock()
	b.calls.gets++
	b.mu.Unlock()
	return b.delegate.Get(ctx, key)
}

func (b *photoCropExitBackend) Delete(ctx context.Context, key string) error {
	b.mu.Lock()
	b.calls.deletes++
	b.mu.Unlock()
	return b.delegate.Delete(ctx, key)
}

func (b *photoCropExitBackend) ListPage(ctx context.Context, prefix, cursor string,
	limit int,
) ([]media.Object, string, error) {
	b.mu.Lock()
	b.calls.lists++
	b.mu.Unlock()
	return b.delegate.ListPage(ctx, prefix, cursor, limit)
}

func (b *photoCropExitBackend) snapshotCalls() photoCropExitObjectCalls {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

type photoCropExitState struct {
	row     wireStoredRow
	records string
	jobs    []string
	objects []string
}

func snapshotPhotoCropExitState(t *testing.T, h *resumeAPITestHarness, backend media.Backend,
	resumeID uuid.UUID,
) photoCropExitState {
	t.Helper()
	return photoCropExitState{
		row:     snapshotStoredResumeRow(t, h, resumeID),
		records: h.snapshotUserTable(t, "idempotency_records"),
		jobs:    photoCropExitDeletionKeys(t, h, resumeID),
		objects: photoCropExitObjectKeys(h.ctx, t, backend, resumeID),
	}
}

func photoCropExitObjectKeys(ctx context.Context, t *testing.T, backend media.Backend,
	resumeID uuid.UUID,
) []string {
	t.Helper()
	objects, cursor, err := backend.ListPage(ctx, "resumes/"+resumeID.String()+"/", "", 10)
	if err != nil {
		t.Fatalf("list photo objects: %v", err)
	}
	if cursor != "" {
		t.Fatalf("photo object list unexpectedly paginated at %q", cursor)
	}
	keys := make([]string, len(objects))
	for index, object := range objects {
		keys[index] = object.Key
	}
	sort.Strings(keys)
	return keys
}

func photoCropExitDeletionKeys(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID) []string {
	t.Helper()
	rows, err := h.pool.Query(h.ctx, `
		SELECT object_key
		FROM media_deletion_jobs
		WHERE resume_id = $1
		ORDER BY object_key`, resumeID)
	if err != nil {
		t.Fatalf("query photo deletion jobs: %v", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan photo deletion job: %v", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate photo deletion jobs: %v", err)
	}
	return keys
}

func photoCropExitIdempotencyCount(t *testing.T, h *resumeAPITestHarness, key uuid.UUID) int {
	t.Helper()
	var count int
	if err := h.pool.QueryRow(h.ctx, `
		SELECT count(*)
		FROM idempotency_records
		WHERE user_id = $1 AND idempotency_key = $2`, h.userID, key).Scan(&count); err != nil {
		t.Fatalf("count photo idempotency records: %v", err)
	}
	return count
}

type photoCropOrderedIdempotency struct {
	delegate   idempotencyBoundary
	uploadKey  uuid.UUID
	cropKey    uuid.UUID
	ready      chan struct{}
	uploadDone chan struct{}

	mu          sync.Mutex
	arrived     int
	inspections map[uuid.UUID]int
	replays     map[uuid.UUID]bool
	readyOnce   sync.Once
	uploadOnce  sync.Once
}

func newPhotoCropOrderedIdempotency(delegate idempotencyBoundary, uploadKey,
	cropKey uuid.UUID,
) *photoCropOrderedIdempotency {
	return &photoCropOrderedIdempotency{
		delegate: delegate, uploadKey: uploadKey, cropKey: cropKey,
		ready: make(chan struct{}), uploadDone: make(chan struct{}),
		inspections: make(map[uuid.UUID]int), replays: make(map[uuid.UUID]bool),
	}
}

func (i *photoCropOrderedIdempotency) Inspect(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, fingerprint [32]byte,
) (resume.StoredResponse, bool, error) {
	response, replayed, err := i.delegate.Inspect(ctx, userID, operation, key, fingerprint)
	i.mu.Lock()
	i.inspections[key]++
	i.replays[key] = replayed
	i.mu.Unlock()
	return response, replayed, err
}

func (i *photoCropOrderedIdempotency) Execute(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, fingerprint [32]byte,
	run func(*store.Queries) (resume.StoredResponse, error),
) (resume.ExecuteResult, error) {
	if key != i.uploadKey && key != i.cropKey {
		return i.delegate.Execute(ctx, userID, operation, key, fingerprint, run)
	}
	i.mu.Lock()
	i.arrived++
	if i.arrived == 2 {
		i.readyOnce.Do(func() { close(i.ready) })
	}
	i.mu.Unlock()
	select {
	case <-i.ready:
	case <-ctx.Done():
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, ctx.Err()
	case <-time.After(10 * time.Second):
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted},
			fmt.Errorf("photo crop race: timed out waiting for both Execute calls")
	}
	if key == i.cropKey {
		select {
		case <-i.uploadDone:
		case <-ctx.Done():
			return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, ctx.Err()
		case <-time.After(10 * time.Second):
			return resume.ExecuteResult{Outcome: resume.CommitNotAttempted},
				fmt.Errorf("photo crop race: timed out waiting for replacement commit")
		}
		return i.delegate.Execute(ctx, userID, operation, key, fingerprint, run)
	}
	defer i.uploadOnce.Do(func() { close(i.uploadDone) })
	return i.delegate.Execute(ctx, userID, operation, key, fingerprint, run)
}

func (i *photoCropOrderedIdempotency) inspectedFresh(keys ...uuid.UUID) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, key := range keys {
		if i.inspections[key] != 1 || i.replays[key] {
			return false
		}
	}
	return i.arrived == len(keys)
}

func newPhotoCropExitRequest(t *testing.T, h *resumeAPITestHarness, method, path string,
	body io.Reader, contentType string, revision int64, key uuid.UUID,
) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(h.ctx, method, h.server.URL+path, body)
	if err != nil {
		t.Fatalf("build photo crop race request: %v", err)
	}
	request.AddCookie(h.cookie)
	request.Header.Set("Origin", resumeAPITestOrigin)
	request.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	request.Header.Set("Idempotency-Key", key.String())
	request.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
	request.Header.Set("Content-Type", contentType)
	return request
}

func performPhotoCropExitRequest(client *http.Client, request *http.Request) (testHTTPResponse, error) {
	response, err := client.Do(request)
	if err != nil {
		return testHTTPResponse{}, err
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return testHTTPResponse{}, readErr
	}
	if closeErr != nil {
		return testHTTPResponse{}, closeErr
	}
	return testHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: body}, nil
}
