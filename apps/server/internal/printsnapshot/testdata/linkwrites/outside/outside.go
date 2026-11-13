package outside

import (
	snapshot "danny.vn/aboutme/apps/server/internal/printsnapshot"
)

func indexed(envelope snapshot.Envelope) {
	envelope.ProjectLinks["entry"] = "https://aboutme.example/ada/projects/atlas"
}

func deleted(envelope snapshot.Envelope) {
	delete(envelope.ProjectLinks, "entry")
}

func addressed(envelope *snapshot.Envelope) *map[string]string {
	return &envelope.ProjectLinks
}

func positional(document snapshot.Envelope) snapshot.Envelope {
	return snapshot.Envelope{2, "id", "1", nil, "en", document.Document, nil}
}

func keyed(pages map[string]string) snapshot.Envelope {
	return snapshot.Envelope{Version: 2, ProjectLinks: pages}
}

func built() snapshot.Envelope {
	return snapshot.Envelope{Version: 1}
}
