package resumeapi

// Community showcase publish rules: the opt-in switch and the owner's role
// (docs/design/showcase.md "Opt-in" and "Data and contract", ADR 0029).

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/showcase"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ShowcasePublisher applies a publish request's showcase state inside the
// publish transaction; showcase.Service implements it.
type ShowcasePublisher interface {
	PublishTx(ctx context.Context, qtx *store.Queries, change showcase.PublishChange) error
}

var _ ShowcasePublisher = (*showcase.Service)(nil)

// currentShowcase is the stored opt-in of a resume as a publish state: on
// while the resume has a showcase row.
func currentShowcase(current resume.Resume) (enabled bool, role *string) {
	if current.Showcase == nil {
		return false, nil
	}
	return true, current.Showcase.Role
}

// resolveShowcase applies the request to the stored opt-in and returns the
// final state, or the issues that reject the request. Omitted fields keep the
// stored value. The switch can be on only while the resume is live with sign in
// to view off: asking for it on otherwise is an issue, and a stored opt-in
// simply ends when a request leaves the resume not live or turns sign in to
// view on, even though the field is omitted.
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

// applyShowcaseTx writes the final showcase state after the resume row holds
// its new publish settings. A resume with no opt-in before or after is left
// alone.
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
