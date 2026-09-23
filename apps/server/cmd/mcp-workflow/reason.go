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

// Failure words. A failed run prints exactly one line,
// "mcp-workflow: <word>", to stderr. The vocabulary is closed and fixed, so
// no path, host, URL, identifier, resume value, token, or error text can
// reach the output. See
// docs/design/mcp-owner-workflow.md#privacy-revocation-and-evidence.
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

// closedFailureWords is the complete output vocabulary.
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

// sdkPhrases classify the official SDK's own OAuth errors, which carry no
// sentinel. Only the fixed phrase is matched; the error text never reaches the
// output.
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

// sentinelReasons maps every sentinel this package returns to one word, most
// specific first.
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

// genericReasons are the broad wrappers. They are consulted only after the
// SDK phrases, so a wrapped registration or metadata failure keeps its stage.
var genericReasons = []struct {
	err  error
	word string
}{
	{errConnect, reasonDiscovery},
	{errRuntime, reasonNetwork},
	{errWorkflowBlocked, reasonWorkflowState},
}

// failureReason returns the one word that names a failure. Transport
// failures are classified before the workflow sentinels that wrap them, so a
// rejected certificate reads as tls rather than as a generic connect failure.
// The sentinels come first: cleanup errors joined onto a failure, such as
// closing an already closed listener, must not outrank the cause.
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

// certificateFailure reports a trust or handshake rejection, which no cleanup
// step can produce, so it is named before the SDK phase.
func certificateFailure(err error) bool {
	var verification *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var recordHeader tls.RecordHeaderError
	var certificateInvalid x509.CertificateInvalidError
	return errors.As(err, &verification) || errors.As(err, &authority) || errors.As(err, &hostname) ||
		errors.As(err, &recordHeader) || errors.As(err, &certificateInvalid)
}

// connectionFailure is the last resort: a closed or failed socket, which a
// joined cleanup step also produces, so every named cause is tried first.
func connectionFailure(err error) bool {
	var network net.Error
	return errors.As(err, &network)
}

// sdkPhaseWord names the OAuth phase of an SDK error from its fixed phrase.
func sdkPhaseWord(err error) (string, bool) {
	text := err.Error()
	for _, mapping := range sdkPhrases {
		if strings.Contains(text, mapping.phrase) {
			return mapping.word, true
		}
	}
	return "", false
}

// reportFailure prints the single failure line. It stays silent once the run
// left evidence, which already names the stage and revocation status.
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
