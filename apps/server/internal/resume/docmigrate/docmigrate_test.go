package docmigrate_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "packages", "schema", "fixtures", name))
	if err != nil {
		t.Fatalf("resolving fixture path %s: %v", name, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

func decodeDoc(doc json.RawMessage) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func normalize(t *testing.T, doc json.RawMessage) []byte {
	t.Helper()
	m, err := decodeDoc(doc)
	if err != nil {
		t.Fatalf("normalize: decode: %v", err)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("normalize: encode: %v", err)
	}
	return out
}

func v1RawSchema(t *testing.T) []byte {
	t.Helper()
	released, err := schema.ReleasedSchemaFor(1)
	if err != nil {
		t.Fatalf("schema.ReleasedSchemaFor(1): %v", err)
	}
	return released.RawSchema
}

func derivedSchema(t *testing.T, version int) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(v1RawSchema(t)))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("derivedSchema: decode v1 schema: %v", err)
	}
	doc["$id"] = fmt.Sprintf("https://aboutme.vn/schema/resume/v%d", version)
	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		t.Fatal("derivedSchema: v1 schema has no $defs object")
	}
	sv, ok := defs["schemaVersion"].(map[string]any)
	if !ok {
		t.Fatal("derivedSchema: v1 schema has no $defs/schemaVersion object")
	}
	sv["const"] = json.Number(fmt.Sprintf("%d", version))
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("derivedSchema: encode: %v", err)
	}
	return out
}

func compileValidator(t *testing.T, raw []byte) docmigrate.ValidateFunc {
	t.Helper()
	var head struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil || head.ID == "" {
		t.Fatalf("compileValidator: schema has no usable $id (err=%v)", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("compileValidator: parse schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	c.UseLoader(jsonschema.SchemeURLLoader{})
	if addErr := c.AddResource(head.ID, doc); addErr != nil {
		t.Fatalf("compileValidator: add resource: %v", addErr)
	}
	sch, err := c.Compile(head.ID)
	if err != nil {
		t.Fatalf("compileValidator: compile: %v", err)
	}
	return func(d json.RawMessage) error {
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(d))
		if err != nil {
			return fmt.Errorf("parse instance: %w", err)
		}
		return sch.Validate(inst)
	}
}

func counted(v docmigrate.ValidateFunc, n *int) docmigrate.ValidateFunc {
	return func(d json.RawMessage) error {
		*n++
		return v(d)
	}
}

const (
	v2Prefix = "v2! "
	v3Prefix = "v3! "
)

func upHeadline(target int32, prefix string) docmigrate.ConvertFunc {
	return func(doc json.RawMessage) (json.RawMessage, error) {
		m, err := decodeDoc(doc)
		if err != nil {
			return nil, err
		}
		pd, ok := m["personalDetails"].(map[string]any)
		if !ok {
			return nil, errors.New("upHeadline: personalDetails is not an object")
		}
		if h, ok := pd["headline"].(string); ok {
			pd["headline"] = prefix + h
		}
		m["schemaVersion"] = json.Number(fmt.Sprintf("%d", target))
		return json.Marshal(m)
	}
}

func downHeadline(target int32, prefix string) docmigrate.ConvertFunc {
	return func(doc json.RawMessage) (json.RawMessage, error) {
		m, err := decodeDoc(doc)
		if err != nil {
			return nil, err
		}
		pd, ok := m["personalDetails"].(map[string]any)
		if !ok {
			return nil, errors.New("downHeadline: personalDetails is not an object")
		}
		if h, ok := pd["headline"].(string); ok {
			if !strings.HasPrefix(h, prefix) {
				return nil, fmt.Errorf("downHeadline: headline %q does not carry the %q prefix", h, prefix)
			}
			pd["headline"] = strings.TrimPrefix(h, prefix)
		}
		m["schemaVersion"] = json.Number(fmt.Sprintf("%d", target))
		return json.Marshal(m)
	}
}

func syntheticV2(t *testing.T) *docmigrate.Projector {
	t.Helper()
	p, err := docmigrate.NewProjector(
		map[int32]docmigrate.AdjacentConverters{
			1: {Up: upHeadline(2, v2Prefix), Down: downHeadline(1, v2Prefix)},
		},
		map[int32]docmigrate.ValidateFunc{
			1: compileValidator(t, v1RawSchema(t)),
			2: compileValidator(t, derivedSchema(t, 2)),
		},
		[]int32{1, 2}, []int32{1, 2}, 2,
	)
	if err != nil {
		t.Fatalf("NewProjector(synthetic v2): %v", err)
	}
	return p
}

func fullV1Doc(t *testing.T) json.RawMessage {
	t.Helper()
	return json.RawMessage(readFixture(t, filepath.Join("v1", "full.json")))
}

func fullCurrentDoc(t *testing.T) json.RawMessage {
	t.Helper()
	return json.RawMessage(readFixture(t, "full.json"))
}

func headlineOf(t *testing.T, doc json.RawMessage) string {
	t.Helper()
	var shape struct {
		PersonalDetails struct {
			Headline string `json:"headline"`
		} `json:"personalDetails"`
	}
	if err := json.Unmarshal(doc, &shape); err != nil {
		t.Fatalf("headlineOf: %v", err)
	}
	return shape.PersonalDetails.Headline
}

func schemaVersionOf(t *testing.T, doc json.RawMessage) int32 {
	t.Helper()
	var shape struct {
		SchemaVersion int32 `json:"schemaVersion"`
	}
	if err := json.Unmarshal(doc, &shape); err != nil {
		t.Fatalf("schemaVersionOf: %v", err)
	}
	return shape.SchemaVersion
}

func splitDoc(t *testing.T, doc json.RawMessage) (pd, content, customization json.RawMessage) {
	t.Helper()
	var parts struct {
		PersonalDetails json.RawMessage `json:"personalDetails"`
		Content         json.RawMessage `json:"content"`
		Customization   json.RawMessage `json:"customization"`
	}
	if err := json.Unmarshal(doc, &parts); err != nil {
		t.Fatalf("splitDoc: %v", err)
	}
	return parts.PersonalDetails, parts.Content, parts.Customization
}

func TestDeclarations_ProductionSetsAreV1ToV6(t *testing.T) {
	t.Parallel()

	if docmigrate.CurrentVersion != 6 {
		t.Fatalf("CurrentVersion = %d, want 6", docmigrate.CurrentVersion)
	}
	for _, tc := range []struct {
		name string
		got  []int32
	}{
		{"accepted", docmigrate.AcceptedVersions()},
		{"emitted", docmigrate.EmittedVersions()},
	} {
		if !slices.Equal(tc.got, []int32{1, 2, 3, 4, 5, 6}) {
			t.Errorf("%sVersions() = %v, want [1 2 3 4 5 6]", tc.name, tc.got)
		}
	}
}

func TestDeclarations_EveryDeclaredVersionIsReleased(t *testing.T) {
	t.Parallel()

	released := map[int32]bool{}
	for _, v := range schema.ReleasedVersions() {
		released[int32(v)] = true
	}
	for _, v := range append(docmigrate.AcceptedVersions(), docmigrate.EmittedVersions()...) {
		if !released[v] {
			t.Errorf("declared version %d has no released schema (released: %v)", v, schema.ReleasedVersions())
		}
	}
	if !released[docmigrate.CurrentVersion] {
		t.Errorf("CurrentVersion %d has no released schema", docmigrate.CurrentVersion)
	}
}

func TestDeclarations_ReturnedSlicesCannotMutateInternalState(t *testing.T) {
	t.Parallel()

	accepted := docmigrate.AcceptedVersions()
	emitted := docmigrate.EmittedVersions()
	accepted[0] = 99
	emitted[0] = 99

	if got := docmigrate.AcceptedVersions(); !slices.Equal(got, []int32{1, 2, 3, 4, 5, 6}) {
		t.Errorf("AcceptedVersions() = %v after mutating a returned copy, want [1 2 3 4 5 6]", got)
	}
	if got := docmigrate.EmittedVersions(); !slices.Equal(got, []int32{1, 2, 3, 4, 5, 6}) {
		t.Errorf("EmittedVersions() = %v after mutating a returned copy, want [1 2 3 4 5 6]", got)
	}
}

func TestIdentityProjector_ConvertIsByteStable(t *testing.T) {
	t.Parallel()
	p := docmigrate.NewIdentityProjector()
	doc := fullV1Doc(t)

	got, err := p.Convert(doc, 1, 1)
	if err != nil {
		t.Fatalf("Convert(doc, 1, 1): %v", err)
	}
	if !bytes.Equal(got, doc) {
		t.Errorf("identity Convert changed the document bytes:\n got %s\nwant %s", got, doc)
	}
}

func TestIdentityProjector_ProjectConvertsV1ToCurrent(t *testing.T) {
	t.Parallel()
	p := docmigrate.NewIdentityProjector()
	pd, content, customization := splitDoc(t, fullV1Doc(t))

	gotPD, gotContent, gotCustomization, err := p.Project(pd, content, customization, 1)
	if err != nil {
		t.Fatalf("Project(parts, 1): %v", err)
	}
	assembled, err := json.Marshal(map[string]json.RawMessage{
		"schemaVersion":   json.RawMessage("3"),
		"personalDetails": gotPD,
		"content":         gotContent,
		"customization":   gotCustomization,
	})
	if err != nil {
		t.Fatalf("assemble projected document: %v", err)
	}
	if got := fontFamilyOfTest(t, assembled); got != "be-vietnam-pro" {
		t.Errorf("projected font family = %q, want be-vietnam-pro", got)
	}
}

func fontFamilyOfTest(t *testing.T, doc json.RawMessage) string {
	t.Helper()
	var value struct {
		Customization struct {
			Font struct {
				Family string `json:"family"`
			} `json:"font"`
		} `json:"customization"`
	}
	if err := json.Unmarshal(doc, &value); err != nil {
		t.Fatalf("decode font family: %v", err)
	}
	return value.Customization.Font.Family
}

func TestIdentityProjector_CurrentVersion(t *testing.T) {
	t.Parallel()
	if got := docmigrate.NewIdentityProjector().CurrentVersion(); got != docmigrate.CurrentVersion {
		t.Errorf("CurrentVersion() = %d, want %d", got, docmigrate.CurrentVersion)
	}
}

func TestIdentityProjector_UnknownStoredVersion_FailsClosed(t *testing.T) {
	t.Parallel()
	p := docmigrate.NewIdentityProjector()
	pd, content, customization := splitDoc(t, fullV1Doc(t))

	for _, stored := range []int32{0, 7, 8} {
		if _, _, _, err := p.Project(pd, content, customization, stored); !errors.Is(err, docmigrate.ErrUnknownVersion) {
			t.Errorf("Project(parts, %d) error = %v, want ErrUnknownVersion", stored, err)
		}
	}
}

func TestConvert_AdjacentUpAndDown(t *testing.T) {
	t.Parallel()
	p := syntheticV2(t)
	v1 := fullV1Doc(t)
	wantHeadline := headlineOf(t, v1)

	up, err := p.Convert(v1, 1, 2)
	if err != nil {
		t.Fatalf("Convert(v1, 1, 2): %v", err)
	}
	if got := schemaVersionOf(t, up); got != 2 {
		t.Errorf("converted schemaVersion = %d, want 2", got)
	}
	if got := headlineOf(t, up); got != v2Prefix+wantHeadline {
		t.Errorf("converted headline = %q, want %q", got, v2Prefix+wantHeadline)
	}

	down, err := p.Convert(up, 2, 1)
	if err != nil {
		t.Fatalf("Convert(v2, 2, 1): %v", err)
	}
	if got := schemaVersionOf(t, down); got != 1 {
		t.Errorf("round-tripped schemaVersion = %d, want 1", got)
	}
	if !bytes.Equal(normalize(t, down), normalize(t, v1)) {
		t.Errorf("1->2->1 lost or changed data:\n got %s\nwant %s", normalize(t, down), normalize(t, v1))
	}
}

func TestConvert_MultiStepChain(t *testing.T) {
	t.Parallel()

	var n1, n2, n3 int
	p, err := docmigrate.NewProjector(
		map[int32]docmigrate.AdjacentConverters{
			1: {Up: upHeadline(2, v2Prefix), Down: downHeadline(1, v2Prefix)},
			2: {Up: upHeadline(3, v3Prefix), Down: downHeadline(2, v3Prefix)},
		},
		map[int32]docmigrate.ValidateFunc{
			1: counted(compileValidator(t, v1RawSchema(t)), &n1),
			2: counted(compileValidator(t, derivedSchema(t, 2)), &n2),
			3: counted(compileValidator(t, derivedSchema(t, 3)), &n3),
		},
		[]int32{1, 2, 3}, []int32{1, 2, 3}, 3,
	)
	if err != nil {
		t.Fatalf("NewProjector(synthetic v3): %v", err)
	}

	v1 := fullV1Doc(t)
	wantHeadline := headlineOf(t, v1)

	up, err := p.Convert(v1, 1, 3)
	if err != nil {
		t.Fatalf("Convert(v1, 1, 3): %v", err)
	}
	if got, want := headlineOf(t, up), v3Prefix+v2Prefix+wantHeadline; got != want {
		t.Errorf("1->3 headline = %q, want %q", got, want)
	}
	if got := schemaVersionOf(t, up); got != 3 {
		t.Errorf("1->3 schemaVersion = %d, want 3", got)
	}
	if n1 != 1 || n2 != 1 || n3 != 1 {
		t.Errorf("1->3 validator calls = (v1:%d v2:%d v3:%d), want (1 1 1): every step must validate its source and its output", n1, n2, n3)
	}

	n1, n2, n3 = 0, 0, 0
	down, err := p.Convert(up, 3, 1)
	if err != nil {
		t.Fatalf("Convert(v3, 3, 1): %v", err)
	}
	if !bytes.Equal(normalize(t, down), normalize(t, v1)) {
		t.Errorf("1->3->1 lost or changed data:\n got %s\nwant %s", normalize(t, down), normalize(t, v1))
	}
	if n1 != 1 || n2 != 1 || n3 != 1 {
		t.Errorf("3->1 validator calls = (v1:%d v2:%d v3:%d), want (1 1 1)", n1, n2, n3)
	}
}

func TestConvert_IdentityValidatesSourceAndPreservesBytes(t *testing.T) {
	t.Parallel()

	var n int
	p, err := docmigrate.NewProjector(nil,
		map[int32]docmigrate.ValidateFunc{1: counted(compileValidator(t, v1RawSchema(t)), &n)},
		[]int32{1}, []int32{1}, 1)
	if err != nil {
		t.Fatalf("NewProjector: %v", err)
	}
	if _, err := p.Convert(fullV1Doc(t), 1, 1); err != nil {
		t.Fatalf("Convert(doc, 1, 1): %v", err)
	}
	if n != 1 {
		t.Errorf("identity Convert ran the validator %d time(s), want 1", n)
	}

	invalid := json.RawMessage(`{"schemaVersion":1,"not":"a resume"}`)
	if _, err := p.Convert(invalid, 1, 1); !errors.Is(err, docmigrate.ErrInvalidDocument) {
		t.Errorf("identity Convert of invalid source returned %v, want ErrInvalidDocument", err)
	}
	if n != 2 {
		t.Errorf("two identity Convert calls ran the validator %d time(s), want 2", n)
	}
}

func TestProject_CurrentVersionBypassesValidation(t *testing.T) {
	t.Parallel()

	var n int
	p, err := docmigrate.NewProjector(nil,
		map[int32]docmigrate.ValidateFunc{1: counted(compileValidator(t, v1RawSchema(t)), &n)},
		[]int32{1}, []int32{1}, 1)
	if err != nil {
		t.Fatalf("NewProjector: %v", err)
	}
	pd := json.RawMessage(`{"not":"schema-valid personal details"}`)
	content := json.RawMessage(`[]`)
	customization := json.RawMessage(`null`)
	gotPD, gotContent, gotCustomization, err := p.Project(pd, content, customization, 1)
	if err != nil {
		t.Fatalf("Project(current parts): %v", err)
	}
	if !bytes.Equal(gotPD, pd) || !bytes.Equal(gotContent, content) || !bytes.Equal(gotCustomization, customization) {
		t.Errorf("Project current-version short circuit changed bytes: got %s / %s / %s", gotPD, gotContent, gotCustomization)
	}
	if n != 0 {
		t.Errorf("Project current-version short circuit ran the validator %d time(s), want 0", n)
	}
}

func TestConvert_FailsClosed(t *testing.T) {
	t.Parallel()

	v1 := fullV1Doc(t)
	broken := func(t *testing.T, up docmigrate.ConvertFunc) *docmigrate.Projector {
		t.Helper()
		p, err := docmigrate.NewProjector(
			map[int32]docmigrate.AdjacentConverters{
				1: {Up: up, Down: downHeadline(1, v2Prefix)},
			},
			map[int32]docmigrate.ValidateFunc{
				1: compileValidator(t, v1RawSchema(t)),
				2: compileValidator(t, derivedSchema(t, 2)),
			},
			[]int32{1, 2}, []int32{1, 2}, 2,
		)
		if err != nil {
			t.Fatalf("NewProjector: %v", err)
		}
		return p
	}

	t.Run("unknown source version", func(t *testing.T) {
		t.Parallel()
		_, err := syntheticV2(t).Convert(v1, 9, 2)
		if !errors.Is(err, docmigrate.ErrUnknownVersion) {
			t.Errorf("error = %v, want ErrUnknownVersion", err)
		}
	})

	t.Run("unknown target version", func(t *testing.T) {
		t.Parallel()
		_, err := syntheticV2(t).Convert(v1, 1, 9)
		if !errors.Is(err, docmigrate.ErrUnknownVersion) {
			t.Errorf("error = %v, want ErrUnknownVersion", err)
		}
	})

	t.Run("missing adjacent pair in the walk", func(t *testing.T) {
		t.Parallel()
		p, err := docmigrate.NewProjector(
			map[int32]docmigrate.AdjacentConverters{
				1: {Up: upHeadline(2, v2Prefix), Down: downHeadline(1, v2Prefix)},
			},
			map[int32]docmigrate.ValidateFunc{
				1: compileValidator(t, v1RawSchema(t)),
				2: compileValidator(t, derivedSchema(t, 2)),
				3: compileValidator(t, derivedSchema(t, 3)),
			},
			[]int32{1, 2}, []int32{1, 2}, 2,
		)
		if err != nil {
			t.Fatalf("NewProjector: %v", err)
		}
		if _, err := p.Convert(v1, 1, 3); !errors.Is(err, docmigrate.ErrNoConverter) {
			t.Errorf("Convert(1, 3) error = %v, want ErrNoConverter", err)
		}
		if _, err := p.Convert(withSchemaVersion(t, v1, 3), 3, 1); !errors.Is(err, docmigrate.ErrNoConverter) {
			t.Errorf("Convert(3, 1) error = %v, want ErrNoConverter", err)
		}
	})

	t.Run("source invalid for its own schema", func(t *testing.T) {
		t.Parallel()
		var m map[string]any
		if err := json.Unmarshal(v1, &m); err != nil {
			t.Fatalf("unmarshal fixture: %v", err)
		}
		delete(m, "content")
		invalid, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, err := syntheticV2(t).Convert(invalid, 1, 2); !errors.Is(err, docmigrate.ErrInvalidDocument) {
			t.Errorf("error = %v, want ErrInvalidDocument", err)
		}
	})

	t.Run("converter returns an error", func(t *testing.T) {
		t.Parallel()
		sentinel := errors.New("converter exploded")
		p := broken(t, func(json.RawMessage) (json.RawMessage, error) { return nil, sentinel })
		if _, err := p.Convert(v1, 1, 2); !errors.Is(err, sentinel) {
			t.Errorf("error = %v, want it to wrap the converter's own error", err)
		}
	})

	t.Run("converter returns invalid JSON", func(t *testing.T) {
		t.Parallel()
		p := broken(t, func(json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage("{not json"), nil
		})
		if _, err := p.Convert(v1, 1, 2); err == nil {
			t.Error("Convert returned nil error for a converter emitting invalid JSON")
		}
	})

	t.Run("converter output invalid for the target schema", func(t *testing.T) {
		t.Parallel()
		p := broken(t, func(doc json.RawMessage) (json.RawMessage, error) {
			m, err := decodeDoc(doc)
			if err != nil {
				return nil, err
			}
			pd, ok := m["personalDetails"].(map[string]any)
			if !ok {
				return nil, errors.New("personalDetails is not an object")
			}
			headline, ok := pd["headline"].(string)
			if !ok {
				return nil, errors.New("headline is not a string")
			}
			pd["headline"] = v2Prefix + headline
			return json.Marshal(m)
		})
		if _, err := p.Convert(v1, 1, 2); !errors.Is(err, docmigrate.ErrInvalidDocument) {
			t.Errorf("error = %v, want ErrInvalidDocument", err)
		}
	})
}

func TestNewProjector_FailsClosed(t *testing.T) {
	t.Parallel()

	validators := func(t *testing.T, versions ...int32) map[int32]docmigrate.ValidateFunc {
		t.Helper()
		out := map[int32]docmigrate.ValidateFunc{}
		for _, v := range versions {
			if v == 1 {
				out[v] = compileValidator(t, v1RawSchema(t))
				continue
			}
			out[v] = compileValidator(t, derivedSchema(t, int(v)))
		}
		return out
	}
	pair := docmigrate.AdjacentConverters{Up: upHeadline(2, v2Prefix), Down: downHeadline(1, v2Prefix)}

	tests := []struct {
		name       string
		pairs      map[int32]docmigrate.AdjacentConverters
		versions   []int32
		accepted   []int32
		emitted    []int32
		current    int32
		wantErrMsg string
	}{
		{
			name:     "missing Up",
			pairs:    map[int32]docmigrate.AdjacentConverters{1: {Down: pair.Down}},
			versions: []int32{1, 2},
			accepted: []int32{1, 2}, emitted: []int32{1, 2}, current: 2,
			wantErrMsg: "Up",
		},
		{
			name:     "missing Down",
			pairs:    map[int32]docmigrate.AdjacentConverters{1: {Up: pair.Up}},
			versions: []int32{1, 2},
			accepted: []int32{1, 2}, emitted: []int32{1, 2}, current: 2,
			wantErrMsg: "Down",
		},
		{
			name:     "pair references a version with no validator",
			pairs:    map[int32]docmigrate.AdjacentConverters{1: pair},
			versions: []int32{1},
			accepted: []int32{1}, emitted: []int32{1}, current: 1,
			wantErrMsg: "2",
		},
		{
			name:     "accepted version with no validator",
			pairs:    nil,
			versions: []int32{1},
			accepted: []int32{1, 2}, emitted: []int32{1}, current: 1,
			wantErrMsg: "accepted",
		},
		{
			name:     "emitted version with no validator",
			pairs:    nil,
			versions: []int32{1},
			accepted: []int32{1}, emitted: []int32{1, 2}, current: 1,
			wantErrMsg: "emitted",
		},
		{
			name:     "current has no validator",
			pairs:    nil,
			versions: []int32{1},
			accepted: []int32{1}, emitted: []int32{1}, current: 2,
			wantErrMsg: "current",
		},
		{
			name:     "current not accepted",
			pairs:    map[int32]docmigrate.AdjacentConverters{1: pair},
			versions: []int32{1, 2},
			accepted: []int32{1}, emitted: []int32{1, 2}, current: 2,
			wantErrMsg: "accepted",
		},
		{
			name:     "current not emitted",
			pairs:    map[int32]docmigrate.AdjacentConverters{1: pair},
			versions: []int32{1, 2},
			accepted: []int32{1, 2}, emitted: []int32{1}, current: 2,
			wantErrMsg: "emitted",
		},
		{
			name:     "duplicate accepted version",
			pairs:    nil,
			versions: []int32{1},
			accepted: []int32{1, 1}, emitted: []int32{1}, current: 1,
			wantErrMsg: "duplicate",
		},
		{
			name:     "empty accepted set",
			pairs:    nil,
			versions: []int32{1},
			accepted: nil, emitted: []int32{1}, current: 1,
			wantErrMsg: "accepted",
		},
		{
			name:     "empty emitted set",
			pairs:    nil,
			versions: []int32{1},
			accepted: []int32{1}, emitted: nil, current: 1,
			wantErrMsg: "emitted",
		},
		{
			name:     "no validators at all",
			pairs:    nil,
			versions: nil,
			accepted: []int32{1}, emitted: []int32{1}, current: 1,
			wantErrMsg: "validator",
		},
		{
			name:     "version below 1",
			pairs:    nil,
			versions: []int32{1},
			accepted: []int32{0, 1}, emitted: []int32{1}, current: 1,
			wantErrMsg: "0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := docmigrate.NewProjector(tc.pairs, validators(t, tc.versions...), tc.accepted, tc.emitted, tc.current)
			if err == nil {
				t.Fatalf("NewProjector returned a projector (%v) and nil error; want a fail-closed error", p)
			}
			if p != nil {
				t.Errorf("NewProjector returned a non-nil projector alongside error %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantErrMsg) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantErrMsg)
			}
		})
	}
}

func TestNewProjector_CopiesItsInputs(t *testing.T) {
	t.Parallel()

	pairs := map[int32]docmigrate.AdjacentConverters{
		1: {Up: upHeadline(2, v2Prefix), Down: downHeadline(1, v2Prefix)},
	}
	validators := map[int32]docmigrate.ValidateFunc{
		1: compileValidator(t, v1RawSchema(t)),
		2: compileValidator(t, derivedSchema(t, 2)),
	}
	accepted := []int32{1, 2}
	emitted := []int32{1, 2}

	p, err := docmigrate.NewProjector(pairs, validators, accepted, emitted, 2)
	if err != nil {
		t.Fatalf("NewProjector: %v", err)
	}

	delete(pairs, 1)
	delete(validators, 2)
	accepted[0] = 99
	emitted[0] = 99

	v1 := fullV1Doc(t)
	if _, err := p.Convert(v1, 1, 2); err != nil {
		t.Errorf("Convert after mutating the constructor's inputs: %v", err)
	}
	if _, _, err := p.AcceptWire(v1, 1); err != nil {
		t.Errorf("AcceptWire(1) after mutating the constructor's inputs: %v", err)
	}
	if _, err := p.EmitWire(mustConvert(t, p, v1, 1, 2), 1); err != nil {
		t.Errorf("EmitWire(1) after mutating the constructor's inputs: %v", err)
	}
}

func withSchemaVersion(t *testing.T, doc json.RawMessage, n int32) json.RawMessage {
	t.Helper()
	m, err := decodeDoc(doc)
	if err != nil {
		t.Fatalf("withSchemaVersion: decode: %v", err)
	}
	m["schemaVersion"] = json.Number(fmt.Sprintf("%d", n))
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("withSchemaVersion: encode: %v", err)
	}
	return out
}

func mustConvert(t *testing.T, p *docmigrate.Projector, doc json.RawMessage, from, to int32) json.RawMessage {
	t.Helper()
	out, err := p.Convert(doc, from, to)
	if err != nil {
		t.Fatalf("Convert(%d, %d): %v", from, to, err)
	}
	return out
}

func TestProject_ConvertsAndResplits(t *testing.T) {
	t.Parallel()
	p := syntheticV2(t)
	v1 := fullV1Doc(t)
	pd, content, customization := splitDoc(t, v1)
	wantHeadline := headlineOf(t, v1)

	gotPD, gotContent, gotCustomization, err := p.Project(pd, content, customization, 1)
	if err != nil {
		t.Fatalf("Project(parts, 1): %v", err)
	}

	var projected struct {
		Headline string `json:"headline"`
	}
	if err := json.Unmarshal(gotPD, &projected); err != nil {
		t.Fatalf("unmarshal projected personalDetails: %v", err)
	}
	if projected.Headline != v2Prefix+wantHeadline {
		t.Errorf("projected headline = %q, want %q", projected.Headline, v2Prefix+wantHeadline)
	}
	if !bytes.Equal(normalize(t, gotContent), normalize(t, content)) {
		t.Errorf("projection changed content:\n got %s\nwant %s", gotContent, content)
	}
	if !bytes.Equal(normalize(t, gotCustomization), normalize(t, customization)) {
		t.Errorf("projection changed customization:\n got %s\nwant %s", gotCustomization, customization)
	}
	for name, part := range map[string]json.RawMessage{
		"personalDetails": gotPD, "content": gotContent, "customization": gotCustomization,
	} {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(part, &m); err != nil {
			continue
		}
		if _, ok := m["schemaVersion"]; ok {
			t.Errorf("projected %s carries a schemaVersion key", name)
		}
	}
}

func TestProject_FailsClosed(t *testing.T) {
	t.Parallel()

	v1 := fullV1Doc(t)
	pd, content, customization := splitDoc(t, v1)

	t.Run("stored part is not valid JSON", func(t *testing.T) {
		t.Parallel()
		if _, _, _, err := syntheticV2(t).Project(json.RawMessage("{oops"), content, customization, 1); err == nil {
			t.Error("Project accepted a stored part that is not valid JSON")
		}
	})

	t.Run("stored part is empty", func(t *testing.T) {
		t.Parallel()
		if _, _, _, err := syntheticV2(t).Project(pd, nil, customization, 1); err == nil {
			t.Error("Project accepted an empty stored part")
		}
	})

	t.Run("converter drops a required top-level key", func(t *testing.T) {
		t.Parallel()
		p, err := docmigrate.NewProjector(
			map[int32]docmigrate.AdjacentConverters{
				1: {
					Up: func(doc json.RawMessage) (json.RawMessage, error) {
						m, err := decodeDoc(doc)
						if err != nil {
							return nil, err
						}
						delete(m, "customization")
						m["schemaVersion"] = json.Number("2")
						return json.Marshal(m)
					},
					Down: downHeadline(1, v2Prefix),
				},
			},
			map[int32]docmigrate.ValidateFunc{
				1: compileValidator(t, v1RawSchema(t)),
				2: func(json.RawMessage) error { return nil },
			},
			[]int32{1, 2}, []int32{1, 2}, 2,
		)
		if err != nil {
			t.Fatalf("NewProjector: %v", err)
		}
		if _, _, _, err := p.Project(pd, content, customization, 1); err == nil {
			t.Error("Project accepted a converted document missing a required top-level key")
		}
	})

	t.Run("converter leaves schemaVersion behind", func(t *testing.T) {
		t.Parallel()
		p, err := docmigrate.NewProjector(
			map[int32]docmigrate.AdjacentConverters{
				1: {
					Up: func(doc json.RawMessage) (json.RawMessage, error) {
						m, err := decodeDoc(doc)
						if err != nil {
							return nil, err
						}
						return json.Marshal(m)
					},
					Down: downHeadline(1, v2Prefix),
				},
			},
			map[int32]docmigrate.ValidateFunc{
				1: compileValidator(t, v1RawSchema(t)),
				2: func(json.RawMessage) error { return nil },
			},
			[]int32{1, 2}, []int32{1, 2}, 2,
		)
		if err != nil {
			t.Fatalf("NewProjector: %v", err)
		}
		if _, _, _, err := p.Project(pd, content, customization, 1); err == nil {
			t.Error("Project accepted a converted document still claiming the source version")
		}
	})
}

func TestWire_OldClientDocumentAcceptedThenEmitted(t *testing.T) {
	t.Parallel()
	p := syntheticV2(t)
	v1 := fullV1Doc(t)
	v1Validate := compileValidator(t, v1RawSchema(t))
	v2Validate := compileValidator(t, derivedSchema(t, 2))

	current, currentVersion, err := p.AcceptWire(v1, 1)
	if err != nil {
		t.Fatalf("AcceptWire(v1, 1): %v", err)
	}
	if currentVersion != 2 {
		t.Errorf("AcceptWire returned version %d, want 2 (the projector's current)", currentVersion)
	}
	if validateErr := v2Validate(current); validateErr != nil {
		t.Errorf("AcceptWire output does not validate against v2: %v", validateErr)
	}
	if got, want := headlineOf(t, current), v2Prefix+headlineOf(t, v1); got != want {
		t.Errorf("AcceptWire headline = %q, want %q", got, want)
	}

	emitted, err := p.EmitWire(current, 1)
	if err != nil {
		t.Fatalf("EmitWire(current, 1): %v", err)
	}
	if err := v1Validate(emitted); err != nil {
		t.Errorf("EmitWire output does not validate against the immutable v1 schema: %v", err)
	}
	if !bytes.Equal(normalize(t, emitted), normalize(t, v1)) {
		t.Errorf("wire round trip lost or changed a v1 field:\n got %s\nwant %s", normalize(t, emitted), normalize(t, v1))
	}
}

func TestWire_EmitCurrentVersionIsByteStable(t *testing.T) {
	t.Parallel()
	p := docmigrate.NewIdentityProjector()
	current := fullCurrentDoc(t)

	got, err := p.EmitWire(current, docmigrate.CurrentVersion)
	if err != nil {
		t.Fatalf("EmitWire(doc, current): %v", err)
	}
	if !bytes.Equal(got, current) {
		t.Errorf("EmitWire at the current version changed the bytes:\n got %s\nwant %s", got, current)
	}
}

func TestWire_AcceptCurrentVersionReturnsCurrent(t *testing.T) {
	t.Parallel()
	p := docmigrate.NewIdentityProjector()
	current := fullCurrentDoc(t)

	got, version, err := p.AcceptWire(current, docmigrate.CurrentVersion)
	if err != nil {
		t.Fatalf("AcceptWire(doc, current): %v", err)
	}
	if version != docmigrate.CurrentVersion {
		t.Errorf("AcceptWire returned version %d, want %d", version, docmigrate.CurrentVersion)
	}
	if !bytes.Equal(got, current) {
		t.Errorf("AcceptWire at the current version changed the bytes:\n got %s\nwant %s", got, current)
	}
}

func TestWire_UndeclaredVersionsFailClosed(t *testing.T) {
	t.Parallel()

	p, err := docmigrate.NewProjector(
		map[int32]docmigrate.AdjacentConverters{
			1: {Up: upHeadline(2, v2Prefix), Down: downHeadline(1, v2Prefix)},
		},
		map[int32]docmigrate.ValidateFunc{
			1: compileValidator(t, v1RawSchema(t)),
			2: compileValidator(t, derivedSchema(t, 2)),
		},
		[]int32{1, 2}, []int32{2}, 2,
	)
	if err != nil {
		t.Fatalf("NewProjector: %v", err)
	}
	v1 := fullV1Doc(t)
	current := mustConvert(t, p, v1, 1, 2)

	if _, err := p.EmitWire(current, 1); !errors.Is(err, docmigrate.ErrUnsupportedVersion) {
		t.Errorf("EmitWire(1) error = %v, want ErrUnsupportedVersion (1 is convertible but not declared emitted)", err)
	}
	if _, _, err := p.AcceptWire(v1, 9); !errors.Is(err, docmigrate.ErrUnsupportedVersion) {
		t.Errorf("AcceptWire(9) error = %v, want ErrUnsupportedVersion", err)
	}
	if _, err := p.EmitWire(current, 9); !errors.Is(err, docmigrate.ErrUnsupportedVersion) {
		t.Errorf("EmitWire(9) error = %v, want ErrUnsupportedVersion", err)
	}
}

func TestWire_InvalidInputFailsClosed(t *testing.T) {
	t.Parallel()

	var m map[string]any
	if err := json.Unmarshal(fullV1Doc(t), &m); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	delete(m, "personalDetails")
	invalid, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	p := docmigrate.NewIdentityProjector()
	if _, _, err := p.AcceptWire(invalid, 1); !errors.Is(err, docmigrate.ErrInvalidDocument) {
		t.Errorf("AcceptWire(invalid v1) error = %v, want ErrInvalidDocument", err)
	}
	if _, err := p.EmitWire(invalid, 1); !errors.Is(err, docmigrate.ErrInvalidDocument) {
		t.Errorf("EmitWire(invalid current) error = %v, want ErrInvalidDocument", err)
	}
}

func TestWire_LossyEmissionFailsClosed(t *testing.T) {
	t.Parallel()

	p, err := docmigrate.NewProjector(
		map[int32]docmigrate.AdjacentConverters{
			1: {
				Up: upHeadline(2, v2Prefix),
				Down: func(doc json.RawMessage) (json.RawMessage, error) {
					m, err := decodeDoc(doc)
					if err != nil {
						return nil, err
					}
					pd, ok := m["personalDetails"].(map[string]any)
					if !ok {
						return nil, errors.New("personalDetails is not an object")
					}
					delete(pd, "photo")
					m["schemaVersion"] = json.Number("1")
					return json.Marshal(m)
				},
			},
		},
		map[int32]docmigrate.ValidateFunc{
			1: compileValidator(t, v1RawSchema(t)),
			2: compileValidator(t, derivedSchema(t, 2)),
		},
		[]int32{1, 2}, []int32{1, 2}, 2,
	)
	if err != nil {
		t.Fatalf("NewProjector: %v", err)
	}

	current := mustConvert(t, p, fullV1Doc(t), 1, 2)
	if _, err := p.EmitWire(current, 1); !errors.Is(err, docmigrate.ErrLossyConversion) {
		t.Errorf("EmitWire error = %v, want ErrLossyConversion for a schema-valid optional-photo loss", err)
	}
}

func TestWire_LosslessEmissionComparesNumbersByExactValue(t *testing.T) {
	t.Parallel()

	doc := func(version int32, value string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"schemaVersion":%d,"value":%s}`, version, value))
	}
	validator := func(version int32) docmigrate.ValidateFunc {
		return func(raw json.RawMessage) error {
			decoded, err := decodeDoc(raw)
			if err != nil {
				return err
			}
			gotVersion, ok := decoded["schemaVersion"].(json.Number)
			if !ok || gotVersion.String() != fmt.Sprint(version) {
				return fmt.Errorf("schemaVersion = %v, want %d", decoded["schemaVersion"], version)
			}
			if _, ok := decoded["value"].(json.Number); !ok {
				return errors.New("value is not a number")
			}
			return nil
		}
	}
	projector := func(t *testing.T, emittedValue, restoredValue string) *docmigrate.Projector {
		t.Helper()
		p, err := docmigrate.NewProjector(
			map[int32]docmigrate.AdjacentConverters{
				1: {
					Up: func(json.RawMessage) (json.RawMessage, error) {
						return doc(2, restoredValue), nil
					},
					Down: func(json.RawMessage) (json.RawMessage, error) {
						return doc(1, emittedValue), nil
					},
				},
			},
			map[int32]docmigrate.ValidateFunc{1: validator(1), 2: validator(2)},
			[]int32{1, 2}, []int32{1, 2}, 2,
		)
		if err != nil {
			t.Fatalf("NewProjector: %v", err)
		}
		return p
	}

	t.Run("equivalent spellings", func(t *testing.T) {
		p := projector(t, "1e0", "1")
		if _, err := p.EmitWire(doc(2, "1.0"), 1); err != nil {
			t.Fatalf("EmitWire rejected mathematically equal 1.0, 1e0, and 1: %v", err)
		}
	})

	t.Run("distinct values above float64 precision", func(t *testing.T) {
		p := projector(t, "9007199254740992", "9007199254740992")
		if _, err := p.EmitWire(doc(2, "9007199254740993"), 1); !errors.Is(err, docmigrate.ErrLossyConversion) {
			t.Fatalf("EmitWire error = %v, want ErrLossyConversion for distinct exact integer values", err)
		}
	})
}
