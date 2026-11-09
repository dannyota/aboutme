package accountapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"

	// Register the standard JPEG decoder used to validate stored portable media.
	_ "image/jpeg"
	// Register the standard PNG decoder used to validate stored portable media.
	_ "image/png"
	"io"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// storedPhotoReference is the photo entry of a projected document's personal
// details, removed from the portable document and read separately.
type storedPhotoReference struct {
	// Key is the stored media object key; it never leaves the server.
	Key string `json:"key"`
	// Crop is the stored crop JSON, set to JSON null when absent.
	Crop json.RawMessage `json:"crop"`
}

func (s *Service) exportDocument(row store.ListAccountExportResumesRow) (json.RawMessage, *storedPhotoReference, error) {
	personalDetails, content, customization, err := s.projector.Project(
		row.PersonalDetails, row.Content, row.Customization, row.SchemaVersion,
	)
	if err != nil {
		return nil, nil, err
	}
	if _, decodeErr := resume.DecodeParts(personalDetails, content, customization, s.projector.CurrentVersion()); decodeErr != nil {
		return nil, nil, fmt.Errorf("decode projected document: %w", decodeErr)
	}

	var personal map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(personalDetails, &personal); unmarshalErr != nil || personal == nil {
		return nil, nil, errors.New("invalid projected personal details")
	}
	var photo *storedPhotoReference
	if raw, ok := personal["photo"]; ok && !bytes.Equal(raw, []byte("null")) {
		var reference storedPhotoReference
		if unmarshalErr := json.Unmarshal(raw, &reference); unmarshalErr != nil || reference.Key == "" {
			return nil, nil, errors.New("invalid projected photo reference")
		}
		if len(reference.Crop) == 0 {
			reference.Crop = json.RawMessage("null")
		}
		photo = &reference
	}
	delete(personal, "photo")
	personalDetails, err = json.Marshal(personal)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal portable personal details: %w", err)
	}
	document, err := json.Marshal(struct {
		Content         json.RawMessage `json:"content"`
		Customization   json.RawMessage `json:"customization"`
		PersonalDetails json.RawMessage `json:"personalDetails"`
		SchemaVersion   int32           `json:"schemaVersion"`
	}{
		Content: content, Customization: customization, PersonalDetails: personalDetails,
		SchemaVersion: s.projector.CurrentVersion(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal portable document: %w", err)
	}
	return document, photo, nil
}

func (s *Service) exportPhoto(ctx context.Context, resumeID uuid.UUID, reference *storedPhotoReference) (*exportPhoto, error) {
	if reference == nil {
		return nil, nil
	}
	extension, err := media.ParsePhotoKey(resumeID, reference.Key)
	if err != nil {
		return nil, errors.New("invalid referenced media key")
	}
	ctx, cancel := context.WithTimeout(ctx, exportPhotoTimeout)
	defer cancel()
	reader, mediaType, err := s.media.Get(ctx, reference.Key)
	if err != nil {
		if reader != nil {
			if closeErr := reader.Close(); closeErr != nil {
				s.logExportFailure(ctx, "close_get_error_reader", closeErr)
			}
		}
		return nil, err
	}
	if reader == nil {
		return nil, errors.New("referenced media reader is missing")
	}

	var closeOnce sync.Once
	var closeErr error
	closeBody := func() { closeOnce.Do(func() { closeErr = reader.Close() }) }
	stopClose := make(chan struct{})
	closeJoined := make(chan struct{})
	go func() {
		defer close(closeJoined)
		select {
		case <-ctx.Done():
			closeBody()
		case <-stopClose:
		}
	}()
	defer func() {
		close(stopClose)
		<-closeJoined
		closeBody()
	}()

	wantMediaType := "image/jpeg"
	if extension == "png" {
		wantMediaType = "image/png"
	}
	if mediaType != wantMediaType {
		return nil, errors.New("referenced media type does not match key")
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, media.MaxObjectBytes+1))
	if readErr != nil {
		return nil, fmt.Errorf("read photo: %w", readErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read photo: %w", err)
	}
	if len(data) == 0 || len(data) > media.MaxObjectBytes {
		return nil, errors.New("referenced media exceeds size limit")
	}
	if !validPortablePhoto(data, extension) {
		return nil, errors.New("referenced media is invalid")
	}
	closeBody()
	if closeErr != nil {
		return nil, fmt.Errorf("close photo: %w", closeErr)
	}
	return &exportPhoto{
		MediaType: mediaType,
		Data:      base64.StdEncoding.EncodeToString(data),
		Crop:      reference.Crop,
	}, nil
}

func validPortablePhoto(data []byte, extension string) bool {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (extension == "jpg" && format != "jpeg") || (extension == "png" && format != "png") {
		return false
	}
	maxEdge := exportJPEGMaxEdge
	if extension == "png" {
		maxEdge = exportPNGMaxEdge
	}
	if config.Width < 1 || config.Height < 1 || config.Width > maxEdge || config.Height > maxEdge {
		return false
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || (extension == "jpg" && format != "jpeg") || (extension == "png" && format != "png") {
		return false
	}
	bounds := decoded.Bounds()
	return bounds.Dx() == config.Width && bounds.Dy() == config.Height
}

func projectExportLanguage(value *string) string {
	if value == nil || *value == "" {
		return language.Und.String()
	}
	tag, err := language.Parse(*value)
	if err != nil {
		return language.Und.String()
	}
	canonical := tag.String()
	if utf8.RuneCountInString(canonical) > resume.MaxLngCharacters {
		return language.Und.String()
	}
	return canonical
}

func portableProviders(providers []string) ([]string, error) {
	out := make([]string, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		if provider != "google" && provider != "github" && provider != "linkedin" {
			return nil, errors.New("unknown linked provider")
		}
		if _, exists := seen[provider]; exists {
			continue
		}
		seen[provider] = struct{}{}
		out = append(out, provider)
	}
	if len(out) > 3 {
		return nil, errors.New("too many linked providers")
	}
	return out, nil
}
