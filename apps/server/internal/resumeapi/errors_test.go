package resumeapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

func TestWriteResumeError_EnvelopeAndDetails(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeResumeError(rec, &clientError{
		Status:  http.StatusPreconditionFailed,
		Code:    "revision_mismatch",
		Message: "the resume changed",
		Details: map[string]any{"revision": "42"},
	})
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 || body["error"] == nil {
		t.Fatalf("body = %#v, want exactly one error key", body)
	}
	errorObject, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("error object = %#v, want object", body["error"])
	}
	if len(errorObject) != 3 || errorObject["code"] != "revision_mismatch" {
		t.Fatalf("error object = %#v", errorObject)
	}
}

func TestWriteStoredResponse_204HasNoRepresentation(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeStoredResponse(rec, resume.StoredResponse{
		Status:  http.StatusNoContent,
		Body:    []byte(`null`),
		Headers: map[string]string{"ETag": `"r2"`},
	})
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("response = status %d body %q, want 204 and zero bytes", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "" {
		t.Fatalf("Content-Type = %q, want absent", got)
	}
}

func TestErrorVocabularyCoversEveryResumeAPICallSite(t *testing.T) {
	t.Parallel()

	allowed := make(map[string]struct{}, len(productionErrorVocabulary)+len(genericErrorVocabulary))
	for _, vocabulary := range []map[string]struct{}{productionErrorVocabulary, genericErrorVocabulary} {
		for code := range vocabulary {
			allowed[code] = struct{}{}
		}
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob source: %v", err)
	}
	for _, filename := range files {
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", filename, parseErr)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch expression := node.(type) {
			case *ast.CompositeLit:
				identifier, ok := expression.Type.(*ast.Ident)
				if !ok || identifier.Name != "clientError" {
					return true
				}
				for _, element := range expression.Elts {
					pair, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := pair.Key.(*ast.Ident)
					if !ok || key.Name != "Code" {
						continue
					}
					assertAllowedErrorCode(t, filename, pair.Value, allowed)
				}
			case *ast.CallExpr:
				selector, ok := expression.Fun.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "WriteError" && len(expression.Args) >= 3 {
					assertAllowedErrorCode(t, filename, expression.Args[2], allowed)
				}
			}
			return true
		})
	}
}

func assertAllowedErrorCode(t *testing.T, filename string, expression ast.Expr, allowed map[string]struct{}) {
	t.Helper()
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		t.Errorf("%s has a non-literal error code", filename)
		return
	}
	code, err := strconv.Unquote(literal.Value)
	if err != nil {
		t.Errorf("%s has invalid error code literal %s: %v", filename, literal.Value, err)
		return
	}
	if _, ok := allowed[code]; !ok {
		t.Errorf("%s uses undeclared error code %q", filename, code)
	}
}

func TestWriteResumeErrorRejectsUndeclaredCodesAndDetails(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  *clientError
	}{
		{name: "undeclared code", err: &clientError{Status: http.StatusTeapot, Code: "surprise", Message: "surprise"}},
		{name: "details outside D7", err: &clientError{Status: http.StatusNotFound, Code: "resume_not_found", Message: "missing", Details: map[string]any{"secret": true}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeResumeError(rec, test.err)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			var envelope errorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if envelope.Error.Code != "internal_error" || envelope.Error.Details != nil {
				t.Fatalf("error = %#v, want opaque internal_error", envelope.Error)
			}
		})
	}
}

func TestDetailsVocabularyIsExactlyD7(t *testing.T) {
	t.Parallel()

	want := map[string]struct{}{
		"document_invalid": {}, "revision_mismatch": {}, "unsupported_schema_version": {},
		"media_invalid": {},
	}
	if !reflect.DeepEqual(detailsErrorVocabulary, want) {
		t.Fatalf("details vocabulary = %#v, want %#v", detailsErrorVocabulary, want)
	}
	for code := range detailsErrorVocabulary {
		if _, ok := productionErrorVocabulary[code]; !ok {
			t.Errorf("details code %q is not in the production vocabulary", code)
		}
	}
}

func TestWriteResumeError_MediaInvalidReasonIsClosed(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{
		"malformed", "animated", "dimensions", "orientation", "trailing_data", "normalization_failed",
	} {
		rec := httptest.NewRecorder()
		writeResumeError(rec, mediaInvalidError(reason))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("reason %q status = %d, want 422", reason, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	writeResumeError(rec, &clientError{
		Status: http.StatusUnprocessableEntity, Code: "media_invalid",
		Message: "image is invalid", Details: map[string]any{"reason": "malformed"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("generic-map details status = %d, want 422", rec.Code)
	}

	for _, details := range []any{
		map[string]string{"reason": "decoder internals"},
		map[string]string{"reason": "malformed", "extra": "detail"},
		map[string]any{"reason": 1},
	} {
		rec := httptest.NewRecorder()
		writeResumeError(rec, &clientError{
			Status: http.StatusUnprocessableEntity, Code: "media_invalid",
			Message: "image is invalid", Details: details,
		})
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("details %#v status = %d, want 500", details, rec.Code)
		}
	}
}

func TestMapMutationError_RevisionMismatchEmitsRequestedWireDocument(t *testing.T) {
	t.Parallel()

	doc := loadMinimalDocument(t)
	projector := docmigrate.NewIdentityProjector()
	service := &Service{projector: projector}
	canonical, err := resume.AssembleCanonical(doc)
	if err != nil {
		t.Fatalf("assemble canonical winner: %v", err)
	}
	for _, version := range []int32{1, docmigrate.CurrentVersion} {
		version := version
		t.Run(wireVersionString(version), func(t *testing.T) {
			t.Parallel()
			wantDocument, emitErr := projector.EmitWire(canonical, version)
			if emitErr != nil {
				t.Fatalf("emit fresh scoped winner: %v", emitErr)
			}
			mapped := service.mapMutationErrorAtWire(&resume.RevisionMismatchError{
				CurrentRevision: 44,
				Current:         resume.Resume{Revision: 44, Doc: doc},
			}, version)
			if mapped.Status != http.StatusPreconditionFailed || mapped.Code != "revision_mismatch" {
				t.Fatalf("mapped error = (%d, %q), want (412, revision_mismatch)", mapped.Status, mapped.Code)
			}
			if mapped.Headers[wireVersionHeader] != wireVersionString(version) {
				t.Fatalf("schema header = %q", mapped.Headers[wireVersionHeader])
			}
			details, ok := mapped.Details.(map[string]any)
			if !ok || details["revision"] != "44" {
				t.Fatalf("details = %#v, want revision 44", mapped.Details)
			}
			gotDocument, ok := details["document"].(json.RawMessage)
			if !ok || !bytes.Equal(gotDocument, wantDocument) {
				t.Fatalf("winner document = %s, fresh emitted winner = %s", gotDocument, wantDocument)
			}
		})
	}
}

func TestMapMutationError_DocumentIssuesAlwaysCarryPathCodeAndMessage(t *testing.T) {
	t.Parallel()

	doc := loadMinimalDocument(t)
	tooLong := strings.Repeat("x", 161)
	doc.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{
			ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", JobTitle: &tooLong,
		}}),
	}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	validationErr := resume.ValidateForStore(doc)

	for name, source := range map[string]error{
		"store":     validationErr,
		"projector": fmt.Errorf("%w: %w", docmigrate.ErrInvalidDocument, validationErr),
	} {
		t.Run(name, func(t *testing.T) {
			mapped := mapMutationError(source)
			details, ok := mapped.Details.(map[string]any)
			if !ok {
				t.Fatalf("details = %#v", mapped.Details)
			}
			issues, ok := details["issues"].([]map[string]string)
			if !ok || len(issues) == 0 {
				t.Fatalf("issues = %#v", details["issues"])
			}
			for _, issue := range issues {
				if issue["path"] == "" || issue["code"] == "" || issue["message"] == "" {
					t.Fatalf("incomplete issue = %#v", issue)
				}
			}
		})
	}
}
