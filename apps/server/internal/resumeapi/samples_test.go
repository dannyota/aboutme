package resumeapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

// The web app seeds a new resume from packages/schema/samples. Each sample
// must pass the server's complete create path (wire decode, schema, store
// validation, bounds, and the rich-text sanitizer) and be stored unchanged,
// so a seeded resume never differs from the preview the owner chose.
func TestSampleDocumentsCreateUnchanged(t *testing.T) {
	all, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "packages", "schema", "samples", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Files starting with "_" fill the gallery and are never create seeds.
	paths := make([]string, 0, len(all))
	for _, path := range all {
		if !strings.HasPrefix(filepath.Base(path), "_") {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		t.Fatal("no sample documents found")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			// A fresh owner per sample stays under the per-user resume cap.
			h := newResumeAPITestHarness(t)
			sample, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			body, marshalErr := json.Marshal(map[string]any{
				"title": "Sample " + filepath.Base(path), "document": json.RawMessage(sample),
			})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			response := resumeRequest(t, h, http.MethodPost, apiResumePath, string(body), 0, uuid.New(),
				wireVersionString(docmigrate.CurrentVersion))
			if response.status != http.StatusCreated {
				t.Fatalf("create = %d %s", response.status, response.body)
			}
			created := decodeResumeResource(t, response)
			stored, getErr := h.resumes.Get(h.ctx, h.userID, created.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			canonical, assembleErr := resume.AssembleCanonical(stored.Doc)
			if assembleErr != nil {
				t.Fatal(assembleErr)
			}
			for name, got := range map[string][]byte{"stored": canonical, "response": created.Document} {
				if differences := jsonDifferences(t, got, sample); len(differences) != 0 {
					t.Fatalf("%s document differs from the sample at %v", name, differences)
				}
			}
		})
	}
}

// jsonDifferences lists the JSON paths where got and want differ.
func jsonDifferences(t *testing.T, got, want []byte) []string {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatal(err)
	}
	var differences []string
	collectJSONDifferences("$", gotValue, wantValue, &differences)
	return differences
}

func collectJSONDifferences(path string, got, want any, differences *[]string) {
	gotObject, gotIsObject := got.(map[string]any)
	wantObject, wantIsObject := want.(map[string]any)
	if gotIsObject && wantIsObject {
		for key := range wantObject {
			collectJSONDifferences(path+"."+key, gotObject[key], wantObject[key], differences)
		}
		for key := range gotObject {
			if _, present := wantObject[key]; !present {
				*differences = append(*differences, fmt.Sprintf("%s.%s (added)", path, key))
			}
		}
		return
	}
	gotArray, gotIsArray := got.([]any)
	wantArray, wantIsArray := want.([]any)
	if gotIsArray && wantIsArray && len(gotArray) == len(wantArray) {
		for index := range wantArray {
			collectJSONDifferences(fmt.Sprintf("%s[%d]", path, index), gotArray[index], wantArray[index], differences)
		}
		return
	}
	if !reflect.DeepEqual(got, want) {
		*differences = append(*differences, fmt.Sprintf("%s: got %v, want %v", path, got, want))
	}
}
