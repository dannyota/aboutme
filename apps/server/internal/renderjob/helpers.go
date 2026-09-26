package renderjob

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"io"
	"time"

	"github.com/google/uuid"
)

func (q *Queue) newJobID() (id uuid.UUID, err error) {
	q.sourceMu.Lock()
	defer q.sourceMu.Unlock()
	defer func() {
		if recover() != nil {
			id, err = uuid.Nil, ErrAuthoritySource
		}
	}()
	id, err = q.newUUID()
	if err != nil || id == uuid.Nil {
		return uuid.Nil, ErrAuthoritySource
	}
	return id, nil
}

func (q *Queue) newAuthority() (token string, hash [32]byte, err error) {
	q.sourceMu.Lock()
	defer q.sourceMu.Unlock()
	defer func() {
		if recover() != nil {
			token, hash, err = "", [32]byte{}, ErrAuthoritySource
		}
	}()
	raw := make([]byte, capabilityBytes)
	if _, err := io.ReadFull(q.entropy, raw); err != nil {
		return "", [32]byte{}, ErrAuthoritySource
	}
	return base64.RawURLEncoding.EncodeToString(raw), sha256.Sum256(raw), nil
}

func (q *Queue) outputLimit(format Format) int {
	switch format {
	case PNG:
		return q.pngLimit
	case Card:
		return q.cardLimit
	default:
		return q.pdfLimit
	}
}

func validFormat(format Format) bool { return format == PDF || format == PNG || format == Card }

func validPriority(priority Priority) bool {
	return priority == PriorityNormal || priority == PriorityLow
}

func validSnapshot(snapshot Snapshot, limit int) bool {
	return snapshot.ResumeID != uuid.Nil && snapshot.Revision > 0 && snapshot.SchemaVersion > 0 &&
		snapshot.PublicGeneration >= 0 &&
		(snapshot.PublicGeneration == 0 || snapshot.PublicGeneration == snapshot.Revision) &&
		len(snapshot.Payload) > 0 && len(snapshot.Payload) <= limit
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Payload = append([]byte(nil), snapshot.Payload...)
	return snapshot
}

func decodeAuthority(token string) ([]byte, bool) {
	if len(token) != base64.RawURLEncoding.EncodedLen(capabilityBytes) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != capabilityBytes || base64.RawURLEncoding.EncodeToString(raw) != token {
		return nil, false
	}
	return raw, true
}

func bindingDigest(stored *job) [32]byte {
	hash := sha256.New()
	writeDigest(hash, stored.snapshot.ResumeID[:])
	writeDigest(hash, stored.attempt.jobID[:])
	writeInt64(hash, stored.snapshot.Revision)
	writeInt64(hash, int64(stored.snapshot.SchemaVersion))
	writeInt64(hash, stored.snapshot.PublicGeneration)
	writeDigest(hash, []byte(stored.format))
	writeDigest(hash, []byte(audiencePrint))
	writeInt64(hash, stored.expiresAt.UnixNano())
	writeDigest(hash, stored.snapshotDigest[:])
	writeDigest(hash, stored.capabilityHash[:])
	writeDigest(hash, stored.controllerHash[:])
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func writeInt64(writer io.Writer, value int64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic("renderjob: digest encoding failed")
	}
}

func writeDigest(writer io.Writer, value []byte) {
	if _, err := writer.Write(value); err != nil {
		panic("renderjob: digest write failed")
	}
}

func boundedInt(value, maximum int) (int, error) {
	if value == 0 {
		return maximum, nil
	}
	if value < 1 || value > maximum {
		return 0, ErrInvalidRequest
	}
	return value, nil
}

func boundedDuration(value, maximum time.Duration) (time.Duration, error) {
	if value == 0 {
		return maximum, nil
	}
	if value < time.Nanosecond || value > maximum {
		return 0, ErrInvalidRequest
	}
	return value, nil
}

func callPrepare(ctx context.Context, prepare func(context.Context) (Snapshot, error)) (snapshot Snapshot, err error) {
	defer func() {
		if recover() != nil {
			snapshot, err = Snapshot{}, ErrPreparation
		}
	}()
	return prepare(ctx)
}

func callRenderer(ctx context.Context, renderer Renderer, navigation Navigation) (output []byte, err error) {
	defer func() {
		if recover() != nil {
			output, err = nil, ErrRendering
		}
	}()
	return renderer.Render(ctx, navigation)
}

func callValidate(ctx context.Context, validate func(context.Context, Snapshot) (err error), snapshot Snapshot) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrGenerationChanged
		}
	}()
	return validate(ctx, snapshot)
}
