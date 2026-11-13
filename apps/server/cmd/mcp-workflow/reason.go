package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

const (
	reasonArguments     = "arguments"
	reasonControlRoot   = "control-root"
	reasonRunRoot       = "run-root"
	reasonEntrySet      = "entry-set"
	reasonArtifact      = "artifact-mode"
	reasonTLS           = "tls"
	reasonNetwork       = "network"
	reasonDiscovery     = "discovery"
	reasonOAuth         = "oauth"
	reasonRegistration  = "registration"
	reasonMetadata      = "metadata"
	reasonIssuer        = "issuer"
	reasonStateMismatch = "state-mismatch"
	reasonExchange      = "token-exchange"
	reasonAuthShape     = "authorize-shape"
	reasonAuthRedirect  = "authorize-redirect"
	reasonAuthScope     = "authorize-scope"
	reasonAuthResource  = "authorize-resource"
	reasonAuthChallenge = "authorize-challenge"
	reasonCallback      = "callback"
	reasonCallbackBind  = "callback-bind"
	reasonSecondFactor  = "second-factor-required"
	reasonReauth        = "reauthorization"
	reasonBrowser       = "browser-handoff"
	reasonGrant         = "grant"
	reasonSource        = "source-selection"
	reasonSourceChanged = "source-changed"
	reasonCandidate     = "candidate"
	reasonCreateIntent  = "create-intent"
	reasonCreate        = "create-rejected"
	reasonMutationCap   = "mutation-cap"
	reasonToolOutput    = "tool-output"
	reasonPhoto         = "photo"
	reasonRecovery      = "recovery"
	reasonRevocation    = "revocation"
	reasonEvidence      = "evidence"
	reasonWorkflowState = "workflow-state"
	reasonContract      = "contract"
	reasonLocalProof    = "local-proof"
	reasonInternal      = "internal"
)

var closedFailureWords = map[string]bool{
	reasonArguments: true, reasonControlRoot: true, reasonRunRoot: true, reasonEntrySet: true,
	reasonArtifact: true, reasonTLS: true, reasonNetwork: true, reasonDiscovery: true,
	reasonOAuth: true, reasonBrowser: true, reasonGrant: true, reasonSource: true,
	reasonSourceChanged: true, reasonCandidate: true, reasonCreateIntent: true, reasonCreate: true,
	reasonMutationCap: true, reasonToolOutput: true, reasonPhoto: true, reasonRecovery: true,
	reasonRevocation: true, reasonEvidence: true, reasonWorkflowState: true, reasonContract: true,
	reasonLocalProof: true, reasonInternal: true, reasonRegistration: true, reasonMetadata: true,
	reasonIssuer: true, reasonStateMismatch: true, reasonExchange: true, reasonAuthShape: true,
	reasonAuthRedirect: true, reasonAuthScope: true, reasonAuthResource: true, reasonAuthChallenge: true,
	reasonCallback: true, reasonCallbackBind: true, reasonReauth: true,
	reasonSecondFactor: true,
}

var sdkPhrases = []struct {
	phrase string
	word   string
}{
	{"failed to register client", reasonRegistration},
	{"registration request failed", reasonRegistration},
	{"registration_endpoint is required", reasonRegistration},
	{"client registration methods", reasonRegistration},
	{"client metadata", reasonRegistration},
	{"authorization server metadata", reasonMetadata},
	{"protected resource metadata", reasonDiscovery},
	{"WWW-Authenticate", reasonDiscovery},
	{"state mismatch", reasonStateMismatch},
	{"token exchange failed", reasonExchange},
	{"constructing token source failed", reasonExchange},
	{"issuer", reasonIssuer},
}

var sentinelReasons = []struct {
	err  error
	word string
}{
	{errArguments, reasonArguments},
	{errControlRoot, reasonControlRoot},
	{errRunRoot, reasonRunRoot},
	{errRunEntries, reasonEntrySet},
	{errBrowserTimeout, reasonBrowser},
	{errBrowserFailed, reasonBrowser},
	{errSecondFactorRequired, reasonSecondFactor},
	{errAuthorizationShape, reasonAuthShape},
	{errAuthorizationRedirect, reasonAuthRedirect},
	{errAuthorizationScope, reasonAuthScope},
	{errAuthorizationResource, reasonAuthResource},
	{errAuthorizationChallenge, reasonAuthChallenge},
	{errInvalidCallback, reasonCallback},
	{errCallbackBind, reasonCallbackBind},
	{errReauthorizationDisabled, reasonReauth},
	{errReauthorizationUnproved, reasonRevocation},
	{errToolRegistry, reasonDiscovery},
	{errCrossOrigin, reasonNetwork},
	{errInvalidOrigin, reasonNetwork},
	{errGrantUnusable, reasonGrant},
	{errLocalProof, reasonLocalProof},
	{errCandidate, reasonCandidate},
	{errSourceChanged, reasonSourceChanged},
	{errSourceSelection, reasonSource},
	{errCreateRejected, reasonCreate},
	{errCreateIntent, reasonCreateIntent},
	{errMutationCap, reasonMutationCap},
	{errToolOutput, reasonToolOutput},
	{errPhoto, reasonPhoto},
	{errEvidence, reasonEvidence},
	{errRevocation, reasonRevocation},
	{errRecovery, reasonRecovery},
	{errUnsafeFile, reasonArtifact},
	{errWorkflowContract, reasonContract},
	{errTLSRoots, reasonTLS},
	{errTLSOverride, reasonTLS},
}

var genericReasons = []struct {
	err  error
	word string
}{
	{errConnect, reasonDiscovery},
	{errRuntime, reasonNetwork},
	{errWorkflowBlocked, reasonWorkflowState},
}

func failureReason(err error) string {
	if err == nil {
		return reasonInternal
	}
	for _, mapping := range sentinelReasons {
		if errors.Is(err, mapping.err) {
			return mapping.word
		}
	}
	if certificateFailure(err) {
		return reasonTLS
	}
	if word, ok := sdkPhaseWord(err); ok {
		return word
	}
	if connectionFailure(err) {
		return reasonNetwork
	}
	for _, mapping := range genericReasons {
		if errors.Is(err, mapping.err) {
			return mapping.word
		}
	}
	return reasonInternal
}

func certificateFailure(err error) bool {
	var verification *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var recordHeader tls.RecordHeaderError
	var certificateInvalid x509.CertificateInvalidError
	return errors.As(err, &verification) || errors.As(err, &authority) || errors.As(err, &hostname) ||
		errors.As(err, &recordHeader) || errors.As(err, &certificateInvalid)
}

func connectionFailure(err error) bool {
	var network net.Error
	return errors.As(err, &network)
}

func sdkPhaseWord(err error) (string, bool) {
	text := err.Error()
	for _, mapping := range sdkPhrases {
		if strings.Contains(text, mapping.phrase) {
			return mapping.word, true
		}
	}
	return "", false
}

func reportFailure(out io.Writer, run privateArtifacts, err error) error {
	if err == nil {
		return nil
	}
	if present, existsErr := run.exists(evidenceName); existsErr == nil && present {
		return nil
	}
	word := failureReason(err)
	if !closedFailureWords[word] {
		word = reasonInternal
	}
	_, writeErr := fmt.Fprintf(out, "mcp-workflow: %s\n", word)
	return writeErr
}
