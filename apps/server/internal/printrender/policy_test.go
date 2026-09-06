package printrender

import (
	"net/http"
	"testing"

	"github.com/chromedp/cdproto/network"
)

func TestRequestPolicyAdmitsOneExactDocumentAndOnlyCatalogAssets(t *testing.T) {
	initial := "http://127.0.0.1:20030/print/00000000-0000-0000-0000-000000000001"
	policy := newRequestPolicy(initial, "token", "00000000-0000-0000-0000-000000000002")

	decision := policy.decide(pausedRequest{
		url: initial, method: http.MethodGet, resourceType: network.ResourceTypeDocument,
		headers: network.Headers{"Cookie": "ambient=1", "authorization": "Bearer account", "X-Render-Job-ID": "ambient", "Accept": "text/html"},
	})
	if !decision.allow || header(decision.headers, "Authorization") != "RenderCapability token" || header(decision.headers, "X-Render-Job-ID") != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("initial decision = %#v", decision)
	}
	if header(decision.headers, "Cookie") != "" || header(decision.headers, "Accept") != "text/html" {
		t.Fatalf("initial sanitized headers = %#v", decision.headers)
	}

	tests := []struct {
		name string
		req  pausedRequest
		want bool
	}{
		{"duplicate document", pausedRequest{url: initial, method: http.MethodGet, resourceType: network.ResourceTypeDocument}, false},
		{"redirected document", pausedRequest{url: initial, method: http.MethodGet, resourceType: network.ResourceTypeDocument, redirected: true}, false},
		{"print css", pausedRequest{id: "print-css", url: "http://127.0.0.1:20030/_nuxt/assets/print.css", method: http.MethodGet, resourceType: network.ResourceTypeStylesheet}, true},
		{"font css", pausedRequest{id: "font-css", url: "http://127.0.0.1:20030/_nuxt/assets/print-fonts.css", method: http.MethodGet, resourceType: network.ResourceTypeStylesheet}, true},
		{"catalog font", pausedRequest{id: "font", url: "http://127.0.0.1:20030/_nuxt/fonts/inter-var.woff2", method: http.MethodGet, resourceType: network.ResourceTypeFont}, true},
		{"unknown font", pausedRequest{url: "http://127.0.0.1:20030/_nuxt/fonts/unknown.woff2", method: http.MethodGet, resourceType: network.ResourceTypeFont}, false},
		{"same-origin api", pausedRequest{url: "http://127.0.0.1:20030/api/v1/resumes", method: http.MethodGet, resourceType: network.ResourceTypeFetch}, false},
		{"external", pausedRequest{url: "http://example.com/_nuxt/assets/print.css", method: http.MethodGet, resourceType: network.ResourceTypeStylesheet}, false},
		{"asset post", pausedRequest{url: "http://127.0.0.1:20030/_nuxt/assets/print.css", method: http.MethodPost, resourceType: network.ResourceTypeStylesheet}, false},
		{"frame", pausedRequest{url: "http://127.0.0.1:20030/_nuxt/assets/print.css", method: http.MethodGet, resourceType: network.ResourceTypeDocument}, false},
		{"websocket", pausedRequest{url: "ws://127.0.0.1:20030/socket", method: http.MethodGet, resourceType: network.ResourceTypeWebSocket}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := policy.decide(test.req)
			if got.allow != test.want {
				t.Fatalf("allow = %v, want %v", got.allow, test.want)
			}
			if got.allow && test.name != "duplicate document" {
				if header(got.headers, "Cookie") != "" || header(got.headers, "Authorization") != "" || header(got.headers, "X-Render-Job-ID") != "" {
					t.Fatalf("asset authority headers = %#v", got.headers)
				}
			}
		})
	}
}

func TestRequestPolicyRequiresSuccessfulStylesheetAndFontResponses(t *testing.T) {
	const origin = "http://127.0.0.1:20030"
	newPolicy := func(t *testing.T) *requestPolicy {
		t.Helper()
		policy := newRequestPolicy(origin+"/print/00000000-0000-0000-0000-000000000001", "token", "00000000-0000-0000-0000-000000000002")
		for _, request := range []pausedRequest{
			{id: "print-css", url: origin + "/_nuxt/assets/print.css", method: http.MethodGet, resourceType: network.ResourceTypeStylesheet},
			{id: "font-css", url: origin + "/_nuxt/assets/print-fonts.css", method: http.MethodGet, resourceType: network.ResourceTypeStylesheet},
			{id: "font", url: origin + "/_nuxt/fonts/inter-var.woff2", method: http.MethodGet, resourceType: network.ResourceTypeFont},
		} {
			if !policy.decide(request).allow {
				t.Fatalf("request not admitted: %#v", request)
			}
		}
		return policy
	}
	validResponses := []pausedResponse{
		{id: "print-css", url: origin + "/_nuxt/assets/print.css", resourceType: network.ResourceTypeStylesheet, status: http.StatusOK, headers: network.Headers{"Content-Type": "text/css"}, mimeType: "text/css"},
		{id: "font-css", url: origin + "/_nuxt/assets/print-fonts.css", resourceType: network.ResourceTypeStylesheet, status: http.StatusOK, headers: network.Headers{"Content-Type": "text/css; charset=utf-8"}, mimeType: "text/css", charset: "utf-8"},
		{id: "font", url: origin + "/_nuxt/fonts/inter-var.woff2", resourceType: network.ResourceTypeFont, status: http.StatusOK, headers: network.Headers{"Content-Type": "font/woff2"}, mimeType: "font/woff2"},
	}
	policy := newPolicy(t)
	for _, response := range validResponses {
		if !policy.acceptResponse(response) {
			t.Fatalf("response not accepted: %#v", response)
		}
		if !policy.finishResponse(response.id) {
			t.Fatalf("response not completed: %#v", response)
		}
	}
	if !policy.assetsComplete() {
		t.Fatal("complete asset set not accepted")
	}

	for _, test := range []struct {
		name   string
		mutate func(*pausedResponse)
	}{
		{name: "status", mutate: func(value *pausedResponse) { value.status = http.StatusNotFound }},
		{name: "wrong MIME", mutate: func(value *pausedResponse) { value.mimeType = "text/plain" }},
		{name: "duplicate content type", mutate: func(value *pausedResponse) {
			value.headers["Content-Type"] = []string{"text/css", "text/css"}
		}},
		{name: "wrong request ID", mutate: func(value *pausedResponse) { value.id = "other" }},
		{name: "wrong URL", mutate: func(value *pausedResponse) { value.url = origin + "/_nuxt/assets/print-fonts.css" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			testPolicy := newPolicy(t)
			response := validResponses[0]
			response.headers = network.Headers{"Content-Type": "text/css"}
			test.mutate(&response)
			if testPolicy.acceptResponse(response) {
				t.Fatal("accepted invalid asset response")
			}
			if testPolicy.assetsComplete() {
				t.Fatal("invalid response completed asset set")
			}
		})
	}
	policy = newPolicy(t)
	if !policy.failResponse("print-css") || policy.assetsComplete() {
		t.Fatal("network failure did not invalidate admitted stylesheet")
	}
}

func TestPrintResponseRequiresExactClosedContract(t *testing.T) {
	valid := network.Headers{
		"Content-Security-Policy": printContentSecurityPolicy,
		"Content-Type":            "text/html; charset=utf-8",
	}
	if err := validatePrintResponse(200, false, valid); err != nil {
		t.Fatalf("valid response: %v", err)
	}
	for _, test := range []struct {
		name    string
		status  int64
		service bool
		headers network.Headers
	}{
		{"status", 302, false, valid},
		{"service worker", 200, true, valid},
		{"missing csp", 200, false, network.Headers{}},
		{"weaker csp", 200, false, network.Headers{"Content-Security-Policy": "default-src 'none'"}},
		{"duplicate csp", 200, false, network.Headers{"Content-Security-Policy": []string{printContentSecurityPolicy, printContentSecurityPolicy}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validatePrintResponse(test.status, test.service, test.headers); err == nil {
				t.Fatal("accepted invalid print response")
			}
		})
	}
}

func header(headers []*headerEntry, name string) string {
	for _, item := range headers {
		if item != nil && http.CanonicalHeaderKey(item.Name) == http.CanonicalHeaderKey(name) {
			return item.Value
		}
	}
	return ""
}
