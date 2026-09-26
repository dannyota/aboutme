// Package verify checks a running digest against the Sigstore-signed
// attestations release-images.yml produced for it
// (docs/design/deployment-transparency/verification.md, "What verified
// means").
package verify

import "time"

// Status is the outcome of one check.
type Status string

// Statuses, as the document writes them.
const (
	Verified  Status = "verified"
	NotFound  Status = "not_found"
	Invalid   Status = "invalid"
	Unchecked Status = "unchecked"
)

// Provenance is the result of checking a digest's SLSA provenance. Every
// field but Status and CheckedAt is set only when Status is Verified, and
// comes from the Fulcio certificate or the Rekor entry, never from the
// predicate the workflow wrote.
type Provenance struct {
	Status    Status    `json:"status"`
	CheckedAt time.Time `json:"checked_at"` // zero when Unchecked
	// Subjects are the statement's subject names bound to this digest. The
	// document marks the image invalid unless its component's image is one.
	Subjects       []string `json:"subjects"`
	Version        string   `json:"version"`         // v1.2.3, from the certificate's source ref
	Commit         string   `json:"commit"`          // 40 hex, from the certificate's source digest
	SignerWorkflow string   `json:"signer_workflow"` // the certificate's subject alternative name
	LogIndex       int64    `json:"log_index"`       // the Rekor entry's log index
	BuildURL       string   `json:"build_url"`       // the certificate's run invocation URI
}

// SBOM is the result of checking a digest's SPDX SBOM attestation.
type SBOM struct {
	Status    Status    `json:"status"`
	CheckedAt time.Time `json:"checked_at"`
	Subjects  []string  `json:"subjects"`
}

// Evidence is everything the observer knows about one digest.
type Evidence struct {
	Digest     string     `json:"digest"`
	Provenance Provenance `json:"provenance"`
	SBOM       SBOM       `json:"sbom"`
}
