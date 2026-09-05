// Package printsnapshot prepares the frozen document passed to private print
// rendering. It performs no authorization, storage, or queue operations.
package printsnapshot

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/jpeg"
	"image/png"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

const (
	// MaxEnvelopeBytes is the complete frozen JSON payload ceiling.
	MaxEnvelopeBytes = 3_407_872
	// MaxPhotoBytes is the decoded normalized-photo ceiling.
	MaxPhotoBytes = media.MaxObjectBytes
	// MaxLanguageCharacters is the canonical render-language ceiling.
	MaxLanguageCharacters = resume.MaxLngCharacters
	maxJPEGEdge           = 2048
	maxPNGEdge            = 1024
)

var errInvalid = errors.New("print snapshot is invalid")

// Envelope is the closed payload redeemed by the private print renderer.
type Envelope struct {
	Version          int                               `json:"version"`
	ResumeID         string                            `json:"resumeId"`
	Revision         string                            `json:"revision"`
	PublicGeneration *string                           `json:"publicGeneration"`
	Lng              string                            `json:"lng"`
	Document         publicresume.PublicResumeDocument `json:"document"`
}

// FromOwner projects and freezes one already-authorized owner revision.
func FromOwner(source resume.Resume, photo []byte, contentType string) (Envelope, error) {
	if source.ID == uuid.Nil || source.Revision <= 0 || source.Doc.SchemaVersion != schema.CurrentVersion {
		return Envelope{}, errInvalid
	}
	canonical, err := resume.AssembleCanonical(source.Doc)
	if err != nil || len(canonical) > resume.MaxDocumentBytes {
		return Envelope{}, errInvalid
	}
	photoURL, err := inlinePhoto(source.Doc.PersonalDetails.Photo != nil, photo, contentType)
	if err != nil {
		return Envelope{}, err
	}
	envelope := Envelope{
		Version:  1,
		ResumeID: source.ID.String(),
		Revision: strconv.FormatInt(source.Revision, 10),
		Lng:      projectLanguage(source.Lng),
		Document: publicresume.ProjectDocument(source.Doc, photoURL),
	}
	if _, err := Marshal(envelope); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

// FromPublic copies and freezes one already-admitted public generation.
func FromPublic(source publicresume.Snapshot, photo []byte, contentType string) (Envelope, error) {
	wantRevision := strconv.FormatInt(source.Revision, 10)
	if source.ResumeID == uuid.Nil || source.Revision <= 0 || source.Public.Revision != wantRevision || source.Public.Document.SchemaVersion != schema.CurrentVersion {
		return Envelope{}, errInvalid
	}
	photoURL, err := inlinePhoto(source.Public.Document.PersonalDetails.Photo != nil, photo, contentType)
	if err != nil {
		return Envelope{}, err
	}
	document := cloneDocument(source.Public.Document)
	if document.PersonalDetails.Photo != nil {
		document.PersonalDetails.Photo.URL = photoURL
	}
	publicGeneration := wantRevision
	envelope := Envelope{
		Version:          1,
		ResumeID:         source.ResumeID.String(),
		Revision:         wantRevision,
		PublicGeneration: &publicGeneration,
		Lng:              projectLanguage(&source.Public.Lng),
		Document:         document,
	}
	if _, err := Marshal(envelope); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

// Marshal validates and emits the exact frozen JSON bytes digested by the
// render queue.
func Marshal(source Envelope) ([]byte, error) {
	if source.Version != 1 || !canonicalUUID(source.ResumeID) || !canonicalPositiveInt64(source.Revision) {
		return nil, errInvalid
	}
	if source.PublicGeneration != nil {
		if !canonicalPositiveInt64(*source.PublicGeneration) || *source.PublicGeneration != source.Revision {
			return nil, errInvalid
		}
	}
	if !canonicalLanguage(source.Lng) || source.Document.SchemaVersion != schema.CurrentVersion {
		return nil, errInvalid
	}
	if err := validateCrop(source.Document.PersonalDetails.Photo); err != nil {
		return nil, err
	}
	if err := validateDataPhoto(source.Document.PersonalDetails.Photo); err != nil {
		return nil, err
	}
	encoded, err := marshalUnchecked(source)
	if err != nil || len(encoded) > MaxEnvelopeBytes {
		return nil, errInvalid
	}
	return encoded, nil
}

func marshalUnchecked(source Envelope) ([]byte, error) {
	return json.Marshal(source)
}

func projectLanguage(value *string) string {
	if value == nil || *value == "" {
		return language.Und.String()
	}
	tag, err := language.Parse(*value)
	if err != nil {
		return language.Und.String()
	}
	canonical := tag.String()
	if utf8.RuneCountInString(canonical) > MaxLanguageCharacters {
		return language.Und.String()
	}
	return canonical
}

func canonicalLanguage(value string) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxLanguageCharacters {
		return false
	}
	tag, err := language.Parse(value)
	return err == nil && tag.String() == value
}

func canonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func canonicalPositiveInt64(value string) bool {
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0 && strconv.FormatInt(parsed, 10) == value
}

func inlinePhoto(expected bool, photo []byte, contentType string) (string, error) {
	if !expected {
		if len(photo) != 0 || contentType != "" {
			return "", errInvalid
		}
		return "", nil
	}
	if len(photo) == 0 || len(photo) > MaxPhotoBytes || (contentType != "image/jpeg" && contentType != "image/png") {
		return "", errInvalid
	}
	if err := validateImage(photo, contentType); err != nil {
		return "", err
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(photo), nil
}

func validateDataPhoto(photo *publicresume.PublicPhoto) error {
	if photo == nil {
		return nil
	}
	contentType := ""
	switch {
	case strings.HasPrefix(photo.URL, "data:image/jpeg;base64,"):
		contentType = "image/jpeg"
	case strings.HasPrefix(photo.URL, "data:image/png;base64,"):
		contentType = "image/png"
	default:
		return errInvalid
	}
	comma := strings.IndexByte(photo.URL, ',')
	if comma < 0 {
		return errInvalid
	}
	raw := photo.URL[comma+1:]
	if len(raw) == 0 || len(raw) > base64.StdEncoding.EncodedLen(MaxPhotoBytes) {
		return errInvalid
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) == 0 || len(decoded) > MaxPhotoBytes || base64.StdEncoding.EncodeToString(decoded) != raw {
		return errInvalid
	}
	return validateImage(decoded, contentType)
}

func validateImage(photo []byte, contentType string) error {
	reader := bytes.NewReader(photo)
	var width, height, maxEdge int
	var err error
	switch contentType {
	case "image/jpeg":
		config, decodeErr := jpeg.DecodeConfig(reader)
		width, height, maxEdge, err = config.Width, config.Height, maxJPEGEdge, decodeErr
	case "image/png":
		config, decodeErr := png.DecodeConfig(reader)
		width, height, maxEdge, err = config.Width, config.Height, maxPNGEdge, decodeErr
	default:
		return errInvalid
	}
	if err != nil || width < 1 || height < 1 || width > maxEdge || height > maxEdge {
		return errInvalid
	}
	return nil
}

func validateCrop(photo *publicresume.PublicPhoto) error {
	if photo == nil || photo.Crop == nil {
		return nil
	}
	crop := photo.Crop
	if !finite(crop.X) || crop.X < 0 || crop.X > 1 || !finite(crop.Y) || crop.Y < 0 || crop.Y > 1 ||
		!finite(crop.Width) || crop.Width <= 0 || crop.Width > 1 || !finite(crop.Height) || crop.Height <= 0 || crop.Height > 1 {
		return errInvalid
	}
	return nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func cloneDocument(source publicresume.PublicResumeDocument) publicresume.PublicResumeDocument {
	out := source
	out.PersonalDetails = clonePersonalDetails(source.PersonalDetails)
	out.Content = make(publicresume.PublicContent, len(source.Content))
	for key, section := range source.Content {
		out.Content[key] = cloneSection(section)
	}
	out.Customization = cloneCustomization(source.Customization)
	return out
}

func clonePersonalDetails(source publicresume.PublicPersonalDetails) publicresume.PublicPersonalDetails {
	out := source
	if source.Details.Present() {
		details := source.Details.Value()
		for index := range details {
			details[index].Label = clonePointer(details[index].Label)
		}
		out.Details = publicresume.PresentPublicDetails(details)
	} else {
		out.Details = publicresume.AbsentPublicDetails()
	}
	out.Headline = clonePointer(source.Headline)
	if source.Photo != nil {
		photo := *source.Photo
		if source.Photo.Crop != nil {
			crop := *source.Photo.Crop
			photo.Crop = &crop
		}
		out.Photo = &photo
	}
	return out
}

func cloneSection(source publicresume.PublicSection) publicresume.PublicSection {
	out := source
	out.DisplayName = clonePointer(source.DisplayName)
	out.IconKey = clonePointer(source.IconKey)
	out.ProfileEntries = append([]publicresume.PublicProfileEntry(nil), source.ProfileEntries...)
	for index := range out.ProfileEntries {
		out.ProfileEntries[index].Text = clonePointer(source.ProfileEntries[index].Text)
	}
	out.WorkEntries = append([]publicresume.PublicWorkEntry(nil), source.WorkEntries...)
	for index := range out.WorkEntries {
		entry, original := &out.WorkEntries[index], source.WorkEntries[index]
		entry.City, entry.Country, entry.Description = clonePointer(original.City), clonePointer(original.Country), clonePointer(original.Description)
		entry.Employer, entry.EmployerLink, entry.JobTitle = clonePointer(original.Employer), clonePointer(original.EmployerLink), clonePointer(original.JobTitle)
		entry.Dates = cloneDates(original.Dates)
	}
	out.EducationEntries = append([]publicresume.PublicEducationEntry(nil), source.EducationEntries...)
	for index := range out.EducationEntries {
		entry, original := &out.EducationEntries[index], source.EducationEntries[index]
		entry.City, entry.Country, entry.Degree = clonePointer(original.City), clonePointer(original.Country), clonePointer(original.Degree)
		entry.Description, entry.School, entry.SchoolLink = clonePointer(original.Description), clonePointer(original.School), clonePointer(original.SchoolLink)
		entry.Dates = cloneDates(original.Dates)
	}
	out.SkillEntries = append([]publicresume.PublicSkillEntry(nil), source.SkillEntries...)
	for index := range out.SkillEntries {
		entry, original := &out.SkillEntries[index], source.SkillEntries[index]
		entry.InfoHTML, entry.Level, entry.Name = clonePointer(original.InfoHTML), clonePointer(original.Level), clonePointer(original.Name)
	}
	out.LanguageEntries = append([]publicresume.PublicLanguageEntry(nil), source.LanguageEntries...)
	for index := range out.LanguageEntries {
		entry, original := &out.LanguageEntries[index], source.LanguageEntries[index]
		entry.Level, entry.Name = clonePointer(original.Level), clonePointer(original.Name)
	}
	out.CertificateEntries = append([]publicresume.PublicCertificateEntry(nil), source.CertificateEntries...)
	for index := range out.CertificateEntries {
		entry, original := &out.CertificateEntries[index], source.CertificateEntries[index]
		entry.Date = cloneYearMonth(original.Date)
		entry.Description, entry.Issuer, entry.Title, entry.TitleLink = clonePointer(original.Description), clonePointer(original.Issuer), clonePointer(original.Title), clonePointer(original.TitleLink)
	}
	out.ProjectEntries = append([]publicresume.PublicProjectEntry(nil), source.ProjectEntries...)
	for index := range out.ProjectEntries {
		entry, original := &out.ProjectEntries[index], source.ProjectEntries[index]
		entry.Dates = cloneDates(original.Dates)
		entry.Description, entry.Link, entry.Title = clonePointer(original.Description), clonePointer(original.Link), clonePointer(original.Title)
	}
	out.CustomEntries = append([]publicresume.PublicCustomEntry(nil), source.CustomEntries...)
	for index := range out.CustomEntries {
		entry, original := &out.CustomEntries[index], source.CustomEntries[index]
		entry.Dates = cloneDates(original.Dates)
		entry.City, entry.Description, entry.Subtitle = clonePointer(original.City), clonePointer(original.Description), clonePointer(original.Subtitle)
		entry.Title, entry.TitleLink = clonePointer(original.Title), clonePointer(original.TitleLink)
	}
	return out
}

func cloneDates(source *publicresume.PublicDateRange) *publicresume.PublicDateRange {
	if source == nil {
		return nil
	}
	out := *source
	out.End = cloneYearMonth(source.End)
	out.Start.M = clonePointer(source.Start.M)
	return &out
}

func cloneYearMonth(source *publicresume.PublicYearMonth) *publicresume.PublicYearMonth {
	if source == nil {
		return nil
	}
	out := *source
	out.M = clonePointer(source.M)
	return &out
}

func cloneCustomization(source schema.Customization) schema.Customization {
	out := source
	out.Colors.Accent = clonePointer(source.Colors.Accent)
	out.Colors.Surface = clonePointer(source.Colors.Surface)
	out.Header = clonePointer(source.Header)
	out.Layout.SurfaceTarget = clonePointer(source.Layout.SurfaceTarget)
	out.Layout.Sections.Main = cloneStrings(source.Layout.Sections.Main)
	out.Layout.Sections.Sidebar = cloneStrings(source.Layout.Sections.Sidebar)
	out.Spacing.PageMargin = clonePointer(source.Spacing.PageMargin)
	return out
}

func clonePointer[T any](source *T) *T {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}

func cloneStrings(source []string) []string {
	if source == nil {
		return nil
	}
	return append([]string{}, source...)
}
