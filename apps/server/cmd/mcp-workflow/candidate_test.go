package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func fixtureCanonical(t *testing.T) []byte {
	t.Helper()
	_, canonical, _, err := canonicalDocument(fixtureDocument(t, true))
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func candidateFrom(t *testing.T, source []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(source, &document); err != nil {
		t.Fatal(err)
	}
	mutate(document)
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func writeReview(t *testing.T, run privateArtifacts, source, candidate []byte, review candidateReview) {
	t.Helper()
	encoded, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{sourceName: source, candidateName: candidate, candidateReviewName: encoded} {
		if writeErr := run.write(name, data); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
}

func TestCandidateRequiresExactDigestBoundReview(t *testing.T) {
	source := fixtureCanonical(t)
	candidate := candidateFrom(t, source, translateFixture)
	snapshot := sourceSnapshot{Canonical: source}
	for name, review := range map[string]candidateReview{
		"valid":            {digest(source), digest(candidate), true},
		"source mismatch":  {digest([]byte("other")), digest(candidate), true},
		"candidate stale":  {digest(source), digest(source), true},
		"facts false":      {digest(source), digest(candidate), false},
		"uppercase digest": {strings.ToUpper(digest(source)), digest(candidate), true},
	} {
		t.Run(name, func(t *testing.T) {
			run := testArtifacts(t, runScope)
			writeReview(t, run, source, candidate, review)
			payload, err := loadReviewedCandidate(run, snapshot)
			if (err == nil) != (name == "valid") {
				t.Fatalf("err = %v", err)
			}
			if err == nil {
				if _, _, photo, decodeErr := canonicalDocument(payload); decodeErr != nil || photo != nil {
					t.Fatal("payload is not a canonical photo-free document")
				}
			}
		})
	}
}

func TestCandidateRejectsExtraReviewFieldsAndReplacedSource(t *testing.T) {
	source := fixtureCanonical(t)
	candidate := candidateFrom(t, source, translateFixture)
	run := testArtifacts(t, runScope)
	writeReview(t, run, source, candidate, candidateReview{digest(source), digest(candidate), true})
	extra := []byte(`{"source_digest":"` + digest(source) + `","candidate_digest":"` + digest(candidate) + `","facts_preserved":true,"note":"x"}`)
	if err := run.write(candidateReviewName, extra); err != nil {
		t.Fatal(err)
	}
	if _, err := loadReviewedCandidate(run, sourceSnapshot{Canonical: source}); !errors.Is(err, errCandidate) {
		t.Fatalf("extra review field err = %v", err)
	}
	if _, err := loadReviewedCandidate(testArtifacts(t, runScope), sourceSnapshot{Canonical: source}); !errors.Is(err, errCandidate) {
		t.Fatalf("missing handoff err = %v", err)
	}
}

func TestCandidateRejectsPhotoUnknownFieldsTrailingDataAndStoreInvalid(t *testing.T) {
	source := fixtureCanonical(t)
	for name, candidate := range map[string][]byte{
		"server photo": candidateFrom(t, source, func(d map[string]any) {
			asMap(d["personalDetails"])["photo"] = map[string]any{"key": "resumes/x/photo.jpg"}
		}),
		"unknown field": candidateFrom(t, source, func(d map[string]any) { d["extension"] = map[string]any{"key": "value"} }),
		"trailing data": append(append([]byte(nil), source...), []byte(` {}`)...),
		"layout orphan": candidateFrom(t, source, func(d map[string]any) { delete(asMap(d["content"]), "skill") }),
		"oversized html": candidateFrom(t, source, func(d map[string]any) {
			entryAt(asMap(asMap(d["content"])["work"]), 0)["description"] = "<p>" + strings.Repeat("a", 20_000) + "</p>"
		}),
		"schema mismatch": candidateFrom(t, source, func(d map[string]any) { d["schemaVersion"] = 3 }),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateCandidate(source, candidate); !errors.Is(err, errCandidate) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestPreservationPermitsOnlyDesignTranslationPaths(t *testing.T) {
	source := fixtureCanonical(t)
	candidate := candidateFrom(t, source, func(d map[string]any) {
		translateFixture(d)
		details := asMap(d["personalDetails"])
		listAt(details["details"], 3)["label"] = "Hồ sơ LinkedIn"
		content := asMap(d["content"])
		asMap(content["skill"])["displayName"] = "Kỹ năng"
		entryAt(asMap(content["skill"]), 0)["infoHtml"] = "<p>Ngôn ngữ backend chính từ năm 2015.</p>"
		entryAt(asMap(content["project"]), 0)["description"] = "<p>Trình thông dịch cho thuật toán trong Note G.</p>"
		entryAt(asMap(content["a6a0a5fa-7fe4-4d52-be40-0da2db95de12"]), 0)["subtitle"] = "Hiệp hội Máy tính Anh"
	})
	payload, err := validateCandidate(source, candidate)
	if err != nil {
		t.Fatalf("rejected a design-permitted translation: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("empty payload")
	}
}

func TestPreservationRejectsProtectedChanges(t *testing.T) {
	source := fixtureCanonical(t)
	work := func(d map[string]any) map[string]any { return entryAt(asMap(asMap(d["content"])["work"]), 0) }
	for name, mutate := range map[string]func(map[string]any){
		"full name":     func(d map[string]any) { asMap(d["personalDetails"])["fullName"] = "Ada Byron" },
		"contact value": func(d map[string]any) { listAt(asMap(d["personalDetails"])["details"], 0)["value"] = "x@example.com" },
		"contact type":  func(d map[string]any) { listAt(asMap(d["personalDetails"])["details"], 1)["type"] = "custom" },
		"employer":      func(d map[string]any) { work(d)["employer"] = "Công ty Máy Phân tích" },
		"employer link": func(d map[string]any) { work(d)["employerLink"] = "https://other.example.com" },
		"entry ID":      func(d map[string]any) { work(d)["id"] = "00000000-0000-4000-8000-000000000001" },
		"date":          func(d map[string]any) { asMap(asMap(work(d)["dates"])["start"])["y"] = 2021 },
		"hidden flag":   func(d map[string]any) { work(d)["isHidden"] = true },
		"city":          func(d map[string]any) { work(d)["city"] = "Hà Nội" },
		"degree": func(d map[string]any) {
			entryAt(asMap(asMap(d["content"])["education"]), 0)["degree"] = "Cử nhân Toán"
		},
		"skill name":        func(d map[string]any) { entryAt(asMap(asMap(d["content"])["skill"]), 0)["name"] = "Ngôn ngữ Go" },
		"language name":     func(d map[string]any) { entryAt(asMap(asMap(d["content"])["language"]), 0)["name"] = "Tiếng Anh" },
		"certificate title": func(d map[string]any) { entryAt(asMap(asMap(d["content"])["certificate"]), 0)["title"] = "Kỹ sư" },
		"project title": func(d map[string]any) {
			entryAt(asMap(asMap(d["content"])["project"]), 0)["title"] = "Trình thông dịch"
		},
		"section order": func(d map[string]any) {
			asMap(asMap(asMap(d["customization"])["layout"])["sections"])["main"] = []any{"work", "profile", "education"}
		},
		"color text": func(d map[string]any) { asMap(asMap(d["customization"])["colors"])["text"] = "#000000" },
		"icon key":   func(d map[string]any) { asMap(asMap(d["content"])["work"])["iconKey"] = "user" },
		"rich text tag": func(d map[string]any) {
			work(d)["description"] = "<p><strong>Leading</strong> design of the difference engine successor.</p>"
		},
		"rich text number": func(d map[string]any) {
			work(d)["description"] = "<p>Dẫn dắt thiết kế 2 thế hệ máy sai phân.</p>"
		},
		"certificate number": func(d map[string]any) {
			entryAt(asMap(asMap(d["content"])["certificate"]), 0)["description"] = "<p>Mã chứng chỉ CAE-1843-002.</p>"
		},
		"plain html":    func(d map[string]any) { asMap(d["personalDetails"])["headline"] = "<b>Kỹ sư</b>" },
		"emptied text":  func(d map[string]any) { asMap(d["personalDetails"])["headline"] = "" },
		"removed entry": func(d map[string]any) { asMap(asMap(d["content"])["work"])["entries"] = []any{} },
		"country":       func(d map[string]any) { work(d)["country"] = "Việt Nam" },
		"added field":   func(d map[string]any) { work(d)["summary"] = "Tóm tắt" },
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePreservedDocument(source, candidateFrom(t, source, mutate)); !errors.Is(err, errCandidate) {
				t.Fatalf("accepted protected change: %v", err)
			}
		})
	}
}

func TestPreservationRichTextKeepsLinkTargetsAndMarks(t *testing.T) {
	source := `<p>Built <strong>systems</strong> for <a href="https://example.com/work" target="_blank">customers</a> in 2020.</p>`
	if !sameRichText(source, `<p>Xây dựng <strong>hệ thống</strong> cho <a href="https://example.com/work" target="_blank">khách hàng</a> năm 2020.</p>`) {
		t.Fatal("rejected a text-node translation")
	}
	for name, candidate := range map[string]string{
		"href":        `<p>Xây dựng <strong>hệ thống</strong> cho <a href="https://other.example" target="_blank">khách hàng</a> năm 2020.</p>`,
		"mark":        `<p>Xây dựng <em>hệ thống</em> cho <a href="https://example.com/work" target="_blank">khách hàng</a> năm 2020.</p>`,
		"lost link":   `<p>Xây dựng <strong>hệ thống</strong> cho khách hàng năm 2020.</p>`,
		"year":        `<p>Xây dựng <strong>hệ thống</strong> cho <a href="https://example.com/work" target="_blank">khách hàng</a> năm 2021.</p>`,
		"extra claim": `<p>Xây dựng <strong>hệ thống</strong> cho <a href="https://example.com/work" target="_blank">khách hàng</a> năm 2020 cho 5 đội.</p>`,
		"attribute":   `<p>Xây dựng <strong>hệ thống</strong> cho <a href="https://example.com/work">khách hàng</a> năm 2020.</p>`,
	} {
		if sameRichText(source, candidate) {
			t.Fatalf("%s change accepted", name)
		}
	}
}

// listAt returns the object at index of a JSON array, or an empty object.
func listAt(value any, index int) map[string]any {
	items, ok := value.([]any)
	if !ok || index >= len(items) {
		return map[string]any{}
	}
	return asMap(items[index])
}
