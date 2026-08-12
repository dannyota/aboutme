package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

func TestPrepareDocumentForPersistence_SanitizesBeforeBounds(t *testing.T) {
	t.Parallel()

	doc := loadMinimalDocument(t)
	removedWrapper := strings.Repeat("<!--"+strings.Repeat("x", 100)+"-->", 200) + "<p>ok</p>"
	doc.Content = map[string]schema.Section{
		"profile": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{
			ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", Text: &removedWrapper,
		}}),
	}
	doc.Customization.Layout.Sections.Main = []string{"profile"}
	doc.Customization.Layout.Sections.Sidebar = []string{}

	prepared, err := prepareDocumentForPersistence(doc)
	if err != nil {
		t.Fatalf("prepareDocumentForPersistence rejected content that is bounded after sanitizing: %v", err)
	}
	if got := *prepared.Content["profile"].ProfileEntries[0].Text; got != "<p>ok</p>" {
		t.Fatalf("persisted rich text = %q, want %q", got, "<p>ok</p>")
	}

	over := "<p>" + strings.Repeat("a", schema.MaxRichTextBytes+1) + "</p>"
	doc.Content["profile"] = schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{
		ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", Text: &over,
	}})
	_, err = prepareDocumentForPersistence(doc)
	var validationError *resume.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("over-bound sanitized content error = %v, want *resume.ValidationError", err)
	}
}

type candidateBackend struct {
	deletes     int
	deletedKey  string
	hadDeadline bool
	deleteErr   error
}

func (*candidateBackend) Put(context.Context, string, string, io.Reader, int64) (media.PutOutcome, error) {
	return media.PutNotCreated, errors.New("unexpected Put")
}
func (*candidateBackend) Get(context.Context, string) (io.ReadCloser, string, error) {
	return nil, "", errors.New("unexpected Get")
}
func (b *candidateBackend) Delete(ctx context.Context, key string) error {
	b.deletes++
	b.deletedKey = key
	_, b.hadDeadline = ctx.Deadline()
	return b.deleteErr
}
func (*candidateBackend) ListPage(context.Context, string, string, int) ([]media.Object, string, error) {
	return nil, "", errors.New("unexpected ListPage")
}

func TestFinalizePhotoCandidate_CompensationMatrixAndImpossibleReplay(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		result     resume.ExecuteResult
		wantDelete bool
		wantLog    bool
	}{
		{"fresh committed winner", resume.ExecuteResult{Outcome: resume.CommitCommitted}, false, false},
		{"concurrent committed replay", resume.ExecuteResult{Replayed: true, Outcome: resume.CommitCommitted}, true, false},
		{"execute not attempted", resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, true, false},
		{"definite rollback", resume.ExecuteResult{Outcome: resume.CommitDefinitelyRolledBack}, true, false},
		{"unknown commit", resume.ExecuteResult{Outcome: resume.CommitUnknown}, false, false},
		{"impossible replay not attempted", resume.ExecuteResult{Replayed: true, Outcome: resume.CommitNotAttempted}, false, true},
		{"impossible replay rolled back", resume.ExecuteResult{Replayed: true, Outcome: resume.CommitDefinitelyRolledBack}, false, true},
		{"impossible replay unknown", resume.ExecuteResult{Replayed: true, Outcome: resume.CommitUnknown}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			backend := &candidateBackend{}
			service := &Service{blobs: backend, logger: slog.New(slog.NewTextHandler(&logs, nil))}
			candidate := photoCandidate{Key: "resume/user/candidate-secret.png", Created: true}
			service.finalizePhotoCandidate(candidate)(context.Background(), preparedInput{}, tc.result, nil)
			if got := backend.deletes > 0; got != tc.wantDelete {
				t.Fatalf("deleted = %v, want %v", got, tc.wantDelete)
			}
			if tc.wantDelete && (backend.deletedKey != candidate.Key || !backend.hadDeadline) {
				t.Fatalf("cleanup = key %q deadline %v", backend.deletedKey, backend.hadDeadline)
			}
			if got := logs.Len() > 0; got != tc.wantLog {
				t.Fatalf("logged = %v, want %v (logs=%q)", got, tc.wantLog, logs.String())
			}
			if strings.Contains(logs.String(), candidate.Key) {
				t.Fatal("candidate key leaked into invariant log")
			}
		})
	}
}

func TestFinalizePhotoCandidate_UncreatedCandidateNeverDeletes(t *testing.T) {
	t.Parallel()

	backend := &candidateBackend{}
	service := &Service{blobs: backend, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	service.finalizePhotoCandidate(photoCandidate{Key: "resume/user/unknown.png"})(
		context.Background(), preparedInput{}, resume.ExecuteResult{Outcome: resume.CommitDefinitelyRolledBack}, nil,
	)
	if backend.deletes != 0 {
		t.Fatalf("deletes = %d, want 0", backend.deletes)
	}
}

func TestPhotoCandidateCleanupTimeoutIsBounded(t *testing.T) {
	if photoCandidateCleanupTimeout != 5*time.Second {
		t.Fatalf("cleanup timeout = %v, want 5s", photoCandidateCleanupTimeout)
	}
}

func TestApplyAtWireVersion_ProductionV1AndV2RoundTrip(t *testing.T) {
	t.Parallel()

	service := &Service{projector: docmigrate.NewIdentityProjector()}
	current := loadMinimalDocument(t)
	for _, version := range []int32{1, 2} {
		version := version
		t.Run(wireVersionString(version), func(t *testing.T) {
			t.Parallel()
			seenVersion := int64(0)
			got, err := service.applyAtWireVersion(current, version, func(wire json.RawMessage) (json.RawMessage, error) {
				var root map[string]any
				if err := json.Unmarshal(wire, &root); err != nil {
					return nil, err
				}
				number, ok := root["schemaVersion"].(float64)
				if !ok {
					return nil, errors.New("schemaVersion is not a number")
				}
				seenVersion = int64(number)
				return wire, nil
			})
			if err != nil {
				t.Fatalf("applyAtWireVersion(%d): %v", version, err)
			}
			if seenVersion != int64(version) || got.SchemaVersion != int64(docmigrate.CurrentVersion) {
				t.Fatalf("versions seen=%d stored=%d", seenVersion, got.SchemaVersion)
			}
		})
	}
}

func TestApplyAtWireVersion_AcceptedButNotEmittedFailsClosed(t *testing.T) {
	t.Parallel()

	setVersion := func(version int32) docmigrate.ConvertFunc {
		return func(raw json.RawMessage) (json.RawMessage, error) {
			var root map[string]any
			if err := json.Unmarshal(raw, &root); err != nil {
				return nil, err
			}
			root["schemaVersion"] = version
			return json.Marshal(root)
		}
	}
	acceptAny := func(json.RawMessage) error { return nil }
	projector, err := docmigrate.NewProjector(
		map[int32]docmigrate.AdjacentConverters{1: {Up: setVersion(2), Down: setVersion(1)}},
		map[int32]docmigrate.ValidateFunc{1: acceptAny, 2: acceptAny},
		[]int32{1, 2}, []int32{2}, 2,
	)
	if err != nil {
		t.Fatalf("NewProjector: %v", err)
	}
	service := &Service{projector: projector}
	_, err = service.applyAtWireVersion(loadMinimalDocument(t), 1, func(raw json.RawMessage) (json.RawMessage, error) {
		return raw, nil
	})
	if !errors.Is(err, docmigrate.ErrUnsupportedVersion) {
		t.Fatalf("error = %v, want ErrUnsupportedVersion", err)
	}
}

func TestDocumentPersistenceSanitizesExactlyOnce(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		run  func(*Service) error
	}{
		{
			name: "wire apply",
			run: func(service *Service) error {
				document := hostileProfileDocument(t)
				raw, err := json.Marshal(document)
				if err != nil {
					return err
				}
				_, err = service.applyAtWireVersion(loadMinimalDocument(t), docmigrate.CurrentVersion,
					func(json.RawMessage) (json.RawMessage, error) { return raw, nil })
				return err
			},
		},
		{
			name: "create",
			run: func(service *Service) error {
				spy := &operationStoreSpy{}
				service.resumes = documentAwareOperationStore{operationStoreSpy: spy}
				_, err := (createOperation{service: service}).Run(context.Background(), nil, mutationContext{
					UserID: uuid.New(), WireVersion: docmigrate.CurrentVersion,
				}, preparedInput{Value: createPreparedInput{
					Document: hostileProfileDocument(t), Title: "created", Response: operationResponse,
				}})
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			service := &Service{
				projector: docmigrate.NewIdentityProjector(),
				sanitizeDocument: func(document schema.Resume) schema.Resume {
					calls++
					return sanitizeDocument(document)
				},
			}
			if err := test.run(service); err != nil {
				t.Fatalf("persist document: %v", err)
			}
			if calls != 1 {
				t.Fatalf("sanitizer calls = %d, want exactly 1", calls)
			}
		})
	}
}
