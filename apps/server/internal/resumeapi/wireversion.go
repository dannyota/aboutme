package resumeapi

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

const wireVersionHeader = "X-Resume-Schema-Version"

func wireVersionString(version int32) string { return strconv.FormatInt(int64(version), 10) }

func resolveWireVersion(header http.Header, accepted []int32) (int32, *clientError) {
	if accepted == nil {
		accepted = docmigrate.AcceptedVersions()
	}
	values := header.Values(wireVersionHeader)
	if len(values) == 0 {
		return docmigrate.CurrentVersion, nil
	}
	if len(values) != 1 || values[0] == "" || strings.Contains(values[0], ",") {
		return 0, unsupportedWireVersion(accepted)
	}
	parsed, err := strconv.ParseInt(values[0], 10, 32)
	if err != nil || parsed < 1 || !slices.Contains(accepted, int32(parsed)) {
		return 0, unsupportedWireVersion(accepted)
	}
	return int32(parsed), nil
}

func unsupportedWireVersion(accepted []int32) *clientError {
	return &clientError{
		Status: http.StatusBadRequest, Code: "unsupported_schema_version",
		Message: "resume schema version is not supported",
		Details: map[string]any{"acceptedVersions": accepted},
	}
}
