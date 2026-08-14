package resumeapi

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

func TestPublishDecode(t *testing.T) {
	t.Parallel()

	valid := `{"live":true,"downloadEnabled":true,"seoGeoEnabled":false}`
	for _, test := range []struct {
		name     string
		body     string
		want     publishInput
		shape    string
		tooLarge bool
	}{
		{name: "omitted slug preserves absence", body: valid, want: publishInput{Live: true, DownloadEnabled: true}},
		{name: "explicit false booleans are accepted", body: `{"live":false,"downloadEnabled":false,"seoGeoEnabled":false}`, want: publishInput{}},
		{name: "nonempty slug is present", body: `{"slug":"ada-lovelace","live":true,"downloadEnabled":true,"seoGeoEnabled":false}`, want: publishInput{Slug: optionalSlug{Present: true, Value: "ada-lovelace"}, Live: true, DownloadEnabled: true}},
		{name: "null slug is malformed", body: `{"slug":null,"live":true,"downloadEnabled":true,"seoGeoEnabled":false}`, shape: "slug"},
		{name: "empty slug is malformed", body: `{"slug":"","live":true,"downloadEnabled":true,"seoGeoEnabled":false}`, shape: "slug"},
		{name: "missing live is malformed", body: `{"downloadEnabled":true,"seoGeoEnabled":false}`, shape: "live"},
		{name: "missing download enabled is malformed", body: `{"live":true,"seoGeoEnabled":false}`, shape: "downloadEnabled"},
		{name: "missing seo geo enabled is malformed", body: `{"live":true,"downloadEnabled":true}`, shape: "seoGeoEnabled"},
		{name: "unknown field is malformed", body: `{"live":true,"downloadEnabled":true,"seoGeoEnabled":false,"owner":"leaky"}`, shape: "body"},
		{name: "duplicate field is malformed", body: `{"live":true,"live":false,"downloadEnabled":true,"seoGeoEnabled":false}`, shape: "body"},
		{name: "wrong boolean type is malformed", body: `{"live":"true","downloadEnabled":true,"seoGeoEnabled":false}`, shape: "live"},
		{name: "wrong slug type is malformed", body: `{"slug":4,"live":true,"downloadEnabled":true,"seoGeoEnabled":false}`, shape: "slug"},
		{name: "trailing value is malformed", body: valid + ` {}`, shape: "body"},
		{name: "body overflow is too large", body: strings.Repeat(" ", maxJSONBodyBytes+1), tooLarge: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := decodePublish(strings.NewReader(test.body))
			if test.tooLarge {
				var client *clientError
				if !errors.As(err, &client) || client.Status != 413 || client.Code != "body_too_large" {
					t.Fatalf("overflow error = %#v, want 413 body_too_large", err)
				}
				return
			}
			if test.shape != "" {
				var shape *publishShapeError
				if !errors.As(err, &shape) || shape.Field != test.shape {
					t.Fatalf("decode error = %#v, want publishShapeError{%q}", err, test.shape)
				}
				if strings.Contains(shape.Error(), "leaky") {
					t.Fatalf("shape error leaked request content: %q", shape.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("decodePublish() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("decodePublish() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestCheapPreflightFailuresDoNotTouchFences(t *testing.T) {
	t.Parallel()

	current := currentPublish{Live: false, Revision: 9}
	invalid := validatePublish(schema.Resume{}, current, publishInput{Live: true})
	limiter := &countingSlugLimiter{}
	if admitChangedSlugAttempt(limiter, uuid.New(), time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC), invalid) {
		t.Fatal("invalid publish preflight was admitted")
	}
	if limiter.calls != 0 {
		t.Fatalf("invalid preflight touched slug limiter %d times", limiter.calls)
	}

	unchangedSlug := "ada-lovelace"
	validDocument := publishCompleteDocument(t)
	unchanged := validatePublish(validDocument, currentPublish{Slug: &unchangedSlug, Revision: 9}, publishInput{
		Live: false, DownloadEnabled: true, SEOGeoEnabled: false,
	})
	if !admitChangedSlugAttempt(limiter, uuid.New(), time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC), unchanged) {
		t.Fatal("unchanged slug was rejected")
	}
	if limiter.calls != 0 {
		t.Fatalf("unchanged slug consumed limiter capacity %d times", limiter.calls)
	}
}

type countingSlugLimiter struct {
	calls int
	allow bool
}

func (l *countingSlugLimiter) AllowChangedSlug(uuid.UUID, time.Time) bool {
	l.calls++
	return l.allow
}

func publishCompleteDocument(t *testing.T) schema.Resume {
	t.Helper()
	name := "Ada Lovelace"
	role := "Mathematician"
	return schema.Resume{
		PersonalDetails: schema.PersonalDetails{FullName: &name},
		Content: map[string]schema.Section{
			"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{
				ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", JobTitle: &role, Employer: &role,
			}}),
		},
	}
}

func TestPublishShapeErrorDoesNotExposeInput(t *testing.T) {
	t.Parallel()

	err := (&publishShapeError{Field: "slug"}).Error()
	if err == "" || strings.Contains(err, "candidate-slug") {
		t.Fatalf("publishShapeError.Error() = %q", err)
	}
}
