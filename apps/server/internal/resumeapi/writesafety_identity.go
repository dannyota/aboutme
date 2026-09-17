package resumeapi

// Mutation request identity: parsing the Idempotency-Key and If-Match
// headers, and hashing a request's canonical operation and body into the
// domain-separated digests that key idempotency records.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const (
	idempotencyOperationDomain = "aboutme.idempotency.operation.v1"
	idempotencyRequestDomain   = "aboutme.idempotency.request.v1"
)

func parseMutationHeaders(r *http.Request, requireMatch bool, accepted []int32) (mutationHeaders, *clientError) {
	keyValues := r.Header.Values("Idempotency-Key")
	if len(keyValues) == 0 {
		return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "idempotency_key_required", Message: "Idempotency-Key is required"}
	}
	if len(keyValues) != 1 || keyValues[0] == "" || strings.Contains(keyValues[0], ",") {
		return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "idempotency_key_invalid", Message: "Idempotency-Key must be one UUID"}
	}
	key, err := uuid.Parse(keyValues[0])
	if err != nil {
		return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "idempotency_key_invalid", Message: "Idempotency-Key must be one UUID"}
	}

	matchValues := r.Header.Values("If-Match")
	var revision *int64
	if !requireMatch {
		if len(matchValues) != 0 {
			return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "precondition_not_supported", Message: "If-Match is not supported when creating a resume"}
		}
	} else {
		if len(matchValues) == 0 {
			return mutationHeaders{}, &clientError{Status: http.StatusPreconditionRequired, Code: "precondition_required", Message: "If-Match is required"}
		}
		parsed, parseErr := parseIfMatch(matchValues)
		if parseErr != nil {
			return mutationHeaders{}, parseErr
		}
		revision = &parsed
	}

	version, versionErr := resolveWireVersion(r.Header, accepted)
	if versionErr != nil {
		return mutationHeaders{}, versionErr
	}
	return mutationHeaders{Key: key, ExpectedRevision: revision, WireVersion: version}, nil
}

func parseIfMatch(values []string) (int64, *clientError) {
	malformed := func() (int64, *clientError) {
		return 0, &clientError{Status: http.StatusBadRequest, Code: "precondition_malformed", Message: `If-Match must have the form "r<revision>"`}
	}
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return malformed()
	}
	value := values[0]
	if len(value) < 4 || !strings.HasPrefix(value, `"r`) || !strings.HasSuffix(value, `"`) {
		return malformed()
	}
	digits := value[2 : len(value)-1]
	if digits == "" || (len(digits) > 1 && digits[0] == '0') {
		return malformed()
	}
	revision, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || revision < 1 {
		return malformed()
	}
	return revision, nil
}

func operationHash(method, operation string, targets []string) [32]byte {
	fields := [][]byte{[]byte("method"), []byte(strings.ToUpper(method)), []byte("operation"), []byte(operation)}
	for _, target := range targets {
		fields = append(fields, []byte(target))
	}
	return tupleHash(idempotencyOperationDomain, fields)
}

func requestHash(version int32, precondition string, semanticInputs []string, payload []byte) [32]byte {
	fields := [][]byte{
		[]byte("wire_version"), []byte(wireVersionString(version)),
		[]byte("if_match"), []byte(precondition),
	}
	for _, input := range semanticInputs {
		fields = append(fields, []byte(input))
	}
	fields = append(fields, []byte("payload"), payload)
	return tupleHash(idempotencyRequestDomain, fields)
}

func tupleHash(domain string, fields [][]byte) [32]byte {
	var encoded bytes.Buffer
	var length [4]byte
	encoded.WriteString(domain)
	encoded.WriteByte(0)
	binary.BigEndian.PutUint32(length[:], tupleLength(len(fields)))
	encoded.Write(length[:])
	for _, field := range fields {
		binary.BigEndian.PutUint32(length[:], tupleLength(len(field)))
		encoded.Write(length[:])
		encoded.Write(field)
	}
	return sha256.Sum256(encoded.Bytes())
}

func tupleLength(length int) uint32 {
	if length < 0 || uint64(length) > uint64(^uint32(0)) {
		panic("resumeapi: tuple field exceeds uint32 length encoding")
	}
	return uint32(length)
}

func hexDigest(digest [32]byte) string { return hex.EncodeToString(digest[:]) }
