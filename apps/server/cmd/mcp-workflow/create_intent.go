package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

var errCreateIntent = errors.New("create_intent_invalid")

const (
	createIntentVersion = 1
	// createReplayLifetime is the design's exact-replay cutoff, far inside the
	// server's 24-hour idempotency lifetime.
	createReplayLifetime = 12 * time.Hour
	// serverDateResolution covers the one-second resolution of HTTP Date.
	serverDateResolution = time.Second
)

// createIntent is the durable, byte-equivalent create request. It is written
// and synced before the first send and survives crashes and failed runs.
type createIntent struct {
	Version        int             `json:"version"`
	Origin         string          `json:"origin"`
	AccountBinding string          `json:"account_binding"`
	BaselineIDs    []string        `json:"baseline_ids"`
	SourceID       sourceHandle    `json:"source_id"`
	SourceRevision string          `json:"source_revision"`
	SourceLive     bool            `json:"source_live"`
	SourceSlug     *string         `json:"source_slug"`
	SourceDigest   string          `json:"source_digest"`
	PhotoDigest    string          `json:"photo_digest"`
	Payload        json.RawMessage `json:"payload"`
	PayloadDigest  string          `json:"payload_digest"`
	IdempotencyKey string          `json:"idempotency_key"`
	Language       string          `json:"lng"`
	Title          string          `json:"title"`
	ServerTime     time.Time       `json:"server_time"`
	FirstAttempt   time.Time       `json:"first_attempt"`
}

func newCreateIntent(origin string, baseline []resumeSummary, source sourceSnapshot, payload []byte, key string, serverTime time.Time) createIntent {
	ids := listingIDs(baseline)
	photoDigest := ""
	if source.Photo != nil {
		photoDigest = digest(source.Photo.Data)
	}
	return createIntent{
		Version: createIntentVersion, Origin: origin, AccountBinding: accountBinding(origin, ids), BaselineIDs: ids,
		SourceID: source.Handle, SourceRevision: source.State.Revision, SourceLive: source.State.Live,
		SourceSlug: copyOptional(source.State.Slug), SourceDigest: digest(source.Canonical),
		PhotoDigest: photoDigest, Payload: append(json.RawMessage(nil), payload...), PayloadDigest: digest(payload),
		IdempotencyKey: key, Language: targetLanguage, Title: targetTitle, ServerTime: serverTime.UTC(), FirstAttempt: serverTime.UTC(),
	}
}

func copyOptional(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func validCreateIntent(intent createIntent, origin string) bool {
	if intent.Version != createIntentVersion || intent.Origin != origin || len(intent.BaselineIDs) == 0 ||
		len(intent.BaselineIDs) > maxBaselineResumes || intent.AccountBinding != accountBinding(origin, intent.BaselineIDs) ||
		!validUUID(string(intent.SourceID)) || !validDecimalRevision(intent.SourceRevision) || !validDigest(intent.SourceDigest) ||
		(intent.SourceSlug != nil && *intent.SourceSlug == "") ||
		(intent.PhotoDigest != "" && !validDigest(intent.PhotoDigest)) || intent.PayloadDigest != digest(intent.Payload) ||
		len(intent.Payload) == 0 || len(intent.Payload) > resume.MaxDocumentBytes || !validUUID(intent.IdempotencyKey) ||
		intent.Language != targetLanguage || intent.Title != targetTitle || intent.ServerTime.IsZero() ||
		intent.FirstAttempt.Before(intent.ServerTime) {
		return false
	}
	sourceFound := false
	seen := make(map[string]bool, len(intent.BaselineIDs))
	for _, id := range intent.BaselineIDs {
		if !validUUID(id) || seen[id] {
			return false
		}
		seen[id] = true
		sourceFound = sourceFound || id == string(intent.SourceID)
	}
	if !sourceFound {
		return false
	}
	_, canonical, photo, err := canonicalDocument(intent.Payload)
	return err == nil && photo == nil && bytes.Equal(canonical, intent.Payload)
}

func encodeCreateIntent(intent createIntent) ([]byte, error) {
	return json.Marshal(intent)
}

// persistCreateIntent creates the intent exclusively. An existing intent,
// resolved or not, is never replaced.
func persistCreateIntent(control privateArtifacts, intent createIntent) error {
	if !validCreateIntent(intent, intent.Origin) {
		return errCreateIntent
	}
	encoded, err := encodeCreateIntent(intent)
	if err != nil {
		return errCreateIntent
	}
	return control.createExclusive(createIntentName, encoded)
}

// loadCreateIntent returns the durable intent. A present but malformed intent
// blocks every create.
func loadCreateIntent(control privateArtifacts, origin string) (createIntent, bool, error) {
	present, err := control.exists(createIntentName)
	if err != nil {
		return createIntent{}, false, errCreateIntent
	}
	if !present {
		return createIntent{}, false, nil
	}
	data, err := control.read(createIntentName)
	if err != nil {
		return createIntent{}, true, errCreateIntent
	}
	var intent createIntent
	if !strictJSON(data, &intent) || !validCreateIntent(intent, origin) {
		return createIntent{}, true, errCreateIntent
	}
	return intent, true, nil
}

// replayDeadline returns the local deadline for an exact replay. Replay needs
// a fresh server time that is not before the baseline and a whole request
// window that ends before the 12-hour cutoff.
func replayDeadline(intent createIntent, serverNow time.Time, localNow time.Time) (time.Time, bool) {
	if serverNow.IsZero() || serverNow.Before(intent.ServerTime) {
		return time.Time{}, false
	}
	cutoff := intent.ServerTime.Add(createReplayLifetime)
	if !serverNow.Add(createSendWindow + serverDateResolution).Before(cutoff) {
		return time.Time{}, false
	}
	return localNow.Add(createSendWindow), true
}
