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
		// AC-VIEW-001: signInToView is optional and absent keeps the stored value.
		{name: "signInToView true is present", body: `{"live":true,"downloadEnabled":true,"seoGeoEnabled":false,"signInToView":true}`, want: publishInput{Live: true, DownloadEnabled: true, SignInToView: optionalBool{Present: true, Value: true}}},
		{name: "signInToView false is present", body: `{"live":true,"downloadEnabled":true,"seoGeoEnabled":false,"signInToView":false}`, want: publishInput{Live: true, DownloadEnabled: true, SignInToView: optionalBool{Present: true, Value: false}}},
		{name: "null signInToView is malformed", body: `{"live":true,"downloadEnabled":true,"seoGeoEnabled":false,"signInToView":null}`, shape: "signInToView"},
		{name: "wrong signInToView type is malformed", body: `{"live":true,"downloadEnabled":true,"seoGeoEnabled":false,"signInToView":"true"}`, shape: "signInToView"},
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
	document := loadMinimalDocument(t)
	document.PersonalDetails = schema.PersonalDetails{FullName: &name}
	document.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{
			ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", JobTitle: &role, Employer: &role,
		}}),
	}
	document.Customization.Layout.Sections.Main = []string{"work"}
	document.Customization.Layout.Sections.Sidebar = []string{}
	return document
}

// AC-VIEW-001, AC-VIEW-007: an omitted signInToView field keeps the stored
// value, and a flag-gated attempt to turn it on is a closed publish issue
// while turning it off, or leaving it on, always succeeds.
func TestMergeSignInToView(t *testing.T) {
	t.Parallel()
	stored := currentPublish{SignInToView: true}
	if merged := mergeSignInToView(stored, publishInput{}); !merged.SignInToView {
		t.Fatal("absent signInToView must keep the stored true value")
	}
	if merged := mergeSignInToView(stored, publishInput{SignInToView: optionalBool{Present: true, Value: false}}); merged.SignInToView {
		t.Fatal("present signInToView=false must turn the switch off")
	}
	off := currentPublish{SignInToView: false}
	if merged := mergeSignInToView(off, publishInput{SignInToView: optionalBool{Present: true, Value: true}}); !merged.SignInToView {
		t.Fatal("present signInToView=true must turn the switch on")
	}
}

func TestValidateSignInToView(t *testing.T) {
	t.Parallel()
	document := publishCompleteDocument(t)
	slug := "ada-lovelace"

	disabledService := &Service{signInToViewEnabled: false}
	before := currentPublish{Slug: &slug, SignInToView: false}
	turnedOn := mergeSignInToView(before, publishInput{Live: true, SignInToView: optionalBool{Present: true, Value: true}})
	prepared := disabledService.validateSignInToView(before, validatePublish(document, turnedOn, publishInput{Live: true}))
	found := false
	for _, issue := range prepared.Issues {
		if issue.Path == "signInToView" && issue.Code == "disabled" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %#v, want a signInToView disabled issue when the flag is off", prepared.Issues)
	}

	enabledService := &Service{signInToViewEnabled: true}
	prepared = enabledService.validateSignInToView(before, validatePublish(document, turnedOn, publishInput{Live: true}))
	for _, issue := range prepared.Issues {
		if issue.Path == "signInToView" {
			t.Fatalf("issues = %#v, want no signInToView issue when the flag is on", prepared.Issues)
		}
	}

	// Turning it off, or leaving it on, is always allowed regardless of the
	// flag: gating never depends on the flag once a resume is already gated.
	alreadyOn := currentPublish{Slug: &slug, SignInToView: true}
	stillOn := mergeSignInToView(alreadyOn, publishInput{})
	prepared = disabledService.validateSignInToView(alreadyOn, validatePublish(document, stillOn, publishInput{Live: true}))
	for _, issue := range prepared.Issues {
		if issue.Path == "signInToView" {
			t.Fatalf("issues = %#v, want no signInToView issue for an already-on switch left on", prepared.Issues)
		}
	}
	turnedOff := mergeSignInToView(alreadyOn, publishInput{SignInToView: optionalBool{Present: true, Value: false}})
	prepared = disabledService.validateSignInToView(alreadyOn, validatePublish(document, turnedOff, publishInput{Live: true}))
	for _, issue := range prepared.Issues {
		if issue.Path == "signInToView" {
			t.Fatalf("issues = %#v, want no signInToView issue when turning it off", prepared.Issues)
		}
	}
}

// AC-VIEW-010: the sign-in-to-view switch flipping alone changes aggregate
// discovery membership, even when live and seoGeoEnabled do not change.
func TestPublishChangesDiscovery_SignInToViewAlone(t *testing.T) {
	t.Parallel()
	before := currentPublish{Live: true, SEOGeoEnabled: true, SignInToView: false}
	turnedOn := before
	turnedOn.SignInToView = true
	if !publishChangesDiscovery(before, turnedOn) {
		t.Fatal("turning sign in to view on alone must change discovery membership")
	}
	if publishChangesDiscovery(before, before) {
		t.Fatal("an unchanged state must not report a discovery change")
	}
}

func TestPublishShapeErrorDoesNotExposeInput(t *testing.T) {
	t.Parallel()

	err := (&publishShapeError{Field: "slug"}).Error()
	if err == "" || strings.Contains(err, "candidate-slug") {
		t.Fatalf("publishShapeError.Error() = %q", err)
	}
}
