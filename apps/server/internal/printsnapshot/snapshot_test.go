package printsnapshot

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

const testResumeID = "18e2e099-6f70-4727-8ea9-5d6c331989b9"

func TestFromOwnerFreezesVisibleSanitizedDocumentAndInlinePhoto(t *testing.T) {
	photo := testPNG(t)
	name, lng, rich := "Ada", "EN-us", `<script>alert(1)</script><strong>safe</strong>`
	source := resume.Resume{
		ID: uuid.MustParse(testResumeID), Revision: 42, Lng: &lng,
		UserID: uuid.MustParse("f36d32e6-968f-4495-85ef-165bb1691975"), Title: "private title",
		Doc: schema.Resume{
			SchemaVersion: schema.CurrentVersion,
			PersonalDetails: schema.PersonalDetails{FullName: &name, Details: []schema.PersonalDetail{
				{ID: "shown", Type: schema.Email, Value: "ada@example.test"},
				{ID: "hidden", IsHidden: true, Type: schema.Phone, Value: "private-contact"},
			}, Photo: &schema.Photo{Key: "resumes/private/photo.png", Crop: &schema.PhotoCrop{X: 0, Y: 0, Width: 1, Height: 1}}},
			Content: map[string]schema.Section{
				"shown":  schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "shown-entry", Text: &rich}}),
				"hidden": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "hidden-entry", IsHidden: boolPointer(true), Text: stringPointer("private-entry")}}),
			},
			Customization: schema.Customization{Layout: schema.Layout{Sections: schema.Sections{Main: []string{"shown", "hidden"}}}},
		},
	}

	envelope, err := FromOwner(source, photo, "image/png")
	if err != nil {
		t.Fatalf("FromOwner: %v", err)
	}
	got, err := Marshal(envelope)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, forbidden := range []string{"private-contact", "private-entry", "private title", "resumes/private/photo.png", source.UserID.String(), "<script", "isHidden"} {
		if bytes.Contains(got, []byte(forbidden)) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, got)
		}
	}
	for _, required := range []string{`"version":1`, `"resumeId":"` + testResumeID + `"`, `"revision":"42"`, `"publicGeneration":null`, `"lng":"en-US"`, `"url":"data:image/png;base64,`, `\u003cstrong\u003esafe\u003c/strong\u003e`} {
		if !bytes.Contains(got, []byte(required)) {
			t.Fatalf("snapshot missing %q: %s", required, got)
		}
	}

	name, lng, rich = "Changed", "fr", "changed"
	source.Doc.PersonalDetails.Details[0].Value = "changed@example.test"
	source.Doc.PersonalDetails.Photo.Crop.Width = .5
	source.Doc.Customization.Layout.Sections.Main[0] = "changed"
	photo[0] = 0
	after, err := Marshal(envelope)
	if err != nil {
		t.Fatalf("Marshal after source mutation: %v", err)
	}
	if !bytes.Equal(after, got) {
		t.Fatal("source mutation changed frozen envelope")
	}
}

func TestFromPublicFreezesAdmittedGeneration(t *testing.T) {
	name, lng, slug, rich := "Ada", "en", "ada", "<strong>safe</strong>"
	owner := resume.Resume{ID: uuid.MustParse(testResumeID), Revision: 7, Lng: &lng, Slug: &slug, Doc: schema.Resume{
		SchemaVersion: schema.CurrentVersion,
		PersonalDetails: schema.PersonalDetails{FullName: &name, Photo: &schema.Photo{
			Key: "private.png", Crop: &schema.PhotoCrop{X: 0, Y: 0, Width: 1, Height: 1},
		}},
		Content: map[string]schema.Section{
			"profile": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "profile-entry", Text: &rich}}),
		},
		Customization: schema.Customization{Layout: schema.Layout{Sections: schema.Sections{Main: []string{"profile"}}}},
	}}
	origin, err := publicresume.ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	projected, err := publicresume.Project(owner, origin)
	if err != nil {
		t.Fatal(err)
	}
	source := publicresume.Snapshot{ResumeID: owner.ID, Revision: owner.Revision, Public: projected}
	envelope, err := FromPublic(source, testPNG(t), "image/png")
	if err != nil {
		t.Fatalf("FromPublic: %v", err)
	}
	first, err := Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(first, []byte(`"publicGeneration":"7"`)) || bytes.Contains(first, []byte("https://resume.example")) {
		t.Fatalf("public snapshot metadata/photo = %s", first)
	}

	name, rich = "Changed", "changed"
	source.Public.Document.PersonalDetails.FullName = "Changed again"
	source.Public.Document.PersonalDetails.Photo.Crop.Width = .5
	*source.Public.Document.Content["profile"].ProfileEntries[0].Text = "changed again"
	source.Public.Document.Customization.Layout.Sections.Main[0] = "changed"
	second, err := Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("public source mutation changed frozen envelope")
	}
}

func TestFromOwnerAcceptsNormalizedJPEG(t *testing.T) {
	owner := ownerWithoutPhoto()
	owner.Doc.PersonalDetails.Photo = &schema.Photo{Key: "private.jpg"}
	envelope, err := FromOwner(owner, testJPEG(t), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Document.PersonalDetails.Photo == nil || !strings.HasPrefix(envelope.Document.PersonalDetails.Photo.URL, "data:image/jpeg;base64,") {
		t.Fatalf("photo = %#v", envelope.Document.PersonalDetails.Photo)
	}
}

func TestValidateImageEnforcesNormalizedDimensions(t *testing.T) {
	jpegAtLimit := jpegWithDimensions(t, 2048, 1)
	if err := validateImage(jpegAtLimit, "image/jpeg"); err != nil {
		t.Fatalf("JPEG at normalized edge rejected: %v", err)
	}
	if err := validateImage(jpegWithDimensions(t, 2049, 1), "image/jpeg"); err == nil {
		t.Fatal("JPEG one pixel above normalized edge accepted")
	}
	if err := validateImage(jpegWithDimensions(t, 65535, 65535), "image/jpeg"); err == nil {
		t.Fatal("hostile small JPEG header with huge dimensions accepted")
	}

	pngAtLimit := pngWithDimensions(t, 1024, 1)
	if err := validateImage(pngAtLimit, "image/png"); err != nil {
		t.Fatalf("PNG at normalized edge rejected: %v", err)
	}
	if err := validateImage(pngWithDimensions(t, 1025, 1), "image/png"); err == nil {
		t.Fatal("PNG one pixel above normalized edge accepted")
	}
	if err := validateImage(pngWithDimensions(t, math.MaxUint32, math.MaxUint32), "image/png"); err == nil {
		t.Fatal("hostile small PNG header with huge dimensions accepted")
	}
	if err := validateImage(pngAtLimit, "image/jpeg"); err == nil {
		t.Fatal("PNG bytes accepted as JPEG")
	}
}

func TestValidateDataPhotoRejectsOversizeEncodingBeforeAllocation(t *testing.T) {
	raw := strings.Repeat("A", base64.StdEncoding.EncodedLen(MaxPhotoBytes)+4)
	photo := &publicresume.PublicPhoto{URL: "data:image/png;base64," + raw}
	if err := validateDataPhoto(photo); err == nil {
		t.Fatal("oversize base64 photo accepted")
	}
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_ = validateDataPhoto(photo)
		}
	})
	if allocated := result.AllocedBytesPerOp(); allocated > 64*1024 {
		t.Fatalf("oversize encoded rejection allocated %d bytes/op, want at most 65536", allocated)
	}
}

func TestSnapshotRejectsInvalidMetadataAndPhoto(t *testing.T) {
	base := ownerWithoutPhoto()
	withPhoto := base
	withPhoto.Doc.PersonalDetails.Photo = &schema.Photo{Key: "private.png", Crop: &schema.PhotoCrop{X: 0, Y: 0, Width: 1, Height: 1}}
	pngBytes := testPNG(t)

	tests := []struct {
		name        string
		source      resume.Resume
		photo       []byte
		contentType string
	}{
		{name: "missing ID", source: mutateOwner(base, func(v *resume.Resume) { v.ID = uuid.Nil })},
		{name: "nonpositive revision", source: mutateOwner(base, func(v *resume.Resume) { v.Revision = 0 })},
		{name: "negative revision", source: mutateOwner(base, func(v *resume.Resume) { v.Revision = -1 })},
		{name: "non-current schema", source: mutateOwner(base, func(v *resume.Resume) { v.Doc.SchemaVersion = schema.CurrentVersion - 1 })},
		{name: "unknown schema", source: mutateOwner(base, func(v *resume.Resume) { v.Doc.SchemaVersion = schema.CurrentVersion + 1 })},
		{name: "unexpected bytes", source: base, photo: pngBytes, contentType: "image/png"},
		{name: "unexpected content type", source: base, contentType: "image/png"},
		{name: "missing bytes", source: withPhoto, contentType: "image/png"},
		{name: "missing content type", source: withPhoto, photo: pngBytes},
		{name: "unsupported type", source: withPhoto, photo: pngBytes, contentType: "image/gif"},
		{name: "mismatched type", source: withPhoto, photo: pngBytes, contentType: "image/jpeg"},
		{name: "malformed bytes", source: withPhoto, photo: []byte("not an image"), contentType: "image/png"},
		{name: "oversize photo", source: withPhoto, photo: make([]byte, MaxPhotoBytes+1), contentType: "image/png"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := FromOwner(test.source, test.photo, test.contentType); err == nil {
				t.Fatal("FromOwner error = nil")
			}
		})
	}

	for _, crop := range []*schema.PhotoCrop{
		{X: -math.SmallestNonzeroFloat64, Y: 0, Width: 1, Height: 1},
		{X: math.Nextafter(1, 2), Y: 0, Width: 1, Height: 1},
		{X: 0, Y: -math.SmallestNonzeroFloat64, Width: 1, Height: 1},
		{X: 0, Y: math.Nextafter(1, 2), Width: 1, Height: 1},
		{X: 0, Y: 0, Width: 0, Height: 1},
		{X: 0, Y: 0, Width: math.Nextafter(1, 2), Height: 1},
		{X: 0, Y: 0, Width: 1, Height: 0},
		{X: 0, Y: 0, Width: 1, Height: math.Nextafter(1, 2)},
		{X: math.NaN(), Y: 0, Width: 1, Height: 1},
	} {
		bad := withPhoto
		bad.Doc.PersonalDetails.Photo = &schema.Photo{Key: "private.png", Crop: crop}
		if _, err := FromOwner(bad, pngBytes, "image/png"); err == nil {
			t.Fatalf("FromOwner accepted crop %#v", crop)
		}
	}
}

func TestLanguageFallbackAndPublicGenerationMismatch(t *testing.T) {
	owner := ownerWithoutPhoto()
	invalid := "not a language"
	owner.Lng = &invalid
	envelope, err := FromOwner(owner, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Lng != "und" {
		t.Fatalf("Lng = %q, want und", envelope.Lng)
	}

	projected := publicresume.PublicResume{Revision: "8", Lng: strings.Repeat("a", MaxLanguageCharacters+1), Document: publicresume.ProjectDocument(owner.Doc, "")}
	_, err = FromPublic(publicresume.Snapshot{ResumeID: owner.ID, Revision: 7, Public: projected}, nil, "")
	if err == nil {
		t.Fatal("FromPublic accepted generation mismatch")
	}
	projected.Revision = "7"
	publicEnvelope, err := FromPublic(publicresume.Snapshot{ResumeID: owner.ID, Revision: 7, Public: projected}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if publicEnvelope.Lng != "und" {
		t.Fatalf("public Lng = %q, want und", publicEnvelope.Lng)
	}
}

func TestMarshalClosedFieldsAndBounds(t *testing.T) {
	owner := ownerWithoutPhoto()
	envelope, err := FromOwner(owner, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"document", "lng", "publicGeneration", "resumeId", "revision", "version"}
	if len(fields) != len(wantKeys) {
		t.Fatalf("field count = %d, want %d: %v", len(fields), len(wantKeys), fields)
	}
	for _, key := range wantKeys {
		if _, ok := fields[key]; !ok {
			t.Errorf("missing field %q", key)
		}
		delete(fields, key)
	}
	if len(fields) != 0 {
		t.Fatalf("extra fields: %v", fields)
	}

	invalid := []Envelope{
		func() Envelope { v := envelope; v.Version = 2; return v }(),
		func() Envelope { v := envelope; v.ResumeID = "18E2E099-6F70-4727-8EA9-5D6C331989B9"; return v }(),
		func() Envelope { v := envelope; v.ResumeID = uuid.Nil.String(); return v }(),
		func() Envelope { v := envelope; v.Revision = "01"; return v }(),
		func() Envelope { v := envelope; v.Revision = "0"; return v }(),
		func() Envelope { v := envelope; v.Revision = "9223372036854775808"; return v }(),
		func() Envelope { v := envelope; v.Lng = strings.Repeat("a", MaxLanguageCharacters+1); return v }(),
		func() Envelope { v := envelope; v.Document.SchemaVersion = schema.CurrentVersion + 1; return v }(),
	}
	for index, value := range invalid {
		if _, err := Marshal(value); err == nil {
			t.Errorf("invalid envelope %d accepted", index)
		}
	}

	atMaxRevision := envelope
	atMaxRevision.Revision = strconv.FormatInt(math.MaxInt64, 10)
	if _, err := Marshal(atMaxRevision); err != nil {
		t.Fatalf("max int64 revision rejected: %v", err)
	}
	atMaxLanguage := envelope
	atMaxLanguage.Lng = "x-abcdefgh-abcdefgh-abcdefgh-abcdef"
	if utf8.RuneCountInString(atMaxLanguage.Lng) != MaxLanguageCharacters {
		t.Fatalf("language test setup has %d characters", utf8.RuneCountInString(atMaxLanguage.Lng))
	}
	if _, err := Marshal(atMaxLanguage); err != nil {
		t.Fatalf("language at limit rejected: %v", err)
	}

	base := envelope
	base.Document.PersonalDetails.FullName = ""
	baseBytes, err := marshalUnchecked(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Document.PersonalDetails.FullName = strings.Repeat("x", MaxEnvelopeBytes-len(baseBytes))
	atLimit, err := Marshal(base)
	if err != nil {
		t.Fatalf("payload at limit rejected: %v", err)
	}
	if len(atLimit) != MaxEnvelopeBytes {
		t.Fatalf("payload size = %d, want %d", len(atLimit), MaxEnvelopeBytes)
	}
	base.Document.PersonalDetails.FullName += "x"
	if _, err := Marshal(base); err == nil {
		t.Fatal("payload one byte above limit accepted")
	}
}

func TestOwnerDocumentAndPhotoByteBounds(t *testing.T) {
	owner := ownerWithoutPhoto()
	emptyDocument, err := resume.AssembleCanonical(owner.Doc)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("x", resume.MaxDocumentBytes-(len(emptyDocument)-len("Ada")))
	owner.Doc.PersonalDetails.FullName = &name
	atLimit, err := resume.AssembleCanonical(owner.Doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(atLimit) != resume.MaxDocumentBytes {
		t.Fatalf("document test setup = %d bytes, want %d", len(atLimit), resume.MaxDocumentBytes)
	}
	if _, err := FromOwner(owner, nil, ""); err != nil {
		t.Fatalf("document at limit rejected: %v", err)
	}
	name += "x"
	if _, err := FromOwner(owner, nil, ""); err == nil {
		t.Fatal("document one byte above limit accepted")
	}

	owner = ownerWithoutPhoto()
	owner.Doc.PersonalDetails.Photo = &schema.Photo{Key: "private.png"}
	photo := testPNG(t)
	photo = append(photo, make([]byte, MaxPhotoBytes-len(photo))...)
	if len(photo) != MaxPhotoBytes {
		t.Fatalf("photo test setup = %d bytes, want %d", len(photo), MaxPhotoBytes)
	}
	if _, err := FromOwner(owner, photo, "image/png"); err != nil {
		t.Fatalf("photo at limit rejected: %v", err)
	}
	photo = append(photo, 0)
	if _, err := FromOwner(owner, photo, "image/png"); err == nil {
		t.Fatal("photo one byte above limit accepted")
	}
}

func ownerWithoutPhoto() resume.Resume {
	name, lng := "Ada", "en"
	return resume.Resume{ID: uuid.MustParse(testResumeID), Revision: 1, Lng: &lng, Doc: schema.Resume{
		SchemaVersion:   schema.CurrentVersion,
		PersonalDetails: schema.PersonalDetails{FullName: &name},
		Content:         map[string]schema.Section{},
	}}
}

func mutateOwner(source resume.Resume, mutate func(*resume.Resume)) resume.Resume {
	mutate(&source)
	return source
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	if err := jpeg.Encode(&out, img, nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func jpegWithDimensions(t *testing.T, width, height uint16) []byte {
	t.Helper()
	encoded := testJPEG(t)
	for index := 0; index+8 < len(encoded); index++ {
		if encoded[index] == 0xff && (encoded[index+1] == 0xc0 || encoded[index+1] == 0xc2) {
			binary.BigEndian.PutUint16(encoded[index+5:index+7], height)
			binary.BigEndian.PutUint16(encoded[index+7:index+9], width)
			return encoded
		}
	}
	t.Fatal("JPEG fixture has no start-of-frame marker")
	return nil
}

func pngWithDimensions(t *testing.T, width, height uint32) []byte {
	t.Helper()
	encoded := testPNG(t)
	binary.BigEndian.PutUint32(encoded[16:20], width)
	binary.BigEndian.PutUint32(encoded[20:24], height)
	binary.BigEndian.PutUint32(encoded[29:33], crc32.ChecksumIEEE(encoded[12:29]))
	return encoded
}

func boolPointer(value bool) *bool       { return &value }
func stringPointer(value string) *string { return &value }
