package publicapi

import (
	"strconv"

	"golang.org/x/net/html"
)

const folioClass = "resume-folio"

type folioCheck struct {
	name  string
	named bool
	pages []string
}

func newFolioCheck(fullName string) *folioCheck {
	return &folioCheck{name: fullName, named: fullName != ""}
}

func (c *folioCheck) visit(node *html.Node) string {
	if !relHasToken(attribute(node, "class"), folioClass) {
		return ""
	}
	if attributeCount(node, "data-folio-page") != 1 || attributeCount(node, "data-folio-name") != boolToInt(c.named) ||
		(c.named && attribute(node, "data-folio-name") != c.name) {
		return "folio"
	}
	c.pages = append(c.pages, attribute(node, "data-folio-page"))
	return ""
}

func (c *folioCheck) finish() string {
	if len(c.pages) == 0 {
		return "folio"
	}
	total := " / " + strconv.Itoa(len(c.pages))
	for index, page := range c.pages {
		if page != strconv.Itoa(index+1)+total {
			return "folio"
		}
	}
	return ""
}
