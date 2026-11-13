package resumeapi

import (
	"net/http"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume"
)

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
