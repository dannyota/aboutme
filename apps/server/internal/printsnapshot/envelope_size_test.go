package printsnapshot

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/resume"
)

func TestLargestLinkedEnvelopeFits(t *testing.T) {
	host := strings.Repeat(strings.Repeat("a", 63)+".", 8)
	origin := parsedOrigin(t, "https://"+host[:512-len("https://")-len(".vn")]+".vn", "production")
	if len(origin.String()) != 512 {
		t.Fatalf("origin = %d bytes, want 512", len(origin.String()))
	}
	slug := "a" + strings.Repeat("b", 28) + "c"
	lng := "x-abcdefgh-abcdefgh-abcdefgh-abcdef"

	source := ownerWithoutPhoto()
	source.Lng, source.Revision = &lng, math.MaxInt64
	source.Doc.PersonalDetails.Photo = &schema.Photo{Key: "private.png"}
	entries := make([]schema.ProjectEntry, 30)
	links := make([]publicresume.ProjectLink, len(entries))
	for index := range entries {
		id := uuid.NewSHA1(uuid.NameSpaceURL, fmt.Appendf(nil, "entry/%d", index)).String()
		title := fmt.Sprintf("Project %02d", index)
		entries[index] = schema.ProjectEntry{ID: id, Title: &title}
		projectSlug := fmt.Sprintf("%02d", index) + strings.Repeat("p", 38)
		links[index] = publicresume.ProjectLink{EntryID: id, ProjectSlug: projectSlug, URL: origin.Resolve("/" + slug + "/projects/" + projectSlug)}
	}
	source.Doc.Content["projects"] = schema.NewProjectSection(nil, nil, entries)
	source.Doc.Customization.Layout.Sections.Main = []string{"projects"}
	short, err := resume.AssembleCanonical(source.Doc)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("x", resume.MaxDocumentBytes-(len(short)-len("Ada")))
	source.Doc.PersonalDetails.FullName = &name
	canonical, err := resume.AssembleCanonical(source.Doc)
	if err != nil || len(canonical) != resume.MaxDocumentBytes {
		t.Fatalf("document = %d bytes, %v; want %d", len(canonical), err, resume.MaxDocumentBytes)
	}
	photo := testPNG(t)
	photo = append(photo, make([]byte, MaxPhotoBytes-len(photo))...)

	plain, err := FromOwner(source, photo, "image/png")
	if err != nil {
		t.Fatalf("FromOwner: %v", err)
	}
	linked, err := WithProjectLinks(plain, origin, slug, links)
	if err != nil {
		t.Fatalf("WithProjectLinks: %v", err)
	}
	payload, err := Marshal(linked)
	if err != nil || linked.Version != 2 {
		t.Fatalf("Marshal: version %d, %v", linked.Version, err)
	}
	t.Logf("largest linked envelope: %d of %d bytes", len(payload), MaxEnvelopeBytes)
}
