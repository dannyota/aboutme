package printsnapshot

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"
)

// printCorpusPath holds the print snapshot envelopes of the entry point
// corpus: the exact Marshal bytes for the linked resume (version 2) and for
// the resume with no available project (version 1). The publicapi test
// TestResumeLinksRenderCorpusIsCurrent writes it, with -update, from the same
// snapshots as the resume page corpus, so the two always describe the same
// resumes.
const printCorpusPath = "testdata/print-envelopes.json"

// TestPrintEnvelopeCorpusMatchesEnvelopeType proves each corpus envelope
// decodes into Envelope's fields with no unknown field and encodes back to
// the same bytes, and keeps the version rule Marshal enforces. The document
// is held raw: the public resume types encode but have no decoder.
func TestPrintEnvelopeCorpusMatchesEnvelopeType(t *testing.T) {
	raw, err := os.ReadFile(printCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Name     string          `json:"name"`
			Envelope json.RawMessage `json:"envelope"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	var versions []int
	for _, entry := range corpus.Cases {
		t.Run(entry.Name, func(t *testing.T) {
			shape := rawDocumentShape()
			decoder := json.NewDecoder(bytes.NewReader(entry.Envelope))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(shape.Addr().Interface()); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			encoded, err := json.Marshal(shape.Interface())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, entry.Envelope) {
				t.Fatalf("envelope does not round-trip:\n got %s\nwant %s", encoded, entry.Envelope)
			}

			var envelope struct {
				Version      int               `json:"version"`
				Document     json.RawMessage   `json:"document"`
				ProjectLinks map[string]string `json:"projectLinks"`
			}
			if err := json.Unmarshal(entry.Envelope, &envelope); err != nil {
				t.Fatal(err)
			}
			versions = append(versions, envelope.Version)
			switch envelope.Version {
			case 1:
				if envelope.ProjectLinks != nil {
					t.Fatalf("version 1 carries projectLinks %v", envelope.ProjectLinks)
				}
			case 2:
				visible := visibleEntriesOf(t, envelope.Document)
				if len(envelope.ProjectLinks) == 0 {
					t.Fatal("version 2 has no projectLinks")
				}
				for entryID, pageURL := range envelope.ProjectLinks {
					if !slices.Contains(visible, entryID) || !pageAddress(pageURL) {
						t.Fatalf("link %s -> %s is not a visible project entry's page address", entryID, pageURL)
					}
				}
			default:
				t.Fatalf("version = %d", envelope.Version)
			}
		})
	}
	slices.Sort(versions)
	if !slices.Equal(versions, []int{1, 2}) {
		t.Fatalf("corpus versions = %v, want one version-1 and one version-2 envelope", versions)
	}
}

// rawDocumentShape is a new value of a struct with Envelope's fields and
// tags, except that Document holds its JSON undecoded.
func rawDocumentShape() reflect.Value {
	typ := reflect.TypeFor[Envelope]()
	fields := make([]reflect.StructField, typ.NumField())
	for index := range fields {
		field := typ.Field(index)
		fields[index] = reflect.StructField{Name: field.Name, Type: field.Type, Tag: field.Tag}
		if field.Name == "Document" {
			fields[index].Type = reflect.TypeFor[json.RawMessage]()
		}
	}
	return reflect.New(reflect.StructOf(fields)).Elem()
}

// visibleEntriesOf lists the entry ids of the project sections a raw
// document's layout shows.
func visibleEntriesOf(t *testing.T, document []byte) []string {
	t.Helper()
	var shown struct {
		Content map[string]struct {
			SectionType string `json:"sectionType"`
			Entries     []struct {
				ID string `json:"id"`
			} `json:"entries"`
		} `json:"content"`
		Customization struct {
			Layout struct {
				Sections struct {
					Main    []string `json:"main"`
					Sidebar []string `json:"sidebar"`
				} `json:"sections"`
			} `json:"layout"`
		} `json:"customization"`
	}
	if err := json.Unmarshal(document, &shown); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, key := range append(shown.Customization.Layout.Sections.Main, shown.Customization.Layout.Sections.Sidebar...) {
		if section := shown.Content[key]; section.SectionType == "project" {
			for _, entry := range section.Entries {
				ids = append(ids, entry.ID)
			}
		}
	}
	return ids
}
