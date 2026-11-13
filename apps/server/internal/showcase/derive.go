package showcase

import (
	"danny.vn/aboutme/apps/server/internal/previewcard"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

type Derived struct {
	CardVersion string
	TemplateID  *string
}

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
