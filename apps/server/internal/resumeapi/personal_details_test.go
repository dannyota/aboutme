package resumeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

func personalDetailsFromResponse(t *testing.T, response testHTTPResponse) schema.PersonalDetails {
	t.Helper()
	resource := decodeResumeResource(t, response)
	var document schema.Resume
	if err := json.Unmarshal(resource.Document, &document); err != nil {
		t.Fatalf("decode response document: %v (document=%s)", err, resource.Document)
	}
	return document.PersonalDetails
}

func TestPersonalDetails_WholeObjectReplacementAndDraftRoundTrip(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String() + "/personal-details"
	detailID := "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"
	body := fmt.Sprintf(`{"headline":"Engineer","details":[{"id":%q,"type":"website","value":"","isHidden":false}]}`, detailID)

	response := resumeRequest(t, h, http.MethodPatch, path, body, created.Revision, uuid.New(), "2")
	if response.status != http.StatusOK {
		t.Fatalf("personal-details patch = %d %s, want 200", response.status, response.body)
	}
	got := personalDetailsFromResponse(t, response)
	if got.FullName != nil || got.Headline == nil || *got.Headline != "Engineer" || len(got.Details) != 1 ||
		got.Details[0].ID != detailID || got.Details[0].Value != "" {
		t.Fatalf("replaced personalDetails = %#v", got)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload resume: %v", err)
	}
	if !reflect.DeepEqual(stored.Doc.PersonalDetails, got) {
		t.Fatalf("stored details = %#v, response details = %#v", stored.Doc.PersonalDetails, got)
	}

	cleared := resumeRequest(t, h, http.MethodPatch, path, `{"fullName":"","details":[]}`,
		stored.Revision, uuid.New(), "2")
	if cleared.status != http.StatusOK {
		t.Fatalf("cleared details patch = %d %s, want 200", cleared.status, cleared.body)
	}
	clearedDetails := personalDetailsFromResponse(t, cleared)
	if clearedDetails.FullName == nil || *clearedDetails.FullName != "" || len(clearedDetails.Details) != 0 {
		t.Fatalf("cleared details = %#v, want present empty fullName and empty details", clearedDetails)
	}
	clearedResource := decodeResumeResource(t, cleared)
	var rawDocument struct {
		PersonalDetails map[string]json.RawMessage `json:"personalDetails"`
	}
	if err := json.Unmarshal(clearedResource.Document, &rawDocument); err != nil {
		t.Fatalf("decode cleared document presence: %v", err)
	}
	if raw, present := rawDocument.PersonalDetails["details"]; !present || string(raw) != "[]" {
		t.Fatalf("cleared details JSON = %s present=%v, want explicit []", raw, present)
	}
}

func TestPersonalDetails_AuthorizationAndNoExistenceOracle(t *testing.T) {
	owner := newResumeAPITestHarness(t)
	attacker := newResumeAPITestHarness(t)
	created := owner.createResume(t)
	path := apiResumePath + "/" + created.ID.String() + "/personal-details"

	unauthenticated := owner.request(t, http.MethodPatch, path, strings.NewReader(`{"fullName":"probe"}`), false, false)
	assertResumeTestError(t, unauthenticated, http.StatusUnauthorized, "session_required")
	missingCSRF := owner.request(t, http.MethodPatch, path, strings.NewReader(`{"fullName":"probe"}`), true, false)
	assertResumeTestError(t, missingCSRF, http.StatusForbidden, "csrf_rejected")

	real := resumeRequest(t, attacker, http.MethodPatch, path, `{"fullName":"probe"}`,
		created.Revision, uuid.New(), "2")
	missing := resumeRequest(t, attacker, http.MethodPatch,
		apiResumePath+"/"+uuid.NewString()+"/personal-details", `{"fullName":"probe"}`,
		created.Revision, uuid.New(), "2")
	if real.status != http.StatusNotFound || real.status != missing.status || !reflect.DeepEqual(real.body, missing.body) ||
		!reflect.DeepEqual(stableResumeHeaders(real.header), stableResumeHeaders(missing.header)) {
		t.Fatalf("wrong-owner differs from missing:\nreal=%d %s %v\nmissing=%d %s %v",
			real.status, real.body, stableResumeHeaders(real.header),
			missing.status, missing.body, stableResumeHeaders(missing.header))
	}
	realRequestID := real.header.Get("X-Request-Id")
	missingRequestID := missing.header.Get("X-Request-Id")
	if _, err := uuid.Parse(realRequestID); err != nil || realRequestID == missingRequestID {
		t.Fatalf("request IDs real=%q missing=%q, want valid distinct UUIDs", realRequestID, missingRequestID)
	}
	if _, err := uuid.Parse(missingRequestID); err != nil {
		t.Fatalf("missing request ID %q is invalid: %v", missingRequestID, err)
	}
}

func TestPersonalDetails_RejectsBoundsSchemesPhotoAndUnknownFieldsWithoutWriting(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String() + "/personal-details"

	details := make([]map[string]any, 17)
	for i := range details {
		details[i] = map[string]any{
			"id":   fmt.Sprintf("10000000-0000-4000-8000-%012d", i),
			"type": "email", "value": "", "isHidden": false,
		}
	}
	tooMany, err := json.Marshal(map[string]any{"details": details})
	if err != nil {
		t.Fatalf("marshal 17 details: %v", err)
	}

	for _, test := range []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{name: "null object", body: `null`, wantStatus: http.StatusUnprocessableEntity, wantCode: "document_invalid"},
		{name: "wrong details type", body: `{"details":"x"}`, wantStatus: http.StatusUnprocessableEntity, wantCode: "document_invalid"},
		{name: "17 details", body: string(tooMany), wantStatus: http.StatusUnprocessableEntity, wantCode: "document_invalid"},
		{name: "non-https website", body: `{"details":[{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60","type":"website","value":"http://example.test","isHidden":false}]}`, wantStatus: http.StatusUnprocessableEntity, wantCode: "document_invalid"},
		{name: "photo", body: `{"photo":{"key":"resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo-0123456789abcdef0123456789abcdef.jpg"}}`, wantStatus: http.StatusUnprocessableEntity, wantCode: "document_invalid"},
		{name: "crop command", body: `{"crop":{"x":0,"y":0,"width":1,"height":1}}`, wantStatus: http.StatusBadRequest, wantCode: "request_invalid"},
		{name: "unknown", body: `{"fullName":"Ada","extra":true}`, wantStatus: http.StatusBadRequest, wantCode: "request_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, getErr := h.resumes.Get(h.ctx, h.userID, created.ID)
			if getErr != nil {
				t.Fatalf("load before: %v", getErr)
			}
			response := resumeRequest(t, h, http.MethodPatch, path, test.body, before.Revision, uuid.New(), "2")
			assertResumeTestError(t, response, test.wantStatus, test.wantCode)
			after, getErr := h.resumes.Get(h.ctx, h.userID, created.ID)
			if getErr != nil {
				t.Fatalf("load after: %v", getErr)
			}
			if after.Revision != before.Revision || !reflect.DeepEqual(after.Doc, before.Doc) || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("rejection wrote state: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestPersonalDetails_PreservesTransactionReadPhotoAndCrop(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	key := "resumes/" + created.ID.String() + "/photo-0123456789abcdef0123456789abcdef.png"
	doc := created.Doc
	doc.PersonalDetails.Photo = &schema.Photo{
		Key:  key,
		Crop: &schema.PhotoCrop{X: 0.1, Y: 0.2, Width: 0.7, Height: 0.6},
	}
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("attach photo: %v", err)
	}

	path := apiResumePath + "/" + created.ID.String() + "/personal-details"
	response := resumeRequest(t, h, http.MethodPatch, path, `{"fullName":"Grace","details":[]}`,
		revision, uuid.New(), "2")
	if response.status != http.StatusOK {
		t.Fatalf("patch with photo = %d %s, want 200", response.status, response.body)
	}
	got := personalDetailsFromResponse(t, response)
	if !reflect.DeepEqual(got.Photo, doc.PersonalDetails.Photo) {
		t.Fatalf("photo changed: got=%#v want=%#v", got.Photo, doc.PersonalDetails.Photo)
	}
}

func TestPersonalDetails_RacingPhotoReplacementCannotRestoreOldKey(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	oldKey := "resumes/" + created.ID.String() + "/photo-0123456789abcdef0123456789abcdef.jpg"
	newKey := "resumes/" + created.ID.String() + "/photo-abcdefabcdefabcdefabcdefabcdefab.png"
	doc := created.Doc
	doc.PersonalDetails.Photo = &schema.Photo{Key: oldKey}
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("seed old photo: %v", err)
	}

	path := apiResumePath + "/" + created.ID.String() + "/personal-details"
	req, err := http.NewRequestWithContext(h.ctx, http.MethodPatch, h.server.URL+path,
		strings.NewReader(`{"fullName":"racing details","details":[]}`))
	if err != nil {
		t.Fatalf("build racing request: %v", err)
	}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
	req.Header.Set("Content-Type", "application/json")

	start := make(chan struct{})
	httpResult := make(chan struct {
		status int
		body   []byte
		err    error
	}, 1)
	storeResult := make(chan error, 1)
	go func() {
		<-start
		response, requestErr := h.client.Do(req)
		if requestErr != nil {
			httpResult <- struct {
				status int
				body   []byte
				err    error
			}{err: requestErr}
			return
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr == nil {
			readErr = closeErr
		}
		httpResult <- struct {
			status int
			body   []byte
			err    error
		}{status: response.StatusCode, body: body, err: readErr}
	}()
	go func() {
		<-start
		replacement := doc
		replacement.PersonalDetails.Photo = &schema.Photo{Key: newKey}
		_, saveErr := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, replacement, revision)
		storeResult <- saveErr
	}()
	close(start)
	httpOutcome := <-httpResult
	storeErr := <-storeResult
	if httpOutcome.err != nil {
		t.Fatalf("racing HTTP request: %v", httpOutcome.err)
	}
	storeWon := storeErr == nil
	var storeMismatch *resume.RevisionMismatchError
	if !storeWon && !errors.As(storeErr, &storeMismatch) {
		t.Fatalf("racing photo save error = %v, want nil or revision mismatch", storeErr)
	}
	if httpOutcome.status != http.StatusOK && httpOutcome.status != http.StatusPreconditionFailed {
		t.Fatalf("racing details status = %d body=%s, want 200 or 412", httpOutcome.status, httpOutcome.body)
	}
	if (httpOutcome.status == http.StatusOK) == storeWon {
		t.Fatalf("race outcomes details=%d photoErr=%v, want exactly one winner", httpOutcome.status, storeErr)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload race winner: %v", err)
	}
	wantKey := oldKey
	if storeWon {
		wantKey = newKey
	}
	if stored.Doc.PersonalDetails.Photo == nil || stored.Doc.PersonalDetails.Photo.Key != wantKey {
		t.Fatalf("stored race photo = %#v, want key %q", stored.Doc.PersonalDetails.Photo, wantKey)
	}
}
