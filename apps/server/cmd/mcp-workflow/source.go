package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

var (
	errSourceSelection = errors.New("source_selection_invalid")
	errSourceChanged   = errors.New("source_changed")
)

const (
	sourceLanguage = "en"
	targetLanguage = "vi"
	targetTitle    = "CV tiếng Việt"
	// maxBaselineResumes is the design's account cap before the create.
	maxBaselineResumes = 2
	// maxListedResumes admits the baseline plus the one target.
	maxListedResumes = maxBaselineResumes + 1
)

type resumeSummary struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Lng             string    `json:"lng"`
	Revision        string    `json:"revision"`
	Live            bool      `json:"live"`
	Slug            *string   `json:"slug"`
	DownloadEnabled bool      `json:"downloadEnabled"`
	SEOGeoEnabled   bool      `json:"seoGeoEnabled"`
	PublicTitle     *string   `json:"publicTitle"`
	FaviconEmoji    *string   `json:"faviconEmoji"`
	SchemaVersion   int32     `json:"schemaVersion"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type resumeState struct {
	resumeSummary
	Document json.RawMessage `json:"document"`
}

type listedResumes struct {
	Resumes []resumeSummary `json:"resumes"`
}

type getResumeResult struct {
	State resumeState `json:"state"`
}

type getPhotoResult struct {
	ContentType string `json:"content_type"`
	DataBase64  string `json:"data_base64"`
}

type mutationResult struct {
	Revision string      `json:"revision"`
	State    resumeState `json:"state"`
}

// sourceSnapshot is everything the workflow compares before and after a
// mutation: summary, canonical document, and photo bytes.
type sourceSnapshot struct {
	Handle    sourceHandle
	State     resumeState
	Canonical []byte
	Crop      *photoCrop
	Photo     *photoBytes
}

func validUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func validLanguageTag(value string) bool {
	if value == "" || len(value) > 35 {
		return false
	}
	for _, r := range value {
		if !tagRune(r) {
			return false
		}
	}
	return true
}

func tagRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-'
}

func validResumeSummary(item resumeSummary) bool {
	return validUUID(item.ID) && validLanguageTag(item.Lng) && item.Title != "" && len(item.Title) <= 1024 &&
		validDecimalRevision(item.Revision) && item.SchemaVersion > 0 && !item.CreatedAt.IsZero() &&
		!item.UpdatedAt.IsZero() && !item.UpdatedAt.Before(item.CreatedAt)
}

func validResumeState(state resumeState) bool {
	return validResumeSummary(state.resumeSummary) && len(state.Document) > 0 && len(state.Document) <= resume.MaxDocumentBytes*2
}

// selectEnglishSource enforces the fresh-run account shape: at most two
// resumes, distinct identifiers, and exactly one with lng en.
func selectEnglishSource(listing []resumeSummary) (sourceHandle, error) {
	if len(listing) == 0 || len(listing) > maxBaselineResumes || !distinctIDs(listing) {
		return "", errSourceSelection
	}
	var source sourceHandle
	for _, item := range listing {
		if item.Lng != sourceLanguage {
			continue
		}
		if source != "" {
			return "", errSourceSelection
		}
		source = sourceHandle(item.ID)
	}
	if source == "" {
		return "", errSourceSelection
	}
	return source, nil
}

func distinctIDs(listing []resumeSummary) bool {
	seen := make(map[string]bool, len(listing))
	for _, item := range listing {
		if seen[item.ID] {
			return false
		}
		seen[item.ID] = true
	}
	return true
}

// canonicalDocument strictly decodes a document, validates it, and returns the
// canonical bytes without the server-owned photo plus that photo.
func canonicalDocument(raw []byte) (schema.Resume, []byte, *schema.Photo, error) {
	var document schema.Resume
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decodeErr := decoder.Decode(&document); decodeErr != nil {
		return schema.Resume{}, nil, nil, errSourceSelection
	}
	if trailingErr := decoder.Decode(&struct{}{}); !errors.Is(trailingErr, io.EOF) {
		return schema.Resume{}, nil, nil, errSourceSelection
	}
	// Store validation applies the full JSON schema, so a document missing
	// required parts cannot pass as a zero-valued struct.
	if resume.ValidateForStore(document) != nil {
		return schema.Resume{}, nil, nil, errSourceSelection
	}
	photo := document.PersonalDetails.Photo
	document.PersonalDetails.Photo = nil
	canonical, err := resume.AssembleCanonical(document)
	if err != nil || len(canonical) > resume.MaxDocumentBytes {
		return schema.Resume{}, nil, nil, errSourceSelection
	}
	return document, canonical, photo, nil
}

// readSource reads the source state and, when the document names a photo,
// the photo bytes. Photo bytes stay in memory.
func readSource(ctx context.Context, guard *toolGuard, handle sourceHandle) (sourceSnapshot, error) {
	state, err := guard.getResume(ctx, handle)
	if err != nil || state.Lng != sourceLanguage {
		return sourceSnapshot{}, errSourceSelection
	}
	_, canonical, photo, err := canonicalDocument(state.Document)
	if err != nil {
		return sourceSnapshot{}, err
	}
	snapshot := sourceSnapshot{Handle: handle, State: state, Canonical: canonical}
	if photo == nil {
		return snapshot, nil
	}
	snapshot.Crop = cropFromSchema(photo.Crop)
	bytesRead, err := guard.getPhoto(ctx, handle)
	if err != nil {
		return sourceSnapshot{}, err
	}
	snapshot.Photo = &bytesRead
	return snapshot, nil
}

// sameSnapshot compares every field that a document, revision, photo, or
// publication change would alter.
func sameSnapshot(left, right sourceSnapshot) bool {
	if left.Handle != right.Handle || !sameSummary(left.State.resumeSummary, right.State.resumeSummary) ||
		!bytes.Equal(left.State.Document, right.State.Document) || !bytes.Equal(left.Canonical, right.Canonical) ||
		!left.Crop.equal(right.Crop) {
		return false
	}
	if left.Photo == nil || right.Photo == nil {
		return left.Photo == nil && right.Photo == nil
	}
	return left.Photo.ContentType == right.Photo.ContentType && bytes.Equal(left.Photo.Data, right.Photo.Data)
}

func sameSummary(left, right resumeSummary) bool {
	return left.ID == right.ID && left.Title == right.Title && left.Lng == right.Lng && left.Revision == right.Revision &&
		left.Live == right.Live && sameOptional(left.Slug, right.Slug) && left.DownloadEnabled == right.DownloadEnabled &&
		left.SEOGeoEnabled == right.SEOGeoEnabled && sameOptional(left.PublicTitle, right.PublicTitle) &&
		sameOptional(left.FaviconEmoji, right.FaviconEmoji) && left.SchemaVersion == right.SchemaVersion &&
		left.CreatedAt.Equal(right.CreatedAt) && left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameOptional(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// sameListing requires the same resumes with unchanged summaries, in any order.
func sameListing(left, right []resumeSummary) bool {
	if len(left) != len(right) {
		return false
	}
	byID := make(map[string]resumeSummary, len(right))
	for _, item := range right {
		byID[item.ID] = item
	}
	for _, item := range left {
		other, ok := byID[item.ID]
		if !ok || !sameSummary(item, other) {
			return false
		}
	}
	return distinctIDs(left) && distinctIDs(right)
}

// listingIDs returns sorted identifiers.
func listingIDs(listing []resumeSummary) []string {
	ids := make([]string, 0, len(listing))
	for _, item := range listing {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	return ids
}

// accountBinding binds a create intent to one origin and the exact baseline
// resume set. It contains no resume content.
func accountBinding(origin string, ids []string) string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return digest([]byte("aboutme-mcp-owner-workflow-account-v1\x00" + origin + "\x00" + strings.Join(sorted, "\x00")))
}

// validTarget proves a private Vietnamese target whose document, without the
// server-owned photo, is exactly the intent payload.
func validTarget(state resumeState, payload []byte) (*schema.Photo, bool) {
	if state.Lng != targetLanguage || state.Title != targetTitle || state.Live || state.Slug != nil {
		return nil, false
	}
	_, canonical, photo, err := canonicalDocument(state.Document)
	return photo, err == nil && bytes.Equal(canonical, payload)
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}
