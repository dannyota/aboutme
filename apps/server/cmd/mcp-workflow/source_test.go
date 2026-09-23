package main

import (
	"bytes"
	"context"
	"testing"
)

func TestSourceSelectionEnforcesAccountShape(t *testing.T) {
	english := fakeSummary(fakeSourceID, "en", "English", "1")
	vietnamese := fakeSummary(fakeOtherID, "vi", "Vietnamese", "1")
	third := fakeSummary("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61", "fr", "French", "1")
	secondEnglish := fakeSummary(fakeOtherID, "en", "English 2", "1")
	duplicate := english
	duplicate.Lng = "vi"
	for name, listing := range map[string][]resumeSummary{
		"empty":             nil,
		"no English":        {vietnamese},
		"two English":       {english, secondEnglish},
		"over two total":    {english, vietnamese, third},
		"duplicate ID":      {english, duplicate},
		"English variant":   {fakeSummary(fakeSourceID, "en-US", "English", "1")},
		"uppercase English": {fakeSummary(fakeSourceID, "EN", "English", "1")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := selectEnglishSource(listing); err == nil {
				t.Fatal("selected a source from an invalid account")
			}
		})
	}
	for _, listing := range [][]resumeSummary{{english}, {vietnamese, english}} {
		if got, err := selectEnglishSource(listing); err != nil || got != sourceHandle(fakeSourceID) {
			t.Fatalf("source = %q, %v", got, err)
		}
	}
}

func TestSourceSummaryValidationRejectsMalformedValues(t *testing.T) {
	valid := fakeSummary(fakeSourceID, "en", "English", "7")
	for name, mutate := range map[string]func(*resumeSummary){
		"nil UUID":       func(s *resumeSummary) { s.ID = "00000000-0000-0000-0000-000000000000" },
		"uppercase UUID": func(s *resumeSummary) { s.ID = "01890F47-7E8A-7B2A-8D70-9A1F2C3D4E5F" },
		"leading zero":   func(s *resumeSummary) { s.Revision = "07" },
		"empty title":    func(s *resumeSummary) { s.Title = "" },
		"bad language":   func(s *resumeSummary) { s.Lng = "en us" },
		"time reversal":  func(s *resumeSummary) { s.UpdatedAt = s.CreatedAt.Add(-1) },
		"zero schema":    func(s *resumeSummary) { s.SchemaVersion = 0 },
	} {
		item := valid
		mutate(&item)
		if validResumeSummary(item) {
			t.Fatalf("%s accepted", name)
		}
	}
	if !validResumeSummary(valid) {
		t.Fatal("valid summary rejected")
	}
}

func TestSourceCanonicalDocumentStripsServerPhoto(t *testing.T) {
	withPhoto := fixtureDocument(t, true)
	withoutPhoto := fixtureDocument(t, false)
	_, canonical, photo, err := canonicalDocument(withPhoto)
	if err != nil || photo == nil || photo.Crop == nil {
		t.Fatalf("photo = %+v, %v", photo, err)
	}
	_, plain, none, err := canonicalDocument(withoutPhoto)
	if err != nil || none != nil || !bytes.Equal(canonical, plain) || bytes.Contains(canonical, []byte(`"photo"`)) {
		t.Fatal("canonical source still names the server-owned photo")
	}
	for _, invalid := range [][]byte{[]byte(`{"schemaVersion":4}`), append(append([]byte(nil), withoutPhoto...), []byte(` {}`)...), bytes.Replace(withoutPhoto, []byte(`"content":`), []byte(`"unknown":1,"content":`), 1)} {
		if _, _, _, invalidErr := canonicalDocument(invalid); invalidErr == nil {
			t.Fatal("accepted an invalid document")
		}
	}
}

func TestSourceSnapshotDetectsDocumentRevisionPhotoAndPublicationChanges(t *testing.T) {
	fake := newFakeMCP(t, true)
	guard := newToolGuard(fake, false)
	first, err := readSource(context.Background(), guard, fakeSourceID)
	if err != nil || first.Photo == nil || first.Crop == nil {
		t.Fatalf("snapshot = %+v, %v", first, err)
	}
	slug := "ada"
	otherSlug := "ada"
	for name, mutate := range map[string]func(*sourceSnapshot){
		"revision": func(s *sourceSnapshot) { s.State.Revision = "4" },
		"live":     func(s *sourceSnapshot) { s.State.Live = true },
		"slug":     func(s *sourceSnapshot) { s.State.Slug = &slug },
		"document": func(s *sourceSnapshot) { s.State.Document = append([]byte(" "), s.State.Document...) },
		"photo bytes": func(s *sourceSnapshot) {
			s.Photo = &photoBytes{ContentType: s.Photo.ContentType, Data: append([]byte(nil), s.Photo.Data[:len(s.Photo.Data)-1]...)}
		},
		"photo crop": func(s *sourceSnapshot) { s.Crop = &photoCrop{X: 0, Y: 0, Width: 1, Height: 1} },
		"photo gone": func(s *sourceSnapshot) { s.Photo = nil },
	} {
		changed := first
		mutate(&changed)
		if sameSnapshot(first, changed) {
			t.Fatalf("%s change not detected", name)
		}
	}
	withSlug, sameSlug := first, first
	withSlug.State.Slug, sameSlug.State.Slug = &slug, &otherSlug
	if !sameSnapshot(withSlug, sameSlug) {
		t.Fatal("equal slugs at different addresses compared unequal")
	}
}

func TestSourceArtifactHoldsCanonicalDocumentOnly(t *testing.T) {
	run := testArtifacts(t, runScope)
	fake := newFakeMCP(t, true)
	source, err := readSource(context.Background(), newToolGuard(fake, false), fakeSourceID)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := writeSourceArtifact(run, source)
	if err != nil {
		t.Fatal(err)
	}
	written, err := run.read(sourceName)
	if err != nil || digest(written) != sum || !bytes.Equal(written, source.Canonical) {
		t.Fatal("source.json is not the canonical document")
	}
	for _, forbidden := range [][]byte{[]byte(fakeSourceID), []byte(`"photo"`), []byte(`"revision"`)} {
		if bytes.Contains(written, forbidden) {
			t.Fatalf("source.json contains %s", forbidden)
		}
	}
}
