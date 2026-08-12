// docmigrate_test.go covers adjacent conversion, projection, synthetic
// old-client preparation, and supported-version emission.
//
// Most tests here use SYNTHETIC released versions to exercise arbitrary
// adjacent paths without coupling them to the production font conversion. The
// synthetic v2/v3 schemas are derived from the real immutable
// resume.v1.schema.json -- only `$id` and `$defs/schemaVersion/const` change --
// so:
//
//   - version 1 always validates against the REAL immutable contract, which
//     is what "EmitWire ... validates immutable v1" has to mean to be worth
//     anything; and
//   - a converted document keeps v1's shape, so the same synthetic pair also
//     drives the live store-level projection test in
//     ../projection_test.go, where the projected parts must still strict-
//     decode into the current Go types.
//
// The synthetic converters move real data (they prefix
// personalDetails.headline) and are exactly invertible, so a v1 -> v2 -> v1
// round trip is provably lossless field by field rather than trivially so.
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

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

// --- fixtures and synthetic schema/converter construction ---

// readFixture reads packages/schema/fixtures/<name> relative to this
// package (apps/server/internal/resume/docmigrate -> five levels up).
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

// decodeDoc decodes a whole document into a generic map with UseNumber, so
// re-marshaling never reformats a number the converter did not touch.
func decodeDoc(doc json.RawMessage) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// normalize renders doc through the same generic decode/encode both sides of
// a round-trip comparison go through, so the comparison is about CONTENT and
// never about object key order or whitespace.
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

// v1RawSchema is the real immutable released v1 schema.
func v1RawSchema(t *testing.T) []byte {
	t.Helper()
	released, err := schema.ReleasedSchemaFor(1)
	if err != nil {
		t.Fatalf("schema.ReleasedSchemaFor(1): %v", err)
	}
	return released.RawSchema
}

// derivedSchema returns the immutable v1 schema retargeted to version: a new
// `$id` (so the two compile as distinct resources) and
// `$defs/schemaVersion/const`. Nothing else changes, which is exactly what
// keeps a converted document decodable by the current Go types.
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

// compileValidator compiles raw into a docmigrate.ValidateFunc using the
// same posture internal/resume uses: format assertion on, no
// URL loader at all.
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

// counted wraps v and increments *n on every call, so a chain test can prove
// EVERY intermediate version was validated, not just the endpoints.
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

// upHeadline is a synthetic N -> N+1 converter: it prefixes
// personalDetails.headline and sets schemaVersion to target. An absent
// headline stays absent (the field is optional in v1), which keeps the
// converter total.
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

// downHeadline inverts upHeadline exactly. A headline that does not carry
// the prefix is an error, not a silent passthrough: the document is then not
// what the source version claims it is.
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

// syntheticV2 builds a two-version projector from immutable v1, derived v2,
// one adjacent pair, and current = 2.
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

// fullV1Doc is the repository's full v1 fixture, used as the "old client"
// document throughout.
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

// splitDoc splits a full document into the three stored jsonb parts, the way
// a resumes row holds them.
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

// --- Production declarations ---

func TestDeclarations_ProductionSetsAreV1AndV2(t *testing.T) {
	t.Parallel()

	if docmigrate.CurrentVersion != 2 {
		t.Fatalf("CurrentVersion = %d, want 2", docmigrate.CurrentVersion)
	}
	for _, tc := range []struct {
		name string
		got  []int32
	}{
		{"accepted", docmigrate.AcceptedVersions()},
		{"emitted", docmigrate.EmittedVersions()},
	} {
		if !slices.Equal(tc.got, []int32{1, 2}) {
			t.Errorf("%sVersions() = %v, want [1 2]", tc.name, tc.got)
		}
	}
}

// TestDeclarations_EveryDeclaredVersionIsReleased is the fail-closed link
// between the two registries: the server must never declare it accepts or
// emits a version packages/schema has no immutable schema for.
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

// TestDeclarations_ReturnedSlicesCannotMutateInternalState proves callers
// receive copies: scribbling on a returned slice must not change what the
// next caller is told the server accepts or emits.
func TestDeclarations_ReturnedSlicesCannotMutateInternalState(t *testing.T) {
	t.Parallel()

	accepted := docmigrate.AcceptedVersions()
	emitted := docmigrate.EmittedVersions()
	accepted[0] = 99
	emitted[0] = 99

	if got := docmigrate.AcceptedVersions(); !slices.Equal(got, []int32{1, 2}) {
		t.Errorf("AcceptedVersions() = %v after mutating a returned copy, want [1 2]", got)
	}
	if got := docmigrate.EmittedVersions(); !slices.Equal(got, []int32{1, 2}) {
		t.Errorf("EmittedVersions() = %v after mutating a returned copy, want [1 2]", got)
	}
}

// --- Identity conversion and projection are byte-stable ---

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
		"schemaVersion":   json.RawMessage("2"),
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

// TestIdentityProjector_UnknownStoredVersion_FailsClosed: a row claiming a
// version this build has no schema for must fail, never pass through
// unconverted.
func TestIdentityProjector_UnknownStoredVersion_FailsClosed(t *testing.T) {
	t.Parallel()
	p := docmigrate.NewIdentityProjector()
	pd, content, customization := splitDoc(t, fullV1Doc(t))

	for _, stored := range []int32{0, 3, 7} {
		if _, _, _, err := p.Project(pd, content, customization, stored); !errors.Is(err, docmigrate.ErrUnknownVersion) {
			t.Errorf("Project(parts, %d) error = %v, want ErrUnknownVersion", stored, err)
		}
	}
}

// --- Adjacent conversion, both directions ---

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

// TestConvert_MultiStepChain walks 1->2->3 and 3->2->1 and proves EVERY
// version's validator ran on the way: the source once and each intermediate
// and final target once.
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

// TestConvert_IdentityValidatesSourceAndPreservesBytes pins Convert's public
// contract: even an identity conversion validates its source, while a valid
// document still returns byte-for-byte unchanged. Project owns the separate
// current-version read short circuit.
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

// TestProject_CurrentVersionBypassesValidation protects the read-path
// behavior independently from Convert: an already-current row is returned
// byte-for-byte and is not turned into a schema validation pass.
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

// TestConvert_FailsClosed covers every conversion failure mode.
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
		// Versions 1, 2 and 3 all have schemas, but only the 1<->2 pair is
		// registered: a 2->3 step has no converter in either direction.
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
		// The same hole in the DOWN direction: a genuine v3 document (v3
		// shares v1's shape, so retagging the fixture produces one that
		// really does validate at 3) still cannot walk back past the
		// missing 2<->3 pair.
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
		delete(m, "content") // required by v1
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
		// Prefixes the headline but never bumps schemaVersion, so the
		// output fails v2's `const: 2`.
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

// --- Constructor fails closed ---

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

// TestNewProjector_CopiesItsInputs proves a caller cannot reach into a
// constructed projector by mutating the maps and slices it handed over --
// production wiring must not be re-configurable after startup.
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

// withSchemaVersion retags doc as version n. The synthetic v2/v3 schemas
// share v1's shape, so this alone produces a document that genuinely
// validates at n.
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

// --- Projection over the three stored parts ---

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
	// The untouched parts survive the assemble/convert/re-split round trip
	// with their content intact.
	if !bytes.Equal(normalize(t, gotContent), normalize(t, content)) {
		t.Errorf("projection changed content:\n got %s\nwant %s", gotContent, content)
	}
	if !bytes.Equal(normalize(t, gotCustomization), normalize(t, customization)) {
		t.Errorf("projection changed customization:\n got %s\nwant %s", gotCustomization, customization)
	}
	// No stored part may ever carry schemaVersion.
	for name, part := range map[string]json.RawMessage{
		"personalDetails": gotPD, "content": gotContent, "customization": gotCustomization,
	} {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(part, &m); err != nil {
			continue
		}
		if _, ok := m["schemaVersion"]; ok {
			t.Errorf("projected %s carries a schemaVersion key (D4 forbids it)", name)
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
				// A permissive v2 schema, so the missing key reaches the
				// re-split rather than being caught by validation first.
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
						return json.Marshal(m) // schemaVersion still 1
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

// --- Old-client preparation and supported-version emission ---

// TestWire_OldClientDocumentAcceptedThenEmitted is the transport-agnostic
// boundary an HTTP layer uses: a v1 ("old client") document is prepared into
// the current canonical shape, target-validated there, and can then be
// emitted back in a DECLARED supported version, validated against that
// version's immutable schema, with every v1 field preserved.
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

// TestWire_UndeclaredVersionsFailClosed proves accepted and emitted are
// DISTINCT declared gates, enforced even when the conversion machinery
// itself could do the job.
func TestWire_UndeclaredVersionsFailClosed(t *testing.T) {
	t.Parallel()

	// Everything about version 2 exists -- schema and both converter
	// directions -- but only version 1 is declared accepted and only
	// version 2 is declared emitted.
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

// TestWire_InvalidInputFailsClosed proves the wire boundary validates
// unconditionally, INCLUDING at the current version where conversion is the
// identity -- the production identity projector must never wave a
// malformed old-client document through.
func TestWire_InvalidInputFailsClosed(t *testing.T) {
	t.Parallel()

	var m map[string]any
	if err := json.Unmarshal(fullV1Doc(t), &m); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	delete(m, "personalDetails") // required by v1
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

// TestWire_LossyEmissionFailsClosed proves schema validity alone cannot hide
// data loss: photo is optional in v1, so the emitted document remains valid
// after a broken Down converter drops it. EmitWire must still reject the
// non-preserving round trip.
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
					delete(pd, "photo") // optional in v1: output remains schema-valid
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

// TestWire_LosslessEmissionComparesNumbersByExactValue proves the lossless
// check treats JSON number spellings as representations of a value, without
// rounding distinct values through float64.
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
