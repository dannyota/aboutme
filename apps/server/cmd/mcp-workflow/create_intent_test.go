package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func testIntent(t *testing.T) createIntent {
	t.Helper()
	_, _, intent := boundGuard(t, false)
	return intent
}

func TestCreateIntentValidatesEveryBinding(t *testing.T) {
	valid := testIntent(t)
	if !validCreateIntent(valid, productionOrigin) {
		t.Fatal("valid intent rejected")
	}
	for name, mutate := range map[string]func(*createIntent){
		"version":          func(i *createIntent) { i.Version = 2 },
		"origin":           func(i *createIntent) { i.Origin = localOrigin },
		"account binding":  func(i *createIntent) { i.AccountBinding = digest([]byte("other")) },
		"baseline":         func(i *createIntent) { i.BaselineIDs = append(i.BaselineIDs, fakeOtherID) },
		"source absent":    func(i *createIntent) { i.SourceID = fakeOtherID },
		"source revision":  func(i *createIntent) { i.SourceRevision = "03" },
		"source digest":    func(i *createIntent) { i.SourceDigest = "short" },
		"photo digest":     func(i *createIntent) { i.PhotoDigest = "short" },
		"payload digest":   func(i *createIntent) { i.PayloadDigest = digest([]byte("other")) },
		"payload shape":    func(i *createIntent) { i.Payload = []byte(`{"a":1}`); i.PayloadDigest = digest(i.Payload) },
		"key":              func(i *createIntent) { i.IdempotencyKey = "not-a-uuid" },
		"language":         func(i *createIntent) { i.Language = "en" },
		"title":            func(i *createIntent) { i.Title = "Other" },
		"server time":      func(i *createIntent) { i.ServerTime = time.Time{} },
		"attempt reversed": func(i *createIntent) { i.FirstAttempt = i.ServerTime.Add(-time.Second) },
		"noncanonical payload": func(i *createIntent) {
			i.Payload = append([]byte(" "), i.Payload...)
			i.PayloadDigest = digest(i.Payload)
		},
	} {
		intent := valid
		intent.BaselineIDs = append([]string(nil), valid.BaselineIDs...)
		mutate(&intent)
		if validCreateIntent(intent, productionOrigin) {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestCreateIntentPersistsOnceAndReloadsExactly(t *testing.T) {
	control := testArtifacts(t, controlScope)
	intent := testIntent(t)
	if err := persistCreateIntent(control, intent); err != nil {
		t.Fatal(err)
	}
	second := intent
	second.IdempotencyKey = "00000000-0000-4000-8000-000000000099"
	if err := persistCreateIntent(control, second); !errors.Is(err, errUnsafeFile) {
		t.Fatalf("second intent err = %v", err)
	}
	loaded, present, err := loadCreateIntent(control, productionOrigin)
	if err != nil || !present || !reflect.DeepEqual(loaded, intent) || !bytes.Equal(loaded.Payload, intent.Payload) {
		t.Fatalf("reload = %t, %v", present, err)
	}
	if _, _, localErr := loadCreateIntent(control, localOrigin); !errors.Is(localErr, errCreateIntent) {
		t.Fatal("intent reloaded under another origin")
	}
}

func TestCreateIntentMalformedDurableStateBlocksCreates(t *testing.T) {
	for name, data := range map[string][]byte{
		"truncated": []byte(`{"version":1`),
		"unknown":   []byte(`{"version":1,"extra":true}`),
		"empty":     []byte(`{}`),
	} {
		t.Run(name, func(t *testing.T) {
			control := testArtifacts(t, controlScope)
			if err := control.createExclusive(createIntentName, data); err != nil {
				t.Fatal(err)
			}
			if _, present, err := loadCreateIntent(control, productionOrigin); !present || !errors.Is(err, errCreateIntent) {
				t.Fatalf("present=%t err=%v", present, err)
			}
			h := newWorkflowHarness(t, modeProduction, false)
			if err := h.config.Control.createExclusive(createIntentName, data); err != nil {
				t.Fatal(err)
			}
			if _, runErr := h.run(); !errors.Is(runErr, errRecovery) || h.connects != 0 {
				t.Fatalf("run err = %v connects = %d", runErr, h.connects)
			}
		})
	}
}

func TestCreateIntentReplayDeadline(t *testing.T) {
	intent := testIntent(t)
	local := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		server time.Time
		want   bool
	}{
		{"baseline", intent.ServerTime, true},
		{"eleven hours", intent.ServerTime.Add(11 * time.Hour), true},
		{"reversed", intent.ServerTime.Add(-time.Second), false},
		{"zero", time.Time{}, false},
		{"window crosses cutoff", intent.ServerTime.Add(createReplayLifetime - createSendWindow), false},
		{"last safe second", intent.ServerTime.Add(createReplayLifetime - createSendWindow - 2*serverDateResolution), true},
		{"at cutoff", intent.ServerTime.Add(createReplayLifetime), false},
	} {
		deadline, ok := replayDeadline(intent, test.server, local)
		if ok != test.want || (ok && !deadline.Equal(local.Add(createSendWindow))) {
			t.Errorf("%s: ok=%t deadline=%v", test.name, ok, deadline)
		}
	}
}

func TestCreateIntentPayloadIsByteEquivalentOnReplay(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	h.fake.dropCreate = true
	if _, runErr := h.run(); !errors.Is(runErr, errRecovery) {
		t.Fatal(runErr)
	}
	h.fake.dropCreate = false
	if _, runErr := h.run(); runErr != nil {
		t.Fatal(runErr)
	}
	var sent [][]byte
	var keys []any
	for index, name := range h.fake.calls {
		if name != "create_resume" {
			continue
		}
		encoded, err := json.Marshal(h.fake.arguments[index]["document"])
		if err != nil {
			t.Fatal(err)
		}
		sent = append(sent, encoded)
		keys = append(keys, h.fake.arguments[index]["idempotency_key"])
	}
	if len(sent) != 2 || !bytes.Equal(sent[0], sent[1]) || keys[0] != keys[1] {
		t.Fatal("replay changed the payload or key")
	}
}

func TestCreateIntentSendWindowExpiresBeforeFirstSend(t *testing.T) {
	h := newWorkflowHarness(t, modeProduction, false)
	deps := h.deps()
	calls := 0
	deps.LocalNow = func() time.Time {
		calls++
		return fakeEpoch.Add(time.Duration(calls) * createSendWindow)
	}
	if _, err := runWorkflow(context.Background(), h.config, deps); !errors.Is(err, errRecovery) {
		t.Fatalf("err = %v", err)
	}
	if h.fake.called("create_resume") != 0 || !h.exists(h.config.Control, createIntentName) {
		t.Fatal("create sent after the one-minute window or intent dropped")
	}
}
