package resumeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

type operationStoreSpy struct {
	calls          []string
	current        resume.Resume
	createdDoc     schema.Resume
	savedDoc       schema.Resume
	deleted        resume.Resume
	createErr      error
	getErr         error
	saveErr        error
	deleteErr      error
	metadataTitle  string
	metadataLng    *string
	expectedCAS    int64
	completeWrites int
}

type documentAwareOperationStore struct{ *operationStoreSpy }

func (s documentAwareOperationStore) CreateTx(_ context.Context, _ *store.Queries, userID uuid.UUID,
	title string, language *string, doc schema.Resume,
) (resume.Resume, error) {
	s.calls = append(s.calls, "create")
	s.completeWrites++
	s.createdDoc = doc
	if s.createErr != nil {
		return resume.Resume{}, s.createErr
	}
	return resume.Resume{ID: uuid.New(), UserID: userID, Title: title, Lng: language, Revision: 1, Doc: doc}, nil
}

func (s documentAwareOperationStore) GetTx(context.Context, *store.Queries, uuid.UUID, uuid.UUID) (resume.Resume, error) {
	s.calls = append(s.calls, "get")
	return s.current, s.getErr
}

func (s documentAwareOperationStore) SaveDocumentTx(_ context.Context, _ *store.Queries, _ uuid.UUID,
	_ uuid.UUID, doc schema.Resume, expected int64,
) (resume.Resume, error) {
	s.calls = append(s.calls, "save-document")
	s.completeWrites++
	s.savedDoc = doc
	s.expectedCAS = expected
	s.current.Revision = expected + 1
	s.current.Doc = doc
	s.current.UpdatedAt = time.Unix(2, 0).UTC()
	return s.current, s.saveErr
}

// skewedSaveStore returns a saved row whose revision is not the expected
// revision plus one.
type skewedSaveStore struct{ documentAwareOperationStore }

func (s skewedSaveStore) SaveDocumentTx(ctx context.Context, qtx *store.Queries, userID uuid.UUID,
	id uuid.UUID, doc schema.Resume, expected int64,
) (resume.Resume, error) {
	saved, err := s.documentAwareOperationStore.SaveDocumentTx(ctx, qtx, userID, id, doc, expected)
	saved.Revision++
	return saved, err
}

func (s documentAwareOperationStore) SaveMetadataAndDocumentTx(_ context.Context, _ *store.Queries, _ uuid.UUID,
	_ uuid.UUID, title string, language *string, doc schema.Resume, expected int64,
) (int64, error) {
	s.calls = append(s.calls, "save-metadata-document")
	s.completeWrites++
	s.metadataTitle = title
	s.metadataLng = language
	s.savedDoc = doc
	s.expectedCAS = expected
	return expected + 1, s.saveErr
}

func (s documentAwareOperationStore) DeleteTx(_ context.Context, _ *store.Queries, _ uuid.UUID,
	_ uuid.UUID, expected int64,
) (resume.Resume, error) {
	s.calls = append(s.calls, "delete")
	s.expectedCAS = expected
	return s.deleted, s.deleteErr
}

func hostileProfileDocument(t *testing.T) schema.Resume {
	t.Helper()
	doc := loadMinimalDocument(t)
	hostile := `<script>alert(1)</script><p>safe</p>`
	doc.Content = map[string]schema.Section{
		"profile": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{
			ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", Text: &hostile,
		}}),
	}
	doc.Customization.Layout.Sections.Main = []string{"profile"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	return doc
}

func assertStoredDocumentSanitizedAndValid(t *testing.T, doc schema.Resume) {
	t.Helper()
	if err := resume.ValidateForStore(doc); err != nil {
		t.Fatalf("stored document failed validation: %v", err)
	}
	text := doc.Content["profile"].ProfileEntries[0].Text
	if text == nil || *text != "<p>safe</p>" {
		t.Fatalf("stored rich text = %v, want sanitized <p>safe</p>", text)
	}
}

func operationResponse(resume.Resume, schema.Resume, int32) (resume.StoredResponse, error) {
	return resume.StoredResponse{Status: http.StatusNoContent}, nil
}

func TestMutationOperationsPersistOnlyCompleteDocuments(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	resumeID := uuid.New()
	expected := int64(9)
	for _, test := range []struct {
		name      string
		kind      operationKind
		prepared  func(*operationStoreSpy) preparedInput
		wantCalls []string
	}{
		{
			name: "create sanitizes and validates before create",
			kind: operationCreate,
			prepared: func(*operationStoreSpy) preparedInput {
				return preparedInput{Value: createPreparedInput{Document: hostileProfileDocument(t), Title: "created", Response: operationResponse}}
			},
			wantCalls: []string{"create"},
		},
		{
			name: "aggregate projects applies sanitizes validates then saves",
			kind: operationAggregate,
			prepared: func(spy *operationStoreSpy) preparedInput {
				spy.current = resume.Resume{ID: resumeID, UserID: userID, Revision: expected, Doc: loadMinimalDocument(t)}
				return preparedInput{Value: aggregatePreparedInput{
					ResumeID: resumeID,
					Apply: func(raw json.RawMessage) (json.RawMessage, error) {
						spy.calls = append(spy.calls, "apply")
						doc := hostileProfileDocument(t)
						return json.Marshal(doc)
					},
					Response: operationResponse,
				}}
			},
			wantCalls: []string{"get", "apply", "save-document"},
		},
		{
			name: "metadata sanitizes validates and rereads fresh metadata",
			kind: operationMetadata,
			prepared: func(spy *operationStoreSpy) preparedInput {
				spy.current = resume.Resume{ID: resumeID, UserID: userID, Revision: expected, Doc: hostileProfileDocument(t)}
				language := "en"
				return preparedInput{Value: resumeMetadataPrepared{
					ResumeID:        resumeID,
					TitlePresent:    true,
					Title:           "updated",
					LanguagePresent: true,
					Language:        &language,
					Response:        operationResponse,
				}}
			},
			wantCalls: []string{"get", "save-metadata-document", "get"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			spy := &operationStoreSpy{}
			service := &Service{
				projector: docmigrate.NewIdentityProjector(),
				resumes:   documentAwareOperationStore{operationStoreSpy: spy},
			}
			operation := test.kind.build(service)
			if operation == nil {
				t.Fatalf("%s operation factory returned nil", test.name)
			}
			result, err := operation.Run(context.Background(), nil, mutationContext{
				UserID: userID, ExpectedRevision: &expected, WireVersion: docmigrate.CurrentVersion,
			}, test.prepared(spy))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if result.Response.Status != http.StatusNoContent {
				t.Fatalf("response status = %d, want 204", result.Response.Status)
			}
			if !reflect.DeepEqual(spy.calls, test.wantCalls) {
				t.Fatalf("calls = %v, want %v", spy.calls, test.wantCalls)
			}
			if spy.completeWrites != 1 {
				t.Fatalf("complete document writes = %d, want 1", spy.completeWrites)
			}
			stored := spy.savedDoc
			if test.kind == operationCreate {
				stored = spy.createdDoc
			}
			assertStoredDocumentSanitizedAndValid(t, stored)
		})
	}
}

func TestDeleteOperationUsesPublicCAS(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	response := h.mutationRequest(t, http.MethodDelete, apiResumePath+"/"+created.ID.String(), nil, created.Revision, uuid.NewString())
	if response.status != http.StatusNoContent || len(response.body) != 0 {
		t.Fatalf("delete response = %d body=%q, want bodyless 204", response.status, response.body)
	}
	if _, err := h.resumes.Get(h.ctx, h.userID, created.ID); !errors.Is(err, resume.ErrNotFound) {
		t.Fatalf("deleted resume lookup error = %v, want resume.ErrNotFound", err)
	}
}

func TestBeforeSaveCannotMutateTheValidatedStoredDocument(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	resumeID := uuid.New()
	expected := int64(7)
	spy := &operationStoreSpy{current: resume.Resume{
		ID: resumeID, UserID: userID, Revision: expected, Doc: loadMinimalDocument(t),
	}}
	sanitizerCalls := 0
	service := &Service{
		resumes:   documentAwareOperationStore{operationStoreSpy: spy},
		projector: docmigrate.NewIdentityProjector(),
		sanitizeDocument: func(document schema.Resume) schema.Resume {
			sanitizerCalls++
			return sanitizeDocument(document)
		},
	}
	_, err := (aggregateOperation{service: service}).Run(context.Background(), nil, mutationContext{
		UserID: userID, ExpectedRevision: &expected, WireVersion: docmigrate.CurrentVersion,
	}, preparedInput{Value: aggregatePreparedInput{
		ResumeID: resumeID,
		Apply: func(json.RawMessage) (json.RawMessage, error) {
			return json.Marshal(hostileProfileDocument(t))
		},
		BeforeSave: func(_ context.Context, _ *store.Queries, _ resume.Resume, document schema.Resume) error {
			overBoundHostile := `<script>alert(1)</script><p>` + strings.Repeat("x", schema.MaxRichTextBytes+1) + `</p>`
			document.Content["profile"] = schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{
				ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", Text: &overBoundHostile,
			}})
			return nil
		},
		Response: operationResponse,
	}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertStoredDocumentSanitizedAndValid(t, spy.savedDoc)
	if sanitizerCalls != 1 {
		t.Fatalf("sanitizer calls = %d, want exactly 1", sanitizerCalls)
	}
}

// TestDocumentSaveOperationsRespondWithTheSavedRow proves the aggregate and
// personal-details saves answer with the row the write returned, without a
// reread, and fail when that row's revision is not the expected revision plus
// one.
func TestDocumentSaveOperationsRespondWithTheSavedRow(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	resumeID := uuid.New()
	expected := int64(4)
	for _, test := range []struct {
		name     string
		run      func(*Service) mutationOperation
		prepared func(mutationResponseBuilder) any
	}{
		{
			name: "aggregate",
			run:  func(service *Service) mutationOperation { return aggregateOperation{service: service} },
			prepared: func(response mutationResponseBuilder) any {
				return aggregatePreparedInput{
					ResumeID: resumeID, Response: response,
					Apply: func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil },
				}
			},
		},
		{
			name: "personal details",
			run:  func(service *Service) mutationOperation { return personalDetailsMutation{service: service} },
			prepared: func(response mutationResponseBuilder) any {
				return personalDetailsPrepared{
					ResumeID: resumeID, Response: response,
					Replacement: map[string]json.RawMessage{"fullName": json.RawMessage(`"Ada"`), "details": json.RawMessage(`[]`)},
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := func(boundary resumeBoundary, spy *operationStoreSpy) (resume.Resume, error) {
				spy.current = resume.Resume{ID: resumeID, UserID: userID, Revision: expected, Doc: loadMinimalDocument(t)}
				var responded resume.Resume
				respond := func(row resume.Resume, _ schema.Resume, _ int32) (resume.StoredResponse, error) {
					responded = row
					return resume.StoredResponse{Status: http.StatusOK}, nil
				}
				service := &Service{projector: docmigrate.NewIdentityProjector(), resumes: boundary}
				_, err := test.run(service).Run(context.Background(), nil, mutationContext{
					UserID: userID, ExpectedRevision: &expected, WireVersion: docmigrate.CurrentVersion,
				}, preparedInput{Value: test.prepared(respond)})
				return responded, err
			}

			spy := &operationStoreSpy{}
			responded, err := run(documentAwareOperationStore{operationStoreSpy: spy}, spy)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !reflect.DeepEqual(spy.calls, []string{"get", "save-document"}) {
				t.Fatalf("calls = %v, want one read before the save and none after", spy.calls)
			}
			if responded.Revision != expected+1 || !responded.UpdatedAt.Equal(time.Unix(2, 0)) {
				t.Fatalf("response row = revision %d updated %v, want the saved row", responded.Revision, responded.UpdatedAt)
			}

			skewed := &operationStoreSpy{}
			if _, err := run(skewedSaveStore{documentAwareOperationStore{operationStoreSpy: skewed}}, skewed); err == nil {
				t.Fatal("Run accepted a saved revision that is not the expected revision plus one")
			}
		})
	}
}
