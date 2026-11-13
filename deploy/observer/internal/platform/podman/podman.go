package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"danny.vn/aboutme/deploy/observer/internal/document"
	"danny.vn/aboutme/deploy/observer/internal/platform"
	"danny.vn/aboutme/deploy/observer/schema"
)

const MaxBytes = 64 << 10

type Lister struct {
	path        string
	now         func() time.Time
	observation time.Time
}

func New(path string) *Lister { return &Lister{path: path, now: time.Now} }

func (l *Lister) ObservationTime() time.Time { return l.observation }

func (l *Lister) Running(ctx context.Context) ([]platform.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Clean(l.path))
	if err != nil {
		return nil, errors.New("cannot open observation file")
	}
	defer func() {
		_ = f.Close() //nolint:errcheck // A read failure stops publication; close cannot change the parsed snapshot.
	}()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("observation is not a regular file")
	}
	body, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, errors.New("cannot read observation file")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	images, observed, err := parse(body, l.now())
	if err != nil {
		return nil, err
	}
	l.observation = observed
	return images, nil
}

func parse(body []byte, now time.Time) ([]platform.Image, time.Time, error) {
	if len(body) > MaxBytes {
		return nil, time.Time{}, errors.New("observation exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := uniqueJSONValue(decoder, 0); err != nil {
		return nil, time.Time{}, errors.New("invalid or repeated observation JSON fields")
	}
	if err := schema.Validate(body); err != nil {
		return nil, time.Time{}, errors.New("invalid observation schema")
	}
	var doc document.Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, time.Time{}, errors.New("invalid observation JSON")
	}
	if doc.Platform.Provider != "greennode" || doc.Platform.Orchestrator != "podman" {
		return nil, time.Time{}, errors.New("observation is not greennode/podman")
	}
	observed, err := time.Parse(time.RFC3339, doc.ObservedAt)
	if err != nil || observed.Before(now.Add(-180*time.Second)) || observed.After(now.Add(60*time.Second)) {
		return nil, time.Time{}, errors.New("observation timestamp is invalid or outside freshness bounds")
	}
	observations := []platform.Observation{}
	for i, component := range doc.Components {
		if component.Name != platform.Components[i] {
			return nil, time.Time{}, errors.New("observation components are missing, repeated, or out of order")
		}
		if component.Name == platform.Maintenance && len(component.RunningImages) == 0 {
			return nil, time.Time{}, errors.New("maintenance must be omitted when stopped")
		}
		seen := map[string]bool{}
		for _, image := range component.RunningImages {
			if !platform.ValidDigest(image.Digest) || seen[image.Digest] {
				return nil, time.Time{}, errors.New("invalid or repeated observation digest")
			}
			seen[image.Digest] = true
			started, parseErr := time.Parse(time.RFC3339, image.RunningSince)
			if parseErr != nil || started.IsZero() {
				return nil, time.Time{}, errors.New("invalid observation start time")
			}
			for replica := 0; replica < image.Replicas; replica++ {
				observations = append(observations, platform.Observation{
					Component: component.Name, Digest: image.Digest, StartedAt: started,
				})
			}
		}
	}
	images, err := platform.Aggregate(observations)
	return images, observed, err
}

// encoding/json keeps the last of duplicate object keys, so tokens are checked for duplicates (encoding/json).
func uniqueJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return errors.New("observation JSON is too deeply nested")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return errors.New("unexpected JSON delimiter")
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			keyToken, keyErr := decoder.Token()
			key, ok := keyToken.(string)
			if keyErr != nil || !ok || seen[key] {
				return errors.New("invalid or repeated JSON key")
			}
			seen[key] = true
		}
		if valueErr := uniqueJSONValue(decoder, depth+1); valueErr != nil {
			return valueErr
		}
	}
	_, err = decoder.Token()
	return err
}
