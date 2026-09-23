package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

// packageFailures lists every sentinel the runner can return. A new sentinel
// must be added here and mapped in sentinelReasons.
func packageFailures() map[string]error {
	return map[string]error{
		"arguments": errArguments, "control root": errControlRoot, "run root": errRunRoot,
		"run entries": errRunEntries, "unsafe file": errUnsafeFile, "workflow blocked": errWorkflowBlocked,
		"workflow contract": errWorkflowContract, "connect": errConnect, "runtime": errRuntime,
		"tls roots": errTLSRoots, "tls override": errTLSOverride, "browser timeout": errBrowserTimeout, "browser failed": errBrowserFailed,
		"second factor required": errSecondFactorRequired,
		"invalid callback":       errInvalidCallback, "reauthorization disabled": errReauthorizationDisabled,
		"reauthorization unproved": errReauthorizationUnproved, "tool registry": errToolRegistry,
		"cross origin": errCrossOrigin, "invalid origin": errInvalidOrigin, "grant": errGrantUnusable,
		"local proof": errLocalProof, "candidate": errCandidate, "source changed": errSourceChanged,
		"source selection": errSourceSelection, "create rejected": errCreateRejected,
		"create intent": errCreateIntent, "mutation cap": errMutationCap, "tool output": errToolOutput,
		"photo": errPhoto, "evidence": errEvidence, "revocation": errRevocation, "recovery": errRecovery,
		"authorization shape": errAuthorizationShape, "authorization redirect": errAuthorizationRedirect,
		"authorization scope": errAuthorizationScope, "authorization resource": errAuthorizationResource,
		"authorization challenge": errAuthorizationChallenge, "callback bind": errCallbackBind,
	}
}

func TestEvidenceFailureWordsAreClosedForEverySentinel(t *testing.T) {
	for name, failure := range packageFailures() {
		word := failureReason(failure)
		if !closedFailureWords[word] {
			t.Fatalf("%s maps to %q, which is outside the closed vocabulary", name, word)
		}
		if word == reasonInternal {
			t.Fatalf("%s falls back to the internal word instead of naming its stage", name)
		}
	}
	for _, mapping := range append(append([]struct {
		err  error
		word string
	}{}, sentinelReasons...), genericReasons...) {
		if !closedFailureWords[mapping.word] {
			t.Fatalf("mapping word %q is outside the closed vocabulary", mapping.word)
		}
	}
	for _, mapping := range sdkPhrases {
		if !closedFailureWords[mapping.word] {
			t.Fatalf("SDK phrase word %q is outside the closed vocabulary", mapping.word)
		}
	}
	if failureReason(errors.New("unmapped")) != reasonInternal || failureReason(nil) != reasonInternal {
		t.Fatal("an unmapped failure does not fall back to the internal word")
	}
}

func TestEvidenceFailureReportPrintsOneClosedLine(t *testing.T) {
	run := testArtifacts(t, runScope)
	for name, failure := range packageFailures() {
		var report strings.Builder
		if err := reportFailure(&report, run, failure); err != nil {
			t.Fatal(err)
		}
		line := report.String()
		word, found := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "mcp-workflow: ")
		if !found || strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") || !closedFailureWords[word] {
			t.Fatalf("%s printed %q", name, line)
		}
		for _, r := range word {
			if (r < 'a' || r > 'z') && r != '-' {
				t.Fatalf("%s printed a word with dynamic text: %q", name, word)
			}
		}
	}
}

func TestEvidenceFailureReportHidesDynamicText(t *testing.T) {
	run := testArtifacts(t, runScope)
	secrets := []string{
		"https://aboutme.vn/oauth/authorize?state=abc", fakeSourceID, fakeAccessToken,
		"/home/runner/.dev/mcp-workflow-local/control/run.XYZ", "Lan Fixture", "localhost:20443",
	}
	for _, secret := range secrets {
		for _, failure := range []error{
			fmt.Errorf("%w: %s", errConnect, secret),
			errors.Join(errRecovery, errors.New(secret)),
			errors.New(secret),
		} {
			var report strings.Builder
			if err := reportFailure(&report, run, failure); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(report.String(), secret) {
				t.Fatalf("report leaked %q: %q", secret, report.String())
			}
		}
	}
	var unmapped strings.Builder
	if err := reportFailure(&unmapped, run, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	if unmapped.String() != "mcp-workflow: "+reasonInternal+"\n" {
		t.Fatalf("unmapped report = %q", unmapped.String())
	}
}

// Passkeys are live in production and the owner workflow signs in with
// password only, so a 202 secondFactorRequired login response must read as
// its own word rather than the generic browser-handoff failure a stuck
// consent page or a rejected password also produces.
func TestEvidenceFailureWordNamesSecondFactorDistinctly(t *testing.T) {
	if got := failureReason(errSecondFactorRequired); got != reasonSecondFactor {
		t.Fatalf("failureReason(errSecondFactorRequired) = %q, want %q", got, reasonSecondFactor)
	}
	if reasonSecondFactor == reasonBrowser {
		t.Fatal("second-factor-required collapses into the generic browser-handoff word")
	}
}

func TestEvidenceFailureReportStaysSilentOnceEvidenceExists(t *testing.T) {
	run := testArtifacts(t, runScope)
	if err := writeEvidence(run, recoveryEvidence(modeLocal, revocationUnconfirmed)); err != nil {
		t.Fatal(err)
	}
	var report strings.Builder
	if err := reportFailure(&report, run, errRevocation); err != nil {
		t.Fatal(err)
	}
	if report.String() != "" {
		t.Fatalf("report = %q, want silence beside evidence", report.String())
	}
}

func TestEvidenceFailureWordsNameTransportStages(t *testing.T) {
	timeout := &net.OpError{Op: "dial", Err: &timeoutError{}}
	for name, test := range map[string]struct {
		failure error
		want    string
	}{
		"unknown authority":  {errors.Join(errConnect, &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}), reasonTLS},
		"hostname mismatch":  {errors.Join(errConnect, x509.HostnameError{Host: "localhost"}), reasonTLS},
		"plain text server":  {errors.Join(errConnect, tls.RecordHeaderError{Msg: "first record"}), reasonTLS},
		"trust store":        {errTLSRoots, reasonTLS},
		"dial failure":       {errors.Join(errConnect, timeout), reasonNetwork},
		"discovery failure":  {errors.Join(errConnect, errors.New("something unnamed")), reasonDiscovery},
		"registration":       {errors.Join(errConnect, errors.New("failed to register client: registration request failed")), reasonRegistration},
		"server metadata":    {errors.Join(errConnect, errors.New("failed to get authorization server metadata: 404")), reasonMetadata},
		"issuer mismatch":    {errors.Join(errConnect, errors.New("authorization response issuer does not match expected issuer")), reasonIssuer},
		"state mismatch":     {errors.Join(errConnect, errors.New("state mismatch")), reasonStateMismatch},
		"token exchange":     {errors.Join(errConnect, errors.New("token exchange failed: 400")), reasonExchange},
		"authorize scope":    {errors.Join(errConnect, errAuthorizationScope), reasonAuthScope},
		"authorize resource": {errors.Join(errConnect, errAuthorizationResource), reasonAuthResource},
		"callback bind":      {errors.Join(errConnect, errCallbackBind), reasonCallbackBind},
		"refused reauth":     {errors.Join(errReauthorizationDisabled, errAuthorizationShape), reasonAuthShape},
		"browser handoff":    {errors.Join(errConnect, errBrowserTimeout), reasonBrowser},
		"blocked reauth":     {errors.Join(errConnect, errReauthorizationDisabled), reasonReauth},
		"callback rejection": {errors.Join(errConnect, errInvalidCallback), reasonCallback},
	} {
		if got := failureReason(test.failure); got != test.want {
			t.Fatalf("%s = %q, want %q", name, got, test.want)
		}
	}
}

type timeoutError struct{}

func (timeoutError) Error() string { return "timeout" }
func (timeoutError) Timeout() bool { return true }

func TestEvidenceFailureWordsIgnoreJoinedCleanupErrors(t *testing.T) {
	closed := &net.OpError{Op: "close", Net: "tcp", Err: net.ErrClosed}
	if !connectionFailure(closed) {
		t.Fatal("a closed-listener error is not recognized as a connection shape")
	}
	for name, test := range map[string]struct {
		failure error
		want    string
	}{
		"browser handoff": {errors.Join(errConnect, errBrowserFailed, closed), reasonBrowser},
		"browser timeout": {errors.Join(errConnect, errBrowserTimeout, closed), reasonBrowser},
		"callback":        {errors.Join(errConnect, errInvalidCallback, closed), reasonCallback},
		"authorize scope": {errors.Join(errConnect, errAuthorizationScope, closed), reasonAuthScope},
		"refused reauth":  {errors.Join(errConnect, errReauthorizationDisabled, closed), reasonReauth},
		"registration":    {errors.Join(errConnect, errors.New("failed to register client: 400"), closed), reasonRegistration},
		"dial failure":    {errors.Join(errConnect, &net.OpError{Op: "dial", Err: &timeoutError{}}), reasonNetwork},
		"handshake":       {errors.Join(errConnect, &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, closed), reasonTLS},
	} {
		if got := failureReason(test.failure); got != test.want {
			t.Fatalf("%s = %q, want %q", name, got, test.want)
		}
	}
}
