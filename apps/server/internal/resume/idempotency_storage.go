package resume

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

func bodyForStorage(stored StoredResponse) (json.RawMessage, error) {
	if stored.Status < 100 || stored.Status > 599 {
		return nil, fmt.Errorf("resume: idempotency: invalid response status %d", stored.Status)
	}
	if stored.Status == 204 {
		if len(stored.Body) != 0 {
			return nil, fmt.Errorf("resume: idempotency: 204 response must have an empty body, got %d bytes", len(stored.Body))
		}
		return json.RawMessage(`null`), nil
	}
	if len(stored.Body) == 0 {
		return nil, fmt.Errorf("resume: idempotency: non-204 response requires a JSON body")
	}
	return stored.Body, nil
}

func storedResponseFromRecord(status int32, body, headersJSON json.RawMessage) (StoredResponse, error) {
	headers, err := decodeStoredHeaders(headersJSON)
	if err != nil {
		return StoredResponse{}, err
	}
	resp := StoredResponse{Status: int(status), Headers: headers}
	if status == 204 {
		if !bytes.Equal(bytes.TrimSpace(body), []byte(`null`)) {
			return StoredResponse{}, fmt.Errorf("resume: idempotency: stored 204 record body is not the null sentinel")
		}
		return resp, nil
	}
	resp.Body = body
	return resp, nil
}

func encodeStoredHeaders(headers map[string]string) (json.RawMessage, error) {
	if len(headers) == 0 {
		return json.RawMessage(`{}`), nil
	}
	for name := range headers {
		if !isStoredHeaderName(name) {
			return nil, fmt.Errorf("resume: idempotency: header %q is not an approved stored response header", name)
		}
	}
	encoded, err := json.Marshal(headers)
	if err != nil {
		return nil, fmt.Errorf("resume: idempotency: encode stored headers: %w", err)
	}
	return encoded, nil
}

func decodeStoredHeaders(headersJSON json.RawMessage) (map[string]string, error) {
	if len(headersJSON) == 0 {
		return nil, fmt.Errorf("resume: idempotency: stored headers value is empty")
	}
	var headers map[string]string
	if err := json.Unmarshal(headersJSON, &headers); err != nil {
		return nil, fmt.Errorf("resume: idempotency: decode stored headers: %w", err)
	}
	if len(headers) == 0 {
		return nil, nil
	}
	for name := range headers {
		if !isStoredHeaderName(name) {
			return nil, fmt.Errorf("resume: idempotency: stored header %q is not approved", name)
		}
	}
	return headers, nil
}

func isStoredHeaderName(name string) bool {
	switch name {
	case storedHeaderLocation, storedHeaderETag, storedHeaderSchemaVersion:
		return true
	default:
		return false
	}
}

// PostgreSQL defines these SQLSTATEs as indeterminate outcomes, even when pgx decodes a *pgconn.PgError.
func commitOutcomeUnknownSQLState(code string) bool {
	return code == "08007" || code == "40003"
}

func isIdempotencyKeyConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == idempotencyRecordUniqueViolationCode && pgErr.ConstraintName == idempotencyRecordUniqueConstraint
}
