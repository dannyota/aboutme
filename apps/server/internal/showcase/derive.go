package showcase

import (
	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// Derived holds the two values Go keeps current on every showcase row.
type Derived struct {
	CardVersion string
	// TemplateID is nil when no preset matches ("Custom design").
	TemplateID *string
}

// Derive computes the card version and template of an admitted public
// snapshot. It reads the same card inputs the preview card builder reads, so
// the version follows exactly what the card shows.
func Derive(snapshot publicresume.Snapshot) (Derived, error) {
	card, err := previewcard.FromSnapshot(snapshot)
	if err != nil {
		return Derived{}, err
	}
	version, err := card.Version()
	if err != nil {
		return Derived{}, err
	}
	template, err := TemplateID(snapshot.Public.Document.Customization)
	if err != nil {
		return Derived{}, err
	}
	return Derived{CardVersion: version, TemplateID: template}, nil
}
