package resumeapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type scriptedTransitionResumes struct {
	first, second resume.Resume
	calls         int
	afterSecond   func()
	getErr        error
}

func (s *scriptedTransitionResumes) Get(context.Context, uuid.UUID, uuid.UUID) (resume.Resume, error) {
	s.calls++
	if s.getErr != nil {
		return resume.Resume{}, s.getErr
	}
	if s.calls == 1 {
		return s.first, nil
	}
	if s.calls == 2 && s.afterSecond != nil {
		s.afterSecond()
	}
	return s.second, nil
}
func (*scriptedTransitionResumes) List(context.Context, uuid.UUID) ([]resume.Resume, error) {
	return nil, errors.New("unexpected List")
}
func (*scriptedTransitionResumes) CreateTx(context.Context, *store.Queries, uuid.UUID, string, *string, schema.Resume) (resume.Resume, error) {
	return resume.Resume{}, errors.New("unexpected CreateTx")
}
func (s *scriptedTransitionResumes) GetTx(context.Context, *store.Queries, uuid.UUID, uuid.UUID) (resume.Resume, error) {
	if s.getErr != nil {
		return resume.Resume{}, s.getErr
	}
	return s.second, nil
}
func (*scriptedTransitionResumes) SaveDocumentTx(context.Context, *store.Queries, uuid.UUID, uuid.UUID, schema.Resume, int64) (int64, error) {
	return 0, errors.New("unexpected SaveDocumentTx")
}
func (*scriptedTransitionResumes) SaveMetadataAndDocumentTx(context.Context, *store.Queries, uuid.UUID, uuid.UUID, string, *string, schema.Resume, int64) (int64, error) {
	return 0, errors.New("unexpected SaveMetadataAndDocumentTx")
}
func (*scriptedTransitionResumes) DeleteTx(context.Context, *store.Queries, uuid.UUID, uuid.UUID, int64) (resume.Resume, error) {
	return resume.Resume{}, errors.New("unexpected DeleteTx")
}

type transactionCallbackIdempotency struct {
	qtx      *store.Queries
	executes int
	recheck  func()
}

type closePauseIdempotency struct {
	qtx     *store.Queries
	closed  chan struct{}
	proceed chan struct{}
}

func (*closePauseIdempotency) Inspect(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.StoredResponse, bool, error) {
	return resume.StoredResponse{}, false, nil
}
func (*closePauseIdempotency) Recheck(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.RecheckResult, error) {
	return resume.RecheckResult{Decision: resume.RecheckFresh}, nil
}
func (s *closePauseIdempotency) Execute(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID, _ [32]byte, callback func(*store.Queries) (resume.StoredResponse, error)) (resume.ExecuteResult, error) {
	close(s.closed)
	<-s.proceed
	response, err := callback(s.qtx)
	if err != nil {
		return resume.ExecuteResult{Outcome: resume.CommitDefinitelyRolledBack}, err
	}
	return resume.ExecuteResult{Response: response, Outcome: resume.CommitCommitted}, nil
}

func (*transactionCallbackIdempotency) Inspect(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.StoredResponse, bool, error) {
	return resume.StoredResponse{}, false, nil
}
func (s *transactionCallbackIdempotency) Recheck(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.RecheckResult, error) {
	if s.recheck != nil {
		s.recheck()
	}
	return resume.RecheckResult{Decision: resume.RecheckFresh}, nil
}
func (s *transactionCallbackIdempotency) Execute(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID, _ [32]byte, callback func(*store.Queries) (resume.StoredResponse, error)) (resume.ExecuteResult, error) {
	s.executes++
	response, err := callback(s.qtx)
	if err != nil {
		return resume.ExecuteResult{Outcome: resume.CommitDefinitelyRolledBack}, err
	}
	return resume.ExecuteResult{Response: response, Outcome: resume.CommitCommitted}, nil
}

type transitionIdempotency struct{ events *[]string }

func (s transitionIdempotency) Inspect(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.StoredResponse, bool, error) {
	return resume.StoredResponse{}, false, nil
}

type failedRecheckIdempotency struct{ executes int }

func (s *failedRecheckIdempotency) Inspect(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.StoredResponse, bool, error) {
	return resume.StoredResponse{}, false, nil
}

type unknownCommitIdempotency struct{ callbacks int }

func (*unknownCommitIdempotency) Inspect(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.StoredResponse, bool, error) {
	return resume.StoredResponse{}, false, nil
}
func (*unknownCommitIdempotency) Recheck(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.RecheckResult, error) {
	return resume.RecheckResult{Decision: resume.RecheckFresh}, nil
}
func (s *unknownCommitIdempotency) Execute(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID, _ [32]byte, callback func(*store.Queries) (resume.StoredResponse, error)) (resume.ExecuteResult, error) {
	s.callbacks++
	if _, callbackErr := callback(nil); callbackErr != nil {
		return resume.ExecuteResult{Outcome: resume.CommitDefinitelyRolledBack}, callbackErr
	}
	return resume.ExecuteResult{Outcome: resume.CommitUnknown}, errors.New("commit result unknown")
}

type recoveredStoredResponse struct {
	proof    publicstate.RecoveryProof
	response *resume.StoredResponse
	err      error
}

func (r recoveredStoredResponse) Resolve(context.Context) (publicstate.RecoveryProof, error) {
	return r.proof, r.err
}
func (r recoveredStoredResponse) recoveredResponse() (resume.StoredResponse, bool) {
	if r.response == nil {
		return resume.StoredResponse{}, false
	}
	return *r.response, true
}
func (s *failedRecheckIdempotency) Recheck(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.RecheckResult, error) {
	return resume.RecheckResult{}, errors.New("recheck unavailable")
}
func (s *failedRecheckIdempotency) Execute(context.Context, uuid.UUID, string, uuid.UUID, [32]byte, func(*store.Queries) (resume.StoredResponse, error)) (resume.ExecuteResult, error) {
	s.executes++
	return resume.ExecuteResult{}, nil
}

type replayBeforeCloseIdempotency struct{ events *[]string }

func (s replayBeforeCloseIdempotency) Inspect(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.StoredResponse, bool, error) {
	return resume.StoredResponse{}, false, nil
}

func (s replayBeforeCloseIdempotency) Recheck(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.RecheckResult, error) {
	*s.events = append(*s.events, "recheck")
	return resume.RecheckResult{Decision: resume.RecheckReplay, Response: resume.StoredResponse{Status: 204}}, nil
}

func (s replayBeforeCloseIdempotency) Execute(context.Context, uuid.UUID, string, uuid.UUID, [32]byte, func(*store.Queries) (resume.StoredResponse, error)) (resume.ExecuteResult, error) {
	*s.events = append(*s.events, "execute")
	return resume.ExecuteResult{}, nil
}

func (s transitionIdempotency) Recheck(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.RecheckResult, error) {
	*s.events = append(*s.events, "recheck")
	return resume.RecheckResult{Decision: resume.RecheckFresh}, nil
}

func (s transitionIdempotency) Execute(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID, _ [32]byte,
	mutate func(*store.Queries) (resume.StoredResponse, error),
) (resume.ExecuteResult, error) {
	*s.events = append(*s.events, "execute")
	response, err := mutate(nil)
	if err != nil {
		return resume.ExecuteResult{Outcome: resume.CommitDefinitelyRolledBack}, err
	}
	return resume.ExecuteResult{Response: response, Outcome: resume.CommitCommitted}, nil
}

func TestRunMutationRechecksBeforeCloseAndCommitsExactGeneration(t *testing.T) {
	t.Parallel()
	events := []string{}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 3})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{coordinator: coordinator, idempotency: transitionIdempotency{events: &events}}
	resumeID := uuid.New()
	nextRevision := int64(8)
	result, err := service.runMutation(context.Background(), mutationIdentity{
		UserID: uuid.New(), Operation: "updateResumeMetadata", Key: uuid.New(),
	}, mutationPlan{
		Fence: publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
			ID: resumeID, ExpectedRevision: 7, Class: publicstate.NonDraining,
		}}},
		Mutate: func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
			events = append(events, "mutate")
			return resume.StoredResponse{Status: 204}, publicstate.CommittedState{
				ResumeRevisions: map[uuid.UUID]int64{resumeID: nextRevision},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("runMutation() error = %v", err)
	}
	if result.Outcome != resume.CommitCommitted || result.Response.Status != 204 {
		t.Fatalf("runMutation() = %#v, want committed 204", result)
	}
	if want := []string{"recheck", "execute", "mutate"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	lease, err := coordinator.AcquireResume(context.Background(), resumeID, nextRevision, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("new revision is not open: %v", err)
	}
	lease.Release()
}

func TestAllRevisionMutationRoutesCloseAdmission(t *testing.T) {
	// The Task02 catalog predates publish. Its twelve mutations include create;
	// create has no existing public revision to fence, leaving these eleven
	// revision mutation handlers to exercise at the real Close/Execute seam.
	h := newResumeAPITestHarness(t)
	fixture := newRemainingFixture(t, h)
	mutations := remainingOpenAPIMutations(t, fixture.resume.ID)
	exercised := make([]string, 0, 11)
	for _, mutation := range mutations {
		if mutation.operationID == "createResume" {
			continue
		}
		t.Run(mutation.operationID, func(t *testing.T) {
			// Each handler gets its own stable revision and fixture because a
			// successful callback advances that revision or retires the resume.
			local := newResumeAPITestHarness(t)
			localFixture := newRemainingFixture(t, local)
			localMutation := remainingMutationByOperation(t, remainingOpenAPIMutations(t, localFixture.resume.ID), mutation.operationID)
			requestMutation := remainingRequestForOperation(t, localMutation, localFixture)
			pause := &closePauseIdempotency{qtx: local.queries, closed: make(chan struct{}), proceed: make(chan struct{})}
			local.service.idempotency = pause
			var body io.Reader
			if requestMutation.body != nil {
				body = bytes.NewReader(requestMutation.body)
			}
			request := remainingBaseRequest(t, local, requestMutation, body)
			remainingSetValidMutationHeaders(request, localFixture.resume.Revision, true)
			response := make(chan testHTTPResponse, 1)
			go func() {
				serverResponse, requestErr := local.client.Do(request)
				if requestErr != nil {
					response <- testHTTPResponse{status: http.StatusInternalServerError, body: []byte(requestErr.Error())}
					return
				}
				response <- snapshotHTTPResponse(t, serverResponse)
			}()
			select {
			case <-pause.closed:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not reach Execute after Close")
			}
			if _, err := local.service.coordinator.AcquireResume(context.Background(), localFixture.resume.ID, localFixture.resume.Revision, publicstate.RepresentationJSON); !errors.Is(err, publicstate.ErrAdmissionClosed) {
				t.Fatalf("old public admission during paused %s = %v, want closed", mutation.operationID, err)
			}
			close(pause.proceed)
			got := <-response
			if got.status < http.StatusOK || got.status >= http.StatusMultipleChoices {
				t.Fatalf("%s response = %d %s", mutation.operationID, got.status, got.body)
			}
			if err := local.service.coordinator.Ready(); err != nil {
				t.Fatalf("%s did not settle coordinator: %v", mutation.operationID, err)
			}
		})
		exercised = append(exercised, mutation.operationID)
	}
	if len(exercised) != 11 {
		t.Fatalf("exercised %d revision routes, want 11: %v", len(exercised), exercised)
	}
}

func TestRunMutationRecheckReplayReleasesBegunTransitionWithoutExecute(t *testing.T) {
	t.Parallel()
	events := []string{}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 3})
	if err != nil {
		t.Fatal(err)
	}
	resumeID := uuid.New()
	service := &Service{coordinator: coordinator, idempotency: replayBeforeCloseIdempotency{events: &events}}
	result, err := service.runMutation(context.Background(), mutationIdentity{UserID: uuid.New(), Operation: "deleteResume", Key: uuid.New()}, mutationPlan{
		Fence: publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: resumeID, ExpectedRevision: 7, Class: publicstate.Revoking}}},
		Mutate: func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
			events = append(events, "mutate")
			return resume.StoredResponse{}, publicstate.CommittedState{}, nil
		},
		ReplayState: func(context.Context, resume.StoredResponse) (publicstate.CommittedState, error) {
			events = append(events, "replay-state")
			return publicstate.CommittedState{}, nil
		},
	})
	if err != nil || !result.Replayed || result.Response.Status != 204 {
		t.Fatalf("runMutation() = %#v, %v; want exact replay", result, err)
	}
	if want := []string{"recheck"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if readyErr := coordinator.Ready(); readyErr != nil {
		t.Fatalf("coordinator readiness after replay = %v", readyErr)
	}
	lease, err := coordinator.AcquireResume(context.Background(), resumeID, 7, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("unchanged admission after replay = %v", err)
	}
	lease.Release()
}

func TestSameKeyContenderRechecksAfterBeginMismatchAndReturnsReplay(t *testing.T) {
	t.Parallel()
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 3})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	winner, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 7, Class: publicstate.NonDraining}}})
	if err != nil {
		t.Fatal(err)
	}
	if closeErr := winner.Close(context.Background(), time.Now().Add(time.Second)); closeErr != nil {
		t.Fatal(closeErr)
	}
	if commitErr := winner.Commit(publicstate.CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 8}}); commitErr != nil {
		t.Fatal(commitErr)
	}
	events := []string{}
	service := &Service{coordinator: coordinator, idempotency: replayBeforeCloseIdempotency{events: &events}}
	result, err := service.runMutation(context.Background(), mutationIdentity{UserID: uuid.New(), Operation: "updateResumeMetadata", Key: uuid.New()}, mutationPlan{Fence: publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 7, Class: publicstate.NonDraining}}}, Mutate: func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
		t.Fatal("same-key contender reran callback")
		return resume.StoredResponse{}, publicstate.CommittedState{}, nil
	}})
	if err != nil || !result.Replayed || result.Response.Status != http.StatusNoContent {
		t.Fatalf("same-key contender = %#v err=%v", result, err)
	}
	if !reflect.DeepEqual(events, []string{"recheck"}) {
		t.Fatalf("events = %v, want only recheck", events)
	}
}

func TestPreflightMismatchRechecksExactReplayBefore412(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id := uuid.New()
	rows := &scriptedTransitionResumes{first: resume.Resume{ID: id, UserID: h.userID, Revision: 8}}
	events := []string{}
	service := h.service
	service.resumes = rows
	service.idempotency = replayBeforeCloseIdempotency{events: &events}
	revision := int64(7)
	result, err := service.executeTransition(context.WithValue(h.ctx, mutationRequestKeyContext{}, uuid.New()), mutationContext{UserID: h.userID, SessionID: h.session.ID, Session: h.session, ExpectedRevision: &revision, Operation: "same-key-contender", RequestHash: [32]byte{1}}, preparedInput{Value: resumeMetadataPrepared{ResumeID: id}}, mutationSpec{
		Transition: func(context.Context, resume.Resume, preparedInput) (mutationTransition, error) {
			t.Fatal("stale replay reached transition classifier")
			return mutationTransition{}, nil
		},
		Run: mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
			t.Fatal("stale replay reran callback")
			return mutationRunResult{}, nil
		}),
	})
	if err != nil || !result.Replayed || result.Response.Status != http.StatusNoContent {
		t.Fatalf("preflight replay = %#v err=%v", result, err)
	}
	if !reflect.DeepEqual(events, []string{"recheck"}) {
		t.Fatalf("events = %v, want exact recheck only", events)
	}
}

func TestDeletePreflightNotFoundRechecksExactReplayBefore404(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id := uuid.New()
	events := []string{}
	service := h.service
	service.resumes = &scriptedTransitionResumes{getErr: resume.ErrNotFound}
	service.idempotency = replayBeforeCloseIdempotency{events: &events}
	revision := int64(7)
	result, err := service.executeTransition(context.WithValue(h.ctx, mutationRequestKeyContext{}, uuid.New()), mutationContext{UserID: h.userID, SessionID: h.session.ID, Session: h.session, ExpectedRevision: &revision, Operation: "same-key-delete", RequestHash: [32]byte{2}}, preparedInput{Value: deletePreparedInput{ResumeID: id}}, mutationSpec{
		Transition: func(context.Context, resume.Resume, preparedInput) (mutationTransition, error) {
			t.Fatal("deleted replay reached transition classifier")
			return mutationTransition{}, nil
		},
		Run: mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
			t.Fatal("deleted replay reran callback")
			return mutationRunResult{}, nil
		}),
	})
	if err != nil || !result.Replayed || result.Response.Status != http.StatusNoContent {
		t.Fatalf("delete preflight replay = %#v err=%v", result, err)
	}
	if !reflect.DeepEqual(events, []string{"recheck"}) {
		t.Fatalf("events = %v, want exact recheck only", events)
	}
}

func TestCommitUnknownRecoveryReplaysExactStoredResponse(t *testing.T) {
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 3})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	idem := &unknownCommitIdempotency{}
	stored := resume.StoredResponse{Status: http.StatusCreated, Body: []byte(`{"data":"stored"}`), Headers: map[string]string{"ETag": `"r8"`, wireVersionHeader: "2"}}
	service := &Service{coordinator: coordinator, idempotency: idem}
	result, runErr := service.runMutation(context.Background(), mutationIdentity{UserID: uuid.New(), Operation: "updateResumeMetadata", Key: uuid.New()}, mutationPlan{Fence: publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 7, Class: publicstate.NonDraining}}}, Mutate: func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
		return resume.StoredResponse{Status: http.StatusNoContent}, publicstate.CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 8}}, nil
	}, Recover: recoveredStoredResponse{proof: publicstate.RecoveryProof{Disposition: publicstate.RecoveryCommitted, State: publicstate.CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 8}}}, response: &stored}})
	if runErr != nil || !result.Replayed || result.Response.Status != stored.Status || !bytes.Equal(result.Response.Body, stored.Body) || !reflect.DeepEqual(result.Response.Headers, stored.Headers) {
		t.Fatalf("recovered result = %#v err=%v, want exact stored replay", result, runErr)
	}
	if idem.callbacks != 1 {
		t.Fatalf("Mutate callbacks = %d, want 1", idem.callbacks)
	}
	lease, err := coordinator.AcquireResume(context.Background(), id, 8, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("proved generation not open: %v", err)
	}
	lease.Release()
}

func TestCommitUnknownNotCommittedReopensOldGeneration(t *testing.T) {
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 3})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	service := &Service{coordinator: coordinator, idempotency: &unknownCommitIdempotency{}}
	_, runErr := service.runMutation(context.Background(), mutationIdentity{UserID: uuid.New(), Operation: "updateResumeMetadata", Key: uuid.New()}, mutationPlan{Fence: publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 7, Class: publicstate.NonDraining}}}, Mutate: func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
		return resume.StoredResponse{}, publicstate.CommittedState{}, nil
	}, Recover: recoveredStoredResponse{proof: publicstate.RecoveryProof{Disposition: publicstate.RecoveryNotCommitted, State: publicstate.CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 7}}}}})
	if runErr == nil {
		t.Fatal("not-committed recovery returned nil safe failure")
	}
	lease, err := coordinator.AcquireResume(context.Background(), id, 7, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("old generation did not reopen: %v", err)
	}
	lease.Release()
}

func TestTransactionalRecheckRaceReopensExactOldGenerations(t *testing.T) {
	t.Parallel()
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 3})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	idem := &failedRecheckIdempotency{}
	service := &Service{coordinator: coordinator, idempotency: idem}
	_, err = service.runMutation(context.Background(), mutationIdentity{UserID: uuid.New(), Operation: "updateResumeMetadata", Key: uuid.New()}, mutationPlan{Fence: publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 7, Class: publicstate.NonDraining}}}, Mutate: func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
		t.Fatal("mutation ran after recheck failure")
		return resume.StoredResponse{}, publicstate.CommittedState{}, nil
	}})
	if err == nil {
		t.Fatal("runMutation() error = nil, want recheck failure")
	}
	if idem.executes != 0 {
		t.Fatalf("Execute calls = %d, want 0", idem.executes)
	}
	lease, err := coordinator.AcquireResume(context.Background(), id, 7, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("old generation did not reopen: %v", err)
	}
	lease.Release()
}

func TestDrainTimeoutBeginsNoTransactionAndReopens(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 3, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	lease, err := coordinator.AcquireResume(context.Background(), id, 7, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	idem := &failedRecheckIdempotency{}
	// Fresh recheck reaches Close; Execute must remain unreachable on the timed-out drain.
	idemFresh := transitionIdempotency{events: &[]string{}}
	service := &Service{coordinator: coordinator, idempotency: idemFresh, clock: func() time.Time { return now }}
	_, err = service.runMutation(context.Background(), mutationIdentity{UserID: uuid.New(), Operation: "deleteResume", Key: uuid.New()}, mutationPlan{Fence: publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 7, Class: publicstate.Revoking}}}, Mutate: func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
		t.Fatal("database callback ran after drain timeout")
		return resume.StoredResponse{}, publicstate.CommittedState{}, nil
	}})
	var timeout *publicstate.DrainTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("runMutation() error = %v, want drain timeout", err)
	}
	if idem.executes != 0 {
		t.Fatalf("unrelated Execute calls = %d", idem.executes)
	}
	if err := coordinator.Ready(); err != nil {
		t.Fatalf("coordinator not ready after timeout: %v", err)
	}
}

func TestRunMutationDeleteReplayReleasesFenceBeforeExecution(t *testing.T) {
	t.Parallel()
	events := []string{}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 3})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	service := &Service{coordinator: coordinator, idempotency: replayBeforeCloseIdempotency{events: &events}}
	result, err := service.runMutation(context.Background(), mutationIdentity{UserID: uuid.New(), Operation: "deleteResume", Key: uuid.New()}, mutationPlan{Fence: publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 7, Class: publicstate.Revoking}}}, Mutate: func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
		t.Fatal("delete replay reran mutation")
		return resume.StoredResponse{}, publicstate.CommittedState{}, nil
	}})
	if err != nil || !result.Replayed || !reflect.DeepEqual(events, []string{"recheck"}) {
		t.Fatalf("replay = %#v events=%v err=%v", result, events, err)
	}
	if err := coordinator.Ready(); err != nil {
		t.Fatalf("replay changed fence readiness: %v", err)
	}
}

func TestRevokingPublishAndDeleteCancelEveryRepresentation(t *testing.T) {
	t.Parallel()
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 3})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	resumeReps := []publicstate.Representation{publicstate.RepresentationJSON, publicstate.RepresentationPhoto, publicstate.RepresentationHTML, publicstate.RepresentationMarkdown}
	globalReps := []publicstate.Representation{publicstate.RepresentationSitemap, publicstate.RepresentationRobots, publicstate.RepresentationLLMS}
	leases := make([]*publicstate.Lease, 0, len(resumeReps)+len(globalReps))
	for _, rep := range resumeReps {
		lease, acquireErr := coordinator.AcquireResume(context.Background(), id, 7, rep)
		if acquireErr != nil {
			t.Fatalf("acquire %s = %v", rep, acquireErr)
		}
		leases = append(leases, lease)
	}
	for _, rep := range globalReps {
		lease, acquireErr := coordinator.AcquireDiscovery(context.Background(), 3, rep)
		if acquireErr != nil {
			t.Fatalf("acquire %s = %v", rep, acquireErr)
		}
		leases = append(leases, lease)
	}
	for _, lease := range leases {
		go func(lease *publicstate.Lease) { <-lease.Context().Done(); lease.Release() }(lease)
	}
	events := []string{}
	service := &Service{coordinator: coordinator, idempotency: transitionIdempotency{events: &events}}
	generation := int64(3)
	next := int64(4)
	_, err = service.runMutation(context.Background(), mutationIdentity{UserID: uuid.New(), Operation: "publishResume", Key: uuid.New()}, mutationPlan{Fence: publicstate.Plan{DiscoveryGeneration: &generation, Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 7, Class: publicstate.Revoking}}}, Mutate: func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
		return resume.StoredResponse{Status: 204}, publicstate.CommittedState{DiscoveryGeneration: &next, RetiredResumes: []uuid.UUID{id}}, nil
	}})
	if err != nil {
		t.Fatalf("revoking transition = %v", err)
	}
	for _, lease := range leases {
		select {
		case <-lease.Context().Done():
		default:
			t.Fatal("revoking transition did not cancel every representation")
		}
	}
}

func TestTransitionTransactionOrderReadsResumeBeforeSessionForRenameAndSlugDelete(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created, err := h.resumes.Create(h.ctx, h.userID, "Lock order", publishCompleteDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	assertOrder := func(name string, want []string, request func() testHTTPResponse) {
		t.Helper()
		var got []string
		h.service.transactionOrderHook = func(step string) { got = append(got, step) }
		t.Cleanup(func() { h.service.transactionOrderHook = nil })
		response := request()
		if response.status != http.StatusOK && response.status != http.StatusNoContent {
			t.Fatalf("%s status = %d %s", name, response.status, response.body)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s transaction order = %v, want %v", name, got, want)
		}
		h.service.transactionOrderHook = nil
	}
	oldSlug := "order-old-" + uuid.NewString()[:8]
	assertOrder("initial claim", []string{"slug", "public_state", "resume", "session", "tombstone", "claim"}, func() testHTTPResponse {
		return h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish", strings.NewReader(`{"slug":"`+oldSlug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), created.Revision, uuid.NewString())
	})
	if _, updateErr := h.pool.Exec(h.ctx, `UPDATE sessions SET reauthenticated_at = now() WHERE id = $1`, h.session.ID); updateErr != nil {
		t.Fatal(updateErr)
	}
	newSlug := "order-new-" + uuid.NewString()[:8]
	assertOrder("rename", []string{"slug", "public_state", "resume", "session", "tombstone", "claim"}, func() testHTTPResponse {
		return h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+created.ID.String()+"/publish", strings.NewReader(`{"slug":"`+newSlug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), created.Revision+1, uuid.NewString())
	})
	assertOrder("slug delete", []string{"slug", "public_state", "resume", "session"}, func() testHTTPResponse {
		return h.mutationRequest(t, http.MethodDelete, apiResumePath+"/"+created.ID.String(), nil, created.Revision+2, uuid.NewString())
	})
}

func TestPublishSlugPreflightRejectsUnavailableWithoutClosingLeaseAndAllowsExactTombstoneBoundary(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owner, err := h.resumes.Create(h.ctx, h.userID, "Claim owner", publishCompleteDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	target, err := h.resumes.Create(h.ctx, h.userID, "Claim target", publishCompleteDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	slug := "preflight-" + uuid.NewString()[:8]
	claimed := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+owner.ID.String()+"/publish", strings.NewReader(`{"slug":"`+slug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), owner.Revision, uuid.NewString())
	if claimed.status != http.StatusOK {
		t.Fatalf("initial claim = %d %s", claimed.status, claimed.body)
	}
	targetSlug := "preflight-target-" + uuid.NewString()[:8]
	targetPublished := h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+target.ID.String()+"/publish", strings.NewReader(`{"slug":"`+targetSlug+`","live":true,"downloadEnabled":true,"seoGeoEnabled":false}`), target.Revision, uuid.NewString())
	if targetPublished.status != http.StatusOK {
		t.Fatalf("target initial claim = %d %s", targetPublished.status, targetPublished.body)
	}
	if _, updateErr := h.pool.Exec(h.ctx, `UPDATE sessions SET reauthenticated_at = now() WHERE id = $1`, h.session.ID); updateErr != nil {
		t.Fatal(updateErr)
	}
	currentRevision := target.Revision + 1
	lease, err := h.service.coordinator.AcquireResume(h.ctx, target.ID, currentRevision, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatal(err)
	}
	assertLeaseOpen := func(name string) {
		t.Helper()
		select {
		case <-lease.Context().Done():
			t.Fatalf("%s canceled target lease before a successful mutation", name)
		default:
		}
	}
	attempt := func(key string) testHTTPResponse {
		return h.mutationRequest(t, http.MethodPost, apiResumePath+"/"+target.ID.String()+"/publish", strings.NewReader(`{"slug":"`+slug+`","live":true,"downloadEnabled":true,"seoGeoEnabled":false}`), currentRevision, key)
	}
	beforeRejected := h.snapshotUserTable(t, "idempotency_records")
	if response := attempt(uuid.NewString()); response.status != http.StatusConflict {
		t.Fatalf("claimed-slug preflight = %d %s, want 409", response.status, response.body)
	}
	assertLeaseOpen("claimed slug")
	if afterRejected := h.snapshotUserTable(t, "idempotency_records"); afterRejected != beforeRejected {
		t.Fatalf("claimed-slug rejection wrote idempotency state: before=%q after=%q", beforeRejected, afterRejected)
	}
	deleted := h.mutationRequest(t, http.MethodDelete, apiResumePath+"/"+owner.ID.String(), nil, owner.Revision+1, uuid.NewString())
	if deleted.status != http.StatusNoContent {
		t.Fatalf("release claimed slug = %d %s", deleted.status, deleted.body)
	}
	beforeTombstoneRejected := h.snapshotUserTable(t, "idempotency_records")
	clock := time.Now().UTC().Truncate(time.Microsecond)
	previousClock := h.service.clock
	h.service.clock = func() time.Time { return clock }
	t.Cleanup(func() { h.service.clock = previousClock })
	if response := attempt(uuid.NewString()); response.status != http.StatusConflict {
		t.Fatalf("unexpired tombstone preflight = %d %s, want 409", response.status, response.body)
	}
	assertLeaseOpen("unexpired tombstone")
	if afterRejected := h.snapshotUserTable(t, "idempotency_records"); afterRejected != beforeTombstoneRejected {
		t.Fatalf("tombstone rejection wrote idempotency state: before=%q after=%q", beforeTombstoneRejected, afterRejected)
	}
	if _, err := h.pool.Exec(h.ctx, `UPDATE slug_tombstones SET released_at = $2 WHERE slug = $1`, slug, clock.Add(-180*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if response := attempt(uuid.NewString()); response.status != http.StatusOK {
		t.Fatalf("exact tombstone boundary claim = %d %s, want 200", response.status, response.body)
	}
}

func TestPublishSlugPreflightQueriesClaimBeforeTombstone(t *testing.T) {
	h := newResumeAPITestHarness(t)
	var got []string
	h.service.publishPreflightOrderHook = func(step string) { got = append(got, step) }
	t.Cleanup(func() { h.service.publishPreflightOrderHook = nil })
	slug := "preflight-order-" + uuid.NewString()[:8]
	if err := h.service.preflightPublishSlugAvailability(h.ctx, uuid.New(), publishPrepared{ChangedSlug: true, Effective: currentPublish{Slug: &slug}}); err != nil {
		t.Fatalf("preflight availability = %v", err)
	}
	if want := []string{"claim", "tombstone"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("preflight query order = %v, want %v", got, want)
	}
}

func TestGlobalMismatchRetriesOnceThenFailsClosedWithoutExecute(t *testing.T) {
	for _, tc := range []struct {
		name         string
		second       bool
		wantStatus   int
		wantExecutes int
	}{{"unrelated global retries", false, 0, 1}, {"second mismatch busy", true, http.StatusServiceUnavailable, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			public, err := h.queries.GetPublicState(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: public.DiscoveryGeneration})
			if err != nil {
				t.Fatal(err)
			}
			advance := func(from, to int64) {
				transition, beginErr := coordinator.Begin(h.ctx, publicstate.Plan{DiscoveryGeneration: &from})
				if beginErr != nil {
					t.Fatal(beginErr)
				}
				if closeErr := transition.Close(h.ctx, time.Now().Add(time.Second)); closeErr != nil {
					t.Fatal(closeErr)
				}
				if commitErr := transition.Commit(publicstate.CommittedState{DiscoveryGeneration: &to}); commitErr != nil {
					t.Fatal(commitErr)
				}
			}
			advance(public.DiscoveryGeneration, public.DiscoveryGeneration+1)
			id := uuid.New()
			row := resume.Resume{ID: id, UserID: h.userID, Revision: 7}
			rows := &scriptedTransitionResumes{first: row, second: row}
			rows.afterSecond = func() {
				if _, updateErr := h.queries.AdvanceDiscoveryGeneration(h.ctx); updateErr != nil {
					t.Errorf("advance durable discovery: %v", updateErr)
				}
				if tc.second {
					advance(public.DiscoveryGeneration+1, public.DiscoveryGeneration+2)
				}
			}
			idem := &transactionCallbackIdempotency{qtx: h.queries}
			service := h.service
			service.coordinator, service.recoveryPool, service.resumes, service.idempotency = coordinator, h.pool, rows, idem
			revision := int64(7)
			result, transitionErr := service.executeTransition(context.WithValue(h.ctx, mutationRequestKeyContext{}, uuid.New()), mutationContext{UserID: h.userID, SessionID: h.session.ID, Session: h.session, ExpectedRevision: &revision, Operation: "global-race"}, preparedInput{Value: resumeMetadataPrepared{ResumeID: id}}, mutationSpec{Transition: func(context.Context, resume.Resume, preparedInput) (mutationTransition, error) {
				return mutationTransition{ResumeID: id, Class: publicstate.NonDraining, Global: true}, nil
			}, Run: mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
				return mutationRunResult{Response: resume.StoredResponse{Status: http.StatusNoContent}}, nil
			})})
			if tc.wantStatus == 0 {
				if transitionErr != nil || result.Response.Status != http.StatusNoContent {
					t.Fatalf("global retry = %#v err=%v", result, transitionErr)
				}
			} else {
				client := mapMutationError(transitionErr)
				if client.Status != tc.wantStatus || client.Code != "public_state_busy" {
					t.Fatalf("second mismatch = %v mapped=%+v", transitionErr, client)
				}
			}
			if idem.executes != tc.wantExecutes {
				t.Fatalf("Execute calls = %d, want %d", idem.executes, tc.wantExecutes)
			}
		})
	}
}

func TestTransactionalDiscoveryRecheckRejectsBeforeGlobalMutationWrites(t *testing.T) {
	h := newResumeAPITestHarness(t)
	public, err := h.queries.GetPublicState(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: public.DiscoveryGeneration})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	row := resume.Resume{ID: id, UserID: h.userID, Revision: 7}
	rows := &scriptedTransitionResumes{first: row, second: row}
	idem := &transactionCallbackIdempotency{qtx: h.queries, recheck: func() {
		if _, advanceErr := h.queries.AdvanceDiscoveryGeneration(h.ctx); advanceErr != nil {
			t.Errorf("advance durable discovery: %v", advanceErr)
		}
	}}
	service := h.service
	service.coordinator, service.recoveryPool, service.resumes, service.idempotency = coordinator, h.pool, rows, idem
	revision := int64(7)
	runs := 0
	_, transitionErr := service.executeTransition(context.WithValue(h.ctx, mutationRequestKeyContext{}, uuid.New()), mutationContext{UserID: h.userID, SessionID: h.session.ID, Session: h.session, ExpectedRevision: &revision, Operation: "global-transactional-recheck"}, preparedInput{Value: resumeMetadataPrepared{ResumeID: id}}, mutationSpec{
		Transition: func(context.Context, resume.Resume, preparedInput) (mutationTransition, error) {
			return mutationTransition{ResumeID: id, Class: publicstate.NonDraining, Global: true}, nil
		},
		Run: mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
			runs++
			return mutationRunResult{Response: resume.StoredResponse{Status: http.StatusNoContent}}, nil
		}),
	})
	client := mapMutationError(transitionErr)
	if client.Status != http.StatusServiceUnavailable || client.Code != "public_state_busy" {
		t.Fatalf("transactional discovery mismatch = %v mapped=%+v", transitionErr, client)
	}
	if runs != 0 {
		t.Fatalf("global operation writes ran %d times, want 0", runs)
	}
	if err := coordinator.Ready(); err != nil {
		t.Fatalf("coordinator did not reopen after transactional mismatch: %v", err)
	}
}
