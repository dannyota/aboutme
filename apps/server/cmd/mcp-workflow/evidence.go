package main

import (
	"encoding/json"
	"errors"
)

const maxEvidenceBytes = 4 << 10

var errEvidence = errors.New("evidence_invalid")

// Fixed evidence labels. Evidence never holds content, identifiers, tokens,
// URLs, hashes, or photos. See docs/design/mcp-owner-workflow.md#privacy-revocation-and-evidence.
const (
	evidenceVersion       = 1
	stageOwnerWorkflow    = "owner_workflow"
	stageRevocationOnly   = "revocation_recovery"
	sdkVersionLabel       = "go-sdk v1.7.0"
	transportLabel        = "streamable-http"
	reconcileCreated      = "created"
	reconcileReplayed     = "replayed"
	reconcileReconciled   = "reconciled"
	revocationRevoked     = "revoked"
	revocationUnconfirmed = "revocation_unconfirmed"
)

type runEvidence struct {
	Version              int    `json:"version"`
	Mode                 string `json:"mode"`
	Stage                string `json:"stage"`
	SDKVersion           string `json:"sdk_version"`
	Transport            string `json:"transport"`
	ToolCount            int    `json:"tool_count"`
	Language             string `json:"language"`
	SourceUnchanged      bool   `json:"source_unchanged"`
	TargetPrivate        bool   `json:"target_private"`
	CountDelta           int    `json:"count_delta"`
	CreateReconciliation string `json:"create_reconciliation"`
	Revocation           string `json:"revocation"`
	PostRevocation401    bool   `json:"post_revocation_401"`
}

func workflowEvidence(mode, reconciliation, revocation string, postRevocation401 bool) runEvidence {
	return runEvidence{
		Version: evidenceVersion, Mode: mode, Stage: stageOwnerWorkflow, SDKVersion: sdkVersionLabel, Transport: transportLabel,
		ToolCount: len(requiredTools), Language: targetLanguage, SourceUnchanged: true, TargetPrivate: true, CountDelta: 1,
		CreateReconciliation: reconciliation, Revocation: revocation, PostRevocation401: postRevocation401,
	}
}

func recoveryEvidence(mode, revocation string) runEvidence {
	return runEvidence{Version: evidenceVersion, Mode: mode, Stage: stageRevocationOnly, Revocation: revocation}
}

func validEvidence(value runEvidence) bool {
	if value.Version != evidenceVersion || (value.Mode != modeLocal && value.Mode != modeProduction) ||
		(value.Revocation != revocationRevoked && value.Revocation != revocationUnconfirmed) {
		return false
	}
	switch value.Stage {
	case stageRevocationOnly:
		return value == recoveryEvidence(value.Mode, value.Revocation)
	case stageOwnerWorkflow:
		switch value.CreateReconciliation {
		case reconcileCreated, reconcileReplayed, reconcileReconciled:
		default:
			return false
		}
		return value == workflowEvidence(value.Mode, value.CreateReconciliation, value.Revocation, value.PostRevocation401)
	default:
		return false
	}
}

// writeEvidence writes the closed allowlisted shape, at most 4 KiB.
func writeEvidence(run privateArtifacts, value runEvidence) error {
	if !validEvidence(value) {
		return errEvidence
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > maxEvidenceBytes {
		return errEvidence
	}
	return run.write(evidenceName, data)
}
