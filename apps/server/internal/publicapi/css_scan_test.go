package publicapi

import (
	"context"
	"testing"

	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicstate"
)

func TestSharedStyleCheck_QuotedCommentsCannotHideAURL(t *testing.T) {
	const payload = `--a:"/*";background-image:url(/api/v1/public/resumes/other-resume/photo);--b:"*/"`
	origin := mustPublicOrigin(t)

	_, reader, _ := formatTestReader(t, false)
	snapshot, lease, err := reader.ReadResume(context.Background(), "ada", publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	jsonLD, err := publicformat.JSONLD(snapshot.Public, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	resume := validHTMLIn("en", "Ada", "https://aboutme.example/ada", "1", "")
	if rule := publicHTMLRejection([]byte(resume), snapshot.Public, origin, jsonLD, false); rule != "" {
		t.Fatalf("resume page control = %q, want accepted", rule)
	}
	styled := replaceOnce(resume, "body</main>", `<p style='`+payload+`'>body</p></main>`)
	if rule := publicHTMLRejection([]byte(styled), snapshot.Public, origin, jsonLD, false); rule != "inline_style" {
		t.Errorf("resume page style attribute = %q, want inline_style", rule)
	}

	envelope := gateTestEnvelope()
	gate := gateValidHTML(envelope, origin)
	if rule := gateHTMLRejection([]byte(gate), envelope, origin); rule != "" {
		t.Fatalf("gate control = %q, want accepted", rule)
	}
	if rule := gateHTMLRejection([]byte(replaceOnce(gate, "</head>", "<style>h1{"+payload+"}</style></head>")), envelope, origin); rule != "style_element" {
		t.Errorf("gate style element = %q, want style_element", rule)
	}
	if rule := gateHTMLRejection([]byte(replaceOnce(gate, `<main id="public-gate">`, `<main id="public-gate"><p style='`+payload+`'>x</p>`)), envelope, origin); rule != "inline_style" {
		t.Errorf("gate style attribute = %q, want inline_style", rule)
	}
}
