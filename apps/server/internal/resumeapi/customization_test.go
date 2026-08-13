package resumeapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

func TestCustomizationAllowlistMatchesEmbeddedSchema(t *testing.T) {
	t.Parallel()

	if err := validateCustomizationAllowlist(schema.RawSchema, fixedCustomizationAllowlist); err != nil {
		t.Fatalf("fixed allowlist does not match the embedded schema: %v", err)
	}
	if err := validateCustomizationValueKinds(schema.RawSchema, customizationSetValueKinds); err != nil {
		t.Fatalf("fixed value kinds do not match the embedded schema: %v", err)
	}
	wrongKinds := make(map[string]customizationValueKind, len(customizationSetValueKinds))
	for path, kind := range customizationSetValueKinds {
		wrongKinds[path] = kind
	}
	wrongKinds["font.baseSizePx"] = customizationString
	if err := validateCustomizationValueKinds(schema.RawSchema, wrongKinds); err == nil {
		t.Fatal("value-kind validation succeeded after schema classification drift")
	}

	mutations := []struct {
		name string
		edit func(customizationAllowlist) customizationAllowlist
	}{
		{"missing set pair", func(a customizationAllowlist) customizationAllowlist {
			delete(a.Set, "font.family")
			return a
		}},
		{"undeclared set pair", func(a customizationAllowlist) customizationAllowlist {
			a.Set["font.secret"] = struct{}{}
			return a
		}},
		{"required property unsettable", func(a customizationAllowlist) customizationAllowlist {
			a.Unset["colors.primary"] = struct{}{}
			return a
		}},
		{"optional object root missing", func(a customizationAllowlist) customizationAllowlist {
			delete(a.Unset, "spacing.pageMargin")
			return a
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			if err := validateCustomizationAllowlist(schema.RawSchema, mutation.edit(cloneCustomizationAllowlist(fixedCustomizationAllowlist))); err == nil {
				t.Fatal("validation succeeded after allowlist drift")
			}
		})
	}

	for _, path := range []string{"colors.accent", "colors.surface", "layout.surfaceTarget"} {
		if _, ok := fixedCustomizationAllowlist.Set[path]; !ok {
			t.Errorf("optional leaf %q missing from set allowlist", path)
		}
		if _, ok := fixedCustomizationAllowlist.Unset[path]; !ok {
			t.Errorf("optional leaf %q missing from unset allowlist", path)
		}
	}
}

func cloneCustomizationAllowlist(source customizationAllowlist) customizationAllowlist {
	clone := customizationAllowlist{Set: make(customizationPathSet, len(source.Set)), Unset: make(customizationPathSet, len(source.Unset))}
	for path := range source.Set {
		clone.Set[path] = struct{}{}
	}
	for path := range source.Unset {
		clone.Unset[path] = struct{}{}
	}
	return clone
}

func TestCustomizationDeniedPathsRejectWholeBatch(t *testing.T) {
	t.Parallel()

	document := customizationTestDocument(t)
	denied := []string{
		"unknown.leaf", "colors", "layout", "layout.sections", "layout.sections.main",
		"layout.sections.sidebar.0", "__proto__.polluted", "constructor.prototype", "prototype.x",
		"colors..accent", ".colors.accent", "colors.accent.", "colors.0", strings.Repeat("a", 257),
		"colors.аccent", // Cyrillic small a.
	}
	for _, path := range denied {
		t.Run(path, func(t *testing.T) {
			got, err := applyCustomizationDeltas(document, []customizationDelta{
				{Op: customizationSet, Path: "font.baseSizePx", Value: json.RawMessage(`18`)},
				{Op: customizationSet, Path: path, Value: json.RawMessage(`1`)},
			})
			var client *clientError
			if !asClientError(err, &client) || client.Code != "customization_path_denied" || client.Status != http.StatusUnprocessableEntity {
				t.Fatalf("apply denied path error = %v, want 422 customization_path_denied", err)
			}
			if got != nil {
				t.Fatalf("denied batch returned a document: %s", got)
			}
		})
	}
}

func TestCustomizationDeltasApplyInOrderAndPreserveUnrelatedSubtrees(t *testing.T) {
	t.Parallel()

	document := customizationTestDocument(t)
	beforeContent := customizationRawField(t, document, "content")
	beforePersonal := customizationRawField(t, document, "personalDetails")
	deltas := []customizationDelta{
		{Op: customizationSet, Path: "font.baseSizePx", Value: json.RawMessage(`17`)},
		{Op: customizationSet, Path: "font.baseSizePx", Value: json.RawMessage(`18`)},
		{Op: customizationSet, Path: "colors.accent", Value: json.RawMessage(`"#aabbcc"`)},
		{Op: customizationUnset, Path: "colors.accent"},
		{Op: customizationSet, Path: "colors.surface", Value: json.RawMessage(`"#112233"`)},
		{Op: customizationUnset, Path: "colors.surface"},
		{Op: customizationSet, Path: "layout.surfaceTarget", Value: json.RawMessage(`"header"`)},
		{Op: customizationUnset, Path: "layout.surfaceTarget"},
		{Op: customizationUnset, Path: "spacing.pageMargin"},
		{Op: customizationSet, Path: "spacing.pageMargin.x", Value: json.RawMessage(`12.5`)},
		{Op: customizationSet, Path: "spacing.pageMargin.y", Value: json.RawMessage(`14`)},
		{Op: customizationUnset, Path: "header"},
		{Op: customizationSet, Path: "header.align", Value: json.RawMessage(`"center"`)},
		{Op: customizationSet, Path: "header.detailsLayout", Value: json.RawMessage(`"stacked"`)},
		{Op: customizationSet, Path: "header.iconStyle", Value: json.RawMessage(`"outline"`)},
	}

	got, err := applyCustomizationDeltas(document, deltas)
	if err != nil {
		t.Fatalf("apply deltas: %v", err)
	}
	if !bytes.Equal(customizationRawField(t, got, "content"), beforeContent) {
		t.Fatal("content subtree bytes changed")
	}
	if !bytes.Equal(customizationRawField(t, got, "personalDetails"), beforePersonal) {
		t.Fatal("personalDetails subtree bytes changed")
	}

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	customization, ok := decoded["customization"].(map[string]any)
	if !ok {
		t.Fatalf("customization = %T, want object", decoded["customization"])
	}
	font, ok := customization["font"].(map[string]any)
	if !ok {
		t.Fatalf("font = %T, want object", customization["font"])
	}
	if font["baseSizePx"] != float64(18) {
		t.Fatalf("last delta did not win: baseSizePx = %v", font["baseSizePx"])
	}
	colors, ok := customization["colors"].(map[string]any)
	if !ok {
		t.Fatalf("colors = %T, want object", customization["colors"])
	}
	for _, key := range []string{"accent", "surface"} {
		if _, exists := colors[key]; exists {
			t.Errorf("colors.%s remains present", key)
		}
	}
	layout, ok := customization["layout"].(map[string]any)
	if !ok {
		t.Fatalf("layout = %T, want object", customization["layout"])
	}
	if _, exists := layout["surfaceTarget"]; exists {
		t.Error("layout.surfaceTarget remains present")
	}
	spacing, ok := customization["spacing"].(map[string]any)
	if !ok {
		t.Fatalf("spacing = %T, want object", customization["spacing"])
	}
	if want := map[string]any{"x": 12.5, "y": float64(14)}; !reflect.DeepEqual(spacing["pageMargin"], want) {
		t.Errorf("pageMargin = %#v, want %#v", spacing["pageMargin"], want)
	}
	if want := map[string]any{"align": "center", "detailsLayout": "stacked", "iconStyle": "outline"}; !reflect.DeepEqual(customization["header"], want) {
		t.Errorf("header = %#v, want %#v", customization["header"], want)
	}

	var typed schema.Resume
	if err := json.Unmarshal(got, &typed); err != nil {
		t.Fatalf("decode typed result: %v", err)
	}
	if err := resume.ValidateForStore(typed); err != nil {
		t.Fatalf("reconstructed document is invalid: %v", err)
	}
}

func TestCustomizationHTTPAtomicDenialValidationReplayAndCAS(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String() + "/customization"

	request := func(body, key string, revision int64) testHTTPResponse {
		t.Helper()
		req, err := http.NewRequestWithContext(h.ctx, http.MethodPatch, h.server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.AddCookie(h.cookie)
		req.Header.Set("Origin", resumeAPITestOrigin)
		req.Header.Set("X-CSRF-Token", h.csrfToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
		response, err := h.client.Do(req)
		if err != nil {
			t.Fatalf("perform request: %v", err)
		}
		return snapshotHTTPResponse(t, response)
	}

	beforeDoc, err := resume.AssembleCanonical(created.Doc)
	if err != nil {
		t.Fatalf("assemble before document: %v", err)
	}
	beforeRecords := h.snapshotUserTable(t, "idempotency_records")
	denied := request(`{"deltas":[{"op":"set","path":"font.baseSizePx","value":18},{"op":"set","path":"layout.sections.main","value":[]}]}`, uuid.NewString(), created.Revision)
	assertRouteError(t, denied, http.StatusUnprocessableEntity, "customization_path_denied")
	customizationAssertStoredUnchanged(t, h, created, beforeDoc, beforeRecords)

	invalidValues := []struct {
		path  string
		value string
	}{
		{"font.baseSizePx", `"large"`},
		{"colors.primary", `"red"`},
		{"spacing.pageMargin.x", `41`},
		{"layout.columns", `3`},
	}
	for _, invalidValue := range invalidValues {
		body := fmt.Sprintf(`{"deltas":[{"op":"set","path":%q,"value":%s}]}`, invalidValue.path, invalidValue.value)
		invalid := request(body, uuid.NewString(), created.Revision)
		assertRouteError(t, invalid, http.StatusUnprocessableEntity, "document_invalid")
		var invalidEnvelope errorEnvelope
		if decodeErr := json.Unmarshal(invalid.body, &invalidEnvelope); decodeErr != nil {
			t.Fatalf("decode invalid response: %v", decodeErr)
		}
		details, ok := invalidEnvelope.Error.Details.(map[string]any)
		if !ok || !strings.Contains(fmt.Sprint(details), invalidValue.path) {
			t.Fatalf("validation details = %#v, want issue naming %s", invalidEnvelope.Error.Details, invalidValue.path)
		}
		customizationAssertStoredUnchanged(t, h, created, beforeDoc, beforeRecords)
	}

	multipleInvalid := request(`{"deltas":[{"op":"set","path":"font.baseSizePx","value":99},{"op":"set","path":"spacing.lineHeight","value":99}]}`, uuid.NewString(), created.Revision)
	assertRouteError(t, multipleInvalid, http.StatusUnprocessableEntity, "document_invalid")
	var multipleEnvelope errorEnvelope
	if decodeErr := json.Unmarshal(multipleInvalid.body, &multipleEnvelope); decodeErr != nil {
		t.Fatalf("decode multiple-invalid response: %v", decodeErr)
	}
	details, ok := multipleEnvelope.Error.Details.(map[string]any)
	if !ok {
		t.Fatalf("multiple-invalid details = %#v, want object", multipleEnvelope.Error.Details)
	}
	renderedDetails := fmt.Sprint(details)
	for _, path := range []string{"customization.font.baseSizePx", "customization.spacing.lineHeight"} {
		if !strings.Contains(renderedDetails, path) {
			t.Fatalf("multiple-invalid details = %#v, want issue naming %s", details, path)
		}
	}
	customizationAssertStoredUnchanged(t, h, created, beforeDoc, beforeRecords)

	for _, deniedBody := range []string{
		`{"deltas":[{"op":"unset","path":"colors.primary"}]}`,
		`{"deltas":[{"op":"set","path":"spacing.pageMargin","value":{"x":1,"y":2}}]}`,
	} {
		denied := request(deniedBody, uuid.NewString(), created.Revision)
		assertRouteError(t, denied, http.StatusUnprocessableEntity, "customization_path_denied")
		customizationAssertStoredUnchanged(t, h, created, beforeDoc, beforeRecords)
	}

	key := uuid.NewString()
	body := `{"deltas":[{"op":"set","path":"font.baseSizePx","value":18}]}`
	first := request(body, key, created.Revision)
	if first.status != http.StatusOK {
		t.Fatalf("first write status = %d, want 200 (body=%s)", first.status, first.body)
	}
	if first.header.Get("ETag") != fmt.Sprintf(`"r%d"`, created.Revision+1) || first.header.Get(wireVersionHeader) != wireVersionString(docmigrate.CurrentVersion) {
		t.Fatalf("first write headers = %#v", first.header)
	}
	getRequest, err := http.NewRequestWithContext(h.ctx, http.MethodGet, h.server.URL+apiResumePath+"/"+created.ID.String(), nil)
	if err != nil {
		t.Fatalf("build GET request: %v", err)
	}
	getRequest.AddCookie(h.cookie)
	getResponse, err := h.client.Do(getRequest)
	if err != nil {
		t.Fatalf("perform GET request: %v", err)
	}
	get := snapshotHTTPResponse(t, getResponse)
	if get.status != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body=%s)", get.status, get.body)
	}
	firstDocument := customizationResponseDocument(t, first.body)
	getDocument := customizationResponseDocument(t, get.body)
	if !customizationJSONEqual(t, firstDocument, getDocument) {
		t.Fatalf("write and GET documents differ:\nwrite=%s\nget=%s", firstDocument, getDocument)
	}
	replay := request(body, key, created.Revision)
	if replay.status != first.status || !bytes.Equal(replay.body, first.body) || replay.header.Get("ETag") != first.header.Get("ETag") {
		t.Fatalf("replay differs: first=%d %s replay=%d %s", first.status, first.body, replay.status, replay.body)
	}
	reused := request(`{"deltas":[{"op":"set","path":"font.baseSizePx","value":19}]}`, key, created.Revision)
	assertRouteError(t, reused, http.StatusConflict, "idempotency_key_reuse")
	stale := request(`{"deltas":[{"op":"set","path":"font.baseSizePx","value":16}]}`, uuid.NewString(), created.Revision)
	assertRouteError(t, stale, http.StatusPreconditionFailed, "revision_mismatch")

	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("get stored resume: %v", err)
	}
	if stored.Revision != created.Revision+1 || stored.Doc.Customization.Font.BaseSizePx != 18 {
		t.Fatalf("stored resume revision/baseSizePx = %d/%d, want %d/18", stored.Revision, stored.Doc.Customization.Font.BaseSizePx, created.Revision+1)
	}
}

func TestCustomizationDeltaCountAndUnionBoundaries(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String() + "/customization"

	deltas := make([]string, 100)
	for i := range deltas {
		deltas[i] = `{"op":"set","path":"font.baseSizePx","value":16}`
	}
	request := func(body string, revision int64) testHTTPResponse {
		t.Helper()
		req, err := http.NewRequestWithContext(h.ctx, http.MethodPatch, h.server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.AddCookie(h.cookie)
		req.Header.Set("Origin", resumeAPITestOrigin)
		req.Header.Set("X-CSRF-Token", h.csrfToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", uuid.NewString())
		req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
		response, err := h.client.Do(req)
		if err != nil {
			t.Fatalf("perform request: %v", err)
		}
		return snapshotHTTPResponse(t, response)
	}

	atLimit := request(`{"deltas":[`+strings.Join(deltas, ",")+`]}`, created.Revision)
	if atLimit.status != http.StatusOK {
		t.Fatalf("100 deltas status = %d, want 200 (body=%s)", atLimit.status, atLimit.body)
	}
	overLimit := request(`{"deltas":[`+strings.Join(append(deltas, deltas[0]), ",")+`]}`, created.Revision+1)
	assertRouteError(t, overLimit, http.StatusUnprocessableEntity, "document_invalid")

	badUnionCases := []string{
		`{"deltas":[{"op":"set","path":"colors.accent"}]}`,
		`{"deltas":[{"op":"set","path":"colors.accent","value":null}]}`,
		`{"deltas":[{"op":"unset","path":"colors.accent","value":"#ffffff"}]}`,
		`{"deltas":[{"op":"replace","path":"colors.accent"}]}`,
	}
	for _, body := range badUnionCases {
		response := request(body, created.Revision+1)
		assertRouteError(t, response, http.StatusBadRequest, "request_invalid")
	}
}

func TestCustomizationMutationHeadersPrecedeTargetAndVersionValidation(t *testing.T) {
	h := newResumeAPITestHarness(t)
	req, err := http.NewRequestWithContext(h.ctx, http.MethodPatch,
		h.server.URL+apiResumePath+"/not-a-uuid/customization", strings.NewReader(`{"deltas":[{"op":"set","path":"font.baseSizePx","value":16}]}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set("X-CSRF-Token", h.csrfToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(wireVersionHeader, "999")
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	assertRouteError(t, snapshotHTTPResponse(t, response), http.StatusBadRequest, "idempotency_key_required")
}

func customizationTestDocument(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := resume.AssembleCanonical(loadMinimalDocument(t))
	if err != nil {
		t.Fatalf("assemble test document: %v", err)
	}
	return raw
}

func customizationRawField(t *testing.T, raw json.RawMessage, field string) json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode document fields: %v", err)
	}
	return fields[field]
}

func customizationResponseDocument(t *testing.T, body []byte) json.RawMessage {
	t.Helper()
	var envelope struct {
		Data struct {
			Document json.RawMessage `json:"document"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode resume response: %v", err)
	}
	if len(envelope.Data.Document) == 0 {
		t.Fatalf("resume response has no document: %s", body)
	}
	return envelope.Data.Document
}

func customizationJSONEqual(t *testing.T, left, right json.RawMessage) bool {
	t.Helper()
	var leftValue, rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		t.Fatalf("decode left JSON value: %v", err)
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		t.Fatalf("decode right JSON value: %v", err)
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func customizationAssertStoredUnchanged(t *testing.T, h *resumeAPITestHarness, created resume.Resume, beforeDoc []byte, beforeRecords string) {
	t.Helper()
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("get resume after rejection: %v", err)
	}
	afterDoc, err := resume.AssembleCanonical(stored.Doc)
	if err != nil {
		t.Fatalf("assemble document after rejection: %v", err)
	}
	if stored.Revision != created.Revision || !bytes.Equal(afterDoc, beforeDoc) {
		t.Fatalf("rejected write changed row: revision %d -> %d, document equal=%t", created.Revision, stored.Revision, bytes.Equal(afterDoc, beforeDoc))
	}
	if afterRecords := h.snapshotUserTable(t, "idempotency_records"); afterRecords != beforeRecords {
		t.Fatalf("rejected write changed idempotency records: before=%q after=%q", beforeRecords, afterRecords)
	}
}

func asClientError(err error, target **clientError) bool {
	if err == nil {
		return false
	}
	client := &clientError{}
	ok := errors.As(err, &client)
	if ok {
		*target = client
	}
	return ok
}
