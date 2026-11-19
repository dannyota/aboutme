package publicapi

import "golang.org/x/net/html"

const projectEvidenceClass = "project-evidence"

func (v *projectValidator) visitEvidence(node *html.Node) {
	index := v.evidenceSeen
	v.evidenceSeen++
	if index >= len(v.want.evidence) {
		v.reject("evidence_link")
		return
	}
	item := v.want.evidence[index]
	switch {
	case attributeCount(node, "href") != 1 || attribute(node, "href") != item.URL:
		v.reject("evidence_link")
	case attribute(node, "class") != projectEvidenceClass || !onlyAttributes(node, "class", "href", "rel"):
		v.reject("evidence_attributes")
	case attribute(node, "rel") != projectExternalRel:
		v.reject("evidence_rel")
	case textNode(node) != item.Title:
		v.reject("evidence_text")
	}
}
