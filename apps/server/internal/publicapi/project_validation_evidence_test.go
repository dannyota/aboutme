package publicapi

import (
	"net/http"
	"strings"
	"testing"
)

const evidenceRel = `rel="noopener noreferrer nofollow"`

func evidenceAnchor(url, title string) string {
	return `<a class="project-evidence" href="` + url + `" ` + evidenceRel + `>` + title + `</a>`
}

func TestProjectHTMLRejection_AcceptsEvidenceInBothLanguages(t *testing.T) {
	origin := mustPublicOrigin(t)
	corpus := readProjectCorpus(t)
	for _, name := range []string{"project-vi-evidence", "project-en-evidence-only"} {
		tc := corpus[name]
		request := projectCorpusPage(t, tc)
		if len(request.Evidence) == 0 || strings.Count(tc.HTML, `class="project-evidence"`) != len(request.Evidence) {
			t.Fatalf("%s: corpus carries %d items and %d anchors", name, len(request.Evidence), strings.Count(tc.HTML, `class="project-evidence"`))
		}
		if rule := projectHTMLRejection([]byte(tc.HTML), request, origin); rule != "" {
			t.Fatalf("%s: rejection = %q, want the corpus HTML accepted", name, rule)
		}
	}
}

func TestProjectHTMLRejection_EvidenceRules(t *testing.T) {
	origin := mustPublicOrigin(t)
	tc := readProjectCorpus(t)["project-vi-evidence"]
	request := projectCorpusPage(t, tc)
	first := evidenceAnchor(corpusEvidenceRepository, "ada/may-phan-tich")
	second := evidenceAnchor(corpusEvidencePull, "Thêm ghi chú G")
	open := `<a class="project-evidence" href="` + corpusEvidenceRepository + `" ` + evidenceRel + `>`
	for _, m := range []projectMutation{
		{"missing", first, "ada/may-phan-tich", "evidence_link"},
		{"duplicated", "</main>", second + "</main>", "evidence_link"},
		{"swapped", first + "</p>", evidenceAnchor(corpusEvidencePull, "ada/may-phan-tich") + "</p>", "evidence_link"},
		{"changed href", first, evidenceAnchor("https://github.com/ada/other", "ada/may-phan-tich"), "evidence_link"},
		{"evidence class on the skip link", `<a href="#public-project">`, `<a class="project-evidence" href="#public-project">`, "evidence_link"},
		{"target", open, strings.Replace(open, "<a ", `<a target="_blank" `, 1), "evidence_attributes"},
		{"aria-label", open, strings.Replace(open, "<a ", `<a aria-label="x" `, 1), "evidence_attributes"},
		{"lang", open, strings.Replace(open, "<a ", `<a lang="en" `, 1), "evidence_attributes"},
		{"data attribute", open, strings.Replace(open, "<a ", `<a data-x="1" `, 1), "evidence_attributes"},
		{"second class token", open, strings.Replace(open, `class="project-evidence"`, `class="project-evidence x"`, 1), "evidence_attributes"},
		{"missing rel", open, strings.Replace(open, " "+evidenceRel, "", 1), "evidence_rel"},
		{"weaker rel", open, strings.Replace(open, evidenceRel, `rel="noopener"`, 1), "evidence_rel"},
		{"reordered rel", open, strings.Replace(open, evidenceRel, `rel="nofollow noopener noreferrer"`, 1), "evidence_rel"},
		{"changed text", first, evidenceAnchor(corpusEvidenceRepository, "ada/other"), "evidence_text"},
		{"padded text", first, evidenceAnchor(corpusEvidenceRepository, " ada/may-phan-tich "), "evidence_text"},
		{"svg title in the link", first, evidenceAnchor(corpusEvidenceRepository, `ada/may-phan-tich<svg aria-hidden="true"><title>x</title></svg>`), "evidence_text"},
		{"unclassed link to an evidence url", "</main>", `<a href="` + corpusEvidenceRepository + `">x</a></main>`, "anchor_unexpected"},
		{"external button to an evidence url", "</main>", `<a class="project-external" href="` + corpusEvidenceRepository + `" ` + evidenceRel + `>x</a></main>`, "external_link"},
		{"evidence link inside the description", "<p>Ghi chú về máy tính cơ học.</p>", "<p>Ghi chú về máy tính cơ học." + first + "</p>", "description_content"},
	} {
		t.Run(m.name, func(t *testing.T) {
			source := m.apply(tc.HTML)
			if source == tc.HTML {
				t.Fatal("the mutation did not change the corpus page")
			}
			if got := projectHTMLRejection([]byte(source), request, origin); got != m.rule {
				t.Fatalf("rejection = %q, want %q", got, m.rule)
			}
		})
	}
}

func TestProjectHTMLRejection_EvidenceAnchorsWithoutItems(t *testing.T) {
	origin := mustPublicOrigin(t)
	corpus := readProjectCorpus(t)
	anchor := evidenceAnchor(corpusEvidenceRepository, "ada/may-phan-tich")
	page := corpus["project-en-name-only"]
	if got := projectHTMLRejection([]byte(replaceOnce(page.HTML, "</main>", anchor+"</main>")), projectCorpusPage(t, page), origin); got != "evidence_link" {
		t.Fatalf("page without items: rejection = %q, want evidence_link", got)
	}
	list := corpus["list-vi-two-projects"]
	if got := projectListHTMLRejection([]byte(replaceOnce(list.HTML, "</main>", anchor+"</main>")), projectCorpusList(t, list), origin); got != "evidence_link" {
		t.Fatalf("list page: rejection = %q, want evidence_link", got)
	}
}

func TestProjectEvidencePages_DroppedAnchorIs503WithAClosedLogLine(t *testing.T) {
	readStore := projectEvidenceFixture(t)
	reader, _ := projectReaderWithEvidence(t, readStore, true)
	renderer, client := newFakeProjectRenderer(t)
	renderer.mutate = func(page string) string {
		return strings.Replace(page, evidenceAnchor(corpusEvidencePull, "Thêm ghi chú G"), "Thêm ghi chú G", 1)
	}
	handlers := newProjectHandlers(t, reader, client)
	response := handlers.serve(http.MethodGet, "/ada/projects/may-phan-tich")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("response = %d, want 503", response.Code)
	}
	logs := handlers.logs.String()
	if strings.Count(logs, "\n") != 1 || !strings.Contains(logs, "reason=html_rejected rule=evidence_link") {
		t.Fatalf("log = %q, want one closed evidence_link line", logs)
	}
	for _, private := range []string{"Thêm ghi chú G", "ghi chú", "github.com", "may-phan-tich"} {
		if strings.Contains(logs, private) {
			t.Fatalf("log %q carries %q", logs, private)
		}
	}
}
