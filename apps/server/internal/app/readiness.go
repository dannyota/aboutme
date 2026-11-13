package app

import (
	"context"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/previewmeta"
	"danny.vn/aboutme/apps/server/internal/printrender"
	"danny.vn/aboutme/apps/server/internal/publicpage"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/renderjob"
)

type ReadinessDependencies struct {
	Coordinator   *publicstate.Coordinator
	PingDatabase  func(context.Context) error
	Renderer      *directrender.Client
	PrintRenderer *printrender.Renderer
	PrintQueue    *renderjob.Queue
	PublicOrigin  publicresume.PublicOrigin
}

func NewReadiness(deps ReadinessDependencies) *publicstate.Readiness {
	return publicstate.NewReadiness(deps.Coordinator, publicstate.ReadinessDependencies{
		PingDatabase: deps.PingDatabase,
		ProbeRenderer: func(probeCtx context.Context) error {
			if deps.PrintRenderer == nil {
				return deps.Renderer.Probe(probeCtx, readinessRenderRequest(deps.PublicOrigin))
			}
			if queueErr := deps.PrintQueue.Ready(); queueErr != nil {
				return queueErr
			}
			if readyErr := deps.PrintRenderer.Ready(); readyErr != nil { //nolint:contextcheck // Ready owns the browser probe context.
				return readyErr
			}
			return deps.Renderer.Probe(probeCtx, readinessRenderRequest(deps.PublicOrigin))
		},
	})
}

func readinessRenderRequest(origin publicresume.PublicOrigin) directrender.PublicRenderRequest {
	resume := publicresume.PublicResume{
		Slug:     "readiness-probe",
		Revision: "1",
		Lng:      "en",
		Document: publicresume.PublicResumeDocument{
			SchemaVersion:   schema.CurrentVersion,
			PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Readiness Probe"},
			Content: publicresume.PublicContent{
				"profile": {SectionType: string(schema.Profile), ProfileEntries: []publicresume.PublicProfileEntry{{ID: "00000000-0000-4000-8000-000000000001"}}},
			},
			Customization: schema.Customization{
				Font:           schema.Font{Family: schema.Inter, BaseSizePx: 14},
				Colors:         schema.Colors{Primary: "#1a1a1a", Text: "#1a1a1a", Background: "#ffffff"},
				Spacing:        schema.Spacing{SectionGap: 16, EntryGap: 8, LineHeight: 1.4},
				Heading:        schema.Heading{Style: schema.Normal},
				Layout:         schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{"profile"}, Sidebar: []string{}}},
				SectionDisplay: schema.SectionDisplay{Skill: schema.SkillClass{Style: schema.Text}, Language: schema.LanguageClass{Style: schema.Text}},
				PageFormat:     schema.A4,
				DateFormat:     schema.MmYyyy,
			},
		},
	}
	return directrender.PublicRenderRequest{
		PublicResume:     resume,
		Mode:             directrender.PublicRenderMode,
		CanonicalOrigin:  origin.String(),
		DiscoveryEnabled: false,
		PageTitle:        publicpage.EffectiveTitle(nil, "Readiness Probe"),
		Preview:          previewmeta.For(resume, nil),
	}
}
