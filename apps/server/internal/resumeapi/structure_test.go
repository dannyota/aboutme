package resumeapi

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

func structureTestDocument(t *testing.T) json.RawMessage {
	t.Helper()
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{
		"a": schema.NewWorkSection(nil, nil, nil),
		"b": schema.NewSkillSection(nil, nil, nil),
		"c": schema.NewProjectSection(nil, nil, nil),
	}
	doc.Customization.Layout.Sections.Main = []string{"a", "b", "c"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal structure document: %v", err)
	}
	return raw
}

func TestStructureCreateIndexBoundAndSectionLimit(t *testing.T) {
	t.Parallel()
	raw := structureTestDocument(t)
	for name, index := range map[string]int{"front": 0, "append": 3} {
		t.Run(name, func(t *testing.T) {
			changed, err := applyStructureCommands(raw, []structureCommand{{
				Op: "createSection", Key: "d", SectionType: "work", Column: "main", Index: index, HasIndex: true,
			}})
			if err != nil {
				t.Fatalf("create at %d: %v", index, err)
			}
			if got := decodeEntryTestDocument(t, changed).Customization.Layout.Sections.Main[index]; got != "d" {
				t.Fatalf("created key at index %d = %q", index, got)
			}
		})
	}
	if _, err := applyStructureCommands(raw, []structureCommand{{
		Op: "createSection", Key: "d", SectionType: "work", Column: "main", Index: 4, HasIndex: true,
	}}); err == nil {
		t.Fatal("create at N+1 succeeded")
	}

	doc := loadMinimalDocument(t)
	doc.Content = make(map[string]schema.Section, 24)
	doc.Customization.Layout.Sections.Main = make([]string, 24)
	doc.Customization.Layout.Sections.Sidebar = []string{}
	for i := 0; i < 24; i++ {
		key := fmt.Sprintf("%c", 'a'+i)
		doc.Content[key] = schema.NewWorkSection(nil, nil, nil)
		doc.Customization.Layout.Sections.Main[i] = key
	}
	atLimit, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal 24-section document: %v", err)
	}
	if _, err := applyStructureCommands(atLimit, []structureCommand{{
		Op: "createSection", Key: "y", SectionType: "work", Column: "main", Index: 24, HasIndex: true,
	}}); err == nil {
		t.Fatal("25th section succeeded")
	}
}

func assertExactlyOncePlacement(t *testing.T, raw json.RawMessage) {
	t.Helper()
	doc := decodeEntryTestDocument(t, raw)
	seen := make(map[string]int)
	for _, key := range append(append([]string{}, doc.Customization.Layout.Sections.Main...), doc.Customization.Layout.Sections.Sidebar...) {
		seen[key]++
		if _, ok := doc.Content[key]; !ok {
			t.Fatalf("layout references missing key %q", key)
		}
	}
	for key := range doc.Content {
		if seen[key] != 1 {
			t.Fatalf("content key %q placement count = %d, want 1", key, seen[key])
		}
	}
}

func TestStructureCommandsApplyInOrderWithExactIndices(t *testing.T) {
	t.Parallel()
	raw := structureTestDocument(t)
	commands := []structureCommand{
		{Op: "createSection", Key: "d", SectionType: "education", Column: "sidebar", Index: 0, HasIndex: true},
		{Op: "moveSection", Key: "b", Column: "main", Index: 2, HasIndex: true},
		{Op: "moveSection", Key: "d", Column: "main", Index: 0, HasIndex: true},
		{Op: "reorderColumn", Column: "main", Keys: []string{"a", "d", "c", "b"}},
	}
	changed, err := applyStructureCommands(raw, commands)
	if err != nil {
		t.Fatalf("apply commands: %v", err)
	}
	doc := decodeEntryTestDocument(t, changed)
	if want := []string{"a", "d", "c", "b"}; !reflect.DeepEqual(doc.Customization.Layout.Sections.Main, want) {
		t.Fatalf("main = %v, want %v", doc.Customization.Layout.Sections.Main, want)
	}
	if len(doc.Customization.Layout.Sections.Sidebar) != 0 {
		t.Fatalf("sidebar = %v, want empty", doc.Customization.Layout.Sections.Sidebar)
	}
	assertExactlyOncePlacement(t, changed)
}

func TestStructureMoveRemovesBeforeMeasuringAndOneColumnKeepsSidebar(t *testing.T) {
	t.Parallel()
	raw := structureTestDocument(t)
	changed, err := applyStructureCommands(raw, []structureCommand{{Op: "moveSection", Key: "b", Column: "main", Index: 2, HasIndex: true}})
	if err != nil {
		t.Fatalf("same-column move: %v", err)
	}
	if got := decodeEntryTestDocument(t, changed).Customization.Layout.Sections.Main; !reflect.DeepEqual(got, []string{"a", "c", "b"}) {
		t.Fatalf("same-column result = %v", got)
	}
	if _, applyErr := applyStructureCommands(raw, []structureCommand{{Op: "moveSection", Key: "b", Column: "main", Index: 3, HasIndex: true}}); applyErr == nil {
		t.Fatal("same-column index 3 accepted after removal left length 2")
	}

	doc := decodeEntryTestDocument(t, raw)
	doc.Customization.Layout.Columns = 1
	oneColumn, marshalErr := json.Marshal(doc)
	if marshalErr != nil {
		t.Fatalf("marshal one-column document: %v", marshalErr)
	}
	moved, err := applyStructureCommands(oneColumn, []structureCommand{{Op: "moveSection", Key: "c", Column: "sidebar", Index: 0, HasIndex: true}})
	if err != nil {
		t.Fatalf("move into one-column sidebar: %v", err)
	}
	if got := decodeEntryTestDocument(t, moved).Customization.Layout.Sections.Sidebar; !reflect.DeepEqual(got, []string{"c"}) {
		t.Fatalf("one-column sidebar = %v", got)
	}
}

func TestStructureRejectsInvalidCommandWithoutChangingInput(t *testing.T) {
	t.Parallel()
	raw := structureTestDocument(t)
	bad := [][]structureCommand{
		{{Op: "createSection", Key: "a", SectionType: "work", Column: "main", Index: 0, HasIndex: true}},
		{{Op: "deleteSection", Key: "missing"}},
		{{Op: "moveSection", Key: "a", Column: "main", Index: -1, HasIndex: true}},
		{{Op: "reorderColumn", Column: "main", Keys: []string{"a", "a", "c"}}},
		{{Op: "createSection", Key: "d", SectionType: "unknown", Column: "main", Index: 0, HasIndex: true}},
		{{Op: "createSection", Key: "d", SectionType: "work", Column: "main", Index: 3, HasIndex: true}, {Op: "deleteSection", Key: "missing"}},
	}
	for i, commands := range bad {
		if _, err := applyStructureCommands(raw, commands); err == nil {
			t.Errorf("case %d accepted: %#v", i, commands)
		}
		if got := string(raw); got != string(structureTestDocument(t)) {
			t.Fatalf("case %d mutated input bytes", i)
		}
	}
}

func TestStructureGeneratedSequencesPreserveExactlyOncePlacement(t *testing.T) {
	t.Parallel()
	const seed = int64(20260813)
	rng := rand.New(rand.NewSource(seed))
	raw := structureTestDocument(t)
	for i := 0; i < 200; i++ {
		doc := decodeEntryTestDocument(t, raw)
		main := doc.Customization.Layout.Sections.Main
		sidebar := doc.Customization.Layout.Sections.Sidebar
		var command structureCommand
		if rng.Intn(2) == 0 && len(main) > 0 {
			key := main[rng.Intn(len(main))]
			command = structureCommand{Op: "moveSection", Key: key, Column: "sidebar", Index: rng.Intn(len(sidebar) + 1), HasIndex: true}
		} else if len(sidebar) > 0 {
			key := sidebar[rng.Intn(len(sidebar))]
			command = structureCommand{Op: "moveSection", Key: key, Column: "main", Index: rng.Intn(len(main) + 1), HasIndex: true}
		} else {
			command = structureCommand{Op: "moveSection", Key: main[0], Column: "main", Index: len(main) - 1, HasIndex: true}
		}
		changed, err := applyStructureCommands(raw, []structureCommand{command})
		if err != nil {
			t.Fatalf("seed %d step %d command %#v: %v", seed, i, command, err)
		}
		assertExactlyOncePlacement(t, changed)
		raw = changed
	}
}
