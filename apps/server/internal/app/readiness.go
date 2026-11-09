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

// ReadinessDependencies are the probes /readyz consults.
type ReadinessDependencies struct {
	// Coordinator gates readiness on the public-state generation.
	Coordinator *publicstate.Coordinator
	// PingDatabase checks the database connection.
	PingDatabase func(context.Context) error
	// Renderer is the direct public render worker client.
	Renderer *directrender.Client
	// PrintRenderer is the private browser renderer, or nil when the platform
	// cannot print.
	PrintRenderer *printrender.Renderer
	// PrintQueue is the private print queue.
	PrintQueue *renderjob.Queue
	// PublicOrigin is the canonical origin the render probe uses.
	PublicOrigin publicresume.PublicOrigin
}

// NewReadiness returns the readiness check. Without a print renderer it
// probes only the direct render worker; with one it also requires the print
// queue and the browser to be ready first.
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
			// The browser probe owns its bound so a canceled caller cannot poison readiness.
			if readyErr := deps.PrintRenderer.Ready(); readyErr != nil { //nolint:contextcheck // Ready owns the browser probe context.
				return readyErr
			}
			return deps.Renderer.Probe(probeCtx, readinessRenderRequest(deps.PublicOrigin))
		},
	})
}

// readinessRenderRequest is a closed, current-version public snapshot. The
// direct worker applies the same OpenAPI validator to probes and viewer
// renders, so readiness cannot use an incomplete synthetic value.
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
		// The worker rejects an empty title, so the probe sends the default.
		PageTitle: publicpage.EffectiveTitle(nil, "Readiness Probe"),
		Preview:   previewmeta.For(resume, nil),
	}
}
