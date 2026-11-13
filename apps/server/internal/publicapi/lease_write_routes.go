package publicapi

import "net/http"

func serveLeasedSelected(w http.ResponseWriter, request *http.Request, lease cancelHooks, response SelectedResponse, unavailable func(http.ResponseWriter, *http.Request, int)) {
	if err := writeWhileLeased(w, lease, func() { response.ServeHTTP(w, request) }); err != nil {
		unavailable(w, request, http.StatusServiceUnavailable)
	}
}
