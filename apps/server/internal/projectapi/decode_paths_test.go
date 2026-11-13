package projectapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeInput_RequestInvalidNamesTheFailingPath(t *testing.T) {
	t.Parallel()

	const base = `"defaultLanguage":"en","text":{"en":{"name":"X"}}`
	for _, test := range []struct {
		name, body, want string
	}{
		{"null demoUrl", `{` + base + `,"demoUrl":null}`, `request body field "demoUrl" must not be null`},
		{"null slug", `{"slug":null,` + base + `}`, `request body field "slug" must not be null`},
		{"null technology", `{` + base + `,"technologies":["go",null]}`, `request body field "technologies[1]" must not be null`},
		{"wrong type", `{` + base + `,"repositoryUrl":7}`, `request body field "repositoryUrl" must be a string`},
		{"wrong nested type", `{"defaultLanguage":"en","text":{"en":{"name":["X"]}}}`, `request body field "text.en.name" must be a string`},
		{"wrong year type", `{` + base + `,"dates":{"start":{"y":"2020"},"end":null,"present":true}}`, `request body field "dates.start.y" must be an integer`},
		{"wrong collection type", `{` + base + `,"technologies":"go"}`, `request body field "technologies" must be an array`},
		{"unknown key", `{` + base + `,"homepage":"https://example.com"}`, `request body field "homepage" is not declared`},
		{"case-variant key", `{"DefaultLanguage":"en","text":{"en":{"name":"X"}}}`, `request body field "DefaultLanguage" is not declared (the declared name is "defaultLanguage")`},
		{"nested case-variant key", `{"defaultLanguage":"en","text":{"en":{"Name":"X"}}}`, `request body field "text.en.Name" is not declared (the declared name is "name")`},
		{"case-variant duplicate", `{` + base + `,"demoUrl":"","DemoUrl":""}`, `request body field "DemoUrl" is not declared (the declared name is "demoUrl")`},
		{"repeated key", `{` + base + `,"demoUrl":"","demoUrl":""}`, `request body field "demoUrl" is repeated`},
		{"missing date end", `{` + base + `,"dates":{"start":{"y":2020},"present":true}}`, `request body field "dates.end" is required`},
		{"root type", `[]`, `request body must be an object`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, projectsPath, strings.NewReader(test.body))
			_, _, failure := decodeInput(r)
			if failure == nil {
				t.Fatalf("decodeInput accepted %s", test.body)
			}
			if failure.status != http.StatusBadRequest || failure.code != "request_invalid" || failure.message != test.want || failure.details != nil {
				t.Fatalf("decodeInput = %d %s %q %v, want 400 request_invalid %q and no details",
					failure.status, failure.code, failure.message, failure.details, test.want)
			}
		})
	}
	for _, body := range []string{
		`{` + base + `}`,
		`{"slug":"example",` + base + `,"demoUrl":"","dates":null,"technologies":["go"]}`,
		`{` + base + `,"dates":{"start":{"y":2020,"m":1},"end":null,"present":true}}`,
	} {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, projectsPath, strings.NewReader(body))
		if _, _, failure := decodeInput(r); failure != nil {
			t.Errorf("decodeInput(%s) = %+v, want accepted", body, failure)
		}
	}
}
