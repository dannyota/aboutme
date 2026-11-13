package verify

import "time"

type Status string

const (
	Verified  Status = "verified"
	NotFound  Status = "not_found"
	Invalid   Status = "invalid"
	Unchecked Status = "unchecked"
)

type Provenance struct {
	Status         Status    `json:"status"`
	CheckedAt      time.Time `json:"checked_at"`
	Subjects       []string  `json:"subjects"`
	Version        string    `json:"version"`
	Commit         string    `json:"commit"`
	SignerWorkflow string    `json:"signer_workflow"`
	LogIndex       int64     `json:"log_index"`
	BuildURL       string    `json:"build_url"`
}

type SBOM struct {
	Status    Status    `json:"status"`
	CheckedAt time.Time `json:"checked_at"`
	Subjects  []string  `json:"subjects"`
}

type Evidence struct {
	Digest     string     `json:"digest"`
	Provenance Provenance `json:"provenance"`
	SBOM       SBOM       `json:"sbom"`
}
