package store

import (
	"context"

	"github.com/google/uuid"
)

// PublicReadQueries is the read-only public publication storage contract.
type PublicReadQueries interface {
	GetPublicState(context.Context) (PublicState, error)
	GetPublicResumeBySlug(context.Context, string) (Resume, error)
	GetPublicResumeByOwner(context.Context, GetPublicResumeByOwnerParams) (Resume, error)
	ListEligiblePublicSlugs(context.Context) ([]string, error)
}

// PublicDiscoveryQueries reads the durable discovery generation and its
// eligible slug set from one PostgreSQL statement.
type PublicDiscoveryQueries interface {
	GetPublicDiscoverySnapshot(context.Context) (GetPublicDiscoverySnapshotRow, error)
}

// PublicMutationQueries adds transaction-scoped publication mutation primitives.
type PublicMutationQueries interface {
	PublicReadQueries
	LockPublicState(context.Context) (PublicState, error)
	AdvanceDiscoveryGeneration(context.Context) (int64, error)
	LockSlugClaim(context.Context, string) error
	GetSlugClaim(context.Context, string) (uuid.UUID, error)
	GetSlugTombstoneForUpdate(context.Context, string) (SlugTombstone, error)
	ConsumeExpiredSlugTombstone(context.Context, ConsumeExpiredSlugTombstoneParams) (uuid.UUID, error)
	InsertSlugTombstone(context.Context, InsertSlugTombstoneParams) (SlugTombstone, error)
	PublishResumeCAS(context.Context, PublishResumeCASParams) (Resume, error)
	DeleteResumePublicCAS(context.Context, DeleteResumePublicCASParams) (Resume, error)
}

var _ PublicMutationQueries = (*Queries)(nil)
var _ PublicDiscoveryQueries = (*Queries)(nil)
