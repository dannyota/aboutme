package resumeapi

import (
	"net/http"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume"
)

// requireLinkAuthority refuses a bearer write that adds a project link or
// changes one. Such a write needs the projects:read scope
// (docs/design/projects-contract.md "Agents"), which no grant can hold yet,
// so every bearer addition or change is refused; keeping or removing stored
// links needs only resumes:write. stored is nil for a new resume.
func requireLinkAuthority(mutation mutationContext, stored *schema.Resume, next schema.Resume) error {
	if mutation.Agent == nil {
		return nil
	}
	changed, err := resume.ChangesProjectLinks(stored, next)
	if err != nil {
		return err
	}
	if changed {
		return &clientError{Status: http.StatusForbidden, Code: "scope_denied", Message: "the grant does not allow changing project links"}
	}
	return nil
}
