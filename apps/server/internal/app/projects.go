package app

import (
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/projectapi"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

// ProjectDependencies supplies private library persistence and shared admission.
type ProjectDependencies struct {
	// Config supplies the exact public origin for CSRF.
	Config config.Config
	// Pool backs project rows and transactional replay records.
	Pool *store.Pool
	// Sessions authenticates owner requests.
	Sessions *auth.SessionManager
	// Resumes owns the read, write and slug claim budgets the library shares.
	Resumes *resumeapi.Service
}

// NewProjects returns the private library's session-authenticated API. Its
// reads, writes and slug claims spend the resume service's budgets.
func NewProjects(deps ProjectDependencies) *projectapi.Service {
	return projectapi.New(deps.Pool, resume.NewIdempotencyStore(deps.Pool), projectapi.Options{
		Sessions: deps.Sessions, PublicOrigin: deps.Config.PublicOrigin,
		ReadLimit: deps.Resumes.ReadAdmission(), WriteLimit: deps.Resumes.WriteAdmission(),
		AllowSlug: deps.Resumes.AllowSlugClaim,
	})
}
