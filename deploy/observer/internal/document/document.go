package document

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"danny.vn/aboutme/deploy/observer/internal/platform"
	"danny.vn/aboutme/deploy/observer/internal/verify"
)

const (
	SchemaVersion    = 1
	Project          = "aboutme"
	SourceRepository = "https://github.com/dannyota/aboutme"
	StaleAfter       = 180 * time.Second
	MaxImages        = 8
)

const (
	SummaryVerified   = "verified"
	SummaryRollingOut = "rolling_out"
	SummaryUnverified = "unverified"
	SummaryMismatch   = "mismatch"
)

const SPDXFormat = "spdx-2.3"

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

type Platform struct {
	Provider     string `json:"provider"`
	Orchestrator string `json:"orchestrator"`
	Region       string `json:"region"`
}

type Release struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	DeployedAt string `json:"deployed_at"`
}

type Component struct {
	Name          string         `json:"name"`
	Image         string         `json:"image"`
	RunningImages []RunningImage `json:"running_images"`
}

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

type Signature struct {
	Status               string  `json:"status"`
	CheckedAt            *string `json:"checked_at"`
	SignerWorkflow       *string `json:"signer_workflow"`
	TransparencyLogIndex *int64  `json:"transparency_log_index"`
}

type SBOM struct {
	Status string  `json:"status"`
	Format *string `json:"format"`
}

type Links struct {
	Commit          *string `json:"commit"`
	Release         *string `json:"release"`
	Build           *string `json:"build"`
	Provenance      string  `json:"provenance"`
	SBOM            *string `json:"sbom"`
	TransparencyLog *string `json:"transparency_log"`
}

type Observer struct {
	Version *string `json:"version"`
	Commit  *string `json:"commit"`
	Source  string  `json:"source"`
}

type BuildInfo struct {
	Version string
	Commit  string
}

type Input struct {
	Environment string
	Site        string
	Platform    platform.Info
	ObservedAt  time.Time
	Images      []platform.Image
	Evidence    map[string]verify.Evidence
	Observer    BuildInfo
}

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
			c.RunningImages = append(c.RunningImages, runningImage(name, in.Platform.Orchestrator, img, in.Evidence[img.Digest]))
		}
		doc.Components = append(doc.Components, c)
	}
	doc.Summary = summarize(doc.Components)
	doc.Release = release(doc.Summary, doc.Components)
	return doc, nil
}

func runningImage(component, orchestrator string, img platform.Image, ev verify.Evidence) RunningImage {
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
	if sbomStatus == verify.Verified {
		ri.Links.Release = optional(SourceRepository + "/releases/tag/" + p.Version)
		asset := imageBase(repo)
		if orchestrator == "podman" {
			asset += "-amd64"
		}
		ri.Links.SBOM = optional(fmt.Sprintf("%s/releases/download/%s/%s.spdx.json", SourceRepository, p.Version, asset))
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
