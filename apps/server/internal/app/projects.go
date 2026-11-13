package app

import (
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/projectapi"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

type ProjectDependencies struct {
	Config   config.Config
	Pool     *store.Pool
	Sessions *auth.SessionManager
	Resumes  *resumeapi.Service
}

func NewProjects(deps ProjectDependencies) *projectapi.Service {
	return projectapi.New(deps.Pool, resume.NewIdempotencyStore(deps.Pool), projectapi.Options{
		Sessions: deps.Sessions, PublicOrigin: deps.Config.PublicOrigin,
		ReadLimit: deps.Resumes.ReadAdmission(), WriteLimit: deps.Resumes.WriteAdmission(),
		AllowSlug: deps.Resumes.AllowSlugClaim, Links: deps.Resumes,
	})
}
