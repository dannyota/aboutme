package resumeapi

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/showcase"
	"danny.vn/aboutme/apps/server/internal/store"
)

type ShowcasePublisher interface {
	PublishTx(ctx context.Context, qtx *store.Queries, change showcase.PublishChange) error
}

var _ ShowcasePublisher = (*showcase.Service)(nil)

func currentShowcase(current resume.Resume) (enabled bool, role *string) {
	if current.Showcase == nil {
		return false, nil
	}
	return true, current.Showcase.Role
}

func resolveShowcase(current currentPublish, effective currentPublish, input publishInput) (enabled bool, role *string, issues []publishIssue) {
	enabled = current.ShowcaseEnabled
	if input.ShowcaseEnabled.Present {
		enabled = input.ShowcaseEnabled.Value
		if enabled && !effective.Live {
			issues = append(issues, publishIssue{Path: "showcaseEnabled", Code: "requires_live", Message: "the community showcase requires live to be enabled"})
		}
		if enabled && effective.SignInToView {
			issues = append(issues, publishIssue{Path: "showcaseEnabled", Code: "requires_open_view", Message: "the community showcase requires sign in to view to be off"})
		}
	}
	if enabled && (!effective.Live || effective.SignInToView) {
		enabled = false
	}
	role = current.ShowcaseRole
	if input.ShowcaseRole.Present {
		role = nil
		if value := input.ShowcaseRole.Value; value != "" {
			switch {
			case !showcase.ValidRole(value):
				issues = append(issues, publishIssue{Path: "showcaseRole", Code: "invalid_format", Message: "showcase role must be one of the listed roles"})
			case !enabled && len(issues) == 0:
				issues = append(issues, publishIssue{Path: "showcaseRole", Code: "invalid_format", Message: "showcase role needs the community showcase to be on"})
			default:
				role = &value
			}
		}
	}
	if !enabled {
		role = nil
	}
	return enabled, role, issues
}

func (s *Service) applyShowcaseTx(ctx context.Context, qtx *store.Queries, resumeID uuid.UUID, before, after currentPublish) error {
	if !before.ShowcaseEnabled && !after.ShowcaseEnabled {
		return nil
	}
	if s.showcase == nil {
		return errors.New("resumeapi: showcase dependency is unavailable")
	}
	return s.showcase.PublishTx(ctx, qtx, showcase.PublishChange{
		ResumeID: resumeID, Enabled: after.ShowcaseEnabled, Role: after.ShowcaseRole,
	})
}
