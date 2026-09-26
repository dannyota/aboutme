// Package document builds /.well-known/deployment.json, schema version 1
// (docs/design/deployment-transparency/document.md). The builder fills
// closed output types from platform.Image values, verification evidence, and
// constants only; Encode validates the bytes before anything is written.
package document

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
	"github.com/dannyota/aboutme/deploy/observer/internal/verify"
)

// Constants every document carries.
const (
	SchemaVersion    = 1
	Project          = "aboutme"
	SourceRepository = "https://github.com/dannyota/aboutme"
	// StaleAfter is how long after observed_at a reader treats the
	// document as stale.
	StaleAfter = 180 * time.Second
	// MaxImages bounds the distinct digests per component.
	MaxImages = 8
)

// Summary values.
const (
	SummaryVerified   = "verified"
	SummaryRollingOut = "rolling_out"
	SummaryUnverified = "unverified"
	SummaryMismatch   = "mismatch"
)

// SPDXFormat is the only SBOM format the release workflow attests.
const SPDXFormat = "spdx-2.3"

// Document is the whole published object. Every struct here is closed: the
// schema sets additionalProperties false at every level.
type Document struct {
	SchemaVersion    int         `json:"schema_version"`
	Project          string      `json:"project"`
	Environment      string      `json:"environment"`
	Site             string      `json:"site"`
	SourceRepository string      `json:"source_repository"`
	Platform         Platform    `json:"platform"`
	ObservedAt       string      `json:"observed_at"`
	StaleAfter       string      `json:"stale_after"`
	Summary          string      `json:"summary"`
	Release          *Release    `json:"release"`
	Components       []Component `json:"components"`
	Observer         Observer    `json:"observer"`
}

// Platform names where the components run.
type Platform struct {
	Provider     string `json:"provider"`
	Orchestrator string `json:"orchestrator"`
	Region       string `json:"region"`
}

// Release is set only when every required component runs one verified
// digest of the same version and commit.
type Release struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	DeployedAt string `json:"deployed_at"`
}

// Component is one serving component and the digests it runs.
type Component struct {
	Name          string         `json:"name"`
	Image         string         `json:"image"`
	RunningImages []RunningImage `json:"running_images"`
}

// RunningImage is one distinct digest of a component.
type RunningImage struct {
	Digest       string    `json:"digest"`
	Replicas     int       `json:"replicas"`
	RunningSince string    `json:"running_since"`
	Version      *string   `json:"version"`
	Commit       *string   `json:"commit"`
	Signature    Signature `json:"signature"`
	SBOM         SBOM      `json:"sbom"`
	Links        Links     `json:"links"`
}

// Signature is the provenance check's result.
type Signature struct {
	Status               string  `json:"status"`
	CheckedAt            *string `json:"checked_at"`
	SignerWorkflow       *string `json:"signer_workflow"`
	TransparencyLogIndex *int64  `json:"transparency_log_index"`
}

// SBOM is the SBOM attestation check's result.
type SBOM struct {
	Status string  `json:"status"`
	Format *string `json:"format"`
}

// Links are built from constants and verified values only.
type Links struct {
	Commit          *string `json:"commit"`
	Release         *string `json:"release"`
	Build           *string `json:"build"`
	Provenance      string  `json:"provenance"`
	SBOM            *string `json:"sbom"`
	TransparencyLog *string `json:"transparency_log"`
}

// Observer is the observer binary's own build information. It is
// self-reported and says so.
type Observer struct {
	Version *string `json:"version"`
	Commit  *string `json:"commit"`
	Source  string  `json:"source"`
}

// BuildInfo is the observer's own version and commit, empty when unknown.
type BuildInfo struct {
	Version string
	Commit  string
}

// Input is everything a document is built from.
type Input struct {
	Environment string
	Site        string
	Platform    platform.Info
	ObservedAt  time.Time
	Images      []platform.Image
	// Evidence is keyed by digest. A digest without evidence is unchecked.
	Evidence map[string]verify.Evidence
	Observer BuildInfo
}

// Build fills a Document from in. It never copies a platform string other
// than a digest, and never a verification string other than the fixed
// fields of verify.Provenance.
func Build(in Input) (*Document, error) {
	if in.ObservedAt.IsZero() {
		return nil, errors.New("observed_at is required")
	}
	observed := in.ObservedAt.UTC().Truncate(time.Second)
	doc := &Document{
		SchemaVersion:    SchemaVersion,
		Project:          Project,
		Environment:      in.Environment,
		Site:             in.Site,
		SourceRepository: SourceRepository,
		Platform: Platform{
			Provider:     in.Platform.Provider,
			Orchestrator: in.Platform.Orchestrator,
			Region:       in.Platform.Region,
		},
		ObservedAt: stamp(observed),
		StaleAfter: stamp(observed.Add(StaleAfter)),
		// A self-reported value outside its pattern (a local or
		// pre-release build) is left out, never allowed to stop the write.
		Observer: Observer{
			Version: optional(matching(versionRE, in.Observer.Version)),
			Commit:  optional(matching(commitRE, in.Observer.Commit)),
			Source:  "self-reported",
		},
	}
	byComponent := map[string][]platform.Image{}
	for _, img := range in.Images {
		if platform.ImageRepository(img.Component) == "" {
			return nil, fmt.Errorf("unknown component %q", img.Component)
		}
		byComponent[img.Component] = append(byComponent[img.Component], img)
	}
	for _, name := range platform.Components {
		imgs := byComponent[name]
		if name == platform.Maintenance && len(imgs) == 0 {
			continue
		}
		if len(imgs) > MaxImages {
			return nil, fmt.Errorf("%s runs %d digests, more than %d", name, len(imgs), MaxImages)
		}
		slices.SortStableFunc(imgs, func(a, b platform.Image) int { return a.RunningSince.Compare(b.RunningSince) })
		c := Component{Name: name, Image: platform.ImageRepository(name), RunningImages: []RunningImage{}}
		for _, img := range imgs {
			c.RunningImages = append(c.RunningImages, runningImage(name, img, in.Evidence[img.Digest]))
		}
		doc.Components = append(doc.Components, c)
	}
	doc.Summary = summarize(doc.Components)
	doc.Release = release(doc.Summary, doc.Components)
	return doc, nil
}

func runningImage(component string, img platform.Image, ev verify.Evidence) RunningImage {
	repo := platform.ImageRepository(component)
	ri := RunningImage{
		Digest:       img.Digest,
		Replicas:     img.Replicas,
		RunningSince: stamp(img.RunningSince),
		Links: Links{
			Provenance: fmt.Sprintf("https://api.github.com/repos/dannyota/aboutme/attestations/%s?predicate_type=provenance", img.Digest),
		},
	}
	p := ev.Provenance
	status := p.Status
	if status == "" {
		status = verify.Unchecked
	}
	// A digest whose signed subject names another image (the web image
	// running as server) is invalid for this component.
	if status == verify.Verified && !slices.Contains(p.Subjects, repo) {
		status = verify.Invalid
	}
	ri.Signature.Status = string(status)
	if status != verify.Unchecked && !p.CheckedAt.IsZero() {
		ri.Signature.CheckedAt = optional(stamp(p.CheckedAt))
	}
	s := ev.SBOM
	sbomStatus := s.Status
	if sbomStatus == "" {
		sbomStatus = verify.Unchecked
	}
	if sbomStatus == verify.Verified && !slices.Contains(s.Subjects, repo) {
		sbomStatus = verify.Invalid
	}
	ri.SBOM.Status = string(sbomStatus)
	if sbomStatus == verify.Verified {
		ri.SBOM.Format = optional(SPDXFormat)
	}
	if status != verify.Verified {
		return ri
	}
	ri.Version = optional(p.Version)
	ri.Commit = optional(p.Commit)
	ri.Signature.SignerWorkflow = optional(p.SignerWorkflow)
	idx := p.LogIndex
	ri.Signature.TransparencyLogIndex = &idx
	ri.Links.Commit = optional(SourceRepository + "/commit/" + p.Commit)
	ri.Links.Build = optional(p.BuildURL)
	ri.Links.TransparencyLog = optional(fmt.Sprintf("https://search.sigstore.dev/?logIndex=%d", p.LogIndex))
	// GitHub Releases exist only for tags built with a signed SBOM, so the
	// release and SBOM links are set together.
	if sbomStatus == verify.Verified {
		ri.Links.Release = optional(SourceRepository + "/releases/tag/" + p.Version)
		ri.Links.SBOM = optional(fmt.Sprintf("%s/releases/download/%s/%s.spdx.json", SourceRepository, p.Version, imageBase(repo)))
	}
	return ri
}

func summarize(components []Component) string {
	present := map[string]bool{}
	anyUnchecked, rolling := false, false
	for _, c := range components {
		if len(c.RunningImages) > 0 {
			present[c.Name] = true
		}
		if len(c.RunningImages) > 1 {
			rolling = true
		}
		for _, ri := range c.RunningImages {
			switch ri.Signature.Status {
			case string(verify.NotFound), string(verify.Invalid):
				return SummaryMismatch
			case string(verify.Unchecked):
				anyUnchecked = true
			}
		}
	}
	for _, name := range platform.Required {
		if !present[name] {
			return SummaryMismatch
		}
	}
	switch {
	case anyUnchecked:
		return SummaryUnverified
	case rolling:
		return SummaryRollingOut
	}
	return SummaryVerified
}

func release(summary string, components []Component) *Release {
	if summary != SummaryVerified {
		return nil
	}
	var version, commit, deployed string
	for _, c := range components {
		if !slices.Contains(platform.Required, c.Name) {
			continue
		}
		ri := c.RunningImages[0]
		if ri.Version == nil || ri.Commit == nil {
			return nil
		}
		if version == "" {
			version, commit = *ri.Version, *ri.Commit
		} else if *ri.Version != version || *ri.Commit != commit {
			return nil
		}
		// Timestamps share one fixed format, so they order as strings.
		if ri.RunningSince > deployed {
			deployed = ri.RunningSince
		}
	}
	return &Release{Version: version, Commit: commit, DeployedAt: deployed}
}

func imageBase(repo string) string {
	for i := len(repo) - 1; i >= 0; i-- {
		if repo[i] == '/' {
			return repo[i+1:]
		}
	}
	return repo
}

var (
	versionRE = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	commitRE  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func matching(re *regexp.Regexp, s string) string {
	if re.MatchString(s) {
		return s
	}
	return ""
}

func stamp(t time.Time) string { return t.UTC().Truncate(time.Second).Format(time.RFC3339) }

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
