// Package printapi exposes the capability redemption endpoint for the private
// render listener.
package printapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/printsnapshot"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

const (
	redeemPath            = "/internal-render/print/redeem"
	redeemAudience        = "nuxt-print"
	redeemBodyMaxBytes    = 128
	redeemRequestDeadline = 5 * time.Second
)

var (
	errMissingRedeemer = errors.New("printapi: redeemer is required")
	redeemFailureBody  = []byte("{\"error\":{\"code\":\"not_found\",\"message\":\"not found\"}}\n")
)

// Redeemer atomically consumes one private render capability.
type Redeemer interface {
	Redeem(context.Context, renderjob.Redemption) (renderjob.Snapshot, error)
}

type redeemHandler struct {
	redeemer Redeemer
}

// NewRedeemHandler constructs the private render-capability adapter.
func NewRedeemHandler(redeemer Redeemer) (http.Handler, error) {
	if redeemer == nil || nilInterfaceValue(redeemer) {
		return nil, errMissingRedeemer
	}
	return &redeemHandler{redeemer: redeemer}, nil
}

func nilInterfaceValue(value any) bool {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (h *redeemHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	deadline := time.Now().Add(redeemRequestDeadline)
	controller := http.NewResponseController(response)
	if controller.SetReadDeadline(deadline) == nil {
		defer controller.SetReadDeadline(time.Time{}) //nolint:errcheck // Deadline cleanup is best effort after the fixed response; retrying cannot repair it.
	}
	if controller.SetWriteDeadline(deadline) == nil {
		defer controller.SetWriteDeadline(time.Time{}) //nolint:errcheck // Deadline cleanup is best effort after the fixed response; retrying cannot repair it.
	}
	ctx, cancel := context.WithDeadline(request.Context(), deadline)
	defer cancel()

	if !exactRedeemPath(request) {
		writeRedeemResponse(response, http.StatusNotFound, redeemFailureBody, false)
		return
	}
	if request.Method != http.MethodPost {
		writeRedeemResponse(response, http.StatusMethodNotAllowed, redeemFailureBody, true)
		return
	}
	redemption, ok := parseRedemptionRequest(ctx, request)
	if !ok {
		writeRedeemResponse(response, http.StatusNotFound, redeemFailureBody, false)
		return
	}

	snapshot, err := h.redeemer.Redeem(ctx, redemption)
	if err != nil || ctx.Err() != nil || !snapshotMatches(snapshot, redemption.ResumeID) {
		writeRedeemResponse(response, http.StatusNotFound, redeemFailureBody, false)
		return
	}
	writeRedeemResponse(response, http.StatusOK, snapshot.Payload, false)
}

func exactRedeemPath(request *http.Request) bool {
	if request.URL == nil {
		return false
	}
	return request.RequestURI == redeemPath && request.URL.Scheme == "" && request.URL.Host == "" &&
		request.URL.User == nil && request.URL.Opaque == "" && request.URL.Path == redeemPath && request.URL.EscapedPath() == redeemPath &&
		request.URL.RawPath == "" && request.URL.RawQuery == "" && !request.URL.ForceQuery &&
		request.URL.Fragment == "" && request.URL.RawFragment == ""
}

func parseRedemptionRequest(ctx context.Context, request *http.Request) (renderjob.Redemption, bool) {
	if ctx.Err() != nil || request.Body == nil || request.ContentLength < 1 || request.ContentLength > redeemBodyMaxBytes ||
		len(request.TransferEncoding) != 0 || len(request.Trailer) != 0 || !validRequestHeaders(request) {
		return renderjob.Redemption{}, false
	}

	body, err := io.ReadAll(io.LimitReader(request.Body, redeemBodyMaxBytes+1))
	if err != nil || ctx.Err() != nil || len(body) == 0 || len(body) > redeemBodyMaxBytes || int64(len(body)) != request.ContentLength || !utf8.Valid(body) {
		return renderjob.Redemption{}, false
	}
	resumeID, audience, ok := decodeRedemptionBody(body)
	if !ok {
		return renderjob.Redemption{}, false
	}

	authorizations, _ := headerValues(request.Header, "Authorization")
	authorization := authorizations[0]
	capability, ok := parseCapability(authorization)
	if !ok {
		return renderjob.Redemption{}, false
	}
	jobIDs, _ := headerValues(request.Header, "X-Render-Job-ID")
	jobID, ok := parseCanonicalUUID(jobIDs[0])
	if !ok {
		return renderjob.Redemption{}, false
	}
	return renderjob.Redemption{
		ResumeID:   resumeID,
		JobID:      jobID,
		Audience:   audience,
		Capability: capability,
	}, true
}

func validRequestHeaders(request *http.Request) bool {
	for name := range request.Header {
		switch {
		case strings.EqualFold(name, "Authorization"),
			strings.EqualFold(name, "X-Render-Job-ID"),
			strings.EqualFold(name, "Content-Type"),
			strings.EqualFold(name, "Content-Length"),
			strings.EqualFold(name, "Connection"):
		default:
			return false
		}
	}
	if values, entries := headerValues(request.Header, "Authorization"); entries != 1 || len(values) != 1 || values[0] == "" {
		return false
	}
	if values, entries := headerValues(request.Header, "X-Render-Job-ID"); entries != 1 || len(values) != 1 || values[0] == "" {
		return false
	}
	if values, entries := headerValues(request.Header, "Content-Type"); entries != 1 || len(values) != 1 || values[0] != "application/json" {
		return false
	}
	if values, entries := headerValues(request.Header, "Connection"); entries != 0 && (entries != 1 || len(values) != 1 || values[0] != "close") {
		return false
	}
	if values, entries := headerValues(request.Header, "Content-Length"); entries != 0 &&
		(entries != 1 || len(values) != 1 || values[0] != strconv.FormatInt(request.ContentLength, 10)) {
		return false
	}
	return true
}

func headerValues(header http.Header, name string) ([]string, int) {
	var values []string
	entries := 0
	for candidate, candidateValues := range header {
		if strings.EqualFold(candidate, name) {
			entries++
			values = append(values, candidateValues...)
		}
	}
	return values, entries
}

func parseCapability(authorization string) (string, bool) {
	const prefix = "RenderCapability "
	if len(authorization) != len(prefix)+43 || !strings.HasPrefix(authorization, prefix) {
		return "", false
	}
	token := authorization[len(prefix):]
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return "", false
	}
	return token, true
}

func decodeRedemptionBody(body []byte) (uuid.UUID, string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return uuid.Nil, "", false
	}
	fields := make(map[string]string, 2)
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		key, isString := keyToken.(string)
		if tokenErr != nil || !isString || (key != "resumeId" && key != "audience") {
			return uuid.Nil, "", false
		}
		if _, duplicate := fields[key]; duplicate {
			return uuid.Nil, "", false
		}
		var value string
		if decodeErr := decoder.Decode(&value); decodeErr != nil {
			return uuid.Nil, "", false
		}
		fields[key] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || len(fields) != 2 {
		return uuid.Nil, "", false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return uuid.Nil, "", false
	}
	resumeID, ok := parseCanonicalUUID(fields["resumeId"])
	if !ok || fields["audience"] != redeemAudience {
		return uuid.Nil, "", false
	}
	return resumeID, fields["audience"], true
}

func parseCanonicalUUID(value string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(value)
	return parsed, err == nil && parsed != uuid.Nil && parsed.String() == value
}

func snapshotMatches(snapshot renderjob.Snapshot, requestedResumeID uuid.UUID) bool {
	if snapshot.ResumeID == uuid.Nil || snapshot.ResumeID != requestedResumeID || snapshot.Revision < 1 ||
		snapshot.SchemaVersion != int(schema.CurrentVersion) || snapshot.PublicGeneration < 0 ||
		(snapshot.PublicGeneration != 0 && snapshot.PublicGeneration != snapshot.Revision) ||
		len(snapshot.Payload) == 0 || len(snapshot.Payload) > printsnapshot.MaxEnvelopeBytes || !utf8.Valid(snapshot.Payload) ||
		validateUniqueJSON(snapshot.Payload) != nil {
		return false
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(snapshot.Payload, &envelope); err != nil || len(envelope) != 6 {
		return false
	}
	for _, key := range []string{"version", "resumeId", "revision", "publicGeneration", "lng", "document"} {
		if _, present := envelope[key]; !present {
			return false
		}
	}
	var version int
	var resumeID, revision, lng string
	if json.Unmarshal(envelope["version"], &version) != nil || version != 1 ||
		json.Unmarshal(envelope["resumeId"], &resumeID) != nil || resumeID != snapshot.ResumeID.String() ||
		json.Unmarshal(envelope["revision"], &revision) != nil || revision != strconv.FormatInt(snapshot.Revision, 10) ||
		json.Unmarshal(envelope["lng"], &lng) != nil || !canonicalLanguage(lng) {
		return false
	}

	wantGeneration := "null"
	if snapshot.PublicGeneration != 0 {
		encoded, err := json.Marshal(strconv.FormatInt(snapshot.PublicGeneration, 10))
		if err != nil {
			return false
		}
		wantGeneration = string(encoded)
	}
	if string(envelope["publicGeneration"]) != wantGeneration {
		return false
	}

	var document map[string]json.RawMessage
	var documentVersion int
	if json.Unmarshal(envelope["document"], &document) != nil || document == nil ||
		json.Unmarshal(document["schemaVersion"], &documentVersion) != nil || documentVersion != snapshot.SchemaVersion {
		return false
	}
	return true
}

func canonicalLanguage(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > printsnapshot.MaxLanguageCharacters {
		return false
	}
	tag, err := language.Parse(value)
	return err == nil && tag.String() == value
}

func validateUniqueJSON(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			key, ok := keyToken.(string)
			if keyErr != nil || !ok {
				return errors.New("invalid object key")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("duplicate object key")
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("invalid object close")
		}
		return nil
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("invalid array close")
		}
		return nil
	default:
		return errors.New("unexpected closing delimiter")
	}
}

func writeRedeemResponse(response http.ResponseWriter, status int, body []byte, allowPost bool) {
	header := response.Header()
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Type", "application/json")
	header.Set("Content-Length", strconv.Itoa(len(body)))
	header.Del("Content-Encoding")
	header.Del("Set-Cookie")
	header.Del("Allow")
	if allowPost {
		header.Set("Allow", http.MethodPost)
	}
	response.WriteHeader(status)
	_, _ = response.Write(body) //nolint:errcheck // The response is committed; retrying can duplicate bytes, and this private boundary must not log details.
}
