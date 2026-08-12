package resumeapi

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

func TestResolveWireVersion(t *testing.T) {
	t.Parallel()

	accepted := docmigrate.AcceptedVersions()
	current := docmigrate.CurrentVersion
	for _, tc := range []struct {
		name     string
		values   []string
		want     int32
		wantCode string
	}{
		{"absent means current", nil, current, ""},
		{"current accepted", []string{wireVersionString(current)}, current, ""},
		{"nonnumeric", []string{"latest"}, 0, "unsupported_schema_version"},
		{"negative", []string{"-1"}, 0, "unsupported_schema_version"},
		{"zero", []string{"0"}, 0, "unsupported_schema_version"},
		{"far future", []string{"999999999999999999999"}, 0, "unsupported_schema_version"},
		{"repeated", []string{wireVersionString(current), wireVersionString(current)}, 0, "unsupported_schema_version"},
		{"folded", []string{wireVersionString(current) + ", " + wireVersionString(current)}, 0, "unsupported_schema_version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := make(http.Header)
			for _, value := range tc.values {
				header.Add(wireVersionHeader, value)
			}
			got, err := resolveWireVersion(header, accepted)
			if tc.wantCode == "" {
				if err != nil || got != tc.want {
					t.Fatalf("resolveWireVersion = (%d, %v), want (%d, nil)", got, err, tc.want)
				}
				return
			}
			if err == nil || err.Code != tc.wantCode {
				t.Fatalf("error = %v, want code %q", err, tc.wantCode)
			}
			if !reflect.DeepEqual(err.Details, map[string]any{"acceptedVersions": accepted}) {
				t.Fatalf("details = %#v, want acceptedVersions %v", err.Details, accepted)
			}
		})
	}
}
