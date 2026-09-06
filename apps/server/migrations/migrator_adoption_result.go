package migrations

// AdoptionOutcome classifies a completed history-owner adoption command.
type AdoptionOutcome uint8

const (
	// Applied means this invocation committed the ownership transition.
	Applied AdoptionOutcome = iota + 1
	// AlreadyConverged means the locked state was already exact.
	AlreadyConverged
	// ReconciledConverged means a read-only pass proved convergence after ambiguity.
	ReconciledConverged
)

// AdoptionResult records the durable outcome without exposing mutable fields.
type AdoptionResult struct {
	outcome        AdoptionOutcome
	ambiguousCause error
}

// Outcome returns the durable adoption classification.
func (r AdoptionResult) Outcome() AdoptionOutcome { return r.outcome }

// AmbiguousCause returns the retained cause for ReconciledConverged.
func (r AdoptionResult) AmbiguousCause() error { return r.ambiguousCause }
