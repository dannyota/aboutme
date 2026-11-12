package resumeapi

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
)

func planResume(t *testing.T, projectID uuid.UUID, live bool, revision int64) resume.Resume {
	t.Helper()
	doc := schema.Resume{Content: map[string]schema.Section{}}
	if projectID != uuid.Nil {
		doc = withProjectSection(doc, "projects", linkedEntry(projectID, "Atlas"))
	}
	return resume.Resume{ID: uuid.New(), Live: live, Revision: revision, Doc: doc}
}

func TestReplacementSet_PlansOnlyLinkingLiveResumes(t *testing.T) {
	projectID := uuid.New()
	liveA, liveB := planResume(t, projectID, true, 4), planResume(t, projectID, true, 9)
	draft := planResume(t, projectID, false, 2)
	other := planResume(t, uuid.New(), true, 3)
	unlinked := planResume(t, uuid.Nil, true, 5)
	ordered := []resume.Resume{liveA, liveB}
	if bytes.Compare(liveB.ID[:], liveA.ID[:]) < 0 {
		ordered = []resume.Resume{liveB, liveA}
	}
	for name, test := range map[string]struct {
		change project.Change
		class  publicstate.TransitionClass
	}{
		"document change": {change: project.DocumentChanged, class: publicstate.NonDraining},
		"slug change":     {change: project.SlugChanged, class: publicstate.Revoking},
	} {
		t.Run(name, func(t *testing.T) {
			set, err := replacementSet([]resume.Resume{draft, liveB, other, unlinked, liveA}, projectID, test.change)
			if err != nil {
				t.Fatal(err)
			}
			want := publicstate.Plan{Resumes: []publicstate.ResumeTarget{
				{ID: ordered[0].ID, ExpectedRevision: ordered[0].Revision, Class: test.class},
				{ID: ordered[1].ID, ExpectedRevision: ordered[1].Revision, Class: test.class},
			}}
			if !reflect.DeepEqual(set.fence, want) {
				t.Fatalf("fence = %+v, want %+v", set.fence, want)
			}
			if len(set.resumes) != 2 || set.resumes[0].ID != ordered[0].ID || set.resumes[1].ID != ordered[1].ID {
				t.Fatalf("resumes = %+v, want the two live linking resumes in id order", set.resumes)
			}
		})
	}
}

func TestSameFence_RequiresTheSameTargetsRevisionsAndClasses(t *testing.T) {
	id := uuid.New()
	base := publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 3, Class: publicstate.NonDraining}}}
	if !sameFence(base, publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 3, Class: publicstate.NonDraining}}}) {
		t.Fatal("identical fences differ")
	}
	if !sameFence(publicstate.Plan{}, publicstate.Plan{Resumes: []publicstate.ResumeTarget{}}) {
		t.Fatal("two empty fences differ")
	}
	for name, other := range map[string]publicstate.Plan{
		"empty":    {},
		"revision": {Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 4, Class: publicstate.NonDraining}}},
		"class":    {Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: 3, Class: publicstate.Revoking}}},
		"resume":   {Resumes: []publicstate.ResumeTarget{{ID: uuid.New(), ExpectedRevision: 3, Class: publicstate.NonDraining}}},
		"added": {Resumes: []publicstate.ResumeTarget{
			{ID: id, ExpectedRevision: 3, Class: publicstate.NonDraining}, {ID: uuid.New(), ExpectedRevision: 1, Class: publicstate.NonDraining},
		}},
	} {
		if sameFence(base, other) {
			t.Fatalf("%s: fences compare equal", name)
		}
	}
}
