package printrender

import (
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
)

const printContentSecurityPolicy = "sandbox allow-same-origin; default-src 'none'; script-src 'none'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src data:; connect-src 'none'; frame-src 'none'; worker-src 'none'; child-src 'none'; media-src 'none'; manifest-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// Keep this fixed allowlist synchronized with the generated web font catalog.
var fontPaths = map[string]struct{}{
	"/_nuxt/fonts/alegreya-400.woff2":                   {},
	"/_nuxt/fonts/alegreya-700.woff2":                   {},
	"/_nuxt/fonts/aleo-var.woff2":                       {},
	"/_nuxt/fonts/atkinson-hyperlegible-next-var.woff2": {},
	"/_nuxt/fonts/barlow-400.woff2":                     {},
	"/_nuxt/fonts/barlow-700.woff2":                     {},
	"/_nuxt/fonts/be-vietnam-pro-var.woff2":             {},
	"/_nuxt/fonts/cormorant-garamond-var.woff2":         {},
	"/_nuxt/fonts/crimson-pro-var.woff2":                {},
	"/_nuxt/fonts/dm-sans-var.woff2":                    {},
	"/_nuxt/fonts/eb-garamond-var.woff2":                {},
	"/_nuxt/fonts/fira-sans-400.woff2":                  {},
	"/_nuxt/fonts/fira-sans-700.woff2":                  {},
	"/_nuxt/fonts/inter-var.woff2":                      {},
	"/_nuxt/fonts/literata-var.woff2":                   {},
	"/_nuxt/fonts/montserrat-var.woff2":                 {},
	"/_nuxt/fonts/newsreader-var.woff2":                 {},
	"/_nuxt/fonts/noto-sans-var.woff2":                  {},
	"/_nuxt/fonts/noto-serif-var.woff2":                 {},
	"/_nuxt/fonts/nunito-sans-var.woff2":                {},
	"/_nuxt/fonts/open-sans-var.woff2":                  {},
	"/_nuxt/fonts/plus-jakarta-sans-var.woff2":          {},
	"/_nuxt/fonts/roboto-mono-var.woff2":                {},
	"/_nuxt/fonts/roboto-serif-var.woff2":               {},
	"/_nuxt/fonts/roboto-var.woff2":                     {},
	"/_nuxt/fonts/source-sans-3-var.woff2":              {},
	"/_nuxt/fonts/space-mono-400.woff2":                 {},
	"/_nuxt/fonts/space-mono-700.woff2":                 {},
	"/_nuxt/fonts/spectral-400.woff2":                   {},
	"/_nuxt/fonts/spectral-700.woff2":                   {},
	"/_nuxt/fonts/work-sans-var.woff2":                  {},
}

type headerEntry = fetch.HeaderEntry

type pausedRequest struct {
	id           network.RequestID
	url          string
	method       string
	resourceType network.ResourceType
	headers      network.Headers
	redirected   bool
}

type pausedResponse struct {
	id            network.RequestID
	url           string
	resourceType  network.ResourceType
	status        int64
	headers       network.Headers
	mimeType      string
	charset       string
	serviceWorker bool
}

type assetExpectation struct {
	url          string
	resourceType network.ResourceType
	contentType  string
	responseSeen bool
}

type requestDecision struct {
	allow   bool
	headers []*headerEntry
}

type requestPolicy struct {
	mu              sync.Mutex
	initialURL      string
	origin          string
	capability      string
	jobID           string
	initialUsed     bool
	pending         map[network.RequestID]assetExpectation
	completedStyles map[string]bool
	violated        bool
}

func newRequestPolicy(initialURL, capability, jobID string) *requestPolicy {
	cut := strings.Index(initialURL, "/print/")
	origin := ""
	if cut > 0 {
		origin = initialURL[:cut]
	}
	return &requestPolicy{
		initialURL: initialURL, origin: origin, capability: capability, jobID: jobID,
		pending: make(map[network.RequestID]assetExpectation), completedStyles: make(map[string]bool),
	}
}

func (p *requestPolicy) decide(request pausedRequest) requestDecision {
	p.mu.Lock()
	defer p.mu.Unlock()
	if request.method != http.MethodGet || request.redirected {
		return requestDecision{}
	}
	if request.url == p.initialURL && request.resourceType == network.ResourceTypeDocument && !p.initialUsed {
		p.initialUsed = true
		headers := sanitizedHeaders(request.headers)
		headers = append(headers,
			&headerEntry{Name: "Authorization", Value: "RenderCapability " + p.capability},
			&headerEntry{Name: "X-Render-Job-ID", Value: p.jobID},
		)
		return requestDecision{allow: true, headers: headers}
	}
	contentType, ok := p.allowedAsset(request.url, request.resourceType)
	if !ok || request.id == "" {
		return requestDecision{}
	}
	if _, exists := p.pending[request.id]; exists {
		return requestDecision{}
	}
	if request.resourceType == network.ResourceTypeStylesheet {
		if _, exists := p.completedStyles[request.url]; exists {
			return requestDecision{}
		}
		p.completedStyles[request.url] = false
	}
	p.pending[request.id] = assetExpectation{url: request.url, resourceType: request.resourceType, contentType: contentType}
	return requestDecision{allow: true, headers: sanitizedHeaders(request.headers)}
}

func (p *requestPolicy) allowedAsset(raw string, resourceType network.ResourceType) (string, bool) {
	if raw == p.origin+"/_nuxt/assets/print.css" || raw == p.origin+"/_nuxt/assets/print-fonts.css" {
		return "text/css", resourceType == network.ResourceTypeStylesheet
	}
	if !strings.HasPrefix(raw, p.origin) {
		return "", false
	}
	_, ok := fontPaths[strings.TrimPrefix(raw, p.origin)]
	return "font/woff2", ok && resourceType == network.ResourceTypeFont
}

func (p *requestPolicy) acceptResponse(response pausedResponse) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	expected, ok := p.pending[response.id]
	if !ok || expected.responseSeen {
		p.violated = true
		return false
	}
	if response.url != expected.url || response.resourceType != expected.resourceType || response.status != http.StatusOK || response.serviceWorker || response.mimeType != expected.contentType || !validAssetContentType(response.headers, expected.contentType, response.charset) {
		p.violated = true
		return false
	}
	expected.responseSeen = true
	p.pending[response.id] = expected
	return true
}

func (p *requestPolicy) finishResponse(id network.RequestID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	expected, ok := p.pending[id]
	if !ok {
		return false
	}
	if !expected.responseSeen {
		p.violated = true
		return false
	}
	delete(p.pending, id)
	if expected.resourceType == network.ResourceTypeStylesheet {
		p.completedStyles[expected.url] = true
	}
	return true
}

func (p *requestPolicy) failResponse(id network.RequestID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.pending[id]; !ok {
		return false
	}
	delete(p.pending, id)
	p.violated = true
	return true
}

func (p *requestPolicy) tracks(id network.RequestID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.pending[id]
	return ok
}

func (p *requestPolicy) assetsComplete() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.violated && len(p.pending) == 0 &&
		p.completedStyles[p.origin+"/_nuxt/assets/print.css"] &&
		p.completedStyles[p.origin+"/_nuxt/assets/print-fonts.css"]
}

func validAssetContentType(headers network.Headers, want, charset string) bool {
	value, ok := exactHeader(headers, "Content-Type")
	if !ok {
		return false
	}
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || mediaType != want {
		return false
	}
	if want == "font/woff2" {
		return len(parameters) == 0 && charset == ""
	}
	for name, parameter := range parameters {
		if !strings.EqualFold(name, "charset") || !strings.EqualFold(parameter, "utf-8") {
			return false
		}
	}
	return charset == "" || strings.EqualFold(charset, "utf-8")
}

func sanitizedHeaders(input network.Headers) []*headerEntry {
	names := make([]string, 0, len(input))
	for name := range input {
		if strings.EqualFold(name, "Cookie") || strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "X-Render-Job-ID") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]*headerEntry, 0, len(names))
	for _, name := range names {
		result = append(result, &headerEntry{Name: name, Value: headerString(input[name])})
	}
	return result
}

func headerString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func validatePrintResponse(status int64, fromServiceWorker bool, headers network.Headers) error {
	if status != http.StatusOK || fromServiceWorker {
		return ErrRenderFailed
	}
	csp, ok := exactHeader(headers, "Content-Security-Policy")
	if !ok || csp != printContentSecurityPolicy {
		return ErrRenderFailed
	}
	contentType, ok := exactHeader(headers, "Content-Type")
	if !ok || contentType != "text/html; charset=utf-8" {
		return ErrRenderFailed
	}
	return nil
}

func exactHeader(headers network.Headers, name string) (string, bool) {
	var result string
	found := false
	for key, value := range headers {
		if !strings.EqualFold(key, name) {
			continue
		}
		if found {
			return "", false
		}
		text, ok := value.(string)
		if !ok {
			return "", false
		}
		result, found = text, true
	}
	return result, found
}
