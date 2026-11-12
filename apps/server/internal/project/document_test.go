package project

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

func draft() Document {
	return Document{DefaultLanguage: "vi", Text: map[string]Text{"vi": {Name: "Dự án"}}}
}

func TestPrepare_FieldBounds(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		set   func(*Document, string)
	}{
		{"name", 120, func(d *Document, v string) { x := d.Text["vi"]; x.Name = v; d.Text["vi"] = x }},
		{"role", 120, func(d *Document, v string) { x := d.Text["vi"]; x.Role = v; d.Text["vi"] = x }},
		{"summary", 300, func(d *Document, v string) { x := d.Text["vi"]; x.Summary = v; d.Text["vi"] = x }},
		{"outcome", 600, func(d *Document, v string) { x := d.Text["vi"]; x.Outcome = v; d.Text["vi"] = x }},
		{"technology", 40, func(d *Document, v string) { d.Technologies = []string{v} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, n := range []int{tc.limit, tc.limit + 1} {
				d := draft()
				tc.set(&d, strings.Repeat("ộ", n))
				_, err := Prepare(d)
				if (err != nil) != (n > tc.limit) {
					t.Fatalf("characters=%d error=%v", n, err)
				}
			}
		})
	}
	for _, n := range []int{16384, 16385} {
		d := draft()
		x := d.Text["vi"]
		x.Description = strings.Repeat("a", n)
		d.Text["vi"] = x
		_, err := Prepare(d)
		if (err != nil) != (n > 16384) {
			t.Fatalf("description bytes=%d error=%v", n, err)
		}
	}
}

func TestPrepare_CollectionBounds(t *testing.T) {
	for _, n := range []int{20, 21} {
		d := draft()
		d.Technologies = make([]string, n)
		_, err := Prepare(d)
		if (err != nil) != (n > 20) {
			t.Fatalf("technologies=%d error=%v", n, err)
		}
	}
	for _, n := range []int{4, 5} {
		d := draft()
		for _, key := range []string{"en", "ja", "fr", "de"}[:n-1] {
			d.Text[key] = Text{}
		}
		_, err := Prepare(d)
		if (err != nil) != (n > 4) {
			t.Fatalf("languages=%d error=%v", n, err)
		}
	}
}

func TestPrepare_CompleteDocumentRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Document)
	}{
		{"blank default name", func(d *Document) { d.Text["vi"] = Text{Name: " \t\n"} }},
		{"missing default", func(d *Document) { d.DefaultLanguage = "en" }},
		{"uppercase language", func(d *Document) { d.Text["EN"] = Text{} }},
		{"regional language", func(d *Document) { d.Text["en-us"] = Text{} }},
		{"empty languages", func(d *Document) { d.Text = nil }},
		{"http repository", func(d *Document) { d.RepositoryURL = "http://example.test" }},
		{"relative demo", func(d *Document) { d.DemoURL = "/demo" }},
		{"year below resume bound", func(d *Document) { d.Dates = &schema.DateRange{Start: schema.YearMonth{Y: 1899}, Present: true} }},
		{"year above resume bound", func(d *Document) { d.Dates = &schema.DateRange{Start: schema.YearMonth{Y: 2101}, Present: true} }},
		{"invalid year", func(d *Document) { d.Dates = &schema.DateRange{Start: schema.YearMonth{Y: 0}, Present: true} }},
		{"missing end", func(d *Document) { d.Dates = &schema.DateRange{Start: schema.YearMonth{Y: 2020}} }},
		{"present with end", func(d *Document) {
			d.Dates = &schema.DateRange{Start: schema.YearMonth{Y: 2020}, End: &schema.YearMonth{Y: 2021}, Present: true}
		}},
		{"reverse dates", func(d *Document) {
			d.Dates = &schema.DateRange{Start: schema.YearMonth{Y: 2022}, End: &schema.YearMonth{Y: 2021}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := draft()
			tc.change(&d)
			if _, err := Prepare(d); err == nil {
				t.Fatal("invalid document accepted")
			}
		})
	}
	d := draft()
	d.RepositoryURL = "https://example.test/repo"
	d.Dates = &schema.DateRange{Start: schema.YearMonth{Y: 2020}, Present: true}
	if _, err := Prepare(d); err != nil {
		t.Fatal(err)
	}
}

func TestPrepare_SanitizesDescription(t *testing.T) {
	d := draft()
	d.Text["en"] = Text{Description: `<p onclick="bad()">Work<script>alert(1)</script></p>`}
	out, err := Prepare(d)
	if err != nil {
		t.Fatal(err)
	}
	if out.Text["en"].Description != "<p>Work</p>" {
		t.Fatalf("description=%q", out.Text["en"].Description)
	}
	if d.Text["en"].Description == out.Text["en"].Description {
		t.Fatal("input was mutated")
	}
}

func TestSlug_GrammarAndProposal(t *testing.T) {
	for _, tc := range []struct {
		slug  string
		valid bool
	}{{"ab", true}, {strings.Repeat("a", 40), true}, {"a", false}, {strings.Repeat("a", 41), false}, {"a-b", true}, {"a--b", false}, {"Ab", false}, {"-ab", false}, {"ab-", false}, {"a_b", false}, {"api", true}} {
		if ValidSlug(tc.slug) != tc.valid {
			t.Errorf("slug=%q valid=%t", tc.slug, tc.valid)
		}
	}
	for _, tc := range []struct{ name, want string }{{"Dự án Đẹp", "du-an-dep"}, {"  Hello, World! ", "hello-world"}, {"日本語", "project"}, {"a", "project"}, {strings.Repeat("a", 45), strings.Repeat("a", 40)}} {
		if got := ProposeSlug(tc.name); got != tc.want {
			t.Errorf("propose=%q want=%q", got, tc.want)
		}
	}
}

func TestDecode_RejectsUnknownDuplicateAndNullFields(t *testing.T) {
	for _, body := range []string{`null`, `{"defaultLanguage":"en","text":{"en":{"name":"X"}},"unknown":1}`, `{"defaultLanguage":"en","defaultLanguage":"vi","text":{"en":{"name":"X"}}}`, `{"defaultLanguage":"en","text":{"en":{"name":"X","name":"Y"}}}`, `{"defaultLanguage":"en","text":{"en":null}}`, `{"defaultLanguage":"en","text":{"en":{"name":"X"}},"technologies":[null]}`, `{"defaultLanguage":"en","text":{"en":{"name":"X"}},"dates":{"start":{"y":2020},"present":true}}`} {
		var d Document
		if err := Decode([]byte(body), &d); !errors.Is(err, ErrInvalid) {
			t.Errorf("Decode(%s) = %v, want ErrInvalid", body, err)
		}
	}
	raw, err := json.Marshal(draft())
	if err != nil {
		t.Fatal(err)
	}
	var d Document
	if err = Decode(raw, &d); err != nil {
		t.Fatal(err)
	}
}
