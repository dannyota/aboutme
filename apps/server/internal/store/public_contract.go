package store

import (
	"context"

	"github.com/google/uuid"
)

type PublicReadQueries interface {
	GetPublicState(context.Context) (PublicState, error)
	GetPublicResumeBySlug(context.Context, string) (GetPublicResumeBySlugRow, error)
	GetPublicResumeProjectList(context.Context, string) (GetPublicResumeProjectListRow, error)
	GetPublicResumeProject(context.Context, GetPublicResumeProjectParams) (GetPublicResumeProjectRow, error)
	GetPublicResumeByOwner(context.Context, GetPublicResumeByOwnerParams) (Resume, error)
	ListEligiblePublicSlugs(context.Context) ([]string, error)
}

type PublicDiscoveryQueries interface {
	GetPublicDiscoverySnapshot(context.Context) (GetPublicDiscoverySnapshotRow, error)
}

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
