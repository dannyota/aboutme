package publicapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type ViewObserver interface {
	ObserveHTML(ctx context.Context, resumeID uuid.UUID, userAgent string)
}

func observeHTMLView(observer ViewObserver, request *http.Request, resumeID uuid.UUID) {
	if observer == nil || request.Method != http.MethodGet {
		return
	}
	observer.ObserveHTML(request.Context(), resumeID, request.UserAgent())
}
