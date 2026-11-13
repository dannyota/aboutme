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

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/resume"
)

const (
	MaxEnvelopeBytes      = 3_407_872
	MaxPhotoBytes         = media.MaxObjectBytes
	MaxLanguageCharacters = resume.MaxLngCharacters
	maxJPEGEdge           = 2048
	maxPNGEdge            = 1024
)

var errInvalid = errors.New("print snapshot is invalid")

type Envelope struct {
	Version          int                               `json:"version"`
	ResumeID         string                            `json:"resumeId"`
	Revision         string                            `json:"revision"`
	PublicGeneration *string                           `json:"publicGeneration"`
	Lng              string                            `json:"lng"`
	Document         publicresume.PublicResumeDocument `json:"document"`
	ProjectLinks     map[string]string                 `json:"projectLinks,omitempty"`
}

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
	document := publicresume.ProjectDocument(source.Doc, photoURL)
	document.Customization = cloneCustomization(document.Customization)
	envelope := Envelope{
		Version:  1,
		ResumeID: source.ID.String(),
		Revision: strconv.FormatInt(source.Revision, 10),
		Lng:      projectLanguage(source.Lng),
		Document: document,
	}
	if _, err := Marshal(envelope); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

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

func Marshal(source Envelope) ([]byte, error) {
	if !validVersionLinks(source) || !canonicalUUID(source.ResumeID) || !canonicalPositiveInt64(source.Revision) {
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
	var contentType string
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
