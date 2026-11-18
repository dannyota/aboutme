package project

import (
	"context"
	"time"

	"danny.vn/aboutme/apps/server/internal/store"
)

type EvidenceQuery struct {
	Kind       string
	Page       int
	Repository int64
}

type CandidateRepository struct {
	ID       *int64 `json:"id"`
	FullName string `json:"fullName"`
}

type EvidenceCandidate struct {
	CandidateID string              `json:"candidateId"`
	Kind        string              `json:"kind"`
	Title       string              `json:"title"`
	Repository  CandidateRepository `json:"repository"`
	Date        time.Time           `json:"date"`
	Attached    bool                `json:"attached"`
}

type EvidenceCandidates struct {
	Items      []EvidenceCandidate `json:"items"`
	NextPage   *int                `json:"nextPage"`
	Incomplete bool                `json:"incomplete"`
}

type EvidenceRequest struct {
	Kind        string
	Link        string
	CandidateID string
}

type EvidenceFields struct {
	RepositoryID   int64
	ItemID         int64
	PullNumber     *int64
	ReleaseTag     *string
	URL            string
	RepositoryName string
	Title          string
	ItemDate       time.Time
	RuleMet        bool
	FlagReason     *string
}

type EvidenceCheck struct {
	Kind    string
	Fields  EvidenceFields
	Confirm func(context.Context, *store.Queries) (int64, error)
}
