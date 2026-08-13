package resumeapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

type richTextRouteCase struct {
	sectionKey string
	field      string
	entryID    string
}

func TestHostileCorpusThroughHTTP(t *testing.T) {
	cases := richTextHTTPDocument(t)
	policy := newHTTPRichTextPredicate()

	for _, payload := range schema.HostileCorpus {
		t.Run(payload.ID, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			created, err := h.resumes.Create(h.ctx, h.userID, "hostile corpus", cases.document)
			if err != nil {
				t.Fatalf("create hostile-corpus fixture: %v", err)
			}
			revision := created.Revision
			for _, routeCase := range cases.routes {
				t.Run(routeCase.sectionKey+"."+routeCase.field, func(t *testing.T) {
					body, marshalErr := json.Marshal(map[string]any{"entry": map[string]any{
						"id": routeCase.entryID, routeCase.field: payload.Payload,
					}})
					if marshalErr != nil {
						t.Fatalf("marshal hostile entry: %v", marshalErr)
					}
					response := resumeRequest(t, h, http.MethodPatch,
						apiResumePath+"/"+created.ID.String()+"/entries/"+routeCase.sectionKey,
						string(body), revision, uuid.New(), "2")
					if response.status != http.StatusOK {
						t.Fatalf("hostile route response = %d %s, want 200", response.status, response.body)
					}
					revision++
					resource := decodeResumeResource(t, response)
					var document schema.Resume
					if unmarshalErr := json.Unmarshal(resource.Document, &document); unmarshalErr != nil {
						t.Fatalf("decode sanitized document: %v", unmarshalErr)
					}
					output := richTextRouteValue(t, document.Content[routeCase.sectionKey], routeCase.field)
					if predicateErr := policy.check(output); predicateErr != nil {
						t.Fatalf("stored sanitizer output failed independent predicate: %v\ninput: %s\noutput: %s",
							predicateErr, payload.Payload, output)
					}
					stored, getErr := h.resumes.Get(h.ctx, h.userID, created.ID)
					if getErr != nil {
						t.Fatalf("reload sanitized document: %v", getErr)
					}
					storedOutput := richTextRouteValue(t, stored.Doc.Content[routeCase.sectionKey], routeCase.field)
					if storedOutput != output {
						t.Fatalf("persisted output = %q, mutation response = %q", storedOutput, output)
					}
					getResponse := resumeRequest(t, h, http.MethodGet,
						apiResumePath+"/"+created.ID.String(), "", 0, uuid.Nil, "2")
					if getResponse.status != http.StatusOK {
						t.Fatalf("GET sanitized document = %d %s, want 200", getResponse.status, getResponse.body)
					}
					getOutput := richTextRouteValue(t, responseResumeDocument(t, getResponse).Content[routeCase.sectionKey], routeCase.field)
					if getOutput != storedOutput {
						t.Fatalf("GET output = %q, persisted output = %q", getOutput, storedOutput)
					}
					if predicateErr := policy.check(getOutput); predicateErr != nil {
						t.Fatalf("GET sanitizer output failed independent predicate: %v", predicateErr)
					}
				})
			}
		})
	}
}

func TestPlainTextRoundTripsAsText(t *testing.T) {
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: "40000000-0000-4000-8000-000000000001"}}),
	}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}

	const title = `<script>literal title</script>`
	createBody, err := json.Marshal(map[string]any{"title": title, "document": doc})
	if err != nil {
		t.Fatalf("marshal plain-text create: %v", err)
	}
	createdResponse := resumeRequest(t, h, http.MethodPost, apiResumePath, string(createBody), 0, uuid.New(), "2")
	if createdResponse.status != http.StatusCreated {
		t.Fatalf("plain-text create = %d %s, want 201", createdResponse.status, createdResponse.body)
	}
	created := decodeResumeResource(t, createdResponse)
	if created.Title != title {
		t.Fatalf("title = %q, want literal %q", created.Title, title)
	}

	const displayName = `<img src=x onerror=alert(1)>`
	sectionBody := mustResumeTestJSON(t, map[string]any{"displayName": displayName})
	sectionResponse := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+created.ID.String()+"/sections/work", string(sectionBody), 1, uuid.New(), "2")
	if sectionResponse.status != http.StatusOK {
		t.Fatalf("plain-text section = %d %s, want 200", sectionResponse.status, sectionResponse.body)
	}
	sectionDoc := responseResumeDocument(t, sectionResponse)
	if got := sectionDoc.Content["work"].DisplayName; got == nil || *got != displayName {
		t.Fatalf("displayName = %v, want literal %q", got, displayName)
	}

	const jobTitle = `<svg onload=alert(1)>Engineer</svg>`
	entryBody := mustResumeTestJSON(t, map[string]any{"entry": map[string]any{
		"id": "40000000-0000-4000-8000-000000000001", "jobTitle": jobTitle,
	}})
	entryResponse := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+created.ID.String()+"/entries/work", string(entryBody), 2, uuid.New(), "2")
	if entryResponse.status != http.StatusOK {
		t.Fatalf("plain-text entry = %d %s, want 200", entryResponse.status, entryResponse.body)
	}
	entryDoc := responseResumeDocument(t, entryResponse)
	if got := entryDoc.Content["work"].WorkEntries[0].JobTitle; got == nil || *got != jobTitle {
		t.Fatalf("jobTitle = %v, want literal %q", got, jobTitle)
	}

	const fullName = `<b>Ada & Grace</b>`
	personalBody := mustResumeTestJSON(t, map[string]any{"fullName": fullName, "details": []any{}})
	personalResponse := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+created.ID.String()+"/personal-details", string(personalBody), 3, uuid.New(), "2")
	if personalResponse.status != http.StatusOK {
		t.Fatalf("plain-text personal details = %d %s, want 200", personalResponse.status, personalResponse.body)
	}
	personalDoc := responseResumeDocument(t, personalResponse)
	if got := personalDoc.PersonalDetails.FullName; got == nil || *got != fullName {
		t.Fatalf("fullName = %v, want literal %q", got, fullName)
	}
	stored, getErr := h.resumes.Get(h.ctx, h.userID, created.ID)
	if getErr != nil {
		t.Fatalf("reload plain-text document: %v", getErr)
	}
	assertPlainTextDocument(t, stored.Title, stored.Doc, title, displayName, jobTitle, fullName)
	getResponse := resumeRequest(t, h, http.MethodGet, apiResumePath+"/"+created.ID.String(), "", 0, uuid.Nil, "2")
	if getResponse.status != http.StatusOK {
		t.Fatalf("GET plain-text document = %d %s, want 200", getResponse.status, getResponse.body)
	}
	getResource := decodeResumeResource(t, getResponse)
	assertPlainTextDocument(t, getResource.Title, responseResumeDocument(t, getResponse), title, displayName, jobTitle, fullName)
}

func assertPlainTextDocument(t *testing.T, gotTitle string, document schema.Resume, title, displayName, jobTitle, fullName string) {
	t.Helper()
	if gotTitle != title {
		t.Fatalf("stored title = %q, want literal %q", gotTitle, title)
	}
	section := document.Content["work"]
	if section.DisplayName == nil || *section.DisplayName != displayName {
		t.Fatalf("stored displayName = %v, want literal %q", section.DisplayName, displayName)
	}
	if section.WorkEntries[0].JobTitle == nil || *section.WorkEntries[0].JobTitle != jobTitle {
		t.Fatalf("stored jobTitle = %v, want literal %q", section.WorkEntries[0].JobTitle, jobTitle)
	}
	if document.PersonalDetails.FullName == nil || *document.PersonalDetails.FullName != fullName {
		t.Fatalf("stored fullName = %v, want literal %q", document.PersonalDetails.FullName, fullName)
	}
}

type richTextHTTPFixture struct {
	document schema.Resume
	routes   []richTextRouteCase
}

func richTextHTTPDocument(t *testing.T) richTextHTTPFixture {
	t.Helper()
	doc := loadMinimalDocument(t)
	routes := []richTextRouteCase{
		{sectionKey: "profile", field: "text", entryID: "50000000-0000-4000-8000-000000000001"},
		{sectionKey: "work", field: "description", entryID: "50000000-0000-4000-8000-000000000002"},
		{sectionKey: "education", field: "description", entryID: "50000000-0000-4000-8000-000000000003"},
		{sectionKey: "skill", field: "infoHtml", entryID: "50000000-0000-4000-8000-000000000004"},
		{sectionKey: "certificate", field: "description", entryID: "50000000-0000-4000-8000-000000000005"},
		{sectionKey: "project", field: "description", entryID: "50000000-0000-4000-8000-000000000006"},
		{sectionKey: "custom", field: "description", entryID: "50000000-0000-4000-8000-000000000007"},
	}
	doc.Content = map[string]schema.Section{
		"profile":     schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: routes[0].entryID}}),
		"work":        schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: routes[1].entryID}}),
		"education":   schema.NewEducationSection(nil, nil, []schema.EducationEntry{{ID: routes[2].entryID}}),
		"skill":       schema.NewSkillSection(nil, nil, []schema.SkillEntry{{ID: routes[3].entryID}}),
		"certificate": schema.NewCertificateSection(nil, nil, []schema.CertificateEntry{{ID: routes[4].entryID}}),
		"project":     schema.NewProjectSection(nil, nil, []schema.ProjectEntry{{ID: routes[5].entryID}}),
		"custom":      schema.NewCustomSection(nil, nil, []schema.CustomEntry{{ID: routes[6].entryID}}),
	}
	doc.Customization.Layout.Sections.Main = []string{"profile", "work", "education", "skill", "certificate", "project", "custom"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	return richTextHTTPFixture{document: doc, routes: routes}
}

func responseResumeDocument(t *testing.T, response testHTTPResponse) schema.Resume {
	t.Helper()
	resource := decodeResumeResource(t, response)
	var document schema.Resume
	if err := json.Unmarshal(resource.Document, &document); err != nil {
		t.Fatalf("decode response document: %v", err)
	}
	return document
}

func richTextRouteValue(t *testing.T, section schema.Section, field string) string {
	t.Helper()
	var value *string
	switch field {
	case "text":
		value = section.ProfileEntries[0].Text
	case "infoHtml":
		value = section.SkillEntries[0].InfoHTML
	case "description":
		switch section.SectionType {
		case schema.Work:
			value = section.WorkEntries[0].Description
		case schema.Education:
			value = section.EducationEntries[0].Description
		case schema.Certificate:
			value = section.CertificateEntries[0].Description
		case schema.Project:
			value = section.ProjectEntries[0].Description
		case schema.SectionTypeCustom:
			value = section.CustomEntries[0].Description
		}
	}
	if value == nil {
		t.Fatalf("response omitted rich-text field %s for %s", field, section.SectionType)
	}
	return *value
}

type httpRichTextPredicate struct {
	tags       map[string]struct{}
	attributes map[string]map[string]struct{}
	schemes    map[string]struct{}
}

func newHTTPRichTextPredicate() httpRichTextPredicate {
	policy := httpRichTextPredicate{
		tags: make(map[string]struct{}, len(schema.AllowedTags)), attributes: make(map[string]map[string]struct{}, len(schema.AllowedAttributes)),
		schemes: make(map[string]struct{}, len(schema.AllowedURLSchemes)),
	}
	for _, tag := range schema.AllowedTags {
		policy.tags[strings.ToLower(tag)] = struct{}{}
	}
	for tag, attributes := range schema.AllowedAttributes {
		policy.attributes[tag] = make(map[string]struct{}, len(attributes))
		for _, attribute := range attributes {
			policy.attributes[tag][strings.ToLower(attribute)] = struct{}{}
		}
	}
	for _, scheme := range schema.AllowedURLSchemes {
		policy.schemes[strings.ToLower(scheme)] = struct{}{}
	}
	return policy
}

func (p httpRichTextPredicate) check(fragment string) error {
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), context)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if err := p.checkNode(node); err != nil {
			return err
		}
	}
	return nil
}

func (p httpRichTextPredicate) checkNode(node *html.Node) error {
	if node.Type == html.TextNode {
		return nil
	}
	if node.Type != html.ElementNode || node.Namespace != "" {
		return fmt.Errorf("node type %d namespace %q is not allowed", node.Type, node.Namespace)
	}
	tag := strings.ToLower(node.Data)
	if _, ok := p.tags[tag]; !ok {
		return fmt.Errorf("tag %q is not allowed", tag)
	}
	values := make(map[string]string, len(node.Attr))
	for _, attribute := range node.Attr {
		name := strings.ToLower(attribute.Key)
		if attribute.Namespace != "" {
			return fmt.Errorf("attribute %s has namespace %q", name, attribute.Namespace)
		}
		if _, duplicate := values[name]; duplicate {
			return fmt.Errorf("duplicate attribute %q", name)
		}
		if _, ok := p.attributes[tag][name]; !ok {
			return fmt.Errorf("attribute %s.%s is not allowed", tag, name)
		}
		values[name] = attribute.Val
	}
	if tag == "a" {
		if values["rel"] != schema.ExternalRel {
			return fmt.Errorf("anchor rel = %q", values["rel"])
		}
		if target := values["target"]; target != "" && target != "_blank" {
			return fmt.Errorf("anchor target = %q", target)
		}
		if href := values["href"]; href != "" {
			normalized := strings.Map(func(r rune) rune {
				if r == '\t' || r == '\n' || r == '\r' {
					return -1
				}
				return r
			}, strings.Trim(href, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f "))
			parsed, err := url.Parse(normalized)
			if err != nil {
				return fmt.Errorf("parse anchor URL: %w", err)
			}
			if _, ok := p.schemes[strings.ToLower(parsed.Scheme)]; !ok || parsed.Scheme == "" {
				return fmt.Errorf("anchor scheme %q is not allowed", parsed.Scheme)
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := p.checkNode(child); err != nil {
			return err
		}
	}
	return nil
}
