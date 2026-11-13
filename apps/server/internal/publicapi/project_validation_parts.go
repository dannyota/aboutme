package publicapi

import (
	"reflect"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"danny.vn/aboutme/apps/server/internal/directrender"
)

// projectBackLabel is the accessible name of the bar's link back to the
// resume (docs/design/ui/project-pages.md "Shared frame").
func projectBackLabel(resume directrender.ProjectPageResume) string {
	if resume.UILng == "vi" {
		return "Về CV của " + resume.OwnerName
	}
	return "Back to " + resume.OwnerName + "'s resume"
}

// visitAnchor classifies one anchor outside the description region: the
// skip link, the credit, the back link, a repository, demo or entry link,
// or an internal link to the resume, the list or a requested project.
func (v *projectValidator) visitAnchor(node *html.Node) {
	href := attribute(node, "href")
	if attributeCount(node, "href") != 1 {
		v.reject("anchor_href")
		return
	}
	class := attribute(node, "class")
	switch {
	case href == "#public-project":
		v.skipLinks++
		if v.skipLinks > 1 || !onlyAttributes(node, "href", "class") {
			v.reject("skip_link")
		}
	case relHasToken(class, creditClass):
		v.credits++
		if v.credits > 1 || !isPublicCredit(node, v.origin.Resolve("/"), creditText(v.want.resume.Lng)) {
			v.reject("credit_link")
		}
	case relHasToken(class, "public-back"):
		v.backs++
		if v.backs > 1 || href != v.want.resume.URL || attribute(node, "aria-label") != projectBackLabel(v.want.resume) ||
			!onlyAttributes(node, "href", "class", "aria-label") {
			v.reject("back_link")
		}
	case relHasToken(class, "project-external"):
		v.externalSeen[href]++
		switch {
		case v.externalSeen[href] > v.want.external[href] || !onlyAttributes(node, "href", "class", "rel"):
			v.reject("external_link")
		case attribute(node, "rel") != projectExternalRel:
			v.reject("external_rel")
		}
	default:
		if !v.want.internal[href] {
			v.reject("anchor_unexpected")
			return
		}
		if !onlyAttributes(node, "href", "class") {
			v.reject("anchor_attributes")
			return
		}
		v.internalSeen[href]++
	}
}

// visitDescription requires the one description region of a page with a
// description, whose content is the request's description as a parsed tree
// with comments ignored.
func (v *projectValidator) visitDescription(node *html.Node) {
	v.descriptions++
	if v.want.description == "" || v.descriptions > 1 {
		v.reject("description_region")
		return
	}
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	want, err := html.ParseFragment(strings.NewReader(v.want.description), context)
	if err != nil {
		v.reject("description_content")
		return
	}
	var got []*html.Node
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		got = append(got, child)
	}
	if !reflect.DeepEqual(comparableTree(got), comparableTree(want)) {
		v.reject("description_content")
	}
}

// treeNode is one node of a comparable tree: comments dropped, adjacent
// text merged, and attributes in a fixed order.
type treeNode struct {
	kind      html.NodeType
	namespace string
	data      string
	attrs     []html.Attribute
	children  []treeNode
}

func comparableTree(nodes []*html.Node) []treeNode {
	var out []treeNode
	for _, node := range nodes {
		switch node.Type {
		case html.CommentNode:
			continue
		case html.TextNode:
			if last := len(out) - 1; last >= 0 && out[last].kind == html.TextNode {
				out[last].data += node.Data
				continue
			}
			out = append(out, treeNode{kind: html.TextNode, data: node.Data})
		default:
			attrs := append([]html.Attribute(nil), node.Attr...)
			slices.SortFunc(attrs, func(a, b html.Attribute) int {
				return strings.Compare(a.Namespace+" "+a.Key, b.Namespace+" "+b.Key)
			})
			var children []*html.Node
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				children = append(children, child)
			}
			out = append(out, treeNode{kind: node.Type, namespace: node.Namespace, data: node.Data, attrs: attrs, children: comparableTree(children)})
		}
	}
	return out
}
