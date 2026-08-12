package resumeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
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
) (int64, error) {
	s.calls = append(s.calls, "save-document")
	s.completeWrites++
	s.savedDoc = doc
	s.expectedCAS = expected
	return expected + 1, s.saveErr
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

func TestPersistenceOperationsUseTheCompleteDocumentChokePoint(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	resumeID := uuid.New()
	expected := int64(9)
	for _, test := range []struct {
		name      string
		operation mutationOperation
		prepared  func(*operationStoreSpy) preparedInput
		wantCalls []string
	}{
		{
			name: "create sanitizes and validates before create",
			operation: createOperation{service: &Service{
				projector: docmigrate.NewIdentityProjector(),
			}},
			prepared: func(*operationStoreSpy) preparedInput {
				return preparedInput{Value: createPreparedInput{Document: hostileProfileDocument(t), Title: "created", Response: operationResponse}}
			},
			wantCalls: []string{"create"},
		},
		{
			name: "aggregate projects applies sanitizes validates then saves",
			operation: aggregateOperation{service: &Service{
				projector: docmigrate.NewIdentityProjector(),
			}},
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
			name: "metadata uses the same document choke point",
			operation: metadataOperation{service: &Service{
				projector: docmigrate.NewIdentityProjector(),
			}},
			prepared: func(spy *operationStoreSpy) preparedInput {
				spy.current = resume.Resume{ID: resumeID, UserID: userID, Revision: expected, Doc: loadMinimalDocument(t)}
				language := "en"
				return preparedInput{Value: metadataPreparedInput{
					aggregatePreparedInput: aggregatePreparedInput{
						ResumeID: resumeID,
						Apply: func(json.RawMessage) (json.RawMessage, error) {
							spy.calls = append(spy.calls, "apply")
							return json.Marshal(hostileProfileDocument(t))
						},
						Response: operationResponse,
					},
					Title: "updated", Language: &language,
				}}
			},
			wantCalls: []string{"get", "apply", "save-metadata-document"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			spy := &operationStoreSpy{}
			storeSpy := documentAwareOperationStore{operationStoreSpy: spy}
			switch operation := test.operation.(type) {
			case createOperation:
				operation.service.resumes = storeSpy
				test.operation = operation
			case aggregateOperation:
				operation.service.resumes = storeSpy
				test.operation = operation
			case metadataOperation:
				operation.service.resumes = storeSpy
				test.operation = operation
			}
			result, err := test.operation.Run(context.Background(), nil, mutationContext{
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
			if test.name == "create sanitizes and validates before create" {
				stored = spy.createdDoc
			}
			assertStoredDocumentSanitizedAndValid(t, stored)
		})
	}
}

func TestDeleteOperationUsesDeleteOnlyAndRunsHookAfterDelete(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	resumeID := uuid.New()
	expected := int64(4)
	spy := &operationStoreSpy{deleted: resume.Resume{ID: resumeID, UserID: userID, Revision: expected, Doc: loadMinimalDocument(t)}}
	service := &Service{resumes: documentAwareOperationStore{operationStoreSpy: spy}}
	hookErr := errors.New("deletion job failed")
	_, err := (deleteOperation{service: service}).Run(context.Background(), nil, mutationContext{
		UserID: userID, ExpectedRevision: &expected, WireVersion: docmigrate.CurrentVersion,
	}, preparedInput{Value: deletePreparedInput{
		ResumeID: resumeID,
		BeforeDelete: func(context.Context, *store.Queries, resume.Resume) error {
			spy.calls = append(spy.calls, "deletion-job")
			return hookErr
		},
		Response: operationResponse,
	}})
	if !errors.Is(err, hookErr) {
		t.Fatalf("error = %v, want deletion job error", err)
	}
	if !reflect.DeepEqual(spy.calls, []string{"delete", "deletion-job"}) {
		t.Fatalf("calls = %v, want delete then deletion-job", spy.calls)
	}
	if spy.completeWrites != 0 {
		t.Fatalf("complete document writes = %d, want none", spy.completeWrites)
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
