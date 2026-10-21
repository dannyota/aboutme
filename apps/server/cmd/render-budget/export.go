package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

type fixtureExportManifest struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Fixtures      []fixtureExportDocument `json:"fixtures"`
}

type fixtureExportDocument struct {
	fixtureEvidence
	File  string              `json:"file"`
	Photo *fixtureExportPhoto `json:"photo,omitempty"`
}

type fixtureExportPhoto struct {
	File        string `json:"file"`
	ContentType string `json:"contentType"`
	Bytes       int    `json:"bytes"`
	SHA256      string `json:"sha256"`
}

func validateExportRepository(path string) error {
	if !cleanAbsolute(path) {
		return errors.New("invalid_repository_root")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return errors.New("invalid_repository_root")
	}
	info, err := os.Stat(filepath.Join(path, "go.work"))
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("invalid_repository_root")
	}
	return nil
}

func exportFixtures(repositoryRoot, output string) (resultErr error) {
	if err := validateExportRepository(repositoryRoot); err != nil {
		return err
	}
	corpus, err := buildFixtureCorpus(repositoryRoot)
	if err != nil {
		return err
	}
	directory, err := createExportDirectory(output)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := unix.Close(directory); closeErr != nil {
			resultErr = errors.Join(resultErr, errors.New("output_directory_close_failed"))
		}
	}()
	manifest := fixtureExportManifest{SchemaVersion: int(schema.CurrentVersion)}
	for _, item := range corpus {
		// The fixed corpus is the only source of output names and payloads.
		if item.Name != "minimal" && item.Name != "full" && item.Name != "maximum" {
			return errors.New("fixture_contract_failed")
		}
		name := item.Name + ".json"
		if writeErr := writeExportFile(directory, name, item.Canonical); writeErr != nil {
			return writeErr
		}
		digest := fixtureDigest(item)
		row := fixtureExportDocument{
			File: name,
			fixtureEvidence: fixtureEvidence{
				Name: item.Name, SHA256: hex.EncodeToString(digest[:]), CanonicalBytes: len(item.Canonical),
				SectionCount: item.Stats.SectionCount, EntryCount: item.Stats.EntryCount,
				LargestRichTextBytes: item.Stats.LargestRichTextBytes, HiddenEntryCount: item.Stats.HiddenEntryCount,
				LayoutSectionCount: item.Stats.LayoutSectionCount, PageFormat: item.Stats.PageFormat,
				FontFamily: item.Stats.FontFamily, InlinePhotoBytes: item.Stats.InlinePhotoBytes,
			},
		}
		if item.Name == "full" {
			if writeErr := writeExportFile(directory, "full-photo.png", item.Photo); writeErr != nil {
				return writeErr
			}
			photoDigest := sha256.Sum256(item.Photo)
			row.Photo = &fixtureExportPhoto{
				File: "full-photo.png", ContentType: item.PhotoContentType,
				Bytes: len(item.Photo), SHA256: hex.EncodeToString(photoDigest[:]),
			}
		}
		manifest.Fixtures = append(manifest.Fixtures, row)
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return errors.New("fixture_manifest_encoding_failed")
	}
	return writeExportFile(directory, "manifest.json", append(encoded, '\n'))
}

func createExportDirectory(path string) (directory int, resultErr error) {
	if !cleanAbsolute(path) {
		return -1, errors.New("invalid_output_directory")
	}
	parent, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, errors.New("output_directory_failed")
	}
	defer func() {
		if closeErr := unix.Close(parent); closeErr != nil {
			resultErr = errors.Join(resultErr, errors.New("output_directory_close_failed"))
			if directory >= 0 {
				if directoryCloseErr := unix.Close(directory); directoryCloseErr != nil {
					resultErr = errors.Join(resultErr, errors.New("output_directory_close_failed"))
				}
				directory = -1
			}
		}
	}()
	parts := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	// Open each parent without following symlinks and keep the final directory descriptor.
	for _, part := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(parent, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return -1, errors.New("invalid_output_directory")
		}
		closeErr := unix.Close(parent)
		parent = next
		if closeErr != nil {
			return -1, errors.New("output_directory_failed")
		}
	}
	name := parts[len(parts)-1]
	// Refuse every existing destination, including empty directories and symlinks.
	if mkdirErr := unix.Mkdirat(parent, name, 0o700); mkdirErr != nil {
		return -1, errors.New("output_directory_create_failed")
	}
	directory, err = unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, errors.New("output_directory_failed")
	}
	return directory, nil
}

func writeExportFile(directory int, name string, data []byte) error {
	descriptor, err := unix.Openat(directory, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return errors.New("fixture_export_write_failed")
	}
	file := os.NewFile(uintptr(descriptor), name)
	written, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || written != len(data) || closeErr != nil {
		return errors.New("fixture_export_write_failed")
	}
	return nil
}
