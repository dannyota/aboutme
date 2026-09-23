package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type workflowHarness struct {
	t         *testing.T
	fake      *fakeMCP
	config    workflowConfig
	connects  int
	keys      int
	localNow  time.Time
	translate func(map[string]any)
	review    func(*candidateReview)
}

func newWorkflowHarness(t *testing.T, mode string, withPhoto bool) *workflowHarness {
	t.Helper()
	base := t.TempDir()
	controlRoot := filepath.Join(base, "control")
	control, err := openPrivateArtifacts(controlRoot, controlScope)
	if err != nil {
		t.Fatal(err)
	}
	run, err := openPrivateArtifacts(filepath.Join(controlRoot, "run"), runScope)
	if err != nil {
		t.Fatal(err)
	}
	browser := filepath.Join(run.root, browserDirectory)
	if mkdirErr := os.Mkdir(browser, privateDirectoryMode); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	origin := productionOrigin
	if mode == modeLocal {
		origin = localOrigin
	}
	return &workflowHarness{
		t: t, fake: newFakeMCP(t, withPhoto), localNow: fakeEpoch,
		config:    workflowConfig{Mode: mode, Origin: origin, Control: control, Run: run, BrowserRoot: browser},
		translate: translateFixture,
	}
}

func (h *workflowHarness) deps() workflowDeps {
	return workflowDeps{
		Connect: func(context.Context) (sdkToolClient, error) {
			h.connects++
			return h.fake, nil
		},
		Observations:  h.fake,
		Tokens:        func(context.Context) (string, string, error) { return fakeAccessToken, fakeRefreshToken, nil },
		Revocation:    h.fake,
		WaitCandidate: h.writeCandidate,
		NewKey: func() (string, error) {
			h.keys++
			return fmt.Sprintf("00000000-0000-4000-8000-%012d", h.keys), nil
		},
		LocalNow: func() time.Time { return h.localNow },
	}
}

func (h *workflowHarness) run() (workflowResult, error) {
	return runWorkflow(context.Background(), h.config, h.deps())
}

// writeCandidate stands in for the manager: it reads source.json, applies the
// translation, and writes the digest-bound review.
func (h *workflowHarness) writeCandidate(_ context.Context, run privateArtifacts) error {
	source, err := run.read(sourceName)
	if err != nil {
		return err
	}
	var document map[string]any
	if decodeErr := json.Unmarshal(source, &document); decodeErr != nil {
		return decodeErr
	}
	h.translate(document)
	candidate, err := json.Marshal(document)
	if err != nil {
		return err
	}
	review := candidateReview{SourceDigest: digest(source), CandidateDigest: digest(candidate), FactsPreserved: true}
	if h.review != nil {
		h.review(&review)
	}
	encoded, err := json.Marshal(review)
	if err != nil {
		return err
	}
	if writeErr := run.write(candidateName, candidate); writeErr != nil {
		return writeErr
	}
	return run.write(candidateReviewName, encoded)
}

func translateFixture(document map[string]any) {
	asMap(document["personalDetails"])["headline"] = "Kỹ sư phân tích"
	content := asMap(document["content"])
	work := asMap(content["work"])
	work["displayName"] = "Kinh nghiệm"
	entry := entryAt(work, 0)
	entry["jobTitle"] = "Kỹ sư chính"
	entry["description"] = "<p>Dẫn dắt thiết kế thế hệ kế tiếp của máy sai phân.</p>"
	entryAt(asMap(content["profile"]), 0)["text"] = "<p>Kỹ sư và nhà toán học tập trung vào máy phân tích.</p>"
}

// asMap and entryAt return an empty value on a shape mismatch, which the
// preservation check then rejects.
func asMap(value any) map[string]any {
	typed, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return typed
}

func entryAt(section map[string]any, index int) map[string]any {
	entries, ok := section["entries"].([]any)
	if !ok || index >= len(entries) {
		return map[string]any{}
	}
	return asMap(entries[index])
}

func (h *workflowHarness) exists(artifacts privateArtifacts, name string) bool {
	h.t.Helper()
	present, err := artifacts.exists(name)
	if err != nil {
		h.t.Fatalf("exists(%s): %v", name, err)
	}
	return present
}

func (h *workflowHarness) evidence() runEvidence {
	h.t.Helper()
	data, err := h.config.Run.read(evidenceName)
	if err != nil {
		h.t.Fatalf("read evidence: %v", err)
	}
	var value runEvidence
	if !strictJSON(data, &value) {
		h.t.Fatalf("evidence is not the closed shape")
	}
	return value
}

func (h *workflowHarness) requireOneExactTarget() *fakeResume {
	h.t.Helper()
	targets := h.fake.targets()
	if len(targets) != 1 {
		h.t.Fatalf("targets = %d, want exactly one", len(targets))
	}
	target := targets[0]
	if target.summary.Lng != targetLanguage || target.summary.Title != targetTitle || target.summary.Live || target.summary.Slug != nil {
		h.t.Fatalf("target is not a private Vietnamese resume: %+v", target.summary)
	}
	source := h.fake.resumes[fakeSourceID]
	if source.summary.Revision != "3" || h.fake.called("delete_resume") != 0 {
		h.t.Fatal("source changed or a delete was attempted")
	}
	return target
}

func TestCreateIntentProductionCreatesOnePrivateTargetWithPhoto(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, true)
	result, err := h.run()
	if err != nil || result != resultCompleted {
		t.Fatalf("run = %v, %v", result, err)
	}
	target := h.requireOneExactTarget()
	if target.photo == nil || target.summary.Revision != "3" || h.fake.called("create_resume") != 1 {
		t.Fatalf("photo or revision chain missing: revision %s", target.summary.Revision)
	}
	if !h.exists(h.config.Control, completionName) || h.exists(h.config.Control, revocationName) || h.exists(h.config.Control, createIntentName) {
		t.Fatal("durable state after success is not sentinel-only")
	}
	for _, name := range []string{sourceName, candidateName, candidateReviewName} {
		if h.exists(h.config.Run, name) {
			t.Fatalf("%s retained after success", name)
		}
	}
	if got := h.evidence(); got != workflowEvidence(modeProduction, reconcileCreated, revocationRevoked, true) {
		t.Fatalf("evidence = %+v", got)
	}
}

func TestCreateIntentLocalProofReplaysSameKeyAndObservesConflict(t *testing.T) {
	h := newWorkflowHarness(t, modeLocal, true)
	if _, runErr := h.run(); runErr != nil {
		t.Fatal(runErr)
	}
	h.requireOneExactTarget()
	if h.fake.called("create_resume") != 2 || h.fake.called("update_photo_crop") != 2 {
		t.Fatalf("create=%d crop=%d, want replay and one conflict probe", h.fake.called("create_resume"), h.fake.called("update_photo_crop"))
	}
	var keys []any
	for index, name := range h.fake.calls {
		if name == "create_resume" {
			keys = append(keys, h.fake.arguments[index]["idempotency_key"])
		}
	}
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Fatalf("replay keys = %v", keys)
	}
	if h.exists(h.config.Control, completionName) {
		t.Fatal("local proof left a sentinel in its separate root")
	}
	if got := h.evidence(); got.CreateReconciliation != reconcileReplayed || got.Mode != modeLocal {
		t.Fatalf("evidence = %+v", got)
	}
}

func TestCreateIntentUnknownUnsentResponseRetainsIntentThenReplaysSameKey(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, true)
	h.fake.dropCreate = true
	if _, runErr := h.run(); !errors.Is(runErr, errRecovery) {
		t.Fatalf("first run runErr = %v, want retained recovery", runErr)
	}
	if !h.exists(h.config.Control, createIntentName) || h.exists(h.config.Control, completionName) || len(h.fake.targets()) != 0 {
		t.Fatal("unknown create did not retain only the intent")
	}
	h.fake.dropCreate = false
	h.translate = func(map[string]any) { t.Fatal("restart asked for a new candidate") }
	result, err := h.run()
	if err != nil || result != resultCompleted {
		t.Fatalf("restart = %v, %v", result, err)
	}
	h.requireOneExactTarget()
	if got := h.evidence(); got.CreateReconciliation != reconcileReplayed {
		t.Fatalf("evidence = %+v", got)
	}
}

func TestCreateIntentLostCommittedResponseReconcilesByReadsWithoutResend(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	h.fake.loseCreate = true
	if _, runErr := h.run(); runErr != nil {
		t.Fatal(runErr)
	}
	h.requireOneExactTarget()
	if h.fake.called("create_resume") != 1 || h.fake.called("upload_photo") != 0 {
		t.Fatalf("create=%d upload=%d", h.fake.called("create_resume"), h.fake.called("upload_photo"))
	}
	if got := h.evidence(); got.CreateReconciliation != reconcileReconciled {
		t.Fatalf("evidence = %+v", got)
	}
}

func TestCreateIntentDefinitiveRejectionRemovesIntent(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	h.fake.rejectCreate = "validation_failed"
	if _, runErr := h.run(); !errors.Is(runErr, errCreateRejected) {
		t.Fatalf("runErr = %v", runErr)
	}
	if h.exists(h.config.Control, createIntentName) || len(h.fake.targets()) != 0 {
		t.Fatal("definitive rejection kept an intent or created a resume")
	}
}

func TestCreateIntentAmbiguousRejectionRetainsIntent(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	h.fake.rejectCreate = "agent_access_unavailable"
	if _, runErr := h.run(); !errors.Is(runErr, errRecovery) {
		t.Fatalf("runErr = %v", runErr)
	}
	if !h.exists(h.config.Control, createIntentName) {
		t.Fatal("ambiguous rejection removed the intent")
	}
}

func TestCreateIntentReplayWindowRefusesCutoffCrossingAndReversal(t *testing.T) {
	for name, offset := range map[string]time.Duration{
		"at cutoff":        createReplayLifetime,
		"deadline crosses": createReplayLifetime - 30*time.Second,
		"time reversal":    -time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			h := newWorkflowHarness(t, modeProduction, false)
			h.fake.dropCreate = true
			if _, runErr := h.run(); !errors.Is(runErr, errRecovery) {
				t.Fatalf("first run runErr = %v", runErr)
			}
			intent, present, err := loadCreateIntent(h.config.Control, h.config.Origin)
			if err != nil || !present {
				t.Fatalf("intent: %v", err)
			}
			h.fake.dropCreate = false
			h.fake.now = intent.ServerTime.Add(offset).Add(-time.Second)
			if _, runErr := h.run(); !errors.Is(runErr, errRecovery) {
				t.Fatalf("restart runErr = %v, want reads-only recovery", runErr)
			}
			if h.fake.called("create_resume") != 1 || len(h.fake.targets()) != 0 || !h.exists(h.config.Control, createIntentName) {
				t.Fatal("replay outside the window sent a create or dropped the intent")
			}
		})
	}
}

func TestCreateIntentCrashRestartFindsExactTargetWithoutReplay(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, true)
	h.fake.uploadConflict = true
	if _, runErr := h.run(); !errors.Is(runErr, errRecovery) {
		t.Fatalf("first run runErr = %v", runErr)
	}
	if len(h.fake.targets()) != 1 || h.fake.targets()[0].photo != nil || !h.exists(h.config.Control, createIntentName) {
		t.Fatal("conflict did not stop with one incomplete target and a retained intent")
	}
	h.fake.uploadConflict = false
	result, err := h.run()
	if err != nil || result != resultCompleted {
		t.Fatalf("restart = %v, %v", result, err)
	}
	target := h.requireOneExactTarget()
	if target.photo == nil || h.fake.called("create_resume") != 1 {
		t.Fatal("restart did not finish the existing target")
	}
	if got := h.evidence(); got.CreateReconciliation != reconcileReconciled {
		t.Fatalf("evidence = %+v", got)
	}
}

func TestCreateIntentRejectsRestartWhenAccountBindingChanges(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	h.fake.dropCreate = true
	if _, runErr := h.run(); !errors.Is(runErr, errRecovery) {
		t.Fatal(runErr)
	}
	h.fake.dropCreate = false
	h.fake.addOther("fr")
	extra := "01890f47-7e8a-7b2a-8d70-000000000999"
	h.fake.resumes[extra] = &fakeResume{summary: fakeSummary(extra, "de", "Other", "1"), document: fixtureDocument(t, false)}
	h.fake.order = append(h.fake.order, extra)
	if _, runErr := h.run(); !errors.Is(runErr, errRecovery) {
		t.Fatalf("runErr = %v", runErr)
	}
	if h.fake.called("create_resume") != 1 {
		t.Fatal("changed account allowed a replay")
	}
}

func TestSourceChangeBetweenReadsStopsBeforeCreate(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, true)
	h.fake.changeSourceOn = 2
	if _, runErr := h.run(); !errors.Is(runErr, errSourceChanged) {
		t.Fatalf("runErr = %v", runErr)
	}
	if h.fake.called("create_resume") != 0 || h.exists(h.config.Control, createIntentName) {
		t.Fatal("source change reached create")
	}
}

func TestSourceAccountShapeStopsBeforeCandidate(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	h.fake.addOther(sourceLanguage)
	h.translate = func(map[string]any) { t.Fatal("candidate requested for an invalid account") }
	if _, runErr := h.run(); !errors.Is(runErr, errSourceSelection) {
		t.Fatalf("runErr = %v", runErr)
	}
	if h.exists(h.config.Run, sourceName) {
		t.Fatal("source written for an invalid account")
	}
}

func TestCandidatePreservationFailureStopsBeforeCreate(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	h.translate = func(document map[string]any) {
		translateFixture(document)
		asMap(document["personalDetails"])["fullName"] = "Ada Byron"
	}
	if _, runErr := h.run(); !errors.Is(runErr, errCandidate) {
		t.Fatalf("runErr = %v", runErr)
	}
	if h.fake.called("create_resume") != 0 || h.exists(h.config.Run, candidateName) || h.exists(h.config.Run, sourceName) {
		t.Fatal("rejected candidate reached create or was retained")
	}
}

func TestPhotoLostUploadResponseAcceptsOnlyTheNextExactRevision(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, true)
	h.fake.uploadLose = true
	if _, runErr := h.run(); runErr != nil {
		t.Fatal(runErr)
	}
	if target := h.requireOneExactTarget(); target.summary.Revision != "3" || h.fake.called("upload_photo") != 1 {
		t.Fatalf("revision %s uploads %d", target.summary.Revision, h.fake.called("upload_photo"))
	}
}

func TestCompletionSecondProductionRunStopsBeforeOAuth(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	if _, runErr := h.run(); runErr != nil {
		t.Fatal(runErr)
	}
	calls, connects := len(h.fake.calls), h.connects
	result, err := h.run()
	if err != nil || result != resultAlreadyComplete || h.connects != connects || len(h.fake.calls) != calls {
		t.Fatalf("second run = %v, %v; connects %d->%d", result, err, connects, h.connects)
	}
	if len(h.fake.targets()) != 1 {
		t.Fatal("second production run created a resume")
	}
}

func TestCompletionLocalSentinelBlocksLocalRun(t *testing.T) {
	h := newWorkflowHarness(t, modeLocal, false)
	journal := testJournal(fakeEpoch, localOrigin)
	if err := ensureSentinel(h.config.Control, journal, localOrigin); err != nil {
		t.Fatal(err)
	}
	if _, runErr := h.run(); !errors.Is(runErr, errWorkflowBlocked) || h.connects != 0 {
		t.Fatalf("runErr = %v connects = %d", runErr, h.connects)
	}
}

func TestRecoveryCrashAfterJournalBeforeSentinelRestoresSentinelFirst(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	journal := testJournal(fakeEpoch, productionOrigin)
	journal.AccessToken, journal.RefreshToken = fakeAccessToken, fakeRefreshToken
	if err := persistJournal(h.config.Control, journal, productionOrigin, false); err != nil {
		t.Fatal(err)
	}
	sentinelBeforeRevoke := false
	h.fake.sentinelProbe = func() {
		present, err := h.config.Control.exists(completionName)
		sentinelBeforeRevoke = err == nil && present
	}
	result, err := h.run()
	if err != nil || result != resultRevocationRecovered || h.connects != 0 || len(h.fake.calls) != 0 {
		t.Fatalf("recovery = %v, %v; connects %d calls %d", result, err, h.connects, len(h.fake.calls))
	}
	if !sentinelBeforeRevoke || h.exists(h.config.Control, revocationName) || !h.exists(h.config.Control, completionName) {
		t.Fatal("sentinel was not durable before revocation or journal was retained")
	}
	if got := h.evidence(); got != recoveryEvidence(modeProduction, revocationRevoked) {
		t.Fatalf("evidence = %+v", got)
	}
}

func TestRecoverySentinelWriteFailureRetainsJournalWithoutNetwork(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	journal := testJournal(fakeEpoch, productionOrigin)
	if err := persistJournal(h.config.Control, journal, productionOrigin, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(h.config.Control.root, completionName), privateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if _, runErr := h.run(); runErr == nil {
		t.Fatal("recovery continued without a durable sentinel")
	}
	if h.fake.revokeCalls != 0 || h.fake.probeCalls != 0 || h.connects != 0 || !h.exists(h.config.Control, revocationName) {
		t.Fatal("recovery reached the network or dropped the journal")
	}
}

func TestRevocationWithoutReauthorizationProofSettlesButFails(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	h.fake.refuseReauth = false
	if _, runErr := h.run(); !errors.Is(runErr, errReauthorizationUnproved) {
		t.Fatalf("runErr = %v", runErr)
	}
	if h.exists(h.config.Control, revocationName) || !h.exists(h.config.Control, completionName) {
		t.Fatal("settled revocation left a journal or no sentinel")
	}
	if got := h.evidence(); got.PostRevocation401 || got.Revocation != revocationRevoked {
		t.Fatalf("evidence = %+v", got)
	}
}

func TestRevocationUncertaintyRetainsJournalAndRestartIsRevocationOnly(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	// A lost revoke: the grant stays live, so the SDK call succeeds and the
	// probe sees 200 before the access cutoff.
	h.fake.ignoreRevoke = true
	if _, runErr := h.run(); !errors.Is(runErr, errRevocation) {
		t.Fatalf("runErr = %v", runErr)
	}
	if !h.exists(h.config.Control, revocationName) || !h.exists(h.config.Control, completionName) {
		t.Fatal("uncertain revocation dropped durable state")
	}
	if got := h.evidence(); got.Revocation != revocationUnconfirmed {
		t.Fatalf("evidence = %+v", got)
	}
	h.fake.ignoreRevoke = false
	connects, creates := h.connects, h.fake.called("create_resume")
	result, err := h.run()
	if err != nil || result != resultRevocationRecovered || h.connects != connects || h.fake.called("create_resume") != creates {
		t.Fatalf("restart = %v, %v", result, err)
	}
}

func TestSourceExistingVietnameseTargetBlocksFreshProductionRun(t *testing.T) {
	for name, mutate := range map[string]func(*resumeSummary){
		"vietnamese language": func(s *resumeSummary) { s.Lng = "vi" },
		"regional vietnamese": func(s *resumeSummary) { s.Lng = "vi-VN" },
		"target title":        func(s *resumeSummary) { s.Lng = "fr"; s.Title = targetTitle },
	} {
		t.Run(name, func(t *testing.T) {
			h := newWorkflowHarness(t, modeProduction, false)
			h.fake.addOther("fr")
			mutate(&h.fake.resumes[fakeOtherID].summary)
			h.translate = func(map[string]any) { t.Fatal("candidate requested with an existing target") }
			if _, runErr := h.run(); !errors.Is(runErr, errSourceSelection) {
				t.Fatalf("err = %v", runErr)
			}
			if h.fake.called("create_resume") != 0 || h.fake.called("get_resume") != 0 || h.exists(h.config.Control, createIntentName) {
				t.Fatal("existing target did not stop the run before source reads")
			}
		})
	}
	h := newWorkflowHarness(t, modeProduction, false)
	h.fake.addOther("fr")
	if _, runErr := h.run(); runErr != nil {
		t.Fatalf("unrelated second resume blocked the run: %v", runErr)
	}
}

func TestCreateIntentGrantIsValidatedBeforeAnyCreate(t *testing.T) {
	for name, setup := range map[string]func(h *workflowHarness, deps *workflowDeps){
		"missing token date": func(h *workflowHarness, _ *workflowDeps) { h.fake.noTokenDate = true },
		"oversized token": func(_ *workflowHarness, deps *workflowDeps) {
			deps.Tokens = func(context.Context) (string, string, error) {
				return string(make([]byte, 600)), fakeRefreshToken, nil
			}
		},
		"token unavailable": func(_ *workflowHarness, deps *workflowDeps) {
			deps.Tokens = func(context.Context) (string, string, error) { return "", "", errRuntime }
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newWorkflowHarness(t, modeProduction, false)
			deps := h.deps()
			setup(h, &deps)
			if _, err := runWorkflow(context.Background(), h.config, deps); !errors.Is(err, errGrantUnusable) {
				t.Fatalf("err = %v", err)
			}
			if h.fake.called("create_resume") != 0 || h.fake.called("list_resumes") != 0 || h.exists(h.config.Control, createIntentName) {
				t.Fatal("unusable grant reached resume work")
			}
		})
	}
}

func TestRevocationJournalFallsBackToTheGrantCapturedBeforeCreate(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	deps := h.deps()
	calls := 0
	deps.Tokens = func(context.Context) (string, string, error) {
		calls++
		if calls == 1 {
			return fakeAccessToken, fakeRefreshToken, nil
		}
		return string(make([]byte, 600)), "", nil
	}
	result, err := runWorkflow(context.Background(), h.config, deps)
	if err != nil || result != resultCompleted || calls < 2 {
		t.Fatalf("run = %v, %v; token reads %d", result, err, calls)
	}
	if !h.exists(h.config.Control, completionName) || h.exists(h.config.Control, revocationName) || h.fake.revokeCalls == 0 {
		t.Fatal("completion did not journal and revoke the captured grant")
	}
}

func TestCreateIntentPublicationBetweenCrashAndReplayStops(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	h.fake.dropCreate = true
	if _, runErr := h.run(); !errors.Is(runErr, errRecovery) {
		t.Fatal(runErr)
	}
	h.fake.dropCreate = false
	slug := "ada"
	source := h.fake.resumes[fakeSourceID]
	source.summary.Live, source.summary.Slug = true, &slug
	if _, runErr := h.run(); !errors.Is(runErr, errSourceChanged) {
		t.Fatalf("err = %v", runErr)
	}
	if h.fake.called("create_resume") != 1 || len(h.fake.targets()) != 0 || !h.exists(h.config.Control, createIntentName) {
		t.Fatal("publication change allowed a replay or dropped the intent")
	}
}
