package projectapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type mutationHeaders struct {
	key      uuid.UUID
	revision int64
}

func parseHeaders(r *http.Request) (mutationHeaders, *clientError) {
	key, err := parseKey(r)
	if err != nil {
		return mutationHeaders{}, err
	}
	if r.Method == http.MethodPost {
		if len(r.Header.Values("If-Match")) > 0 {
			return mutationHeaders{}, failure(400, "precondition_not_supported", "If-Match is not supported when creating a project")
		}
		return mutationHeaders{key: key}, nil
	}
	revision, err := parseMatch(r)
	if err != nil {
		return mutationHeaders{}, err
	}
	return mutationHeaders{key: key, revision: revision}, nil
}

func parseKey(r *http.Request) (uuid.UUID, *clientError) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) == 0 {
		return uuid.Nil, failure(400, "idempotency_key_required", "Idempotency-Key is required")
	}
	if len(values) != 1 || values[0] == "" || strings.Contains(values[0], ",") {
		return uuid.Nil, failure(400, "idempotency_key_invalid", "Idempotency-Key must be one UUID")
	}
	key, err := uuid.Parse(values[0])
	if err != nil {
		return uuid.Nil, failure(400, "idempotency_key_invalid", "Idempotency-Key must be one UUID")
	}
	return key, nil
}

func parseMatch(r *http.Request) (int64, *clientError) {
	matches := r.Header.Values("If-Match")
	if len(matches) == 0 {
		return 0, failure(428, "precondition_required", "If-Match is required")
	}
	malformed := failure(400, "precondition_malformed", `If-Match must have the form "r<revision>"`)
	if len(matches) != 1 || len(matches[0]) < 4 || !strings.HasPrefix(matches[0], `"r`) || !strings.HasSuffix(matches[0], `"`) {
		return 0, malformed
	}
	digits := matches[0][2 : len(matches[0])-1]
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, malformed
		}
	}
	revision, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || revision < 1 || len(digits) > 1 && digits[0] == '0' {
		return 0, malformed
	}
	return revision, nil
}
func revisionTag(revision int64) string { return `"r` + strconv.FormatInt(revision, 10) + `"` }

func identity(method string, id uuid.UUID, revision int64, caller string, body []byte) (string, [32]byte) {
	operation := sha256.Sum256([]byte("aboutme.project.operation.v1\x00" + method + "\x00" + id.String()))
	request := append(strconv.AppendInt(nil, revision, 10), 0)
	request = append(append(request, caller...), 0)
	return hex.EncodeToString(operation[:]), sha256.Sum256(append(request, body...))
}
