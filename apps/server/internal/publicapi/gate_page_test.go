package publicapi

import (
	"strings"
	"testing"
)

// TestGateHTMLRejection_ProjectPageLinksCarryThePage proves a gate standing
// in for a project page expects each provider link to end in exactly
// &page={page}, and that the resume page's gate is unchanged.
func TestGateHTMLRejection_ProjectPageLinksCarryThePage(t *testing.T) {
	origin := mustPublicOrigin(t)
	resume := gateTestEnvelope()
	for _, page := range []string{"projects", "projects/note-g"} {
		t.Run(page, func(t *testing.T) {
			envelope := gateTestEnvelope()
			envelope.Page = page
			anchors := gateExpectedAnchors(envelope, origin)
			for _, provider := range envelope.Providers {
				if anchors["/api/v1/auth/"+provider+"/start?purpose=view&slug=ada&page="+page] != "gate-provider" {
					t.Fatalf("anchors = %v, want the %s link with &page=%s", anchors, provider, page)
				}
			}
			valid := gateValidHTML(envelope, origin)
			if !strings.Contains(valid, `start?purpose=view&amp;slug=ada&amp;page=`+page+`"`) {
				t.Fatalf("fixture lacks the escaped page link:\n%s", valid)
			}
			if rule := gateHTMLRejection([]byte(valid), envelope, origin); rule != "" {
				t.Fatalf("gateHTMLRejection() = %q for a project gate", rule)
			}
			if rule := gateHTMLRejection([]byte(gateValidHTML(resume, origin)), envelope, origin); rule != "anchor_unexpected" {
				t.Fatalf("resume links on a project gate = %q, want anchor_unexpected", rule)
			}
			if rule := gateHTMLRejection([]byte(valid), resume, origin); rule != "anchor_unexpected" {
				t.Fatalf("project links on the resume gate = %q, want anchor_unexpected", rule)
			}
			other := envelope
			other.Page = page + "-other"
			if rule := gateHTMLRejection([]byte(gateValidHTML(other, origin)), envelope, origin); rule != "anchor_unexpected" {
				t.Fatalf("another page's links = %q, want anchor_unexpected", rule)
			}
		})
	}
	if _, found := gateExpectedAnchors(resume, origin)["/api/v1/auth/google/start?purpose=view&slug=ada"]; !found {
		t.Fatal("the resume page's gate no longer links the plain view start")
	}
}
