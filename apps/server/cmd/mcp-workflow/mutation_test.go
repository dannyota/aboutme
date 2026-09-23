package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func boundGuard(t *testing.T, local bool) (*toolGuard, *fakeMCP, createIntent) {
	t.Helper()
	fake := newFakeMCP(t, true)
	guard := newToolGuard(fake, local)
	if sourceErr := guard.setSource(fakeSourceID); sourceErr != nil {
		t.Fatal(sourceErr)
	}
	source, err := readSource(context.Background(), guard, fakeSourceID)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := validateCandidate(source.Canonical, candidateFrom(t, source.Canonical, translateFixture))
	if err != nil {
		t.Fatal(err)
	}
	intent := newCreateIntent(productionOrigin, []resumeSummary{source.State.resumeSummary}, source, payload, "00000000-0000-4000-8000-000000000001", fakeEpoch)
	if bindErr := guard.bindIntent(intent, true); bindErr != nil {
		t.Fatal(bindErr)
	}
	return guard, fake, intent
}

func TestMutationCapsRefuseEveryToolOutsideTheWorkflowBeforeSending(t *testing.T) {
	guard, fake, _ := boundGuard(t, false)
	before := len(fake.calls)
	for _, name := range append(requiredTools[:], "unknown_tool", "") {
		if readTools[name] || mutationTools[name] {
			continue
		}
		if _, err := guard.call(context.Background(), name, map[string]any{"resume_id": fakeSourceID}); !errors.Is(err, errMutationCap) {
			t.Fatalf("%q err = %v", name, err)
		}
	}
	if len(fake.calls) != before || fake.called("delete_resume") != 0 {
		t.Fatalf("refused tools reached the server: %v", fake.calls[before:])
	}
	if len(readTools)+len(mutationTools) != 6 {
		t.Fatal("allowlist grew beyond reads, create, upload, and crop")
	}
}

func TestMutationCapsSourceCannotInhabitTargetWrites(t *testing.T) {
	// Method expressions reach unexported methods; In(0) is the receiver,
	// In(1) the context, and In(2) the resume handle.
	for method, value := range map[string]any{
		"uploadPhoto":    (*toolGuard).uploadPhoto,
		"cropPhoto":      (*toolGuard).cropPhoto,
		"staleCropProbe": (*toolGuard).staleCropProbe,
	} {
		if reflect.TypeOf(value).In(2) != reflect.TypeOf(targetHandle("")) {
			t.Fatalf("%s does not require a targetHandle", method)
		}
	}
	guard, fake, _ := boundGuard(t, false)
	if _, bindErr := guard.bindTarget(fakeSourceID); !errors.Is(bindErr, errMutationCap) {
		t.Fatalf("bound the source as target: %v", bindErr)
	}
	if _, _, uploadErr := guard.uploadPhoto(context.Background(), targetHandle(fakeSourceID), "3", "00000000-0000-4000-8000-000000000002", []byte{1}); !errors.Is(uploadErr, errMutationCap) {
		t.Fatalf("source upload err = %v", uploadErr)
	}
	if fake.called("upload_photo") != 0 {
		t.Fatal("source write reached the server")
	}
}

func TestMutationCapsOneCreatePerProcessAndOneWritePerKind(t *testing.T) {
	guard, fake, intent := boundGuard(t, false)
	ctx, deadline := context.Background(), time.Now().Add(time.Minute)
	state, outcome, createErr := guard.create(ctx, deadline)
	if createErr != nil || outcome != createCreated {
		t.Fatalf("create = %v, %v", outcome, createErr)
	}
	if _, _, secondErr := guard.create(ctx, deadline); !errors.Is(secondErr, errMutationCap) {
		t.Fatalf("second create err = %v", secondErr)
	}
	if rebindErr := guard.bindIntent(intent, true); !errors.Is(rebindErr, errMutationCap) {
		t.Fatal("rebound a second intent")
	}
	target, bindErr := guard.bindTarget(state.ID)
	if bindErr != nil {
		t.Fatal(bindErr)
	}
	if _, otherErr := guard.bindTarget(fakeOtherID); !errors.Is(otherErr, errMutationCap) {
		t.Fatal("rebound a second target")
	}
	photo := fake.resumes[fakeSourceID].photo.Data
	output, code, uploadErr := guard.uploadPhoto(ctx, target, "1", "00000000-0000-4000-8000-000000000003", photo)
	if uploadErr != nil || code != "" {
		t.Fatalf("upload = %q, %v", code, uploadErr)
	}
	if _, _, againErr := guard.uploadPhoto(ctx, target, output.Revision, "00000000-0000-4000-8000-000000000004", photo); !errors.Is(againErr, errMutationCap) {
		t.Fatalf("second upload err = %v", againErr)
	}
	crop := &photoCrop{X: 0.1, Y: 0.05, Width: 0.8, Height: 0.8}
	if _, _, cropErr := guard.cropPhoto(ctx, target, output.Revision, "00000000-0000-4000-8000-000000000005", crop); cropErr != nil {
		t.Fatal(cropErr)
	}
	if _, _, cropAgainErr := guard.cropPhoto(ctx, target, "3", "00000000-0000-4000-8000-000000000006", crop); !errors.Is(cropAgainErr, errMutationCap) {
		t.Fatalf("second crop err = %v", cropAgainErr)
	}
	if _, probeErr := guard.staleCropProbe(ctx, target, "2", "00000000-0000-4000-8000-000000000007", crop); !errors.Is(probeErr, errMutationCap) {
		t.Fatal("production allowed the local conflict probe")
	}
	if fake.called("create_resume") != 1 || fake.called("upload_photo") != 1 || fake.called("update_photo_crop") != 1 {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestMutationCapsConcurrentCreatesHaveOneSender(t *testing.T) {
	guard, fake, _ := boundGuard(t, false)
	results := make(chan error, 8)
	for range 8 {
		go func() {
			_, _, err := guard.create(context.Background(), time.Now().Add(time.Minute))
			results <- err
		}()
	}
	winners := 0
	for range 8 {
		if <-results == nil {
			winners++
		}
	}
	if winners != 1 || fake.called("create_resume") != 1 {
		t.Fatalf("winners = %d creates = %d", winners, fake.called("create_resume"))
	}
}

func TestMutationCapsBoundTotalToolCalls(t *testing.T) {
	guard, _, _ := boundGuard(t, false)
	guard.calls = maxToolCalls
	if _, err := guard.list(context.Background()); !errors.Is(err, errMutationCap) {
		t.Fatalf("call over budget err = %v", err)
	}
}

func TestMutationCapsDefinitiveOnlyOnFirstSendValidation(t *testing.T) {
	for name, fresh := range map[string]bool{"first send": true, "replay": false} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeMCP(t, false)
			fake.rejectCreate = "validation_failed"
			guard := newToolGuard(fake, false)
			if err := guard.setSource(fakeSourceID); err != nil {
				t.Fatal(err)
			}
			_, _, intent := boundGuard(t, false)
			if err := guard.bindIntent(intent, fresh); err != nil {
				t.Fatal(err)
			}
			_, outcome, createErr := guard.create(context.Background(), time.Now().Add(time.Minute))
			if want := map[bool]createOutcome{true: createDefinitiveFailure, false: createUnknown}[fresh]; outcome != want || createErr == nil {
				t.Fatalf("outcome = %v, %v; want %v", outcome, createErr, want)
			}
		})
	}
}

func TestMutationRevisionChain(t *testing.T) {
	for _, test := range []struct {
		previous, next string
		want           bool
	}{
		{"7", "8", true}, {"7", "9", false}, {"7", "7", false}, {"01", "2", false}, {"1", "02", false},
		{"18446744073709551615", "0", false}, {"18446744073709551614", "18446744073709551615", true},
	} {
		if got := nextDecimalRevision(test.previous, test.next); got != test.want {
			t.Errorf("nextDecimalRevision(%q, %q) = %t, want %t", test.previous, test.next, got, test.want)
		}
	}
}

func TestMutationToolOutputAcceptsSDKTextFallbackOnly(t *testing.T) {
	good := fakeResult(getPhotoResult{ContentType: "image/png", DataBase64: "AA=="})
	if _, err := decodeToolOutput[getPhotoResult](good, maxListResultBytes); err != nil {
		t.Fatalf("SDK result shape rejected: %v", err)
	}
	for name, result := range map[string]*mcp.CallToolResult{
		"error":      fakeError("revision_conflict"),
		"unknown":    fakeResult(map[string]any{"content_type": "image/png", "data_base64": "AA==", "extra": true}),
		"oversized":  fakeResult(getPhotoResult{ContentType: "image/png", DataBase64: string(make([]byte, maxListResultBytes))}),
		"two blocks": {StructuredContent: getPhotoResult{}, Content: []mcp.Content{&mcp.TextContent{Text: "{}"}, &mcp.TextContent{Text: "{}"}}},
	} {
		if _, err := decodeToolOutput[getPhotoResult](result, maxListResultBytes); !errors.Is(err, errToolOutput) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if code, ok := toolErrorCode(fakeError("revision_conflict")); !ok || code != "revision_conflict" {
		t.Fatal("closed error code not recognized")
	}
	if _, ok := toolErrorCode(fakeError("stack trace: secret")); ok {
		t.Fatal("open error text treated as a closed code")
	}
}

func TestMutationCapsDirectCallsCannotNameTheSourceOrAnotherIntent(t *testing.T) {
	guard, fake, intent := boundGuard(t, false)
	ctx := context.Background()
	state, _, createErr := guard.create(ctx, time.Now().Add(time.Minute))
	if createErr != nil {
		t.Fatal(createErr)
	}
	if _, bindErr := guard.bindTarget(state.ID); bindErr != nil {
		t.Fatal(bindErr)
	}
	before := len(fake.calls)
	for name, arguments := range map[string]map[string]any{
		"upload_photo":      {"idempotency_key": "00000000-0000-4000-8000-000000000010", "resume_id": fakeSourceID, "revision": "3", "data_base64": "AA=="},
		"update_photo_crop": {"idempotency_key": "00000000-0000-4000-8000-000000000011", "resume_id": fakeSourceID, "revision": "3", "crop": nil},
	} {
		if _, err := guard.call(ctx, name, arguments); !errors.Is(err, errMutationCap) {
			t.Fatalf("direct %s on the source err = %v", name, err)
		}
	}
	for name, arguments := range map[string]map[string]any{
		"other key":     {"idempotency_key": "00000000-0000-4000-8000-000000000012", "lng": "vi", "title": targetTitle, "document": intent.Payload},
		"other payload": {"idempotency_key": intent.IdempotencyKey, "lng": "vi", "title": targetTitle, "document": json.RawMessage(`{}`)},
		"names resume":  {"idempotency_key": intent.IdempotencyKey, "resume_id": fakeSourceID, "document": intent.Payload},
	} {
		if _, err := guard.call(ctx, "create_resume", arguments); !errors.Is(err, errMutationCap) {
			t.Fatalf("direct create with %s err = %v", name, err)
		}
	}
	if len(fake.calls) != before {
		t.Fatalf("refused direct calls reached the server: %v", fake.calls[before:])
	}
}
