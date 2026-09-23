package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testJournal(now time.Time, origin string) revocationJournal {
	return revocationJournal{
		Version: journalVersion, Origin: origin, Workflow: workflowLabel, TargetConfirmed: true, TargetConfirmedAt: now,
		RevokeEndpoint: origin + "/oauth/revoke", AccessToken: "access-a", RefreshToken: "refresh-a", TokenTypeHint: tokenHintRefresh,
		AccessUntil: now.Add(time.Hour), RefreshUntil: now.Add(24 * time.Hour), Status: statusPending,
	}
}

func TestRecoveryJournalRejectsMalformedFields(t *testing.T) {
	now := fakeEpoch
	for name, mutate := range map[string]func(*revocationJournal){
		"origin":        func(j *revocationJournal) { j.Origin = localOrigin },
		"endpoint":      func(j *revocationJournal) { j.RevokeEndpoint = productionOrigin + "/oauth/other" },
		"confirmation":  func(j *revocationJournal) { j.TargetConfirmed = false },
		"workflow":      func(j *revocationJournal) { j.Workflow = "other" },
		"token hint":    func(j *revocationJournal) { j.TokenTypeHint = "access_token" },
		"status":        func(j *revocationJournal) { j.Status = "revoked" },
		"cutoff order":  func(j *revocationJournal) { j.AccessUntil = j.RefreshUntil.Add(time.Second) },
		"header inject": func(j *revocationJournal) { j.RefreshToken = "refresh\r\nCookie: x" },
		"same tokens":   func(j *revocationJournal) { j.RefreshToken = j.AccessToken },
		"zero cutoff":   func(j *revocationJournal) { j.RefreshUntil = time.Time{} },
	} {
		journal := testJournal(now, productionOrigin)
		mutate(&journal)
		if validJournal(journal, productionOrigin) {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestRecoveryJournalIsNeverOverwrittenByANewGrant(t *testing.T) {
	control := testArtifacts(t, controlScope)
	journal := testJournal(fakeEpoch, productionOrigin)
	if err := persistJournal(control, journal, productionOrigin, false); err != nil {
		t.Fatal(err)
	}
	next := journal
	next.RefreshToken = "refresh-b"
	if err := persistJournal(control, next, productionOrigin, false); !errors.Is(err, errRecovery) {
		t.Fatalf("first-journal write replaced an unresolved journal: %v", err)
	}
	loaded, present, err := loadJournal(control, productionOrigin)
	if err != nil || !present || loaded.RefreshToken != journal.RefreshToken {
		t.Fatal("unresolved journal changed")
	}
}

func TestCompletionSentinelMustMatchJournal(t *testing.T) {
	control := testArtifacts(t, controlScope)
	journal := testJournal(fakeEpoch, productionOrigin)
	if err := ensureSentinel(control, journal, productionOrigin); err != nil {
		t.Fatal(err)
	}
	if err := ensureSentinel(control, journal, productionOrigin); err != nil {
		t.Fatalf("matching sentinel rejected: %v", err)
	}
	conflict := journal
	conflict.TargetConfirmedAt = conflict.TargetConfirmedAt.Add(time.Second)
	if err := ensureSentinel(control, conflict, productionOrigin); !errors.Is(err, errRecovery) {
		t.Fatalf("conflicting sentinel accepted: %v", err)
	}
	data, err := control.read(completionName)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"access", "refresh", fakeSourceID, "revoke"} {
		if strings.Contains(strings.ToLower(string(data)), forbidden) {
			t.Fatalf("sentinel contains %q", forbidden)
		}
	}
}

// scriptedRevocation returns fixed outcomes and records the durable state
// seen at each call. It never loops and has a fixed call budget.
type scriptedRevocation struct {
	control       privateArtifacts
	probes        []probeOutcome
	refreshes     []refreshOutcome
	calls         []string
	revokedTokens []string
	journalAtCall []string
}

type probeOutcome struct {
	status int
	date   time.Time
	err    error
}

func (s *scriptedRevocation) record(name string) {
	s.calls = append(s.calls, name)
	journal, present, err := loadJournal(s.control, productionOrigin)
	if err != nil || !present {
		s.journalAtCall = append(s.journalAtCall, "")
		return
	}
	s.journalAtCall = append(s.journalAtCall, journal.RefreshToken)
}

func (s *scriptedRevocation) Revoke(_ context.Context, refreshToken string) error {
	if len(s.calls) >= 12 {
		return errFakeBudget
	}
	s.record("revoke")
	s.revokedTokens = append(s.revokedTokens, refreshToken)
	return nil
}

func (s *scriptedRevocation) Probe(context.Context, string) (int, time.Time, error) {
	if len(s.calls) >= 12 || len(s.probes) == 0 {
		return 0, time.Time{}, errFakeBudget
	}
	s.record("probe")
	next := s.probes[0]
	s.probes = s.probes[1:]
	return next.status, next.date, next.err
}

func (s *scriptedRevocation) Refresh(context.Context, string) (refreshOutcome, error) {
	if len(s.calls) >= 12 || len(s.refreshes) == 0 {
		return refreshOutcome{}, errFakeBudget
	}
	s.record("refresh")
	next := s.refreshes[0]
	s.refreshes = s.refreshes[1:]
	return next, nil
}

func TestRevocationSettlementMatrix(t *testing.T) {
	now := fakeEpoch
	journal := testJournal(now, productionOrigin)
	afterAccess := journal.AccessUntil.Add(time.Second)
	for name, test := range map[string]struct {
		probes    []probeOutcome
		refreshes []refreshOutcome
		want      error
		refresh   string
	}{
		"timely 401":                {probes: []probeOutcome{{http.StatusUnauthorized, now, nil}}, refresh: "refresh-a"},
		"401 at access cutoff":      {probes: []probeOutcome{{http.StatusUnauthorized, journal.AccessUntil, nil}}, refreshes: []refreshOutcome{{Dead: true, ServerDate: afterAccess}}, refresh: "refresh-a"},
		"live before cutoff":        {probes: []probeOutcome{{http.StatusOK, now, nil}}, want: errRevocation, refresh: "refresh-a"},
		"probe unavailable":         {probes: []probeOutcome{{0, time.Time{}, errFakeLost}}, want: errRevocation, refresh: "refresh-a"},
		"missing date":              {probes: []probeOutcome{{http.StatusUnauthorized, time.Time{}, nil}}, want: errRevocation, refresh: "refresh-a"},
		"past refresh cutoff":       {probes: []probeOutcome{{http.StatusOK, journal.RefreshUntil, nil}}, want: errRevocation, refresh: "refresh-a"},
		"invalid grant after cut":   {probes: []probeOutcome{{http.StatusOK, afterAccess, nil}}, refreshes: []refreshOutcome{{Dead: true, ServerDate: journal.RefreshUntil}}, want: errRevocation, refresh: "refresh-a"},
		"rotation then timely 401":  {probes: []probeOutcome{{http.StatusOK, afterAccess, nil}, {http.StatusUnauthorized, afterAccess.Add(time.Second), nil}}, refreshes: []refreshOutcome{{AccessToken: "access-b", RefreshToken: "refresh-b", ServerDate: afterAccess}}, refresh: "refresh-b"},
		"rotation then still live":  {probes: []probeOutcome{{http.StatusOK, afterAccess, nil}, {http.StatusOK, afterAccess.Add(time.Second), nil}}, refreshes: []refreshOutcome{{AccessToken: "access-b", RefreshToken: "refresh-b", ServerDate: afterAccess}}, want: errRevocation, refresh: "refresh-b"},
		"refresh response unknown":  {probes: []probeOutcome{{http.StatusOK, afterAccess, nil}}, want: errRevocation, refresh: "refresh-a"},
		"rotation twice never ends": {probes: []probeOutcome{{http.StatusOK, afterAccess, nil}, {http.StatusOK, afterAccess.Add(2 * time.Hour), nil}}, refreshes: []refreshOutcome{{AccessToken: "access-b", RefreshToken: "refresh-b", ServerDate: afterAccess}, {AccessToken: "access-c", RefreshToken: "refresh-c", ServerDate: afterAccess.Add(2 * time.Hour)}}, want: errRevocation, refresh: "refresh-c"},
	} {
		t.Run(name, func(t *testing.T) {
			control := testArtifacts(t, controlScope)
			if err := persistJournal(control, journal, productionOrigin, false); err != nil {
				t.Fatal(err)
			}
			transport := &scriptedRevocation{control: control, probes: test.probes, refreshes: test.refreshes}
			err := settleRevocation(context.Background(), transport, control, productionOrigin, journal)
			if (test.want == nil) != (err == nil) || (test.want != nil && !errors.Is(err, test.want)) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
			stored, present, loadErr := loadJournal(control, productionOrigin)
			if loadErr != nil || !present || stored.RefreshToken != test.refresh {
				t.Fatalf("journal = %q present=%t err=%v, want %q", stored.RefreshToken, present, loadErr, test.refresh)
			}
			revokes := 0
			for index, call := range transport.calls {
				if call != "revoke" {
					continue
				}
				if transport.revokedTokens[revokes] != transport.journalAtCall[index] {
					t.Fatal("a refresh token was revoked before it was journaled")
				}
				revokes++
			}
		})
	}
}

func TestRecoveryOnlyRestoresSentinelBeforeAnyRevocationAction(t *testing.T) {
	control := testArtifacts(t, controlScope)
	journal := testJournal(fakeEpoch, productionOrigin)
	if err := persistJournal(control, journal, productionOrigin, false); err != nil {
		t.Fatal(err)
	}
	if err := control.createExclusive(createIntentName, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	sentinelFirst := false
	transport := &sentinelCheckingRevocation{check: func() {
		present, err := control.exists(completionName)
		sentinelFirst = err == nil && present
	}}
	if err := recoverRevocationOnly(context.Background(), transport, control, productionOrigin, false); err != nil {
		t.Fatal(err)
	}
	if !sentinelFirst || transport.calls != 2 {
		t.Fatalf("sentinel before revoke = %t calls = %d", sentinelFirst, transport.calls)
	}
	for name, want := range map[string]bool{completionName: true, revocationName: false, createIntentName: false} {
		if present, err := control.exists(name); err != nil || present != want {
			t.Fatalf("%s present = %t err = %v", name, present, err)
		}
	}
}

type sentinelCheckingRevocation struct {
	check func()
	calls int
}

func (s *sentinelCheckingRevocation) Revoke(context.Context, string) error {
	s.calls++
	s.check()
	return nil
}

func (s *sentinelCheckingRevocation) Probe(context.Context, string) (int, time.Time, error) {
	s.calls++
	return http.StatusUnauthorized, fakeEpoch, nil
}

func (s *sentinelCheckingRevocation) Refresh(context.Context, string) (refreshOutcome, error) {
	s.calls++
	return refreshOutcome{}, errFakeBudget
}

func TestRecoveryRetainsJournalOnConflictingOrUnwritableSentinel(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, control privateArtifacts){
		"conflicting": func(t *testing.T, control privateArtifacts) {
			other := testJournal(fakeEpoch.Add(time.Minute), productionOrigin)
			if err := ensureSentinel(control, other, productionOrigin); err != nil {
				t.Fatal(err)
			}
		},
		"unwritable": func(t *testing.T, control privateArtifacts) {
			if err := os.Mkdir(filepath.Join(control.root, completionName), privateDirectoryMode); err != nil {
				t.Fatal(err)
			}
		},
		"malformed": func(t *testing.T, control privateArtifacts) {
			if err := control.createExclusive(completionName, []byte(`{"version":1}`)); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			control := testArtifacts(t, controlScope)
			if err := persistJournal(control, testJournal(fakeEpoch, productionOrigin), productionOrigin, false); err != nil {
				t.Fatal(err)
			}
			setup(t, control)
			transport := &sentinelCheckingRevocation{check: func() {}}
			if err := recoverRevocationOnly(context.Background(), transport, control, productionOrigin, false); !errors.Is(err, errRecovery) {
				t.Fatalf("err = %v", err)
			}
			if present, err := control.exists(revocationName); err != nil || !present || transport.calls != 0 {
				t.Fatalf("journal present=%t calls=%d err=%v", present, transport.calls, err)
			}
		})
	}
}
