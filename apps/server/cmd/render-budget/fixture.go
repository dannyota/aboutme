package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/printsnapshot"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

const (
	maximumSectionCount = 24
	maximumEntries      = 64
	maximumRichText     = 16_384
)

var fixtureNamespace = uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")

type fixture struct {
	Name             string
	Document         schema.Resume
	Canonical        []byte
	Photo            []byte
	PhotoContentType string
	Snapshot         renderjob.Snapshot
	Stats            fixtureStats
}

type fixtureStats struct {
	SectionCount         int
	EntryCount           int
	LargestRichTextBytes int
	HiddenEntryCount     int
	LayoutSectionCount   int
	PageFormat           string
	FontFamily           string
	InlinePhotoBytes     int
}

func buildFixtureCorpus(repositoryRoot string) ([]fixture, error) {
	minimal, err := readDocument(filepath.Join(repositoryRoot, "packages", "schema", "fixtures", "minimal.json"))
	if err != nil {
		return nil, errors.New("minimal_fixture_decode_failed")
	}
	full, err := readDocument(filepath.Join(repositoryRoot, "packages", "schema", "fixtures", "full.json"))
	if err != nil {
		return nil, errors.New("full_fixture_decode_failed")
	}
	photoSource, err := os.ReadFile(filepath.Join(repositoryRoot, "apps", "server", "internal", "media", "testdata", "alpha-max-pixels.png"))
	if err != nil {
		return nil, errors.New("photo_fixture_read_failed")
	}
	normalizedPhoto, err := media.NormalizePhoto(photoSource)
	if err != nil || normalizedPhoto.ContentType != "image/png" {
		return nil, errors.New("photo_fixture_normalization_failed")
	}
	photo := normalizedPhoto.Bytes
	maximum, err := buildMaximumDocument(minimal.Customization)
	if err != nil {
		return nil, err
	}
	sources := []struct {
		name        string
		document    schema.Resume
		photo       []byte
		contentType string
	}{
		{name: "minimal", document: minimal},
		{name: "full", document: full, photo: photo, contentType: "image/png"},
		{name: "maximum", document: maximum},
	}
	result := make([]fixture, 0, len(sources))
	for _, source := range sources {
		if err := resume.ValidateForStore(source.document); err != nil {
			return nil, errors.New(source.name + "_fixture_validation_failed")
		}
		canonical, err := resume.AssembleCanonical(source.document)
		if err != nil {
			return nil, errors.New(source.name + "_fixture_encoding_failed")
		}
		resumeID := deterministicUUID("fixture/" + source.name)
		owner := resume.Resume{ID: resumeID, UserID: deterministicUUID("owner"), Title: "Synthetic " + source.name, Revision: 1, Doc: source.document}
		envelope, err := printsnapshot.FromOwner(owner, source.photo, source.contentType)
		if err != nil {
			return nil, errors.New(source.name + "_snapshot_build_failed")
		}
		payload, err := printsnapshot.Marshal(envelope)
		if err != nil {
			return nil, errors.New(source.name + "_snapshot_encoding_failed")
		}
		result = append(result, fixture{
			Name: source.name, Document: source.document, Canonical: canonical,
			Photo: source.photo, PhotoContentType: source.contentType,
			Snapshot: renderjob.Snapshot{ResumeID: resumeID, Revision: 1, SchemaVersion: int(schema.CurrentVersion), Payload: payload},
			Stats:    documentStats(source.document, len(source.photo)),
		})
	}
	return result, nil
}

func readDocument(path string) (schema.Resume, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return schema.Resume{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var document schema.Resume
	if err := decoder.Decode(&document); err != nil {
		return schema.Resume{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return schema.Resume{}, errors.New("trailing fixture data")
	}
	return document, nil
}

func buildMaximumDocument(customization schema.Customization) (schema.Resume, error) {
	name := "Synthetic maximum resume"
	document := schema.Resume{
		SchemaVersion:   int64(schema.CurrentVersion),
		PersonalDetails: schema.PersonalDetails{FullName: &name, Details: []schema.PersonalDetail{}},
		Content:         make(map[string]schema.Section, maximumSectionCount),
		Customization:   customization,
	}
	document.Customization.Layout.Columns = 1
	document.Customization.Layout.Sections.Main = make([]string, 0, maximumSectionCount)
	document.Customization.Layout.Sections.Sidebar = []string{}
	sectionKeys := make([]string, 0, maximumSectionCount)
	empty := ""
	for sectionIndex := range maximumSectionCount {
		key := deterministicUUID(fmt.Sprintf("maximum/section/%02d", sectionIndex)).String()
		displayName := fmt.Sprintf("Professional Experience Section %02d", sectionIndex+1)
		entries := make([]schema.CustomEntry, maximumEntries)
		for entryIndex := range maximumEntries {
			title := fmt.Sprintf("Visible Entry %02d-%02d", sectionIndex+1, entryIndex+1)
			entries[entryIndex] = schema.CustomEntry{
				ID:    deterministicUUID(fmt.Sprintf("maximum/entry/%02d/%02d", sectionIndex, entryIndex)).String(),
				Title: &title, Description: &empty,
			}
		}
		document.Content[key] = schema.NewCustomSection(&displayName, nil, entries)
		document.Customization.Layout.Sections.Main = append(document.Customization.Layout.Sections.Main, key)
		sectionKeys = append(sectionKeys, key)
	}
	canonical, err := resume.AssembleCanonical(document)
	if err != nil || len(canonical) >= resume.MaxDocumentBytes {
		return schema.Resume{}, errors.New("fixture_contract_failed")
	}
	remaining := resume.MaxDocumentBytes - len(canonical)
	for _, key := range sectionKeys {
		section := document.Content[key]
		for index := range section.CustomEntries {
			if remaining == 0 {
				break
			}
			length := min(remaining, maximumRichText)
			value := strings.Repeat("x", length)
			section.CustomEntries[index].Description = &value
			remaining -= length
		}
		document.Content[key] = section
	}
	if remaining != 0 {
		return schema.Resume{}, errors.New("fixture_contract_failed")
	}
	canonical, err = resume.AssembleCanonical(document)
	if err != nil {
		return schema.Resume{}, errors.New("maximum_fixture_encoding_failed")
	}
	if len(canonical) != resume.MaxDocumentBytes {
		return schema.Resume{}, errors.New("maximum_fixture_size_failed")
	}
	if resume.ValidateForStore(document) != nil {
		return schema.Resume{}, errors.New("maximum_fixture_validation_failed")
	}
	return document, nil
}

func deterministicUUID(name string) uuid.UUID {
	return uuid.NewSHA1(fixtureNamespace, []byte("aboutme/render-budget/"+name))
}

func documentStats(document schema.Resume, photoBytes int) fixtureStats {
	stats := fixtureStats{
		SectionCount:       len(document.Content),
		LayoutSectionCount: len(document.Customization.Layout.Sections.Main) + len(document.Customization.Layout.Sections.Sidebar),
		PageFormat:         string(document.Customization.PageFormat), FontFamily: string(document.Customization.Font.Family), InlinePhotoBytes: photoBytes,
	}
	for _, section := range document.Content {
		countSectionStats(&stats, section)
	}
	return stats
}

func countSectionStats(stats *fixtureStats, section schema.Section) {
	texts := make([]*string, 0, 64)
	hidden := make([]*bool, 0, 64)
	switch section.SectionType {
	case schema.Profile:
		stats.EntryCount += len(section.ProfileEntries)
		for _, entry := range section.ProfileEntries {
			texts = append(texts, entry.Text)
			hidden = append(hidden, entry.IsHidden)
		}
	case schema.Work:
		stats.EntryCount += len(section.WorkEntries)
		for _, entry := range section.WorkEntries {
			texts = append(texts, entry.Description)
			hidden = append(hidden, entry.IsHidden)
		}
	case schema.Education:
		stats.EntryCount += len(section.EducationEntries)
		for _, entry := range section.EducationEntries {
			texts = append(texts, entry.Description)
			hidden = append(hidden, entry.IsHidden)
		}
	case schema.Skill:
		stats.EntryCount += len(section.SkillEntries)
		for _, entry := range section.SkillEntries {
			texts = append(texts, entry.InfoHTML)
			hidden = append(hidden, entry.IsHidden)
		}
	case schema.Language:
		stats.EntryCount += len(section.LanguageEntries)
		for _, entry := range section.LanguageEntries {
			hidden = append(hidden, entry.IsHidden)
		}
	case schema.Certificate:
		stats.EntryCount += len(section.CertificateEntries)
		for _, entry := range section.CertificateEntries {
			texts = append(texts, entry.Description)
			hidden = append(hidden, entry.IsHidden)
		}
	case schema.Project:
		stats.EntryCount += len(section.ProjectEntries)
		for _, entry := range section.ProjectEntries {
			texts = append(texts, entry.Description)
			hidden = append(hidden, entry.IsHidden)
		}
	case schema.SectionTypeCustom:
		stats.EntryCount += len(section.CustomEntries)
		for _, entry := range section.CustomEntries {
			texts = append(texts, entry.Description)
			hidden = append(hidden, entry.IsHidden)
		}
	}
	for _, text := range texts {
		if text != nil && len(*text) > stats.LargestRichTextBytes {
			stats.LargestRichTextBytes = len(*text)
		}
	}
	for _, value := range hidden {
		if value != nil && *value {
			stats.HiddenEntryCount++
		}
	}
}

func fixtureDigest(item fixture) [32]byte { return sha256.Sum256(item.Canonical) }
