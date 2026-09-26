package publicapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// ViewObserver records layer 1 of view counting for a live resume page:
// link-preview fetchers and declared crawlers, by user agent
// (docs/design/viewer-analytics/counting.md, "Layer 1"). It never changes
// the response, so the page stays identical for every viewer.
type ViewObserver interface {
	ObserveHTML(ctx context.Context, resumeID uuid.UUID, userAgent string)
}

// observeHTMLView reports a GET of a live resume page. HEAD requests are
// not fetches of the page and are not recorded.
func observeHTMLView(observer ViewObserver, request *http.Request, resumeID uuid.UUID) {
	if observer == nil || request.Method != http.MethodGet {
		return
	}
	observer.ObserveHTML(request.Context(), resumeID, request.UserAgent())
}
