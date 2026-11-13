package printsnapshot

func WithProjectLinks(envelope Envelope, pages map[string]string) Envelope {
	envelope.Version, envelope.ProjectLinks = 2, pages
	return envelope
}

func assigned(envelope Envelope, pages map[string]string) Envelope {
	envelope.ProjectLinks = pages
	return envelope
}

func literal(pages map[string]string) Envelope {
	return Envelope{Version: 2, ProjectLinks: pages}
}

func read(envelope Envelope) int {
	return len(envelope.ProjectLinks)
}
