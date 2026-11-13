package publicapi

import "net/http"

// serveLeasedSelected writes a public JSON, photo, Markdown, or discovery
// response while lease stays live, so a revocation ends a write blocked on a
// slow reader (writeWhileLeased). When the cancel hook cannot be registered
// it fails closed: unavailable, the route's own error writer, writes a 503,
// and no header or byte of response is written.
func serveLeasedSelected(w http.ResponseWriter, request *http.Request, lease cancelHooks, response SelectedResponse, unavailable func(http.ResponseWriter, *http.Request, int)) {
	if err := writeWhileLeased(w, lease, func() { response.ServeHTTP(w, request) }); err != nil {
		unavailable(w, request, http.StatusServiceUnavailable)
	}
}
